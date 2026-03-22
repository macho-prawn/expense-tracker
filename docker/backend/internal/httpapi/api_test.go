package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"sharetab/service/internal/app"
	"sharetab/service/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fixedFXConverter struct{}

func (fixedFXConverter) Convert(_ context.Context, amountCents int64, fromCurrency, toCurrency string) (app.FXQuote, error) {
	quote := app.FXQuote{
		BaseAmountCents: amountCents,
		Rate:            1,
		Source:          "test",
		FetchedAt:       time.Date(2026, time.March, 19, 0, 0, 0, 0, time.UTC),
	}
	if fromCurrency == "EUR" && toCurrency == "USD" {
		quote.BaseAmountCents = amountCents + amountCents/5
		quote.Rate = 1.2
	}
	return quote, nil
}

func TestDeleteExpenseAllowsSelectedOwnerAndRefreshesGroupState(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	expense := createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&otherMember.ID,
		1200,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	if _, err := server.createSettlement(userContext(otherMember), &createSettlementInput{
		GroupID: group.ID,
		Body: createSettlementBody{
			FromUserID:  otherMember.ID,
			ToUserID:    creator.ID,
			AmountCents: 600,
			Currency:    "USD",
			SettledOn:   "2026-03-19",
		},
	}); err != nil {
		t.Fatalf("settle expense before delete: %v", err)
	}

	originalUpdatedAt := time.Now().UTC().Add(-time.Hour)
	if err := server.db.Model(&models.Group{}).Where("id = ?", group.ID).Update("updated_at", originalUpdatedAt).Error; err != nil {
		t.Fatalf("set original group timestamp: %v", err)
	}

	if _, err := server.deleteExpense(userContext(otherMember), &pathExpenseInput{
		GroupID:   group.ID,
		ExpenseID: expense.ID,
	}); err != nil {
		t.Fatalf("delete expense: %v", err)
	}

	var expenseCount int64
	if err := server.db.Model(&models.Expense{}).Where("id = ?", expense.ID).Count(&expenseCount).Error; err != nil {
		t.Fatalf("count expenses: %v", err)
	}
	if expenseCount != 0 {
		t.Fatalf("expected deleted expense to be removed, found %d rows", expenseCount)
	}

	var splitCount int64
	if err := server.db.Model(&models.ExpenseSplit{}).Where("expense_id = ?", expense.ID).Count(&splitCount).Error; err != nil {
		t.Fatalf("count expense splits: %v", err)
	}
	if splitCount != 0 {
		t.Fatalf("expected deleted expense splits to be removed, found %d rows", splitCount)
	}

	var refreshedGroup models.Group
	if err := server.db.Where("id = ?", group.ID).First(&refreshedGroup).Error; err != nil {
		t.Fatalf("reload group: %v", err)
	}
	if !refreshedGroup.UpdatedAt.After(originalUpdatedAt) {
		t.Fatalf("expected group updated_at to advance, before=%s after=%s", originalUpdatedAt, refreshedGroup.UpdatedAt)
	}

	groupDetail, err := server.getGroup(userContext(otherMember), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("load group after delete: %v", err)
	}

	if groupDetail.Body.Group.ExpenseCount != 0 {
		t.Fatalf("expected no expenses after delete, got %d", groupDetail.Body.Group.ExpenseCount)
	}
	if len(groupDetail.Body.Expenses) != 0 {
		t.Fatalf("expected no expense payload rows after delete, got %d", len(groupDetail.Body.Expenses))
	}
	if len(groupDetail.Body.Balances.Transfers) != 0 {
		t.Fatalf("expected no transfers after delete, got %d", len(groupDetail.Body.Balances.Transfers))
	}
	for _, memberBalance := range groupDetail.Body.Balances.Members {
		if memberBalance.PaidCents != 0 || memberBalance.OwesCents != 0 || memberBalance.NetCents != 0 {
			t.Fatalf("expected zeroed balances after delete, got %+v", memberBalance)
		}
	}
}

func TestDeleteExpenseAllowsOwnerBeforeAnyPaymentsRecorded(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	expense := createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		nil,
		1200,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	if _, err := server.deleteExpense(userContext(creator), &pathExpenseInput{
		GroupID:   group.ID,
		ExpenseID: expense.ID,
	}); err != nil {
		t.Fatalf("delete unpaid expense before settlements: %v", err)
	}

	var expenseCount int64
	if err := server.db.Model(&models.Expense{}).Where("id = ?", expense.ID).Count(&expenseCount).Error; err != nil {
		t.Fatalf("count expenses after delete: %v", err)
	}
	if expenseCount != 0 {
		t.Fatalf("expected expense to be deleted before any payments, found %d rows", expenseCount)
	}
}

func TestDeleteExpenseRejectsCreatorWhenAnotherOwnerIsSelected(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	expense := createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&otherMember.ID,
		1200,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	_, err := server.deleteExpense(userContext(creator), &pathExpenseInput{
		GroupID:   group.ID,
		ExpenseID: expense.ID,
	})
	if err == nil {
		t.Fatal("expected creator delete to fail when another owner is selected")
	}
	if got := errorStatus(t, err); got != 403 {
		t.Fatalf("expected 403 for creator without ownership, got %d", got)
	}

	var expenseCount int64
	if err := server.db.Model(&models.Expense{}).Where("id = ?", expense.ID).Count(&expenseCount).Error; err != nil {
		t.Fatalf("count expenses after forbidden delete: %v", err)
	}
	if expenseCount != 1 {
		t.Fatalf("expected expense to remain after forbidden delete, found %d rows", expenseCount)
	}
}

func TestDeleteExpenseReturnsNotFoundForUnknownExpense(t *testing.T) {
	server, creator, _, group := newTestServer(t)

	_, err := server.deleteExpense(userContext(creator), &pathExpenseInput{
		GroupID:   group.ID,
		ExpenseID: uuid.New(),
	})
	if err == nil {
		t.Fatal("expected deleting a missing expense to fail")
	}
	if got := errorStatus(t, err); got != 404 {
		t.Fatalf("expected 404 for missing expense delete, got %d", got)
	}
}

func TestGetGroupFallsBackToCreatorAsLegacyExpenseOwner(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	expense := createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		nil,
		1200,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	groupDetail, err := server.getGroup(userContext(creator), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("load group detail: %v", err)
	}
	if len(groupDetail.Body.Expenses) != 1 {
		t.Fatalf("expected one expense in group detail, got %d", len(groupDetail.Body.Expenses))
	}

	payload := groupDetail.Body.Expenses[0]
	if payload.ID != expense.ID {
		t.Fatalf("expected expense %s, got %s", expense.ID, payload.ID)
	}
	if payload.OwnerUserID != creator.ID {
		t.Fatalf("expected legacy owner to fall back to creator %s, got %s", creator.ID, payload.OwnerUserID)
	}
	if payload.OwnerName != creator.Name {
		t.Fatalf("expected legacy owner name %q, got %q", creator.Name, payload.OwnerName)
	}
	if !payload.CanDelete {
		t.Fatal("expected legacy owner fallback expense to remain deletable before any payments are recorded")
	}
}

func TestDeleteGroupAllowsOwnerWhenNoExpensesExist(t *testing.T) {
	server, creator, _, group := newTestServer(t)

	if _, err := server.deleteGroup(userContext(creator), &pathGroupInput{GroupID: group.ID}); err != nil {
		t.Fatalf("delete empty group: %v", err)
	}

	var groupCount int64
	if err := server.db.Model(&models.Group{}).Where("id = ?", group.ID).Count(&groupCount).Error; err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if groupCount != 0 {
		t.Fatalf("expected group to be deleted, found %d rows", groupCount)
	}
}

func TestDeleteGroupRejectsOwnerWhenExpensesExist(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		1200,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	_, err := server.deleteGroup(userContext(creator), &pathGroupInput{GroupID: group.ID})
	if err == nil {
		t.Fatal("expected deleting a group with expenses to fail")
	}
	if got := errorStatus(t, err); got != 409 {
		t.Fatalf("expected 409 when deleting a group with expenses, got %d", got)
	}
}

func TestDashboardShowsDeleteForEmptyOwnerGroupOnly(t *testing.T) {
	server, creator, otherMember, _ := newTestServer(t)

	ownerDashboard, err := server.getDashboard(userContext(creator), &dashboardInput{})
	if err != nil {
		t.Fatalf("load owner dashboard: %v", err)
	}
	if len(ownerDashboard.Body.Groups) != 1 {
		t.Fatalf("expected one owner dashboard group, got %d", len(ownerDashboard.Body.Groups))
	}

	ownerGroup := ownerDashboard.Body.Groups[0]
	if !ownerGroup.CanDelete {
		t.Fatal("expected empty owner group to be deletable")
	}
	if ownerGroup.CanArchive {
		t.Fatal("expected empty owner group to not be archivable")
	}
	if ownerGroup.CreatedAt.IsZero() {
		t.Fatal("expected dashboard group to include createdAt")
	}

	memberDashboard, err := server.getDashboard(userContext(otherMember), &dashboardInput{})
	if err != nil {
		t.Fatalf("load member dashboard: %v", err)
	}
	if len(memberDashboard.Body.Groups) != 1 {
		t.Fatalf("expected one member dashboard group, got %d", len(memberDashboard.Body.Groups))
	}

	memberGroup := memberDashboard.Body.Groups[0]
	if memberGroup.CanDelete || memberGroup.CanArchive || memberGroup.CanUnarchive {
		t.Fatalf("expected non-owner lifecycle actions to be hidden, got %+v", memberGroup)
	}
}

func TestDashboardShowsArchiveForOwnerWhenAllExpensesAreClosed(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		6000,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	ownerDashboard, err := server.getDashboard(userContext(creator), &dashboardInput{})
	if err != nil {
		t.Fatalf("load owner dashboard with open expense: %v", err)
	}
	if len(ownerDashboard.Body.Groups) != 1 {
		t.Fatalf("expected one owner dashboard group, got %d", len(ownerDashboard.Body.Groups))
	}
	if ownerDashboard.Body.Groups[0].CanArchive {
		t.Fatal("expected group with open expenses to not be archivable")
	}

	if _, err := server.createSettlement(userContext(otherMember), &createSettlementInput{
		GroupID: group.ID,
		Body: createSettlementBody{
			FromUserID:  otherMember.ID,
			ToUserID:    creator.ID,
			AmountCents: 3000,
			Currency:    "USD",
			SettledOn:   "2026-03-19",
		},
	}); err != nil {
		t.Fatalf("settle owner group expense: %v", err)
	}

	ownerDashboard, err = server.getDashboard(userContext(creator), &dashboardInput{})
	if err != nil {
		t.Fatalf("load owner dashboard with closed expense: %v", err)
	}
	if len(ownerDashboard.Body.Groups) != 1 {
		t.Fatalf("expected one owner dashboard group after settlement, got %d", len(ownerDashboard.Body.Groups))
	}

	ownerGroup := ownerDashboard.Body.Groups[0]
	if !ownerGroup.CanArchive {
		t.Fatal("expected group with only closed expenses to be archivable")
	}
	if ownerGroup.CanDelete {
		t.Fatal("expected group with expense history to not be deletable")
	}
}

func TestCreateExpenseRejectsArchivedGroup(t *testing.T) {
	server, creator, _, group := newTestServer(t)

	if _, err := server.archiveGroup(userContext(creator), &pathGroupInput{GroupID: group.ID}); err == nil {
		t.Fatal("expected archiving empty group to fail")
	}

	createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		1200,
		[]uuid.UUID{creator.ID},
	)

	if _, err := server.archiveGroup(userContext(creator), &pathGroupInput{GroupID: group.ID}); err != nil {
		t.Fatalf("archive group before create-expense guard test: %v", err)
	}

	input := createExpenseInput{GroupID: group.ID}
	input.Body.Description = "Late fee"
	input.Body.Category = "transport"
	input.Body.ExpenseType = models.ExpenseTypeOneTime
	input.Body.AmountCents = 900
	input.Body.Currency = "USD"
	input.Body.SplitMode = "equal"
	input.Body.PaidByUserID = creator.ID
	input.Body.OwnerUserID = creator.ID
	input.Body.ParticipantUserIDs = []uuid.UUID{creator.ID}
	input.Body.IncurredOn = "2026-03-19"

	_, err := server.createExpense(userContext(creator), &input)
	if err == nil {
		t.Fatal("expected creating an expense in an archived group to fail")
	}
	if got := errorStatus(t, err); got != 409 {
		t.Fatalf("expected 409 for archived group expense creation, got %d", got)
	}
}

func newTestServer(t *testing.T) (*Server, models.User, models.User, models.Group) {
	t.Helper()

	dsn := "file:" + uuid.NewString() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("database handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(5)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	if err := db.AutoMigrate(
		&models.User{},
		&models.Session{},
		&models.Group{},
		&models.Membership{},
		&models.Invitation{},
		&models.RecurringExpenseTemplate{},
		&models.RecurringExpenseSplit{},
		&models.GroupMessage{},
		&models.Expense{},
		&models.Settlement{},
		&models.ExpenseSplit{},
		&models.SettlementAllocation{},
		&models.Obligation{},
		&models.SettlementApplication{},
	); err != nil {
		t.Fatalf("auto migrate sqlite schema: %v", err)
	}

	server := &Server{
		db: db,
		config: app.Config{
			SessionCookieName: "test_session",
			SessionTTL:        time.Hour,
		},
		fx: fixedFXConverter{},
	}

	creator := createUser(t, db, "Alice", "alice@example.com")
	otherMember := createUser(t, db, "Bob", "bob@example.com")
	group := createGroupRecord(t, db, "Weekend Trip", creator.ID)
	createMembership(t, db, group.ID, creator.ID, "owner")
	createMembership(t, db, group.ID, otherMember.ID, "member")

	return server, creator, otherMember, group
}

func createUser(t *testing.T, db *gorm.DB, name, email string) models.User {
	t.Helper()

	user := models.User{
		Name:         name,
		Email:        email,
		PasswordHash: "hashed-password",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return user
}

func createGroupRecord(t *testing.T, db *gorm.DB, name string, createdByUserID uuid.UUID) models.Group {
	t.Helper()

	group := models.Group{
		Name:            name,
		BaseCurrency:    "USD",
		CreatedByUserID: createdByUserID,
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group %s: %v", name, err)
	}
	return group
}

func createMembership(t *testing.T, db *gorm.DB, groupID, userID uuid.UUID, role string) models.Membership {
	t.Helper()

	membership := models.Membership{
		GroupID: groupID,
		UserID:  userID,
		Role:    role,
	}
	if err := db.Create(&membership).Error; err != nil {
		t.Fatalf("create membership: %v", err)
	}
	return membership
}

func createGroupMessageRecord(
	t *testing.T,
	db *gorm.DB,
	groupID, userID uuid.UUID,
	body string,
	createdAt time.Time,
) models.GroupMessage {
	t.Helper()

	message := models.GroupMessage{
		GroupID:   groupID,
		UserID:    userID,
		Body:      body,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
	if err := db.Create(&message).Error; err != nil {
		t.Fatalf("create group message: %v", err)
	}
	return message
}

func createExpenseRecord(
	t *testing.T,
	db *gorm.DB,
	groupID, createdByUserID, paidByUserID uuid.UUID,
	ownerUserID *uuid.UUID,
	amountCents int64,
	participantIDs []uuid.UUID,
) models.Expense {
	t.Helper()

	expense := models.Expense{
		GroupID:             groupID,
		Description:         "Dinner",
		Category:            "food",
		ExpenseType:         models.ExpenseTypeOneTime,
		AmountCents:         amountCents,
		OriginalAmountCents: amountCents,
		OriginalCurrency:    "USD",
		FXRate:              1,
		FXSource:            "test",
		FXFetchedAt:         time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
		SplitMode:           "equal",
		PaidByUserID:        paidByUserID,
		OwnerUserID:         ownerUserID,
		CreatedByUserID:     createdByUserID,
		IncurredOn:          time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
	}
	if err := db.Create(&expense).Error; err != nil {
		t.Fatalf("create expense: %v", err)
	}

	splitAmounts := equalSplits(amountCents, participantIDs)
	for index, participantID := range participantIDs {
		split := models.ExpenseSplit{
			ExpenseID:   expense.ID,
			UserID:      participantID,
			AmountCents: splitAmounts[index],
		}
		if err := db.Create(&split).Error; err != nil {
			t.Fatalf("create expense split: %v", err)
		}

		if participantID != paidByUserID && split.AmountCents > 0 {
			expenseID := expense.ID
			obligation := models.Obligation{
				GroupID:         groupID,
				FromUserID:      participantID,
				ToUserID:        paidByUserID,
				SourceType:      models.ObligationSourceExpenseSplit,
				SourceExpenseID: &expenseID,
				AmountCents:     split.AmountCents,
			}
			if err := db.Create(&obligation).Error; err != nil {
				t.Fatalf("create obligation: %v", err)
			}
		}
	}

	return expense
}

func userContext(user models.User) context.Context {
	return context.WithValue(context.Background(), currentUserKey, &user)
}

func newExpenseReportRequest(t *testing.T, user models.User, groupID uuid.UUID, format string) *http.Request {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/v1/groups/"+groupID.String()+"/expense-report?format="+format, nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("groupID", groupID.String())

	ctx := context.WithValue(request.Context(), chi.RouteCtxKey, routeContext)
	ctx = context.WithValue(ctx, currentUserKey, &user)
	return request.WithContext(ctx)
}

func newAuthenticatedAPIRequest(t *testing.T, server *Server, user models.User, method, path string, body string) *http.Request {
	t.Helper()

	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}

	session, rawToken, err := server.createSession(user.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	request.AddCookie(&http.Cookie{
		Name:    server.config.SessionCookieName,
		Value:   rawToken,
		Path:    "/",
		Expires: session.ExpiresAt,
	})

	return request
}

func errorStatus(t *testing.T, err error) int {
	t.Helper()

	var statusErr interface {
		GetStatus() int
	}
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected huma status error, got %T: %v", err, err)
	}
	return statusErr.GetStatus()
}

func TestCreateExpenseSupportsCustomSplits(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)

	input := createExpenseInput{GroupID: group.ID}
	input.Body.Description = "Cab ride"
	input.Body.Category = "transport"
	input.Body.ExpenseType = models.ExpenseTypeOneTime
	input.Body.AmountCents = 5000
	input.Body.Currency = "USD"
	input.Body.SplitMode = "custom"
	input.Body.PaidByUserID = creator.ID
	input.Body.OwnerUserID = creator.ID
	input.Body.Splits = []expenseSplitInput{
		{UserID: creator.ID, AmountCents: 1500},
		{UserID: otherMember.ID, AmountCents: 3500},
	}
	input.Body.IncurredOn = "2026-03-19"

	result, err := server.createExpense(userContext(creator), &input)
	if err != nil {
		t.Fatalf("create custom expense: %v", err)
	}

	if result.Body.SplitMode != "custom" {
		t.Fatalf("expected custom split mode, got %q", result.Body.SplitMode)
	}
	if result.Body.Category != "transport" {
		t.Fatalf("expected transport category, got %q", result.Body.Category)
	}
	if len(result.Body.Splits) != 2 {
		t.Fatalf("expected two split rows, got %d", len(result.Body.Splits))
	}

	groupDetail, err := server.getGroup(userContext(creator), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("load group detail: %v", err)
	}
	if len(groupDetail.Body.Expenses) != 1 {
		t.Fatalf("expected one expense, got %d", len(groupDetail.Body.Expenses))
	}
	if groupDetail.Body.Expenses[0].Category != "transport" {
		t.Fatalf("expected expense category transport, got %q", groupDetail.Body.Expenses[0].Category)
	}

	var obligations []models.Obligation
	if err := server.db.Where("group_id = ?", group.ID).Order("amount_cents DESC").Find(&obligations).Error; err != nil {
		t.Fatalf("load obligations: %v", err)
	}
	if len(obligations) != 1 {
		t.Fatalf("expected one obligation for custom splits, got %d", len(obligations))
	}
	if obligations[0].SourceType != models.ObligationSourceExpenseSplit {
		t.Fatalf("expected expense split obligations, got %#v", obligations)
	}
}

func TestCreateMonthlyExpenseCreatesCurrentBrowserMonthOccurrence(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	location, _, err := resolveBrowserLocation("America/New_York")
	if err != nil {
		t.Fatalf("load browser time zone: %v", err)
	}
	localNow := time.Now().In(location)

	input := createExpenseInput{
		GroupID:         group.ID,
		BrowserTimeZone: "America/New_York",
	}
	input.Body.Description = "Apartment rent"
	input.Body.Category = "rent"
	input.Body.ExpenseType = models.ExpenseTypeMonthly
	input.Body.DueDayOfMonth = localNow.Day()
	input.Body.AmountCents = 4000
	input.Body.Currency = "USD"
	input.Body.SplitMode = "equal"
	input.Body.PaidByUserID = otherMember.ID
	input.Body.OwnerUserID = creator.ID
	input.Body.ParticipantUserIDs = []uuid.UUID{creator.ID, otherMember.ID}
	input.Body.IncurredOn = "2026-03-19"

	result, err := server.createExpense(userContext(creator), &input)
	if err != nil {
		t.Fatalf("create monthly expense: %v", err)
	}

	if result.Body.ExpenseType != "Monthly" {
		t.Fatalf("expected monthly expense type label, got %q", result.Body.ExpenseType)
	}
	if result.Body.PaidByUserID != creator.ID {
		t.Fatalf("expected owner to be monthly payee, got %s", result.Body.PaidByUserID)
	}
	if result.Body.Category != "rent" {
		t.Fatalf("expected rent category, got %q", result.Body.Category)
	}

	var expense models.Expense
	if err := server.db.Preload("Splits").First(&expense, "id = ?", result.Body.ID).Error; err != nil {
		t.Fatalf("load monthly occurrence: %v", err)
	}
	if normalizeExpenseType(expense.ExpenseType) != models.ExpenseTypeMonthly {
		t.Fatalf("expected stored monthly occurrence, got %q", expense.ExpenseType)
	}
	if expense.RecurringTemplateID == nil {
		t.Fatal("expected recurring template linkage on monthly occurrence")
	}
	expectedMonth, err := recurringTemplateStartMonth(time.Now().UTC(), input.Body.DueDayOfMonth, location)
	if err != nil {
		t.Fatalf("expected start month for monthly occurrence: %v", err)
	}
	if expense.OccurrenceMonth != expectedMonth {
		t.Fatalf("expected occurrence month %q, got %q", expectedMonth, expense.OccurrenceMonth)
	}
	expectedIncurredOn, err := recurringOccurrenceDateUTC(expectedMonth, input.Body.DueDayOfMonth, location)
	if err != nil {
		t.Fatalf("expected incurredOn for monthly occurrence: %v", err)
	}
	if !expense.IncurredOn.Equal(expectedIncurredOn) {
		t.Fatalf("expected incurredOn %s, got %s", expectedIncurredOn.Format(time.RFC3339), expense.IncurredOn.Format(time.RFC3339))
	}

	if _, err := server.deleteExpense(userContext(creator), &pathExpenseInput{
		GroupID:   group.ID,
		ExpenseID: expense.ID,
	}); err == nil {
		t.Fatal("expected unpaid monthly occurrence delete to fail")
	} else if got := errorStatus(t, err); got != 409 {
		t.Fatalf("expected 409 for unpaid monthly occurrence delete, got %d", got)
	}

	if _, err := server.createSettlement(userContext(otherMember), &createSettlementInput{
		GroupID: group.ID,
		Body: createSettlementBody{
			Kind:        models.SettlementKindDirectExpense,
			FromUserID:  otherMember.ID,
			ToUserID:    creator.ID,
			ExpenseID:   &expense.ID,
			AmountCents: 2000,
			Currency:    "USD",
			SettledOn:   "2026-03-19",
		},
	}); err != nil {
		t.Fatalf("settle monthly occurrence: %v", err)
	}

	if _, err := server.deleteExpense(userContext(creator), &pathExpenseInput{
		GroupID:   group.ID,
		ExpenseID: expense.ID,
	}); err != nil {
		t.Fatalf("delete settled monthly occurrence: %v", err)
	}
}

func TestRecurringTemplateStartMonth(t *testing.T) {
	location, _, err := resolveBrowserLocation("America/New_York")
	if err != nil {
		t.Fatalf("load browser time zone: %v", err)
	}

	tests := []struct {
		name         string
		createdAt    time.Time
		dueDayOfMonth int
		wantMonth    string
	}{
		{
			name:          "same day stays current month",
			createdAt:     time.Date(2026, time.March, 15, 10, 0, 0, 0, location),
			dueDayOfMonth: 15,
			wantMonth:     "2026-03",
		},
		{
			name:          "after due day shifts to next month",
			createdAt:     time.Date(2026, time.March, 22, 10, 0, 0, 0, location),
			dueDayOfMonth: 15,
			wantMonth:     "2026-04",
		},
		{
			name:          "short month clamps due day before comparing",
			createdAt:     time.Date(2026, time.April, 30, 10, 0, 0, 0, location),
			dueDayOfMonth: 31,
			wantMonth:     "2026-04",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotMonth, err := recurringTemplateStartMonth(tc.createdAt.UTC(), tc.dueDayOfMonth, location)
			if err != nil {
				t.Fatalf("compute recurring template start month: %v", err)
			}
			if gotMonth != tc.wantMonth {
				t.Fatalf("expected month %q, got %q", tc.wantMonth, gotMonth)
			}
		})
	}
}

func TestCreateSettlementUpdatesGroupBalances(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		6000,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	result, err := server.createSettlement(userContext(otherMember), &createSettlementInput{
		GroupID: group.ID,
		Body: createSettlementBody{
			FromUserID:  otherMember.ID,
			ToUserID:    creator.ID,
			AmountCents: 1000,
			Currency:    "USD",
			SettledOn:   "2026-03-19",
		},
	})
	if err != nil {
		t.Fatalf("create settlement: %v", err)
	}
	if result.Body.AmountCents != 1000 {
		t.Fatalf("expected settlement amount 1000, got %d", result.Body.AmountCents)
	}

	groupDetail, err := server.getGroup(userContext(creator), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("load group detail: %v", err)
	}
	if len(groupDetail.Body.Settlements) != 1 {
		t.Fatalf("expected one settlement in payload, got %d", len(groupDetail.Body.Settlements))
	}
	if len(groupDetail.Body.Balances.Transfers) != 1 {
		t.Fatalf("expected one remaining transfer, got %d", len(groupDetail.Body.Balances.Transfers))
	}
	if groupDetail.Body.Balances.Transfers[0].AmountCents != 2000 {
		t.Fatalf("expected remaining transfer of 2000, got %d", groupDetail.Body.Balances.Transfers[0].AmountCents)
	}

	var settlement models.Settlement
	if err := server.db.Preload("Applications").First(&settlement, "id = ?", result.Body.ID).Error; err != nil {
		t.Fatalf("load saved settlement: %v", err)
	}
	if settlement.Kind != models.SettlementKindDirectExpense {
		t.Fatalf("expected settlement kind %q, got %q", models.SettlementKindDirectExpense, settlement.Kind)
	}
	if len(settlement.Applications) != 1 {
		t.Fatalf("expected one settlement application, got %d", len(settlement.Applications))
	}
	if settlement.Applications[0].AmountCents != 1000 {
		t.Fatalf("expected settlement application amount 1000, got %d", settlement.Applications[0].AmountCents)
	}
}

func TestCreateSettlementHTTPAllowsSimplifiedDebtWithoutExpenseID(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		6000,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	handler := NewHandler(server.config, server.db)
	request := newAuthenticatedAPIRequest(
		t,
		server,
		otherMember,
		http.MethodPost,
		"/v1/groups/"+group.ID.String()+"/settlements",
		`{
			"kind":"netted",
			"fromUserId":"`+otherMember.ID.String()+`",
			"toUserId":"`+creator.ID.String()+`",
			"amountCents":3000,
			"currency":"USD",
			"settledOn":"2026-03-19"
		}`,
	)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected simplified settlement without expenseId to succeed, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var settlement models.Settlement
	if err := server.db.Order("created_at DESC").First(&settlement).Error; err != nil {
		t.Fatalf("load created settlement: %v", err)
	}
	if settlement.Kind != models.SettlementKindNetted {
		t.Fatalf("expected HTTP simplified settlement kind %q, got %q", models.SettlementKindNetted, settlement.Kind)
	}
}

func TestGroupReadsUseSettlementApplicationsWhenLegacyAllocationsAreAbsent(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	expense := createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		6000,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	result, err := server.createSettlement(userContext(otherMember), &createSettlementInput{
		GroupID: group.ID,
		Body: createSettlementBody{
			FromUserID:  otherMember.ID,
			ToUserID:    creator.ID,
			ExpenseID:   &expense.ID,
			AmountCents: 3000,
			Currency:    "USD",
			SettledOn:   "2026-03-19",
		},
	})
	if err != nil {
		t.Fatalf("create settlement: %v", err)
	}

	if err := server.db.Delete(&models.SettlementAllocation{}, "settlement_id = ?", result.Body.ID).Error; err != nil {
		t.Fatalf("delete legacy settlement allocations: %v", err)
	}

	groupDetail, err := server.getGroup(userContext(creator), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("load group detail: %v", err)
	}
	if len(groupDetail.Body.Balances.Transfers) != 0 {
		t.Fatalf("expected no remaining transfers, got %d", len(groupDetail.Body.Balances.Transfers))
	}
	if len(groupDetail.Body.Expenses) != 1 {
		t.Fatalf("expected one expense, got %d", len(groupDetail.Body.Expenses))
	}
	if groupDetail.Body.Expenses[0].OutstandingAmountCents != 0 {
		t.Fatalf("expected outstanding amount 0 from settlement applications, got %d", groupDetail.Body.Expenses[0].OutstandingAmountCents)
	}
}

func TestGetGroupIncludesSettleUpPayeesForCurrentUser(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		6000,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	groupDetail, err := server.getGroup(userContext(otherMember), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("load group detail: %v", err)
	}
	if len(groupDetail.Body.Balances.SettleUpPayees) != 1 {
		t.Fatalf("expected one settle-up payee, got %d", len(groupDetail.Body.Balances.SettleUpPayees))
	}

	payee := groupDetail.Body.Balances.SettleUpPayees[0]
	if payee.UserID != creator.ID {
		t.Fatalf("expected creator payee %s, got %s", creator.ID, payee.UserID)
	}
	if payee.AmountCents != 3000 {
		t.Fatalf("expected 3000 owed to payee, got %d", payee.AmountCents)
	}
}

func TestGetGroupIncludesExpenseLevelSettleUpRowsAndExactSettlementValidation(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	dinnerExpense := createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		6000,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)
	taxiExpense := createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		3000,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	groupDetail, err := server.getGroup(userContext(otherMember), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("load group detail: %v", err)
	}
	if len(groupDetail.Body.Balances.SettleUpExpenses) != 2 {
		t.Fatalf("expected two settle-up expense options, got %d", len(groupDetail.Body.Balances.SettleUpExpenses))
	}

	firstOption := groupDetail.Body.Balances.SettleUpExpenses[0]
	secondOption := groupDetail.Body.Balances.SettleUpExpenses[1]
	if firstOption.ToUserID != creator.ID || secondOption.ToUserID != creator.ID {
		t.Fatal("expected both settle-up expense options to point to the creator")
	}

	optionByExpenseID := map[uuid.UUID]settleUpExpenseRow{
		firstOption.ExpenseID: firstOption,
		secondOption.ExpenseID: secondOption,
	}
	if optionByExpenseID[dinnerExpense.ID].AmountCents != 3000 {
		t.Fatalf("expected dinner option amount 3000, got %d", optionByExpenseID[dinnerExpense.ID].AmountCents)
	}
	if optionByExpenseID[taxiExpense.ID].AmountCents != 1500 {
		t.Fatalf("expected taxi option amount 1500, got %d", optionByExpenseID[taxiExpense.ID].AmountCents)
	}

	if _, err := server.createSettlement(userContext(otherMember), &createSettlementInput{
		GroupID: group.ID,
		Body: createSettlementBody{
			FromUserID:  otherMember.ID,
			ToUserID:    creator.ID,
			ExpenseID:   &taxiExpense.ID,
			AmountCents: 1200,
			Currency:    "USD",
			SettledOn:   "2026-03-19",
		},
	}); err == nil {
		t.Fatal("expected non-exact expense settlement amount to fail")
	} else if got := errorStatus(t, err); got != 400 {
		t.Fatalf("expected 400 for non-exact expense settlement, got %d", got)
	}

	result, err := server.createSettlement(userContext(otherMember), &createSettlementInput{
		GroupID: group.ID,
		Body: createSettlementBody{
			FromUserID:  otherMember.ID,
			ToUserID:    creator.ID,
			ExpenseID:   &taxiExpense.ID,
			AmountCents: 1500,
			Currency:    "USD",
			SettledOn:   "2026-03-19",
		},
	})
	if err != nil {
		t.Fatalf("create exact expense settlement: %v", err)
	}
	if result.Body.AmountCents != 1500 {
		t.Fatalf("expected exact settlement amount 1500, got %d", result.Body.AmountCents)
	}
}

func TestGetGroupIncludesCurrentUserSimplifiedSettleTo(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	thirdMember := createUser(t, server.db, "Carol", "carol@example.com")
	createMembership(t, server.db, group.ID, thirdMember.ID, "member")

	createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		2000,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)
	createExpenseRecord(
		t,
		server.db,
		group.ID,
		thirdMember.ID,
		thirdMember.ID,
		&thirdMember.ID,
		2000,
		[]uuid.UUID{otherMember.ID, thirdMember.ID},
	)
	createExpenseRecord(
		t,
		server.db,
		group.ID,
		thirdMember.ID,
		thirdMember.ID,
		&thirdMember.ID,
		2000,
		[]uuid.UUID{creator.ID, thirdMember.ID},
	)

	groupDetail, err := server.getGroup(userContext(otherMember), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("load group detail: %v", err)
	}
	if len(groupDetail.Body.Balances.SimplifiedSettleTo) != 1 {
		t.Fatalf("expected one simplified settle-to row, got %d", len(groupDetail.Body.Balances.SimplifiedSettleTo))
	}

	row := groupDetail.Body.Balances.SimplifiedSettleTo[0]
	if row.FromUserID != otherMember.ID {
		t.Fatalf("expected Bob to be debtor, got %s", row.FromUserID)
	}
	if row.ToUserID != thirdMember.ID {
		t.Fatalf("expected Carol to be creditor, got %s", row.ToUserID)
	}
	if row.AmountCents != 2000 {
		t.Fatalf("expected simplified amount 2000, got %d", row.AmountCents)
	}
}

func TestCreateSettlementSupportsSimplifiedDebtAcrossIntermediateMember(t *testing.T) {
	server, arun, nura, group := newTestServer(t)
	sdas := createUser(t, server.db, "Sdas", "sdas@example.com")
	createMembership(t, server.db, group.ID, sdas.ID, "member")

	createExpenseRecord(
		t,
		server.db,
		group.ID,
		arun.ID,
		arun.ID,
		&arun.ID,
		2000,
		[]uuid.UUID{arun.ID, nura.ID},
	)
	createExpenseRecord(
		t,
		server.db,
		group.ID,
		sdas.ID,
		sdas.ID,
		&sdas.ID,
		2000,
		[]uuid.UUID{nura.ID, sdas.ID},
	)
	createExpenseRecord(
		t,
		server.db,
		group.ID,
		sdas.ID,
		sdas.ID,
		&sdas.ID,
		2000,
		[]uuid.UUID{arun.ID, sdas.ID},
	)

	groupDetailBefore, err := server.getGroup(userContext(nura), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("load group detail before simplified settlement: %v", err)
	}
	if len(groupDetailBefore.Body.Balances.SimplifiedSettleTo) != 1 {
		t.Fatalf("expected one simplified settle-to row, got %d", len(groupDetailBefore.Body.Balances.SimplifiedSettleTo))
	}
	if row := groupDetailBefore.Body.Balances.SimplifiedSettleTo[0]; row.ToUserID != sdas.ID || row.AmountCents != 2000 {
		t.Fatalf("expected Nura to owe Sdas 2000, got %+v", row)
	}

	result, err := server.createSettlement(userContext(nura), &createSettlementInput{
		GroupID: group.ID,
		Body: createSettlementBody{
			Kind:        models.SettlementKindNetted,
			FromUserID:  nura.ID,
			ToUserID:    sdas.ID,
			AmountCents: 2000,
			Currency:    "USD",
			SettledOn:   "2026-03-19",
		},
	})
	if err != nil {
		t.Fatalf("create simplified settlement: %v", err)
	}
	if result.Body.AmountCents != 2000 {
		t.Fatalf("expected settlement amount 2000, got %d", result.Body.AmountCents)
	}

	var settlement models.Settlement
	if err := server.db.Preload("Applications").First(&settlement, "id = ?", result.Body.ID).Error; err != nil {
		t.Fatalf("reload settlement: %v", err)
	}
	if settlement.Kind != models.SettlementKindNetted {
		t.Fatalf("expected netted settlement kind, got %q", settlement.Kind)
	}
	if len(settlement.Applications) != 4 {
		t.Fatalf("expected four settlement applications for the simplified flow, got %d", len(settlement.Applications))
	}

	var reimbursementObligations []models.Obligation
	if err := server.db.
		Where("source_type = ? AND source_settlement_id = ?", models.ObligationSourceReimbursement, settlement.ID).
		Find(&reimbursementObligations).Error; err != nil {
		t.Fatalf("load reimbursement obligations: %v", err)
	}
	if len(reimbursementObligations) != 1 {
		t.Fatalf("expected one reimbursement obligation, got %d", len(reimbursementObligations))
	}
	if reimbursementObligations[0].FromUserID != arun.ID || reimbursementObligations[0].ToUserID != nura.ID {
		t.Fatalf("expected reimbursement Arun -> Nura, got %+v", reimbursementObligations[0])
	}

	groupDetailAfter, err := server.getGroup(userContext(nura), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("load group detail after simplified settlement: %v", err)
	}
	if len(groupDetailAfter.Body.Balances.SimplifiedSettleTo) != 0 {
		t.Fatalf("expected simplified debts to be cleared, got %+v", groupDetailAfter.Body.Balances.SimplifiedSettleTo)
	}
	if len(groupDetailAfter.Body.Balances.Settlements) != 0 {
		t.Fatalf("expected no remaining settlements owed by Nura, got %+v", groupDetailAfter.Body.Balances.Settlements)
	}
}

func TestGetGroupIncludesInvitationDatesForMembers(t *testing.T) {
	server, creator, _, group := newTestServer(t)
	invitedUser := createUser(t, server.db, "Carol", "carol@example.com")

	invitation, err := server.createInvitation(userContext(creator), &inviteInput{
		GroupID: group.ID,
		Body: struct {
			Email string `json:"email" format:"email"`
		}{
			Email: invitedUser.Email,
		},
	})
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	if _, err := server.acceptInvitation(userContext(invitedUser), &acceptInvitationInput{
		InvitationID: invitation.Body.ID,
	}); err != nil {
		t.Fatalf("accept invitation: %v", err)
	}

	groupDetail, err := server.getGroup(userContext(creator), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("load group detail: %v", err)
	}

	var ownerMember *groupMember
	var invitedMember *groupMember
	for index := range groupDetail.Body.Members {
		member := &groupDetail.Body.Members[index]
		switch member.ID {
		case creator.ID:
			ownerMember = member
		case invitedUser.ID:
			invitedMember = member
		}
	}

	if ownerMember == nil {
		t.Fatal("expected owner member in group payload")
	}
	if ownerMember.JoinedAt.IsZero() {
		t.Fatal("expected owner member joinedAt to be present")
	}
	if ownerMember.InvitedAt != nil {
		t.Fatalf("expected owner member invitedAt to be nil, got %s", ownerMember.InvitedAt)
	}
	if ownerMember.AcceptedAt != nil {
		t.Fatalf("expected owner member acceptedAt to be nil, got %s", ownerMember.AcceptedAt)
	}

	if invitedMember == nil {
		t.Fatal("expected invited member in group payload")
	}
	if invitedMember.JoinedAt.IsZero() {
		t.Fatal("expected invited member joinedAt to be present")
	}
	if invitedMember.InvitedAt == nil || invitedMember.InvitedAt.IsZero() {
		t.Fatal("expected invited member invitedAt to be present")
	}
	if invitedMember.AcceptedAt == nil || invitedMember.AcceptedAt.IsZero() {
		t.Fatal("expected invited member acceptedAt to be present")
	}
}

func TestGetGroupIncludesExpenseSummaryCounts(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	closedExpense := createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		2400,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	if _, err := server.createSettlement(userContext(otherMember), &createSettlementInput{
		GroupID: group.ID,
		Body: createSettlementBody{
			FromUserID:  otherMember.ID,
			ToUserID:    creator.ID,
			AmountCents: 1200,
			Currency:    "USD",
			SettledOn:   "2026-03-19",
		},
	}); err != nil {
		t.Fatalf("settle expense before archive: %v", err)
	}

	if _, err := server.archiveExpense(userContext(creator), &pathExpenseInput{
		GroupID:   group.ID,
		ExpenseID: closedExpense.ID,
	}); err != nil {
		t.Fatalf("archive closed expense: %v", err)
	}

	openExpense := createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		6000,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	groupDetail, err := server.getGroup(userContext(creator), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("load group detail: %v", err)
	}

	if groupDetail.Body.ExpenseSummary.ActiveCount != 1 {
		t.Fatalf("expected one active expense, got %d", groupDetail.Body.ExpenseSummary.ActiveCount)
	}
	if groupDetail.Body.ExpenseSummary.ClosedCount != 1 {
		t.Fatalf("expected one closed expense, got %d", groupDetail.Body.ExpenseSummary.ClosedCount)
	}
	if groupDetail.Body.Group.ExpenseCount != 1 {
		t.Fatalf("expected visible group expense count to remain one active expense, got %d", groupDetail.Body.Group.ExpenseCount)
	}
	if openExpense.ID == uuid.Nil {
		t.Fatal("expected open expense fixture to be created")
	}
}

func TestDownloadExpenseReportSupportsJSONAndCSVForOwner(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		2400,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	if _, err := server.createSettlement(userContext(otherMember), &createSettlementInput{
		GroupID: group.ID,
		Body: createSettlementBody{
			FromUserID:  otherMember.ID,
			ToUserID:    creator.ID,
			AmountCents: 1200,
			Currency:    "USD",
			SettledOn:   "2026-03-19",
		},
	}); err != nil {
		t.Fatalf("settle expense before report: %v", err)
	}

	createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		6000,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)
	now := time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC)
	if err := server.db.Model(&models.Group{}).Where("id = ?", group.ID).Update("archived_at", now).Error; err != nil {
		t.Fatalf("archive group for report: %v", err)
	}

	jsonRequest := newExpenseReportRequest(t, creator, group.ID, "json")
	jsonRecorder := httptest.NewRecorder()
	server.downloadExpenseReport(jsonRecorder, jsonRequest)

	if jsonRecorder.Code != http.StatusOK {
		t.Fatalf("expected json export 200, got %d: %s", jsonRecorder.Code, jsonRecorder.Body.String())
	}
	if got := jsonRecorder.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected json content type, got %q", got)
	}
	if got := jsonRecorder.Header().Get("Content-Disposition"); !regexp.MustCompile(
		`attachment; filename="` + regexp.QuoteMeta(sanitizeReportFilename(group.Name)) + `-\d{8}-\d{6}-expense-report\.json"`,
	).MatchString(got) {
		t.Fatalf("expected timestamped json filename, got %q", got)
	}
	var report expenseReportDocument
	if err := json.Unmarshal(jsonRecorder.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode json report: %v", err)
	}
	if report.Group.Status != "Archived" {
		t.Fatalf("expected archived group status, got %q", report.Group.Status)
	}
	if report.Group.ActiveExpenseCount != 1 {
		t.Fatalf("expected one active expense, got %d", report.Group.ActiveExpenseCount)
	}
	if report.Group.ClosedExpenseCount != 1 {
		t.Fatalf("expected one closed expense, got %d", report.Group.ClosedExpenseCount)
	}
	if report.Group.GroupExpenditureCents != 2400 {
		t.Fatalf("expected group expenditure 2400, got %d", report.Group.GroupExpenditureCents)
	}
	if len(report.Expenses) != 2 {
		t.Fatalf("expected two expenses in report, got %d", len(report.Expenses))
	}
	if report.Expenses[0].Status != "Open" {
		t.Fatalf("expected newest expense to be open, got %q", report.Expenses[0].Status)
	}
	if report.Expenses[0].AmountCents != 6000 {
		t.Fatalf("expected newest expense amount 6000, got %d", report.Expenses[0].AmountCents)
	}
	if report.Expenses[0].Category != "Food" {
		t.Fatalf("expected newest expense category Food, got %q", report.Expenses[0].Category)
	}
	if report.Expenses[0].ExpenseType != "One-time" {
		t.Fatalf("expected newest expense type One-time, got %q", report.Expenses[0].ExpenseType)
	}
	if report.Expenses[0].PayByDate != "N/A" {
		t.Fatalf("expected one-time expense pay-by date to be N/A, got %q", report.Expenses[0].PayByDate)
	}
	if report.Expenses[1].Status != "Closed" {
		t.Fatalf("expected settled expense to be closed, got %q", report.Expenses[1].Status)
	}
	if report.Expenses[1].AmountCents != 2400 {
		t.Fatalf("expected settled expense amount 2400, got %d", report.Expenses[1].AmountCents)
	}
	if len(report.Expenses[1].SplitWith) != 1 || report.Expenses[1].SplitWith[0] != "Bob [2026-03-19]" {
		t.Fatalf("expected split-with Bob [2026-03-19], got %#v", report.Expenses[1].SplitWith)
	}

	csvRequest := newExpenseReportRequest(t, creator, group.ID, "csv")
	csvRecorder := httptest.NewRecorder()
	server.downloadExpenseReport(csvRecorder, csvRequest)

	if csvRecorder.Code != http.StatusOK {
		t.Fatalf("expected csv export 200, got %d: %s", csvRecorder.Code, csvRecorder.Body.String())
	}
	if got := csvRecorder.Header().Get("Content-Type"); !strings.Contains(got, "text/csv") {
		t.Fatalf("expected csv content type, got %q", got)
	}
	if got := csvRecorder.Header().Get("Content-Disposition"); !regexp.MustCompile(
		`attachment; filename="` + regexp.QuoteMeta(sanitizeReportFilename(group.Name)) + `-\d{8}-\d{6}-expense-report\.csv"`,
	).MatchString(got) {
		t.Fatalf("expected timestamped csv filename, got %q", got)
	}
	csvBody := csvRecorder.Body.String()
	if !strings.Contains(csvBody, "Group Status,Archived") {
		t.Fatalf("expected csv report to include archived group status, body=%s", csvBody)
	}
	if !strings.Contains(csvBody, "Active Expenses,1") {
		t.Fatalf("expected csv report to include one active expense, body=%s", csvBody)
	}
	if !strings.Contains(csvBody, "Closed Expenses,1") {
		t.Fatalf("expected csv report to include one closed expense, body=%s", csvBody)
	}
	if !strings.Contains(csvBody, "Group Expenditure,24.00") {
		t.Fatalf("expected csv report to include group expenditure, body=%s", csvBody)
	}
	if !strings.Contains(csvBody, "Expense Name,Expense Category,Expense Type,PayByDate,Expense Status,Amount,Created At,Owner,Paid By,Split With") {
		t.Fatalf("expected csv report to include category/type/pay-by/split-with columns, body=%s", csvBody)
	}
	if !strings.Contains(csvBody, "Dinner,Food,One-time,N/A,Open,60.00") {
		t.Fatalf("expected csv report to include open expense amount, body=%s", csvBody)
	}
	if !strings.Contains(csvBody, "Dinner,Food,One-time,N/A,Closed,24.00") {
		t.Fatalf("expected csv report to include closed expense amount, body=%s", csvBody)
	}
	if !strings.Contains(csvBody, "Bob [2026-03-19]") {
		t.Fatalf("expected csv report to include split-with Bob [2026-03-19], body=%s", csvBody)
	}
}

func TestDownloadExpenseReportRejectsNonOwner(t *testing.T) {
	server, _, otherMember, group := newTestServer(t)

	request := newExpenseReportRequest(t, otherMember, group.ID, "json")
	recorder := httptest.NewRecorder()
	server.downloadExpenseReport(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected non-owner export to be forbidden, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestDownloadExpenseReportMarksMonthlyExpenses(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)

	input := createExpenseInput{
		GroupID:         group.ID,
		BrowserTimeZone: "UTC",
	}
	input.Body.Description = "Streaming plan"
	input.Body.Category = "subscription"
	input.Body.ExpenseType = models.ExpenseTypeMonthly
	input.Body.DueDayOfMonth = 9
	input.Body.AmountCents = 1800
	input.Body.Currency = "USD"
	input.Body.SplitMode = "equal"
	input.Body.PaidByUserID = otherMember.ID
	input.Body.OwnerUserID = creator.ID
	input.Body.ParticipantUserIDs = []uuid.UUID{creator.ID, otherMember.ID}
	input.Body.IncurredOn = "2026-03-19"

	if _, err := server.createExpense(userContext(creator), &input); err != nil {
		t.Fatalf("create monthly expense for report: %v", err)
	}

	request := newExpenseReportRequest(t, creator, group.ID, "json")
	request.Header.Set("X-Time-Zone", "UTC")
	recorder := httptest.NewRecorder()
	server.downloadExpenseReport(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected json export 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var report expenseReportDocument
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode json report: %v", err)
	}
	if len(report.Expenses) != 1 {
		t.Fatalf("expected one expense in report, got %d", len(report.Expenses))
	}
	if report.Expenses[0].Category != "Subscription" {
		t.Fatalf("expected subscription category, got %q", report.Expenses[0].Category)
	}
	if report.Expenses[0].ExpenseType != "Monthly" {
		t.Fatalf("expected Monthly expense type, got %q", report.Expenses[0].ExpenseType)
	}
	if report.Expenses[0].PayByDate == "" {
		t.Fatal("expected monthly expense report row to include pay-by date")
	}
	if len(report.Expenses[0].SplitWith) != 1 || report.Expenses[0].SplitWith[0] != "Bob [ToPay]" {
		t.Fatalf("expected monthly split-with Bob [ToPay], got %#v", report.Expenses[0].SplitWith)
	}
}

func TestExpenseReportPayByDate(t *testing.T) {
	recurringTemplate := &models.RecurringExpenseTemplate{DueDayOfMonth: 9}

	oneTimeExpense := models.Expense{
		ExpenseType: models.ExpenseTypeOneTime,
		IncurredOn:  time.Date(2026, time.March, 9, 0, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, time.March, 22, 14, 0, 0, 0, time.UTC),
	}
	if got := expenseReportPayByDate(oneTimeExpense); got != "N/A" {
		t.Fatalf("expected one-time pay-by date N/A, got %q", got)
	}

	openMonthlyExpense := models.Expense{
		ExpenseType:       models.ExpenseTypeMonthly,
		IncurredOn:        time.Date(2026, time.March, 9, 0, 0, 0, 0, time.UTC),
		CreatedAt:         time.Date(2026, time.March, 22, 14, 0, 0, 0, time.UTC),
		RecurringTemplate: recurringTemplate,
	}
	if got := expenseReportPayByDate(openMonthlyExpense); got != "2026-04-09" {
		t.Fatalf("expected retrofitted monthly pay-by date 2026-04-09, got %q", got)
	}

	closedMonthlyExpense := models.Expense{
		ExpenseType:       models.ExpenseTypeMonthly,
		IncurredOn:        time.Date(2026, time.March, 9, 0, 0, 0, 0, time.UTC),
		CreatedAt:         time.Date(2026, time.March, 22, 14, 0, 0, 0, time.UTC),
		RecurringTemplate: recurringTemplate,
	}
	if got := expenseReportPayByDate(closedMonthlyExpense); got != "2026-04-09" {
		t.Fatalf("expected closed monthly pay-by date to retrofit to 2026-04-09, got %q", got)
	}
}

func TestCreateExpenseNormalizesIntoGroupBaseCurrency(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)

	if err := server.db.Model(&models.Group{}).Where("id = ?", group.ID).Update("base_currency", "USD").Error; err != nil {
		t.Fatalf("set group base currency: %v", err)
	}

	input := createExpenseInput{GroupID: group.ID}
	input.Body.Description = "Museum tickets"
	input.Body.Category = "entertainment"
	input.Body.ExpenseType = models.ExpenseTypeOneTime
	input.Body.AmountCents = 5000
	input.Body.Currency = "EUR"
	input.Body.SplitMode = "equal"
	input.Body.PaidByUserID = creator.ID
	input.Body.OwnerUserID = creator.ID
	input.Body.ParticipantUserIDs = []uuid.UUID{creator.ID, otherMember.ID}
	input.Body.IncurredOn = "2026-03-19"

	result, err := server.createExpense(userContext(creator), &input)
	if err != nil {
		t.Fatalf("create converted expense: %v", err)
	}

	if result.Body.AmountCents != 6000 {
		t.Fatalf("expected converted base amount 6000, got %d", result.Body.AmountCents)
	}
	if result.Body.OriginalCurrency != "EUR" {
		t.Fatalf("expected original currency EUR, got %q", result.Body.OriginalCurrency)
	}
	if result.Body.BaseCurrency != "USD" {
		t.Fatalf("expected base currency USD, got %q", result.Body.BaseCurrency)
	}
}

func TestArchiveExpenseRequiresFullSettlement(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	expense := createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		6000,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	if _, err := server.archiveExpense(userContext(creator), &pathExpenseInput{
		GroupID:   group.ID,
		ExpenseID: expense.ID,
	}); err == nil {
		t.Fatal("expected unsettled expense archive to fail")
	} else if got := errorStatus(t, err); got != 409 {
		t.Fatalf("expected 409 for unsettled archive, got %d", got)
	}

	if _, err := server.createSettlement(userContext(otherMember), &createSettlementInput{
		GroupID: group.ID,
		Body: createSettlementBody{
			FromUserID:  otherMember.ID,
			ToUserID:    creator.ID,
			AmountCents: 3000,
			Currency:    "USD",
			SettledOn:   "2026-03-19",
		},
	}); err != nil {
		t.Fatalf("settle expense: %v", err)
	}

	if _, err := server.archiveExpense(userContext(creator), &pathExpenseInput{
		GroupID:   group.ID,
		ExpenseID: expense.ID,
	}); err != nil {
		t.Fatalf("archive settled expense: %v", err)
	}

	groupDetail, err := server.getGroup(userContext(creator), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("load group detail after archive: %v", err)
	}
	if len(groupDetail.Body.Expenses) != 0 {
		t.Fatalf("expected archived expense to be hidden from active payload, got %d expenses", len(groupDetail.Body.Expenses))
	}
}

func TestArchiveGroupRequiresClosedExpensesAndSupportsUnarchive(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		6000,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	if _, err := server.archiveGroup(userContext(creator), &pathGroupInput{GroupID: group.ID}); err == nil {
		t.Fatal("expected archiving a group with open expenses to fail")
	} else if got := errorStatus(t, err); got != 409 {
		t.Fatalf("expected 409 for open group archive, got %d", got)
	}

	if _, err := server.createSettlement(userContext(otherMember), &createSettlementInput{
		GroupID: group.ID,
		Body: createSettlementBody{
			FromUserID:  otherMember.ID,
			ToUserID:    creator.ID,
			AmountCents: 3000,
			Currency:    "USD",
			SettledOn:   "2026-03-19",
		},
	}); err != nil {
		t.Fatalf("settle group expense: %v", err)
	}

	archiveResult, err := server.archiveGroup(userContext(creator), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("archive settled group: %v", err)
	}
	if !archiveResult.Body.IsArchived || !archiveResult.Body.CanUnarchive {
		t.Fatalf("expected archived group flags after archive, got %+v", archiveResult.Body)
	}

	unarchiveResult, err := server.unarchiveGroup(userContext(creator), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("unarchive group: %v", err)
	}
	if unarchiveResult.Body.IsArchived || !unarchiveResult.Body.CanArchive {
		t.Fatalf("expected active group flags after unarchive, got %+v", unarchiveResult.Body)
	}
}

func TestGetGroupIncludesMessagesNewestFirst(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	older := time.Date(2026, time.March, 18, 9, 0, 0, 0, time.UTC)
	newer := older.Add(2 * time.Hour)

	createGroupMessageRecord(t, server.db, group.ID, creator.ID, "Older message", older)
	newest := createGroupMessageRecord(t, server.db, group.ID, otherMember.ID, "Newest message", newer)

	groupDetail, err := server.getGroup(userContext(otherMember), &pathGroupInput{GroupID: group.ID})
	if err != nil {
		t.Fatalf("load group detail: %v", err)
	}
	if len(groupDetail.Body.Messages) != 2 {
		t.Fatalf("expected two messages, got %d", len(groupDetail.Body.Messages))
	}
	if groupDetail.Body.Messages[0].ID != newest.ID {
		t.Fatalf("expected newest message first, got %+v", groupDetail.Body.Messages[0])
	}
	if !groupDetail.Body.Messages[0].CanDelete {
		t.Fatal("expected current user to be able to delete their own message")
	}
	if groupDetail.Body.Messages[1].CanDelete {
		t.Fatal("expected current user not to be able to delete another user's message")
	}
}

func TestCreateGroupMessageAllowsMember(t *testing.T) {
	server, _, otherMember, group := newTestServer(t)

	result, err := server.createGroupMessage(userContext(otherMember), &createGroupMessageInput{
		GroupID: group.ID,
		Body: struct {
			Body string `json:"body" minLength:"1" maxLength:"2000"`
		}{
			Body: "Hello from Bob",
		},
	})
	if err != nil {
		t.Fatalf("create group message: %v", err)
	}
	if result.Body.UserID != otherMember.ID {
		t.Fatalf("expected message author %s, got %s", otherMember.ID, result.Body.UserID)
	}

	var count int64
	if err := server.db.Model(&models.GroupMessage{}).Where("group_id = ?", group.ID).Count(&count).Error; err != nil {
		t.Fatalf("count group messages: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one message row, got %d", count)
	}
}

func TestDeleteGroupMessageRejectsOtherMember(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	message := createGroupMessageRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		"Owner message",
		time.Date(2026, time.March, 18, 10, 0, 0, 0, time.UTC),
	)

	_, err := server.deleteGroupMessage(userContext(otherMember), &pathGroupMessageInput{
		GroupID:   group.ID,
		MessageID: message.ID,
	})
	if err == nil {
		t.Fatal("expected deleting another user's message to fail")
	}
	if got := errorStatus(t, err); got != 403 {
		t.Fatalf("expected 403 for deleting another user's message, got %d", got)
	}
}

func TestCreateGroupMessageRejectsArchivedGroup(t *testing.T) {
	server, creator, otherMember, group := newTestServer(t)
	createExpenseRecord(
		t,
		server.db,
		group.ID,
		creator.ID,
		creator.ID,
		&creator.ID,
		6000,
		[]uuid.UUID{creator.ID, otherMember.ID},
	)

	if _, err := server.createSettlement(userContext(otherMember), &createSettlementInput{
		GroupID: group.ID,
		Body: createSettlementBody{
			FromUserID:  otherMember.ID,
			ToUserID:    creator.ID,
			AmountCents: 3000,
			Currency:    "USD",
			SettledOn:   "2026-03-19",
		},
	}); err != nil {
		t.Fatalf("settle group expense: %v", err)
	}

	if _, err := server.archiveGroup(userContext(creator), &pathGroupInput{GroupID: group.ID}); err != nil {
		t.Fatalf("archive group: %v", err)
	}

	_, err := server.createGroupMessage(userContext(otherMember), &createGroupMessageInput{
		GroupID: group.ID,
		Body: struct {
			Body string `json:"body" minLength:"1" maxLength:"2000"`
		}{
			Body: "Should not post",
		},
	})
	if err == nil {
		t.Fatal("expected archived group message create to fail")
	}
	if got := errorStatus(t, err); got != 409 {
		t.Fatalf("expected 409 for archived group message create, got %d", got)
	}
}
