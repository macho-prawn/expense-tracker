package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"sharetab/service/internal/app"
	"sharetab/service/internal/models"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type contextKey string

const (
	currentUserKey    contextKey = "current-user"
	currentSessionKey contextKey = "current-session"
)

type Server struct {
	db     *gorm.DB
	config app.Config
	fx     app.CurrencyConverter
}

func NewHandler(cfg app.Config, db *gorm.DB) http.Handler {
	server := &Server{
		db:     db,
		config: cfg,
		fx:     app.NewFXClient(cfg.FXAPIBaseURL, nil),
	}

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(server.authMiddleware)
	router.Get("/v1/groups/{groupID}/expense-report", server.downloadExpenseReport)

	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	api := humachi.New(router, huma.DefaultConfig("ShareTab API", "1.0.0"))
	server.registerRoutes(api)

	return router
}

type userResponse struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

type authEnvelope struct {
	User userResponse `json:"user"`
}

type registerInput struct {
	Body struct {
		Name     string `json:"name" minLength:"1" maxLength:"120"`
		Email    string `json:"email" format:"email"`
		Password string `json:"password" minLength:"8" maxLength:"72"`
	}
}

type authOutput struct {
	SetCookie string       `header:"Set-Cookie"`
	Body      authEnvelope `json:"body"`
}

type loginInput struct {
	Body struct {
		Email    string `json:"email" format:"email"`
		Password string `json:"password" minLength:"8" maxLength:"72"`
	}
}

type logoutOutput struct {
	SetCookie string `header:"Set-Cookie"`
}

type meOutput struct {
	Body authEnvelope `json:"body"`
}

type dashboardGroup struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	BaseCurrency string    `json:"baseCurrency"`
	MemberCount  int64     `json:"memberCount"`
	ExpenseCount int64     `json:"expenseCount"`
	IsOwner      bool      `json:"isOwner"`
	IsArchived   bool      `json:"isArchived"`
	CanDelete    bool      `json:"canDelete"`
	CanArchive   bool      `json:"canArchive"`
	CanUnarchive bool      `json:"canUnarchive"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type dashboardInvitation struct {
	ID            uuid.UUID `json:"id"`
	GroupID       uuid.UUID `json:"groupId"`
	GroupName     string    `json:"groupName"`
	InvitedByName string    `json:"invitedByName"`
	Email         string    `json:"email"`
	CreatedAt     time.Time `json:"createdAt"`
}

type dashboardOutput struct {
	Body struct {
		User        userResponse          `json:"user"`
		Groups      []dashboardGroup      `json:"groups"`
		Invitations []dashboardInvitation `json:"invitations"`
	} `json:"body"`
}

type dashboardInput struct {
	BrowserTimeZone string `header:"X-Time-Zone"`
}

type createGroupInput struct {
	Body struct {
		Name         string `json:"name" minLength:"1" maxLength:"160"`
		BaseCurrency string `json:"baseCurrency"`
	}
}

type createGroupOutput struct {
	Body dashboardGroup `json:"body"`
}

type deleteGroupOutput struct{}

type groupLifecycleOutput struct {
	Body dashboardGroup `json:"body"`
}

type pathGroupInput struct {
	GroupID         uuid.UUID `path:"groupID"`
	BrowserTimeZone string    `header:"X-Time-Zone"`
}

type inviteInput struct {
	GroupID uuid.UUID `path:"groupID"`
	Body    struct {
		Email string `json:"email" format:"email"`
	}
}

type invitationOutput struct {
	Body struct {
		ID        uuid.UUID `json:"id"`
		Email     string    `json:"email"`
		Status    string    `json:"status"`
		CreatedAt time.Time `json:"createdAt"`
	} `json:"body"`
}

type acceptInvitationInput struct {
	InvitationID uuid.UUID `path:"invitationID"`
}

type groupMember struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Email      string     `json:"email"`
	Role       string     `json:"role"`
	JoinedAt   time.Time  `json:"joinedAt"`
	InvitedAt  *time.Time `json:"invitedAt,omitempty"`
	AcceptedAt *time.Time `json:"acceptedAt,omitempty"`
}

type groupInvitation struct {
	ID            uuid.UUID `json:"id"`
	Email         string    `json:"email"`
	Status        string    `json:"status"`
	InvitedByName string    `json:"invitedByName"`
	CreatedAt     time.Time `json:"createdAt"`
}

type groupExpenseSplit struct {
	UserID      uuid.UUID `json:"userId"`
	UserName    string    `json:"userName"`
	AmountCents int64     `json:"amountCents"`
}

type groupExpense struct {
	ID                  uuid.UUID          `json:"id"`
	Description         string             `json:"description"`
	Category            string             `json:"category"`
	ExpenseType         string             `json:"expenseType"`
	AmountCents         int64              `json:"amountCents"`
	BaseCurrency        string             `json:"baseCurrency"`
	OriginalAmountCents int64              `json:"originalAmountCents"`
	OriginalCurrency    string             `json:"originalCurrency"`
	FXRate              float64            `json:"fxRate"`
	SplitMode           string             `json:"splitMode"`
	PaidByUserID        uuid.UUID          `json:"paidByUserId"`
	PaidByName          string             `json:"paidByName"`
	OwnerUserID         uuid.UUID          `json:"ownerUserId"`
	OwnerName           string             `json:"ownerName"`
	ParticipantUserIDs  []uuid.UUID        `json:"participantUserIds"`
	ParticipantNames    []string           `json:"participantNames"`
	Splits              []groupExpenseSplit `json:"splits"`
	IncurredOn          string             `json:"incurredOn"`
	OutstandingAmountCents int64          `json:"outstandingAmountCents"`
	CanDelete           bool               `json:"canDelete"`
	CanArchive          bool               `json:"canArchive"`
	CreatedAt           time.Time          `json:"createdAt"`
}

type groupSettlement struct {
	ID                  uuid.UUID `json:"id"`
	FromUserID          uuid.UUID `json:"fromUserId"`
	FromName            string    `json:"fromName"`
	ToUserID            uuid.UUID `json:"toUserId"`
	ToName              string    `json:"toName"`
	AmountCents         int64     `json:"amountCents"`
	BaseCurrency        string    `json:"baseCurrency"`
	OriginalAmountCents int64     `json:"originalAmountCents"`
	OriginalCurrency    string    `json:"originalCurrency"`
	FXRate              float64   `json:"fxRate"`
	SettledOn           string    `json:"settledOn"`
	CreatedAt           time.Time `json:"createdAt"`
}

type currentUserBalanceRow struct {
	Who         string `json:"who"`
	AmountCents int64  `json:"amountCents"`
	Expense     string `json:"expense"`
}

type currentUserHistoryRow struct {
	Action      string `json:"action"`
	Who         string `json:"who"`
	AmountCents int64  `json:"amountCents"`
	Expense     string `json:"expense"`
}

type openExpensePaymentRow struct {
	ExpenseID    uuid.UUID `json:"expenseId"`
	Expense      string    `json:"expense"`
	Who          string    `json:"who"`
	AmountCents  int64     `json:"amountCents"`
	OwnerUserID  uuid.UUID `json:"ownerUserId"`
	CanDelete    bool      `json:"canDelete"`
}

type settleUpPayeeRow struct {
	UserID      uuid.UUID `json:"userId"`
	Who         string    `json:"who"`
	AmountCents int64     `json:"amountCents"`
}

type settleUpExpenseRow struct {
	ExpenseID   uuid.UUID `json:"expenseId"`
	ToUserID    uuid.UUID `json:"toUserId"`
	ToName      string    `json:"toName"`
	Expense     string    `json:"expense"`
	AmountCents int64     `json:"amountCents"`
}

type simplifiedSettleToRow struct {
	FromUserID  uuid.UUID `json:"fromUserId"`
	FromName    string    `json:"fromName"`
	ToUserID    uuid.UUID `json:"toUserId"`
	ToName      string    `json:"toName"`
	AmountCents int64     `json:"amountCents"`
}

type groupBalances struct {
	Members             []app.MemberBalance      `json:"members"`
	Transfers           []app.TransferSuggestion `json:"transfers"`
	SimplifiedSettleTo  []simplifiedSettleToRow  `json:"simplifiedSettleTo"`
	Payments            []currentUserBalanceRow  `json:"payments"`
	Settlements         []currentUserBalanceRow  `json:"settlements"`
	History             []currentUserHistoryRow  `json:"history"`
	OpenExpensePayments []openExpensePaymentRow  `json:"openExpensePayments"`
	SettleUpPayees      []settleUpPayeeRow       `json:"settleUpPayees"`
	SettleUpExpenses    []settleUpExpenseRow     `json:"settleUpExpenses"`
}

type groupMessageEntry struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"userId"`
	UserName  string    `json:"userName"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	CanDelete bool      `json:"canDelete"`
}

type groupExpenseSummary struct {
	ActiveCount int64 `json:"activeCount"`
	ClosedCount int64 `json:"closedCount"`
}

type groupDetailOutput struct {
	Body struct {
		CurrentUserID  uuid.UUID           `json:"currentUserId"`
		Group          dashboardGroup      `json:"group"`
		ExpenseSummary groupExpenseSummary `json:"expenseSummary"`
		Members        []groupMember       `json:"members"`
		Invitations    []groupInvitation   `json:"invitations"`
		Messages       []groupMessageEntry `json:"messages"`
		Expenses       []groupExpense      `json:"expenses"`
		Settlements    []groupSettlement   `json:"settlements"`
		Balances       groupBalances       `json:"balances"`
	} `json:"body"`
}

type createGroupMessageInput struct {
	GroupID uuid.UUID `path:"groupID"`
	Body    struct {
		Body string `json:"body" minLength:"1" maxLength:"2000"`
	}
}

type groupMessagesOutput struct {
	Body []groupMessageEntry `json:"body"`
}

type createGroupMessageOutput struct {
	Body groupMessageEntry `json:"body"`
}

type pathGroupMessageInput struct {
	GroupID   uuid.UUID `path:"groupID"`
	MessageID uuid.UUID `path:"messageID"`
}

type deleteGroupMessageOutput struct{}

type expenseSplitInput struct {
	UserID      uuid.UUID `json:"userId"`
	AmountCents int64     `json:"amountCents" minimum:"1"`
}

type createExpenseInput struct {
	GroupID uuid.UUID `path:"groupID"`
	Body    struct {
		Description        string      `json:"description" minLength:"1" maxLength:"240"`
		Category           string      `json:"category"`
		ExpenseType        string      `json:"expenseType,omitempty"`
		DueDayOfMonth      int         `json:"dueDayOfMonth,omitempty"`
		AmountCents        int64       `json:"amountCents" minimum:"1"`
		Currency           string      `json:"currency"`
		SplitMode          string      `json:"splitMode"`
		PaidByUserID       uuid.UUID   `json:"paidByUserId"`
		OwnerUserID        uuid.UUID   `json:"ownerUserId"`
		ParticipantUserIDs []uuid.UUID `json:"participantUserIds,omitempty"`
		Splits             []expenseSplitInput `json:"splits,omitempty"`
		IncurredOn         string      `json:"incurredOn"`
	}
	BrowserTimeZone string `header:"X-Time-Zone"`
}

type createExpenseOutput struct {
	Body groupExpense `json:"body"`
}

type createSettlementBody struct {
	Kind        string    `json:"kind,omitempty"`
	FromUserID  uuid.UUID `json:"fromUserId"`
	ToUserID    uuid.UUID `json:"toUserId"`
	ExpenseID   *uuid.UUID `json:"expenseId,omitempty"`
	AmountCents int64     `json:"amountCents" minimum:"1"`
	Currency    string    `json:"currency"`
	SettledOn   string    `json:"settledOn"`
}

type createSettlementInput struct {
	GroupID uuid.UUID          `path:"groupID"`
	Body    createSettlementBody
}

type createSettlementOutput struct {
	Body groupSettlement `json:"body"`
}

type archiveExpenseOutput struct {
	Body groupExpense `json:"body"`
}

type pathExpenseInput struct {
	GroupID   uuid.UUID `path:"groupID"`
	ExpenseID uuid.UUID `path:"expenseID"`
}

type deleteExpenseOutput struct{}

type expenseReportGroupSummary struct {
	Name               string    `json:"name"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"createdAt"`
	BaseCurrency       string    `json:"baseCurrency"`
	ActiveExpenseCount int64     `json:"activeExpenseCount"`
	ClosedExpenseCount int64     `json:"closedExpenseCount"`
	GroupExpenditureCents int64  `json:"groupExpenditureCents"`
}

type expenseReportExpenseRow struct {
	Name        string    `json:"name"`
	Category    string    `json:"category"`
	ExpenseType string    `json:"expenseType"`
	PayByDate   string    `json:"payByDate"`
	Status      string    `json:"status"`
	AmountCents int64     `json:"amountCents"`
	CreatedAt   time.Time `json:"createdAt"`
	Owner       string    `json:"owner"`
	PaidBy      string    `json:"paidBy"`
	SplitWith   []string  `json:"splitWith"`
}

type expenseReportDocument struct {
	Group    expenseReportGroupSummary `json:"group"`
	Expenses []expenseReportExpenseRow `json:"expenses"`
}

func (s *Server) registerRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "register",
		Method:      http.MethodPost,
		Path:        "/v1/auth/register",
		Summary:     "Register a new user",
	}, s.registerUser)

	huma.Register(api, huma.Operation{
		OperationID: "login",
		Method:      http.MethodPost,
		Path:        "/v1/auth/login",
		Summary:     "Log in a user",
	}, s.loginUser)

	huma.Register(api, huma.Operation{
		OperationID: "logout",
		Method:      http.MethodPost,
		Path:        "/v1/auth/logout",
		Summary:     "Log out a user",
	}, s.logoutUser)

	huma.Register(api, huma.Operation{
		OperationID: "me",
		Method:      http.MethodGet,
		Path:        "/v1/auth/me",
		Summary:     "Return the current session user",
	}, s.getMe)

	huma.Register(api, huma.Operation{
		OperationID: "dashboard",
		Method:      http.MethodGet,
		Path:        "/v1/dashboard",
		Summary:     "Return dashboard data for the current user",
	}, s.getDashboard)

	huma.Register(api, huma.Operation{
		OperationID: "createGroup",
		Method:      http.MethodPost,
		Path:        "/v1/groups",
		Summary:     "Create a new expense group",
	}, s.createGroup)

	huma.Register(api, huma.Operation{
		OperationID: "deleteGroup",
		Method:      http.MethodDelete,
		Path:        "/v1/groups/{groupID}",
		Summary:     "Delete an empty expense group",
	}, s.deleteGroup)

	huma.Register(api, huma.Operation{
		OperationID: "archiveGroup",
		Method:      http.MethodPost,
		Path:        "/v1/groups/{groupID}/archive",
		Summary:     "Archive a fully settled expense group",
	}, s.archiveGroup)

	huma.Register(api, huma.Operation{
		OperationID: "unarchiveGroup",
		Method:      http.MethodPost,
		Path:        "/v1/groups/{groupID}/unarchive",
		Summary:     "Unarchive an archived expense group",
	}, s.unarchiveGroup)

	huma.Register(api, huma.Operation{
		OperationID: "getGroup",
		Method:      http.MethodGet,
		Path:        "/v1/groups/{groupID}",
		Summary:     "Return group details",
	}, s.getGroup)

	huma.Register(api, huma.Operation{
		OperationID: "inviteToGroup",
		Method:      http.MethodPost,
		Path:        "/v1/groups/{groupID}/invitations",
		Summary:     "Invite a user to a group by email",
	}, s.createInvitation)

	huma.Register(api, huma.Operation{
		OperationID: "listGroupMessages",
		Method:      http.MethodGet,
		Path:        "/v1/groups/{groupID}/messages",
		Summary:     "List messages for a group",
	}, s.listGroupMessages)

	huma.Register(api, huma.Operation{
		OperationID: "createGroupMessage",
		Method:      http.MethodPost,
		Path:        "/v1/groups/{groupID}/messages",
		Summary:     "Post a message to a group message board",
	}, s.createGroupMessage)

	huma.Register(api, huma.Operation{
		OperationID: "deleteGroupMessage",
		Method:      http.MethodDelete,
		Path:        "/v1/groups/{groupID}/messages/{messageID}",
		Summary:     "Delete a message from a group message board",
	}, s.deleteGroupMessage)

	huma.Register(api, huma.Operation{
		OperationID: "acceptInvitation",
		Method:      http.MethodPost,
		Path:        "/v1/invitations/{invitationID}/accept",
		Summary:     "Accept a group invitation",
	}, s.acceptInvitation)

	huma.Register(api, huma.Operation{
		OperationID: "createExpense",
		Method:      http.MethodPost,
		Path:        "/v1/groups/{groupID}/expenses",
		Summary:     "Add a shared expense to a group",
	}, s.createExpense)

	huma.Register(api, huma.Operation{
		OperationID: "createSettlement",
		Method:      http.MethodPost,
		Path:        "/v1/groups/{groupID}/settlements",
		Summary:     "Record a settlement payment in a group",
	}, s.createSettlement)

	huma.Register(api, huma.Operation{
		OperationID: "deleteExpense",
		Method:      http.MethodDelete,
		Path:        "/v1/groups/{groupID}/expenses/{expenseID}",
		Summary:     "Delete a shared expense from a group",
	}, s.deleteExpense)

	huma.Register(api, huma.Operation{
		OperationID: "archiveExpense",
		Method:      http.MethodPost,
		Path:        "/v1/groups/{groupID}/expenses/{expenseID}/archive",
		Summary:     "Archive a settled expense",
	}, s.archiveExpense)
}

func (s *Server) downloadExpenseReport(w http.ResponseWriter, r *http.Request) {
	user, err := requireUser(r.Context())
	if err != nil {
		writeHTTPAPIError(w, err)
		return
	}

	groupID, err := uuid.Parse(chi.URLParam(r, "groupID"))
	if err != nil {
		http.Error(w, "invalid group id", http.StatusBadRequest)
		return
	}

	group, err := s.requireGroupOwner(user.ID, groupID)
	if err != nil {
		writeHTTPAPIError(w, err)
		return
	}
	if err := s.ensureRecurringExpensesForGroup(s.db, group.ID, r.Header.Get("X-Time-Zone")); err != nil {
		http.Error(w, "unable to materialize recurring expenses", http.StatusInternalServerError)
		return
	}

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format != "csv" && format != "json" {
		http.Error(w, "format must be csv or json", http.StatusBadRequest)
		return
	}

	expenses, _, err := s.loadGroupExpenses(group.ID, user.ID)
	if err != nil {
		http.Error(w, "unable to load expenses", http.StatusInternalServerError)
		return
	}

	settlements, _, err := s.loadGroupSettlements(group.ID)
	if err != nil {
		http.Error(w, "unable to load settlements", http.StatusInternalServerError)
		return
	}

	report := buildExpenseReportDocument(group, expenses, settlements)
	filenameBase := sanitizeReportFilename(group.Name)
	filenameTimestamp := time.Now().UTC()

	switch format {
	case "csv":
		reportCSV, err := marshalExpenseReportCSV(report)
		if err != nil {
			http.Error(w, "unable to build csv report", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set(
			"Content-Disposition",
			fmt.Sprintf(
				"attachment; filename=\"%s\"",
				expenseReportDownloadFilename(filenameBase, "csv", filenameTimestamp),
			),
		)
		_, _ = w.Write(reportCSV)
	case "json":
		reportJSON, err := marshalExpenseReportJSON(report)
		if err != nil {
			http.Error(w, "unable to build json report", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set(
			"Content-Disposition",
			fmt.Sprintf(
				"attachment; filename=\"%s\"",
				expenseReportDownloadFilename(filenameBase, "json", filenameTimestamp),
			),
		)
		_, _ = w.Write(reportJSON)
	}
}

func (s *Server) registerUser(ctx context.Context, input *registerInput) (*authOutput, error) {
	email := normalizeEmail(input.Body.Email)
	if email == "" {
		return nil, huma.Error400BadRequest("email is required")
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.Body.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to secure password")
	}

	user := models.User{
		Name:         strings.TrimSpace(input.Body.Name),
		Email:        email,
		PasswordHash: string(passwordHash),
	}

	if user.Name == "" {
		return nil, huma.Error400BadRequest("name is required")
	}

	if err := s.db.Create(&user).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, huma.Error409Conflict("an account with that email already exists")
		}
		return nil, huma.Error500InternalServerError("unable to create user")
	}

	session, rawToken, err := s.createSession(user.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to create session")
	}

	return &authOutput{
		SetCookie: s.buildSessionCookie(rawToken, session.ExpiresAt),
		Body: authEnvelope{
			User: toUserResponse(user),
		},
	}, nil
}

func (s *Server) loginUser(ctx context.Context, input *loginInput) (*authOutput, error) {
	email := normalizeEmail(input.Body.Email)
	var user models.User
	if err := s.db.Where("email = ?", email).First(&user).Error; err != nil {
		return nil, huma.Error401Unauthorized("invalid email or password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Body.Password)); err != nil {
		return nil, huma.Error401Unauthorized("invalid email or password")
	}

	session, rawToken, err := s.createSession(user.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to create session")
	}

	return &authOutput{
		SetCookie: s.buildSessionCookie(rawToken, session.ExpiresAt),
		Body: authEnvelope{
			User: toUserResponse(user),
		},
	}, nil
}

func (s *Server) logoutUser(ctx context.Context, _ *struct{}) (*logoutOutput, error) {
	session := currentSession(ctx)
	if session != nil {
		_ = s.db.Delete(&models.Session{}, "id = ?", session.ID).Error
	}

	return &logoutOutput{
		SetCookie: s.expiredSessionCookie(),
	}, nil
}

func (s *Server) getMe(ctx context.Context, _ *struct{}) (*meOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	return &meOutput{
		Body: authEnvelope{
			User: toUserResponse(*user),
		},
	}, nil
}

func (s *Server) getDashboard(ctx context.Context, input *dashboardInput) (*dashboardOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	groups, err := s.loadDashboardGroups(user.ID, input.BrowserTimeZone)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load groups")
	}

	invitations, err := s.loadDashboardInvitations(user.Email)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load invitations")
	}

	output := &dashboardOutput{}
	output.Body.User = toUserResponse(*user)
	output.Body.Groups = groups
	output.Body.Invitations = invitations
	return output, nil
}

func (s *Server) createGroup(ctx context.Context, input *createGroupInput) (*createGroupOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(input.Body.Name)
	if name == "" {
		return nil, huma.Error400BadRequest("group name is required")
	}

	group := models.Group{
		Name:            name,
		BaseCurrency:    normalizedGroupCurrency(input.Body.BaseCurrency),
		CreatedByUserID: user.ID,
	}
	if !isValidCurrencyCode(group.BaseCurrency) {
		return nil, huma.Error400BadRequest("baseCurrency must be a supported 3-letter currency code")
	}
	membership := models.Membership{
		GroupID: group.ID,
		UserID:  user.ID,
		Role:    "owner",
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&group).Error; err != nil {
			return err
		}
		membership.GroupID = group.ID
		return tx.Create(&membership).Error
	}); err != nil {
		return nil, huma.Error500InternalServerError("unable to create group")
	}

	memberCount, expenseCount, err := s.groupCounts(group.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load group summary")
	}
	lifecycle, err := s.groupLifecycleStatus(group.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load group lifecycle")
	}

	return &createGroupOutput{
		Body: dashboardGroupFromState(group, "owner", memberCount, expenseCount, lifecycle),
	}, nil
}

func (s *Server) deleteGroup(ctx context.Context, input *pathGroupInput) (*deleteGroupOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	group, err := s.requireGroupOwner(user.ID, input.GroupID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureRecurringExpensesForGroup(s.db, group.ID, input.BrowserTimeZone); err != nil {
		return nil, huma.Error500InternalServerError("unable to materialize recurring expenses")
	}

	lifecycle, err := s.groupLifecycleStatus(group.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load group lifecycle")
	}
	if lifecycle.TotalExpenses > 0 {
		return nil, huma.Error409Conflict("group can only be deleted when it has no expenses at all")
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&models.Invitation{}, "group_id = ?", group.ID).Error; err != nil {
			return err
		}
		if err := tx.Delete(&models.Membership{}, "group_id = ?", group.ID).Error; err != nil {
			return err
		}
		return tx.Delete(&models.Group{}, "id = ?", group.ID).Error
	}); err != nil {
		return nil, huma.Error500InternalServerError("unable to delete group")
	}

	return &deleteGroupOutput{}, nil
}

func (s *Server) archiveGroup(ctx context.Context, input *pathGroupInput) (*groupLifecycleOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	group, err := s.requireGroupOwner(user.ID, input.GroupID)
	if err != nil {
		return nil, err
	}
	if group.ArchivedAt != nil {
		return nil, huma.Error409Conflict("group is already archived")
	}
	if err := s.ensureRecurringExpensesForGroup(s.db, group.ID, input.BrowserTimeZone); err != nil {
		return nil, huma.Error500InternalServerError("unable to materialize recurring expenses")
	}

	lifecycle, err := s.groupLifecycleStatus(group.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load group lifecycle")
	}
	if lifecycle.TotalExpenses == 0 {
		return nil, huma.Error409Conflict("group without expenses can be deleted instead of archived")
	}
	if lifecycle.OpenExpenses > 0 {
		return nil, huma.Error409Conflict("group can only be archived after all open expenses are closed")
	}

	now := time.Now().UTC()
	if err := s.db.Model(&models.Group{}).
		Where("id = ?", group.ID).
		Updates(map[string]any{
			"archived_at":        now,
			"archived_by_user_id": user.ID,
			"updated_at":         now,
		}).Error; err != nil {
		return nil, huma.Error500InternalServerError("unable to archive group")
	}

	group.ArchivedAt = &now
	group.ArchivedByUserID = &user.ID
	group.UpdatedAt = now
	memberCount, expenseCount, err := s.groupCounts(group.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load group summary")
	}

	return &groupLifecycleOutput{
		Body: dashboardGroupFromState(group, "owner", memberCount, expenseCount, lifecycle),
	}, nil
}

func (s *Server) unarchiveGroup(ctx context.Context, input *pathGroupInput) (*groupLifecycleOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	group, err := s.requireGroupOwner(user.ID, input.GroupID)
	if err != nil {
		return nil, err
	}
	if group.ArchivedAt == nil {
		return nil, huma.Error409Conflict("group is not archived")
	}

	now := time.Now().UTC()
	if err := s.db.Model(&models.Group{}).
		Where("id = ?", group.ID).
		Updates(map[string]any{
			"archived_at":         nil,
			"archived_by_user_id": nil,
			"updated_at":          now,
		}).Error; err != nil {
		return nil, huma.Error500InternalServerError("unable to unarchive group")
	}

	group.ArchivedAt = nil
	group.ArchivedByUserID = nil
	group.UpdatedAt = now
	memberCount, expenseCount, err := s.groupCounts(group.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load group summary")
	}
	lifecycle, err := s.groupLifecycleStatus(group.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load group lifecycle")
	}

	return &groupLifecycleOutput{
		Body: dashboardGroupFromState(group, "owner", memberCount, expenseCount, lifecycle),
	}, nil
}

func (s *Server) getGroup(ctx context.Context, input *pathGroupInput) (*groupDetailOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	group, err := s.requireGroupMembership(user.ID, input.GroupID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureRecurringExpensesForGroup(s.db, group.ID, input.BrowserTimeZone); err != nil {
		return nil, huma.Error500InternalServerError("unable to materialize recurring expenses")
	}

	members, memberSnapshots, err := s.loadGroupMembers(group.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load members")
	}

	invitations, err := s.loadGroupInvitations(group.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load invitations")
	}

	messages, err := s.loadGroupMessages(group.ID, user.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load messages")
	}

	expenses, expensePayload, err := s.loadGroupExpenses(group.ID, user.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load expenses")
	}

	settlements, settlementPayload, err := s.loadGroupSettlements(group.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load settlements")
	}

	openObligations, err := s.loadOpenExpenseSplitObligations(group.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load group obligations")
	}

	memberBalances, transfers := calculateBalancesFromOpenObligations(memberSnapshots, openObligations)
	paymentRows, settlementRows, historyRows, openExpenseRows, settleUpPayees, settleUpExpenses := buildCurrentUserBalanceRows(
		user.ID,
		expenses,
		settlements,
		openObligations,
	)
	memberCount, expenseCount, err := s.groupCounts(group.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load group summary")
	}
	lifecycle, err := s.groupLifecycleStatus(group.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load group lifecycle")
	}

	output := &groupDetailOutput{}
	output.Body.CurrentUserID = user.ID
	groupRole := "member"
	if group.CreatedByUserID == user.ID {
		groupRole = "owner"
	}
	output.Body.Group = dashboardGroupFromState(group, groupRole, memberCount, expenseCount, lifecycle)
	output.Body.ExpenseSummary = groupExpenseSummary{
		ActiveCount: lifecycle.OpenExpenses,
		ClosedCount: lifecycle.TotalExpenses - lifecycle.OpenExpenses,
	}
	output.Body.Members = members
	output.Body.Invitations = invitations
	output.Body.Messages = messages
	output.Body.Expenses = expensePayload
	output.Body.Settlements = settlementPayload
	output.Body.Balances = groupBalances{
		Members:             memberBalances,
		Transfers:           transfers,
		SimplifiedSettleTo:  simplifiedSettleToRows(user.ID, memberSnapshots, openObligations),
		Payments:            paymentRows,
		Settlements:         settlementRows,
		History:             historyRows,
		OpenExpensePayments: openExpenseRows,
		SettleUpPayees:      settleUpPayees,
		SettleUpExpenses:    settleUpExpenses,
	}

	return output, nil
}

func (s *Server) listGroupMessages(ctx context.Context, input *pathGroupInput) (*groupMessagesOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	group, err := s.requireGroupMembership(user.ID, input.GroupID)
	if err != nil {
		return nil, err
	}
	if group.ArchivedAt != nil {
		return nil, huma.Error409Conflict("archived groups do not support group chat")
	}

	messages, err := s.loadGroupMessages(group.ID, user.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load messages")
	}

	return &groupMessagesOutput{Body: messages}, nil
}

func (s *Server) createGroupMessage(ctx context.Context, input *createGroupMessageInput) (*createGroupMessageOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	group, err := s.requireGroupMembership(user.ID, input.GroupID)
	if err != nil {
		return nil, err
	}
	if group.ArchivedAt != nil {
		return nil, huma.Error409Conflict("archived groups do not support group chat")
	}

	body := strings.TrimSpace(input.Body.Body)
	if body == "" {
		return nil, huma.Error400BadRequest("message body is required")
	}

	now := time.Now().UTC()
	message := models.GroupMessage{
		GroupID:   group.ID,
		UserID:    user.ID,
		Body:      body,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&message).Error; err != nil {
			return err
		}
		return tx.Model(&models.Group{}).
			Where("id = ?", group.ID).
			Updates(map[string]any{"updated_at": now}).Error
	}); err != nil {
		return nil, huma.Error500InternalServerError("unable to create message")
	}

	return &createGroupMessageOutput{
		Body: groupMessageEntry{
			ID:        message.ID,
			UserID:    user.ID,
			UserName:  user.Name,
			Body:      message.Body,
			CreatedAt: message.CreatedAt,
			CanDelete: true,
		},
	}, nil
}

func (s *Server) deleteGroupMessage(ctx context.Context, input *pathGroupMessageInput) (*deleteGroupMessageOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	group, err := s.requireGroupMembership(user.ID, input.GroupID)
	if err != nil {
		return nil, err
	}
	if group.ArchivedAt != nil {
		return nil, huma.Error409Conflict("archived groups do not support group chat")
	}

	var message models.GroupMessage
	if err := s.db.Where("id = ? AND group_id = ?", input.MessageID, group.ID).First(&message).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, huma.Error404NotFound("message not found")
		}
		return nil, huma.Error500InternalServerError("unable to load message")
	}
	if message.UserID != user.ID {
		return nil, huma.Error403Forbidden("you can only delete your own messages")
	}

	now := time.Now().UTC()
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&models.GroupMessage{}, "id = ?", message.ID).Error; err != nil {
			return err
		}
		return tx.Model(&models.Group{}).
			Where("id = ?", group.ID).
			Updates(map[string]any{"updated_at": now}).Error
	}); err != nil {
		return nil, huma.Error500InternalServerError("unable to delete message")
	}

	return &deleteGroupMessageOutput{}, nil
}

func (s *Server) createInvitation(ctx context.Context, input *inviteInput) (*invitationOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	group, err := s.requireGroupMembership(user.ID, input.GroupID)
	if err != nil {
		return nil, err
	}

	email := normalizeEmail(input.Body.Email)
	if email == "" {
		return nil, huma.Error400BadRequest("email is required")
	}

	var existingMemberCount int64
	if err := s.db.Model(&models.Membership{}).
		Joins("JOIN users ON users.id = memberships.user_id").
		Where("memberships.group_id = ? AND users.email = ?", group.ID, email).
		Count(&existingMemberCount).Error; err != nil {
		return nil, huma.Error500InternalServerError("unable to verify membership")
	}
	if existingMemberCount > 0 {
		return nil, huma.Error409Conflict("that user is already a member of this group")
	}

	var existingInvite models.Invitation
	if err := s.db.Where("group_id = ? AND email = ? AND status = ?", group.ID, email, "pending").First(&existingInvite).Error; err == nil {
		return nil, huma.Error409Conflict("that email already has a pending invitation")
	} else if err != nil && err != gorm.ErrRecordNotFound {
		return nil, huma.Error500InternalServerError("unable to verify invitations")
	}

	invitation := models.Invitation{
		GroupID:         group.ID,
		InvitedByUserID: user.ID,
		Email:           email,
		Token:           randomToken(24),
		Status:          "pending",
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&invitation).Error; err != nil {
			return err
		}
		return tx.Model(&models.Group{}).
			Where("id = ?", group.ID).
			Update("updated_at", time.Now().UTC()).
			Error
	}); err != nil {
		return nil, huma.Error500InternalServerError("unable to create invitation")
	}

	output := &invitationOutput{}
	output.Body.ID = invitation.ID
	output.Body.Email = invitation.Email
	output.Body.Status = invitation.Status
	output.Body.CreatedAt = invitation.CreatedAt
	return output, nil
}

func (s *Server) acceptInvitation(ctx context.Context, input *acceptInvitationInput) (*groupDetailOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	var invitation models.Invitation
	if err := s.db.Where("id = ?", input.InvitationID).First(&invitation).Error; err != nil {
		return nil, huma.Error404NotFound("invitation not found")
	}

	if invitation.Status != "pending" {
		return nil, huma.Error409Conflict("invitation is no longer pending")
	}

	if invitation.Email != normalizeEmail(user.Email) {
		return nil, huma.Error403Forbidden("you cannot accept this invitation")
	}

	now := time.Now().UTC()
	acceptedBy := user.ID
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var membership models.Membership
		if err := tx.Where("group_id = ? AND user_id = ?", invitation.GroupID, user.ID).First(&membership).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				membership = models.Membership{
					GroupID: invitation.GroupID,
					UserID:  user.ID,
					Role:    "member",
				}
				if err := tx.Create(&membership).Error; err != nil {
					return err
				}
			} else {
				return err
			}
		}

		if err := tx.Model(&invitation).Updates(map[string]any{
			"status":              "accepted",
			"accepted_by_user_id": &acceptedBy,
			"accepted_at":         &now,
			"updated_at":          now,
		}).Error; err != nil {
			return err
		}

		return tx.Model(&models.Group{}).
			Where("id = ?", invitation.GroupID).
			Update("updated_at", now).
			Error
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to accept invitation")
	}

	return s.getGroup(ctx, &pathGroupInput{GroupID: invitation.GroupID})
}

func (s *Server) createExpense(ctx context.Context, input *createExpenseInput) (*createExpenseOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	group, err := s.requireGroupMembership(user.ID, input.GroupID)
	if err != nil {
		return nil, err
	}
	if group.ArchivedAt != nil {
		return nil, huma.Error409Conflict("archived groups cannot accept new expenses")
	}

	description := strings.TrimSpace(input.Body.Description)
	if description == "" {
		return nil, huma.Error400BadRequest("description is required")
	}
	category := normalizeExpenseCategory(input.Body.Category)
	if !isValidExpenseCategory(category) {
		return nil, huma.Error400BadRequest("category must be one of food, transport, accommodation, entertainment, rent, or subscription")
	}
	expenseType := normalizeExpenseType(input.Body.ExpenseType)
	if !isValidExpenseType(expenseType) {
		return nil, huma.Error400BadRequest("expenseType must be one-time or monthly")
	}
	if input.Body.AmountCents <= 0 {
		return nil, huma.Error400BadRequest("amount must be greater than zero")
	}
	currencyCode := normalizeCurrencyCode(input.Body.Currency)
	if !isValidCurrencyCode(currencyCode) {
		return nil, huma.Error400BadRequest("currency must be a supported 3-letter currency code")
	}
	splitMode := normalizeSplitMode(input.Body.SplitMode)
	if !isValidSplitMode(splitMode) {
		return nil, huma.Error400BadRequest("splitMode must be equal or custom")
	}

	var (
		incurredOn      time.Time
		dueDayOfMonth   int
		browserLocation *time.Location
		browserTimeZone string
	)
	if expenseType == models.ExpenseTypeOneTime {
		incurredOn, err = time.Parse("2006-01-02", input.Body.IncurredOn)
		if err != nil {
			return nil, huma.Error400BadRequest("incurredOn must be in YYYY-MM-DD format")
		}
	} else {
		if input.Body.DueDayOfMonth < 1 || input.Body.DueDayOfMonth > 31 {
			return nil, huma.Error400BadRequest("dueDayOfMonth must be between 1 and 31")
		}
		dueDayOfMonth = input.Body.DueDayOfMonth
		browserLocation, browserTimeZone, err = resolveBrowserLocation(input.BrowserTimeZone)
		if err != nil {
			return nil, huma.Error400BadRequest("browser time zone is invalid")
		}
	}

	memberMap, err := s.groupMemberMap(group.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load group membership")
	}

	if _, ok := memberMap[input.Body.OwnerUserID]; !ok {
		return nil, huma.Error400BadRequest("owner must be a group member")
	}
	paidByUserID := input.Body.PaidByUserID
	if expenseType == models.ExpenseTypeMonthly {
		paidByUserID = input.Body.OwnerUserID
	}
	if _, ok := memberMap[paidByUserID]; !ok {
		return nil, huma.Error400BadRequest("payer must be a group member")
	}

	ownerUserID := input.Body.OwnerUserID
	participantIDs, originalSplitAmounts, err := s.resolveExpenseSplits(memberMap, input.Body.AmountCents, splitMode, input.Body.ParticipantUserIDs, input.Body.Splits)
	if err != nil {
		return nil, err
	}

	groupBaseCurrency := effectiveGroupBaseCurrency(group)
	quote := app.FXQuote{
		BaseAmountCents: input.Body.AmountCents,
		Rate:            1,
		Source:          "group-base",
		FetchedAt:       time.Now().UTC(),
	}
	if currencyCode != groupBaseCurrency {
		quote, err = s.fx.Convert(ctx, input.Body.AmountCents, currencyCode, groupBaseCurrency)
		if err != nil {
			return nil, huma.Error500InternalServerError("unable to convert expense into the group base currency")
		}
	}

	splitAmounts := equalSplits(quote.BaseAmountCents, participantIDs)
	if splitMode == "custom" {
		splitAmounts = convertCustomSplitAmounts(originalSplitAmounts, input.Body.AmountCents, quote.BaseAmountCents)
	}

	expense := models.Expense{}
	expenseSplits := make([]models.ExpenseSplit, 0, len(participantIDs))

	err = s.db.Transaction(func(tx *gorm.DB) error {
		if expenseType == models.ExpenseTypeMonthly {
			startMonth, err := recurringTemplateStartMonth(time.Now().UTC(), dueDayOfMonth, browserLocation)
			if err != nil {
				return err
			}
			template := models.RecurringExpenseTemplate{
				GroupID:             group.ID,
				Description:         description,
				Category:            category,
				AmountCents:         quote.BaseAmountCents,
				OriginalAmountCents: input.Body.AmountCents,
				OriginalCurrency:    currencyCode,
				FXRate:              quote.Rate,
				FXSource:            quote.Source,
				FXFetchedAt:         quote.FetchedAt,
				SplitMode:           splitMode,
				OwnerUserID:         ownerUserID,
				DueDayOfMonth:       dueDayOfMonth,
				TimeZone:            browserTimeZone,
				StartMonth:          startMonth,
				CreatedByUserID:     user.ID,
			}
			if err := tx.Create(&template).Error; err != nil {
				return err
			}

			templateSplits := make([]models.RecurringExpenseSplit, 0, len(participantIDs))
			for index, participantID := range participantIDs {
				templateSplits = append(templateSplits, models.RecurringExpenseSplit{
					RecurringTemplateID: template.ID,
					UserID:              participantID,
					AmountCents:         splitAmounts[index],
				})
			}
			if len(templateSplits) > 0 {
				if err := tx.Create(&templateSplits).Error; err != nil {
					return err
				}
			}

			expense, expenseSplits, err = createRecurringExpenseOccurrence(tx, template, templateSplits, startMonth, browserLocation)
			if err != nil {
				return err
			}
		} else {
			expense = models.Expense{
				GroupID:             group.ID,
				Description:         description,
				Category:            category,
				ExpenseType:         models.ExpenseTypeOneTime,
				AmountCents:         quote.BaseAmountCents,
				OriginalAmountCents: input.Body.AmountCents,
				OriginalCurrency:    currencyCode,
				FXRate:              quote.Rate,
				FXSource:            quote.Source,
				FXFetchedAt:         quote.FetchedAt,
				SplitMode:           splitMode,
				PaidByUserID:        paidByUserID,
				OwnerUserID:         &ownerUserID,
				CreatedByUserID:     user.ID,
				IncurredOn:          incurredOn.UTC(),
			}
			if err := tx.Create(&expense).Error; err != nil {
				return err
			}

			for index, participantID := range participantIDs {
				expenseSplits = append(expenseSplits, models.ExpenseSplit{
					ExpenseID:   expense.ID,
					UserID:      participantID,
					AmountCents: splitAmounts[index],
				})
			}
			if err := tx.Create(&expenseSplits).Error; err != nil {
				return err
			}
			if err := createExpenseSplitObligations(tx, expense, expenseSplits); err != nil {
				return err
			}
		}

		return tx.Model(&models.Group{}).
			Where("id = ?", group.ID).
			Update("updated_at", time.Now().UTC()).
			Error
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to create expense")
	}

	participantNames := make([]string, 0, len(participantIDs))
	splitPayload := make([]groupExpenseSplit, 0, len(participantIDs))
	for _, participantID := range participantIDs {
		participantNames = append(participantNames, memberMap[participantID].Name)
	}
	for index, participantID := range participantIDs {
		splitPayload = append(splitPayload, groupExpenseSplit{
			UserID:      participantID,
			UserName:    memberMap[participantID].Name,
			AmountCents: expenseSplits[index].AmountCents,
		})
	}

	return &createExpenseOutput{
		Body: groupExpense{
			ID:                 expense.ID,
			Description:        expense.Description,
			Category:           expense.Category,
			ExpenseType:        expenseTypeLabel(expense.ExpenseType),
			AmountCents:        expense.AmountCents,
			BaseCurrency:       groupBaseCurrency,
			OriginalAmountCents: expense.OriginalAmountCents,
			OriginalCurrency:   expense.OriginalCurrency,
			FXRate:             expense.FXRate,
			SplitMode:          expense.SplitMode,
			PaidByUserID:       expense.PaidByUserID,
			PaidByName:         memberMap[expense.PaidByUserID].Name,
			OwnerUserID:        ownerUserID,
			OwnerName:          memberMap[ownerUserID].Name,
			ParticipantUserIDs: participantIDs,
			ParticipantNames:   participantNames,
			Splits:             splitPayload,
			IncurredOn:         expense.IncurredOn.Format("2006-01-02"),
			OutstandingAmountCents: outstandingExpenseAmount(expense.PaidByUserID, participantIDs, extractExpenseSplitAmounts(expenseSplits)),
			CanDelete: ownerUserID == user.ID && expenseDeleteAllowed(
				expense,
				outstandingExpenseAmount(expense.PaidByUserID, participantIDs, extractExpenseSplitAmounts(expenseSplits)),
				outstandingExpenseAmount(expense.PaidByUserID, participantIDs, extractExpenseSplitAmounts(expenseSplits)),
			),
			CanArchive:         false,
			CreatedAt:          expense.CreatedAt,
		},
	}, nil
}

func (s *Server) createSettlement(ctx context.Context, input *createSettlementInput) (*createSettlementOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	group, err := s.requireGroupMembership(user.ID, input.GroupID)
	if err != nil {
		return nil, err
	}

	if input.Body.AmountCents <= 0 {
		return nil, huma.Error400BadRequest("amount must be greater than zero")
	}
	if input.Body.FromUserID != user.ID {
		return nil, huma.Error403Forbidden("you can only record settlements paid by your own account")
	}
	if input.Body.FromUserID == input.Body.ToUserID {
		return nil, huma.Error400BadRequest("settlement payer and recipient must be different members")
	}
	currencyCode := normalizeCurrencyCode(input.Body.Currency)
	if !isValidCurrencyCode(currencyCode) {
		return nil, huma.Error400BadRequest("currency must be a supported 3-letter currency code")
	}

	settledOn, err := time.Parse("2006-01-02", input.Body.SettledOn)
	if err != nil {
		return nil, huma.Error400BadRequest("settledOn must be in YYYY-MM-DD format")
	}

	memberMap, err := s.groupMemberMap(group.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load group membership")
	}
	if _, ok := memberMap[input.Body.FromUserID]; !ok {
		return nil, huma.Error400BadRequest("settlement payer must be a group member")
	}
	if _, ok := memberMap[input.Body.ToUserID]; !ok {
		return nil, huma.Error400BadRequest("settlement recipient must be a group member")
	}

	groupBaseCurrency := effectiveGroupBaseCurrency(group)
	if currencyCode != groupBaseCurrency {
		return nil, huma.Error400BadRequest("settlement currency must match the group base currency")
	}
	quote := app.FXQuote{
		BaseAmountCents: input.Body.AmountCents,
		Rate:            1,
		Source:          "group-base",
		FetchedAt:       time.Now().UTC(),
	}

	requestedSettlementKind := normalizeSettlementKind(input.Body.Kind)
	if strings.TrimSpace(input.Body.Kind) != "" && requestedSettlementKind == "" {
		return nil, huma.Error400BadRequest("settlement kind must be direct_expense or netted")
	}
	if requestedSettlementKind == models.SettlementKindNetted && input.Body.ExpenseID != nil {
		return nil, huma.Error400BadRequest("netted settlements cannot target a single expense")
	}

	allocationPlan := make([]settlementAllocationPlanRow, 0)
	nettedPathFlows := make([]nettedSettlementPathFlow, 0)
	settlementKind := models.SettlementKindDirectExpense
	if input.Body.ExpenseID != nil {
		expenses, _, err := s.loadGroupExpenses(group.ID, user.ID)
		if err != nil {
			return nil, huma.Error500InternalServerError("unable to load group expenses")
		}

		planRow, err := s.planExpenseSettlementAllocation(
			group.ID,
			input.Body.FromUserID,
			input.Body.ToUserID,
			*input.Body.ExpenseID,
			quote.BaseAmountCents,
			expenses,
		)
		if err != nil {
			return nil, err
		}
		allocationPlan = append(allocationPlan, planRow)
	} else {
		if requestedSettlementKind == models.SettlementKindNetted {
			simplifiedAmount, nettedPlan, pathFlows, nettedErr := s.planNettedSettlementAllocations(
				group.ID,
				input.Body.FromUserID,
				input.Body.ToUserID,
				quote.BaseAmountCents,
			)
			if nettedErr != nil {
				if simplifiedAmount > 0 {
					return nil, nettedErr
				}
				return nil, nettedErr
			}
			allocationPlan = nettedPlan
			nettedPathFlows = pathFlows
			settlementKind = models.SettlementKindNetted
		} else {
			directPlan, directErr := s.planSettlementAllocations(group.ID, input.Body.FromUserID, input.Body.ToUserID, quote.BaseAmountCents)
			if directErr == nil {
				allocationPlan = directPlan
			} else if requestedSettlementKind == models.SettlementKindDirectExpense {
				return nil, directErr
			} else {
				simplifiedAmount, nettedPlan, pathFlows, nettedErr := s.planNettedSettlementAllocations(
					group.ID,
					input.Body.FromUserID,
					input.Body.ToUserID,
					quote.BaseAmountCents,
				)
				if nettedErr != nil {
					if simplifiedAmount > 0 {
						return nil, nettedErr
					}
					return nil, directErr
				}
				allocationPlan = nettedPlan
				nettedPathFlows = pathFlows
				settlementKind = models.SettlementKindNetted
			}
		}
	}

	settlement := models.Settlement{
		GroupID:             group.ID,
		FromUserID:          input.Body.FromUserID,
		ToUserID:            input.Body.ToUserID,
		Kind:                settlementKind,
		AmountCents:         quote.BaseAmountCents,
		OriginalAmountCents: input.Body.AmountCents,
		OriginalCurrency:    currencyCode,
		FXRate:              quote.Rate,
		FXSource:            quote.Source,
		FXFetchedAt:         quote.FetchedAt,
		SettledOn:           settledOn.UTC(),
		CreatedByUserID:     user.ID,
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&settlement).Error; err != nil {
			return err
		}
		if len(allocationPlan) > 0 {
			allocations := settlementAllocationsFromPlan(settlement.ID, allocationPlan)
			applications := make([]models.SettlementApplication, 0, len(allocationPlan))
			for _, planRow := range allocationPlan {
				applications = append(applications, models.SettlementApplication{
					SettlementID: settlement.ID,
					ObligationID: planRow.ObligationID,
					AmountCents:  planRow.AmountCents,
				})
			}
			if len(allocations) > 0 {
				if err := tx.Create(&allocations).Error; err != nil {
					return err
				}
			}
			if settlementKind == models.SettlementKindNetted {
				reimbursementObligations, reimbursementApplications := buildNettedReimbursementLedgerRows(
					settlement.ID,
					settlement.GroupID,
					settlement.FromUserID,
					nettedPathFlows,
				)
				if len(reimbursementObligations) > 0 {
					if err := tx.Create(&reimbursementObligations).Error; err != nil {
						return err
					}
					for index := range reimbursementObligations {
						reimbursementApplications[index].ObligationID = reimbursementObligations[index].ID
					}
				}
				applications = append(applications, reimbursementApplications...)
			}
			if err := tx.Create(&applications).Error; err != nil {
				return err
			}
		}
		return tx.Model(&models.Group{}).
			Where("id = ?", group.ID).
			Update("updated_at", time.Now().UTC()).
			Error
	}); err != nil {
		return nil, huma.Error500InternalServerError("unable to create settlement")
	}

	return &createSettlementOutput{
		Body: groupSettlement{
			ID:          settlement.ID,
			FromUserID:  settlement.FromUserID,
			FromName:    memberMap[settlement.FromUserID].Name,
			ToUserID:    settlement.ToUserID,
			ToName:      memberMap[settlement.ToUserID].Name,
			AmountCents: settlement.AmountCents,
			BaseCurrency: groupBaseCurrency,
			OriginalAmountCents: settlement.OriginalAmountCents,
			OriginalCurrency: settlement.OriginalCurrency,
			FXRate:      settlement.FXRate,
			SettledOn:   settlement.SettledOn.Format("2006-01-02"),
			CreatedAt:   settlement.CreatedAt,
		},
	}, nil
}

func (s *Server) deleteExpense(ctx context.Context, input *pathExpenseInput) (*deleteExpenseOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	group, err := s.requireGroupMembership(user.ID, input.GroupID)
	if err != nil {
		return nil, err
	}

	var expense models.Expense
	if err := s.db.
		Where("id = ? AND group_id = ?", input.ExpenseID, group.ID).
		First(&expense).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, huma.Error404NotFound("expense not found")
		}
		return nil, huma.Error500InternalServerError("unable to load expense")
	}

	if effectiveExpenseOwnerID(expense) != user.ID {
		return nil, huma.Error403Forbidden("only the expense owner can delete this expense")
	}

	if err := s.db.Preload("Splits").First(&expense, "id = ?", expense.ID).Error; err != nil {
		return nil, huma.Error500InternalServerError("unable to load expense details")
	}

	allocationTotals, err := s.loadExpenseAllocationTotals(s.db, []uuid.UUID{expense.ID})
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to verify expense settlements")
	}
	outstandingAmount := expenseOutstandingBalance(expense, allocationTotals[expense.ID])
	if !expenseDeleteAllowed(expense, outstandingAmount, totalExpenseOwedByOthers(expense)) {
		return nil, huma.Error409Conflict("expense can only be deleted after all payments are received")
	}

	now := time.Now().UTC()
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		var obligationIDs []uuid.UUID
		if err := tx.Model(&models.Obligation{}).
			Where("source_type = ? AND source_expense_id = ?", models.ObligationSourceExpenseSplit, expense.ID).
			Pluck("id", &obligationIDs).Error; err != nil {
			return err
		}
		if len(obligationIDs) > 0 {
			if err := tx.Delete(&models.SettlementApplication{}, "obligation_id IN ?", obligationIDs).Error; err != nil {
				return err
			}
			if err := tx.Delete(&models.Obligation{}, "id IN ?", obligationIDs).Error; err != nil {
				return err
			}
		}
		if err := tx.Delete(&models.SettlementAllocation{}, "expense_id = ?", expense.ID).Error; err != nil {
			return err
		}

		if err := tx.Delete(&models.ExpenseSplit{}, "expense_id = ?", expense.ID).Error; err != nil {
			return err
		}

		if err := tx.Delete(&models.Expense{}, "id = ?", expense.ID).Error; err != nil {
			return err
		}

		return tx.Model(&models.Group{}).
			Where("id = ?", group.ID).
			Update("updated_at", now).
			Error
	}); err != nil {
		return nil, huma.Error500InternalServerError("unable to delete expense")
	}

	return &deleteExpenseOutput{}, nil
}

func (s *Server) archiveExpense(ctx context.Context, input *pathExpenseInput) (*archiveExpenseOutput, error) {
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}

	group, err := s.requireGroupMembership(user.ID, input.GroupID)
	if err != nil {
		return nil, err
	}

	var expense models.Expense
	if err := s.db.
		Preload("PaidByUser").
		Preload("OwnerUser").
		Preload("CreatedByUser").
		Preload("Splits.User").
		Where("id = ? AND group_id = ?", input.ExpenseID, group.ID).
		First(&expense).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, huma.Error404NotFound("expense not found")
		}
		return nil, huma.Error500InternalServerError("unable to load expense")
	}

	if effectiveExpenseOwnerID(expense) != user.ID {
		return nil, huma.Error403Forbidden("only the expense owner can archive this expense")
	}
	if expense.ArchivedAt != nil {
		return nil, huma.Error409Conflict("expense is already archived")
	}

	allocationTotals, err := s.loadExpenseAllocationTotals(s.db, []uuid.UUID{expense.ID})
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load expense allocations")
	}

	outstandingAmount := expenseOutstandingBalance(expense, allocationTotals[expense.ID])
	if outstandingAmount > 0 {
		return nil, huma.Error409Conflict("expense can only be archived once fully settled")
	}

	now := time.Now().UTC()
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Expense{}).
			Where("id = ?", expense.ID).
			Updates(map[string]any{
				"archived_at":       now,
				"archived_by_user_id": user.ID,
				"updated_at":        now,
			}).Error; err != nil {
			return err
		}

		return tx.Model(&models.Group{}).
			Where("id = ?", group.ID).
			Update("updated_at", now).
			Error
	}); err != nil {
		return nil, huma.Error500InternalServerError("unable to archive expense")
	}

	participantNames := make([]string, 0, len(expense.Splits))
	for _, split := range expense.Splits {
		participantNames = append(participantNames, split.User.Name)
	}
	sort.Strings(participantNames)

	return &archiveExpenseOutput{
		Body: groupExpense{
			ID:                     expense.ID,
			Description:            expense.Description,
			Category:               effectiveExpenseCategory(expense),
			AmountCents:            expense.AmountCents,
			BaseCurrency:           effectiveGroupBaseCurrency(group),
			OriginalAmountCents:    effectiveOriginalAmount(expense.AmountCents, expense.OriginalAmountCents),
			OriginalCurrency:       effectiveOriginalCurrency(effectiveGroupBaseCurrency(group), expense.OriginalCurrency),
			FXRate:                 effectiveFXRate(expense.FXRate),
			SplitMode:              effectiveExpenseSplitMode(expense),
			PaidByUserID:           expense.PaidByUserID,
			PaidByName:             expense.PaidByUser.Name,
			OwnerUserID:            effectiveExpenseOwnerID(expense),
			OwnerName:              effectiveExpenseOwnerName(expense),
			ParticipantUserIDs:     participantIDsFromSplits(expense.Splits),
			ParticipantNames:       participantNames,
			Splits:                 toGroupExpenseSplits(expense.Splits),
			IncurredOn:             expense.IncurredOn.Format("2006-01-02"),
			OutstandingAmountCents: 0,
			CanDelete:              effectiveExpenseOwnerID(expense) == user.ID,
			CanArchive:             false,
			CreatedAt:              expense.CreatedAt,
		},
	}, nil
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(s.config.SessionCookieName)
		if err != nil || cookie.Value == "" {
			next.ServeHTTP(w, r)
			return
		}

		var session models.Session
		if err := s.db.Preload("User").
			Where("token_hash = ? AND expires_at > ?", hashToken(cookie.Value), time.Now().UTC()).
			First(&session).Error; err != nil {
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), currentUserKey, &session.User)
		ctx = context.WithValue(ctx, currentSessionKey, &session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) createSession(userID uuid.UUID) (models.Session, string, error) {
	rawToken := randomToken(32)
	session := models.Session{
		UserID:    userID,
		TokenHash: hashToken(rawToken),
		ExpiresAt: time.Now().UTC().Add(s.config.SessionTTL),
	}
	if err := s.db.Create(&session).Error; err != nil {
		return models.Session{}, "", err
	}
	return session, rawToken, nil
}

func (s *Server) buildSessionCookie(rawToken string, expiresAt time.Time) string {
	cookie := &http.Cookie{
		Name:     s.config.SessionCookieName,
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.config.SessionSecure,
		Expires:  expiresAt.UTC(),
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	}
	return cookie.String()
}

func (s *Server) expiredSessionCookie() string {
	cookie := &http.Cookie{
		Name:     s.config.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.config.SessionSecure,
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
	}
	return cookie.String()
}

func (s *Server) loadDashboardGroups(userID uuid.UUID, browserTimeZone string) ([]dashboardGroup, error) {
	type row struct {
		ID              uuid.UUID
		Name            string
		BaseCurrency    string
		CreatedByUserID uuid.UUID
		ArchivedByUserID *uuid.UUID
		ArchivedAt      *time.Time
		CreatedAt       time.Time
		UpdatedAt       time.Time
		Role            string
	}

	var rows []row
	if err := s.db.Model(&models.Group{}).
		Select("groups.id, groups.name, groups.base_currency, groups.created_by_user_id, groups.archived_by_user_id, groups.archived_at, groups.created_at, groups.updated_at, memberships.role").
		Joins("JOIN memberships ON memberships.group_id = groups.id").
		Where("memberships.user_id = ?", userID).
		Order("groups.updated_at DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	result := make([]dashboardGroup, 0, len(rows))
	for _, row := range rows {
		group := models.Group{
			ID:               row.ID,
			Name:             row.Name,
			BaseCurrency:     row.BaseCurrency,
			CreatedByUserID:  row.CreatedByUserID,
			ArchivedByUserID: row.ArchivedByUserID,
			ArchivedAt:       row.ArchivedAt,
			CreatedAt:        row.CreatedAt,
			UpdatedAt:        row.UpdatedAt,
		}
		if err := s.ensureRecurringExpensesForGroup(s.db, group.ID, browserTimeZone); err != nil {
			return nil, err
		}
		memberCount, expenseCount, err := s.groupCounts(group.ID)
		if err != nil {
			return nil, err
		}
		lifecycle, err := s.groupLifecycleStatus(group.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, dashboardGroupFromState(group, row.Role, memberCount, expenseCount, lifecycle))
	}

	return result, nil
}

func (s *Server) loadDashboardInvitations(email string) ([]dashboardInvitation, error) {
	type row struct {
		ID            uuid.UUID
		GroupID       uuid.UUID
		GroupName     string
		InvitedByName string
		Email         string
		CreatedAt     time.Time
	}

	var rows []row
	err := s.db.Table("invitations").
		Select("invitations.id, invitations.group_id, groups.name AS group_name, users.name AS invited_by_name, invitations.email, invitations.created_at").
		Joins("JOIN groups ON groups.id = invitations.group_id").
		Joins("JOIN users ON users.id = invitations.invited_by_user_id").
		Where("invitations.email = ? AND invitations.status = ?", normalizeEmail(email), "pending").
		Order("invitations.created_at DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make([]dashboardInvitation, 0, len(rows))
	for _, row := range rows {
		result = append(result, dashboardInvitation{
			ID:            row.ID,
			GroupID:       row.GroupID,
			GroupName:     row.GroupName,
			InvitedByName: row.InvitedByName,
			Email:         row.Email,
			CreatedAt:     row.CreatedAt,
		})
	}
	return result, nil
}

func (s *Server) requireGroupMembership(userID, groupID uuid.UUID) (models.Group, error) {
	var group models.Group
	err := s.db.
		Joins("JOIN memberships ON memberships.group_id = groups.id").
		Where("groups.id = ? AND memberships.user_id = ?", groupID, userID).
		First(&group).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return models.Group{}, huma.Error404NotFound("group not found")
		}
		return models.Group{}, huma.Error500InternalServerError("unable to load group")
	}
	return group, nil
}

func (s *Server) requireGroupOwner(userID, groupID uuid.UUID) (models.Group, error) {
	var result struct {
		ID              uuid.UUID
		Name            string
		BaseCurrency    string
		CreatedByUserID uuid.UUID
		ArchivedByUserID *uuid.UUID
		ArchivedAt      *time.Time
		CreatedAt       time.Time
		UpdatedAt       time.Time
		Role            string
	}

	err := s.db.Model(&models.Group{}).
		Select("groups.id, groups.name, groups.base_currency, groups.created_by_user_id, groups.archived_by_user_id, groups.archived_at, groups.created_at, groups.updated_at, memberships.role").
		Joins("JOIN memberships ON memberships.group_id = groups.id").
		Where("groups.id = ? AND memberships.user_id = ?", groupID, userID).
		Scan(&result).Error
	if err != nil {
		return models.Group{}, huma.Error500InternalServerError("unable to load group")
	}
	if result.ID == uuid.Nil {
		return models.Group{}, huma.Error404NotFound("group not found")
	}
	if result.Role != "owner" {
		return models.Group{}, huma.Error403Forbidden("only the group owner can perform this action")
	}

	return models.Group{
		ID:               result.ID,
		Name:             result.Name,
		BaseCurrency:     result.BaseCurrency,
		CreatedByUserID:  result.CreatedByUserID,
		ArchivedByUserID: result.ArchivedByUserID,
		ArchivedAt:       result.ArchivedAt,
		CreatedAt:        result.CreatedAt,
		UpdatedAt:        result.UpdatedAt,
	}, nil
}

type groupLifecycleState struct {
	TotalExpenses  int64
	ActiveExpenses int64
	OpenExpenses   int64
}

func (s *Server) groupLifecycleStatus(groupID uuid.UUID) (groupLifecycleState, error) {
	var expenses []models.Expense
	if err := s.db.
		Preload("Splits").
		Where("group_id = ?", groupID).
		Find(&expenses).Error; err != nil {
		return groupLifecycleState{}, err
	}

	allocationTotals, err := s.loadExpenseAllocationTotals(s.db, expenseIDs(expenses))
	if err != nil {
		return groupLifecycleState{}, err
	}

	state := groupLifecycleState{
		TotalExpenses: int64(len(expenses)),
	}
	for _, expense := range expenses {
		if expense.ArchivedAt != nil {
			continue
		}
		state.ActiveExpenses++
		if expenseOutstandingBalance(expense, allocationTotals[expense.ID]) > 0 {
			state.OpenExpenses++
		}
	}

	return state, nil
}

func dashboardGroupFromState(
	group models.Group,
	role string,
	memberCount, expenseCount int64,
	lifecycle groupLifecycleState,
) dashboardGroup {
	isOwner := role == "owner"
	isArchived := group.ArchivedAt != nil

	return dashboardGroup{
		ID:           group.ID,
		Name:         group.Name,
		BaseCurrency: effectiveGroupBaseCurrency(group),
		MemberCount:  memberCount,
		ExpenseCount: expenseCount,
		IsOwner:      isOwner,
		IsArchived:   isArchived,
		CanDelete:    isOwner && !isArchived && lifecycle.TotalExpenses == 0,
		CanArchive:   isOwner && !isArchived && lifecycle.TotalExpenses > 0 && lifecycle.OpenExpenses == 0,
		CanUnarchive: isOwner && isArchived,
		CreatedAt:    group.CreatedAt,
		UpdatedAt:    group.UpdatedAt,
	}
}

func (s *Server) loadGroupMembers(groupID uuid.UUID) ([]groupMember, []app.MemberSnapshot, error) {
	type row struct {
		ID         uuid.UUID      `gorm:"column:id"`
		Name       string         `gorm:"column:name"`
		Email      string         `gorm:"column:email"`
		Role       string         `gorm:"column:role"`
		JoinedAt   time.Time      `gorm:"column:joined_at"`
		InvitedAt  sql.NullString `gorm:"column:invited_at"`
		AcceptedAt sql.NullString `gorm:"column:accepted_at"`
	}

	var rows []row
	err := s.db.Table("memberships").
		Select(`
			users.id,
			users.name,
			users.email,
			memberships.role,
			memberships.created_at AS joined_at,
			accepted_invitations.invited_at,
			accepted_invitations.accepted_at
		`).
		Joins("JOIN users ON users.id = memberships.user_id").
		Joins(`
			LEFT JOIN (
				SELECT
					group_id,
					accepted_by_user_id,
					MAX(created_at) AS invited_at,
					MAX(accepted_at) AS accepted_at
				FROM invitations
				WHERE status = 'accepted'
					AND accepted_by_user_id IS NOT NULL
					AND accepted_at IS NOT NULL
				GROUP BY group_id, accepted_by_user_id
			) AS accepted_invitations
				ON accepted_invitations.group_id = memberships.group_id
				AND accepted_invitations.accepted_by_user_id = memberships.user_id
		`).
		Where("memberships.group_id = ?", groupID).
		Order("users.name ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, nil, err
	}

	members := make([]groupMember, 0, len(rows))
	snapshots := make([]app.MemberSnapshot, 0, len(rows))
	for _, row := range rows {
		invitedAt, err := parseOptionalDBTime(row.InvitedAt)
		if err != nil {
			return nil, nil, err
		}
		acceptedAt, err := parseOptionalDBTime(row.AcceptedAt)
		if err != nil {
			return nil, nil, err
		}
		members = append(members, groupMember{
			ID:         row.ID,
			Name:       row.Name,
			Email:      row.Email,
			Role:       row.Role,
			JoinedAt:   row.JoinedAt,
			InvitedAt:  invitedAt,
			AcceptedAt: acceptedAt,
		})
		snapshots = append(snapshots, app.MemberSnapshot{
			ID:    row.ID,
			Name:  row.Name,
			Email: row.Email,
		})
	}

	return members, snapshots, nil
}

func (s *Server) loadGroupInvitations(groupID uuid.UUID) ([]groupInvitation, error) {
	type row struct {
		ID            uuid.UUID
		Email         string
		Status        string
		InvitedByName string
		CreatedAt     time.Time
	}

	var rows []row
	err := s.db.Table("invitations").
		Select("invitations.id, invitations.email, invitations.status, users.name AS invited_by_name, invitations.created_at").
		Joins("JOIN users ON users.id = invitations.invited_by_user_id").
		Where("invitations.group_id = ?", groupID).
		Order("invitations.created_at DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make([]groupInvitation, 0, len(rows))
	for _, row := range rows {
		result = append(result, groupInvitation{
			ID:            row.ID,
			Email:         row.Email,
			Status:        row.Status,
			InvitedByName: row.InvitedByName,
			CreatedAt:     row.CreatedAt,
		})
	}

	return result, nil
}

func (s *Server) loadGroupMessages(groupID, currentUserID uuid.UUID) ([]groupMessageEntry, error) {
	type row struct {
		ID        uuid.UUID
		UserID    uuid.UUID
		UserName  string
		Body      string
		CreatedAt time.Time
	}

	var rows []row
	err := s.db.Table("group_messages").
		Select("group_messages.id, group_messages.user_id, users.name AS user_name, group_messages.body, group_messages.created_at").
		Joins("JOIN users ON users.id = group_messages.user_id").
		Where("group_messages.group_id = ?", groupID).
		Order("group_messages.created_at DESC, group_messages.id DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make([]groupMessageEntry, 0, len(rows))
	for _, row := range rows {
		result = append(result, groupMessageEntry{
			ID:        row.ID,
			UserID:    row.UserID,
			UserName:  row.UserName,
			Body:      row.Body,
			CreatedAt: row.CreatedAt,
			CanDelete: row.UserID == currentUserID,
		})
	}

	return result, nil
}

func (s *Server) ensureRecurringExpensesForGroup(tx *gorm.DB, groupID uuid.UUID, browserTimeZone string) error {
	_, _, err := resolveBrowserLocation(browserTimeZone)
	if err != nil {
		return err
	}

	var templates []models.RecurringExpenseTemplate
	if err := tx.
		Preload("Splits").
		Where("group_id = ?", groupID).
		Order("created_at ASC").
		Find(&templates).Error; err != nil {
		return err
	}
	if len(templates) == 0 {
		return nil
	}

	requestLocation, _, err := resolveBrowserLocation(browserTimeZone)
	if err != nil {
		return err
	}
	targetMonth := monthKeyForTime(time.Now().In(requestLocation))
	createdAny := false

	for _, template := range templates {
		templateLocation, _, err := resolveBrowserLocation(template.TimeZone)
		if err != nil {
			return err
		}

		var occurrenceCount int64
		if err := tx.Model(&models.Expense{}).
			Where("recurring_template_id = ?", template.ID).
			Count(&occurrenceCount).Error; err != nil {
			return err
		}
		if occurrenceCount == 0 {
			effectiveStartMonth, err := recurringTemplateStartMonth(template.CreatedAt, template.DueDayOfMonth, templateLocation)
			if err != nil {
				return err
			}
			if effectiveStartMonth != template.StartMonth {
				if err := tx.Model(&models.RecurringExpenseTemplate{}).
					Where("id = ?", template.ID).
					Update("start_month", effectiveStartMonth).Error; err != nil {
					return err
				}
				template.StartMonth = effectiveStartMonth
			}
		}

		for monthKey := template.StartMonth; monthKey != "" && monthKeyLessOrEqual(monthKey, targetMonth); {
			var count int64
			if err := tx.Model(&models.Expense{}).
				Where("recurring_template_id = ? AND occurrence_month = ?", template.ID, monthKey).
				Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				if _, _, err := createRecurringExpenseOccurrence(tx, template, template.Splits, monthKey, templateLocation); err != nil {
					return err
				}
				createdAny = true
			}

			nextMonth, err := nextMonthKey(monthKey)
			if err != nil {
				return err
			}
			monthKey = nextMonth
		}
	}

	if createdAny {
		return tx.Model(&models.Group{}).
			Where("id = ?", groupID).
			Update("updated_at", time.Now().UTC()).
			Error
	}
	return nil
}

func createRecurringExpenseOccurrence(
	tx *gorm.DB,
	template models.RecurringExpenseTemplate,
	templateSplits []models.RecurringExpenseSplit,
	monthKey string,
	location *time.Location,
) (models.Expense, []models.ExpenseSplit, error) {
	incurredOn, err := recurringOccurrenceDateUTC(monthKey, template.DueDayOfMonth, location)
	if err != nil {
		return models.Expense{}, nil, err
	}

	ownerUserID := template.OwnerUserID
	templateID := template.ID
	expense := models.Expense{
		GroupID:             template.GroupID,
		Description:         template.Description,
		Category:            template.Category,
		ExpenseType:         models.ExpenseTypeMonthly,
		RecurringTemplateID: &templateID,
		OccurrenceMonth:     monthKey,
		AmountCents:         template.AmountCents,
		OriginalAmountCents: template.OriginalAmountCents,
		OriginalCurrency:    template.OriginalCurrency,
		FXRate:              template.FXRate,
		FXSource:            template.FXSource,
		FXFetchedAt:         template.FXFetchedAt,
		SplitMode:           template.SplitMode,
		PaidByUserID:        template.OwnerUserID,
		OwnerUserID:         &ownerUserID,
		CreatedByUserID:     template.CreatedByUserID,
		IncurredOn:          incurredOn,
	}
	if err := tx.Create(&expense).Error; err != nil {
		return models.Expense{}, nil, err
	}

	expenseSplits := make([]models.ExpenseSplit, 0, len(templateSplits))
	for _, split := range templateSplits {
		expenseSplits = append(expenseSplits, models.ExpenseSplit{
			ExpenseID:   expense.ID,
			UserID:      split.UserID,
			AmountCents: split.AmountCents,
		})
	}
	if len(expenseSplits) > 0 {
		if err := tx.Create(&expenseSplits).Error; err != nil {
			return models.Expense{}, nil, err
		}
		if err := createExpenseSplitObligations(tx, expense, expenseSplits); err != nil {
			return models.Expense{}, nil, err
		}
	}

	return expense, expenseSplits, nil
}

func (s *Server) loadGroupExpenses(groupID, currentUserID uuid.UUID) ([]models.Expense, []groupExpense, error) {
	var group models.Group
	if err := s.db.Select("id, base_currency").Where("id = ?", groupID).First(&group).Error; err != nil {
		return nil, nil, err
	}
	groupBaseCurrency := effectiveGroupBaseCurrency(group)

	var expenses []models.Expense
	err := s.db.
		Preload("PaidByUser").
		Preload("OwnerUser").
		Preload("ArchivedByUser").
		Preload("CreatedByUser").
		Preload("RecurringTemplate").
		Preload("Splits.User").
		Where("group_id = ?", groupID).
		Order("incurred_on DESC, created_at DESC").
		Find(&expenses).Error
	if err != nil {
		return nil, nil, err
	}

	allocationTotals, err := s.loadExpenseAllocationTotals(s.db, expenseIDs(expenses))
	if err != nil {
		return nil, nil, err
	}

	payload := make([]groupExpense, 0, len(expenses))
	for _, expense := range expenses {
		outstandingAmount := expenseOutstandingBalance(expense, allocationTotals[expense.ID])
		if expense.ArchivedAt != nil {
			continue
		}

		payload = append(payload, groupExpense{
			ID:                     expense.ID,
			Description:            expense.Description,
			Category:               effectiveExpenseCategory(expense),
			ExpenseType:            expenseTypeLabel(expense.ExpenseType),
			AmountCents:            expense.AmountCents,
			BaseCurrency:           groupBaseCurrency,
			OriginalAmountCents:    effectiveOriginalAmount(expense.AmountCents, expense.OriginalAmountCents),
			OriginalCurrency:       effectiveOriginalCurrency(groupBaseCurrency, expense.OriginalCurrency),
			FXRate:                 effectiveFXRate(expense.FXRate),
			SplitMode:              effectiveExpenseSplitMode(expense),
			PaidByUserID:           expense.PaidByUserID,
			PaidByName:             expense.PaidByUser.Name,
			OwnerUserID:            effectiveExpenseOwnerID(expense),
			OwnerName:              effectiveExpenseOwnerName(expense),
			ParticipantUserIDs:     participantIDsFromSplits(expense.Splits),
			ParticipantNames:       participantNamesFromSplits(expense.Splits),
			Splits:                 toGroupExpenseSplits(expense.Splits),
			IncurredOn:             expense.IncurredOn.Format("2006-01-02"),
			OutstandingAmountCents: outstandingAmount,
			CanDelete: effectiveExpenseOwnerID(expense) == currentUserID &&
				expenseDeleteAllowed(expense, outstandingAmount, totalExpenseOwedByOthers(expense)),
			CanArchive:             false,
			CreatedAt:              expense.CreatedAt,
		})
	}

	return expenses, payload, nil
}

func (s *Server) loadGroupSettlements(groupID uuid.UUID) ([]models.Settlement, []groupSettlement, error) {
	var settlements []models.Settlement
	err := s.db.
		Preload("FromUser").
		Preload("ToUser").
		Preload("Allocations.Expense").
		Preload("Applications.Obligation").
		Where("group_id = ?", groupID).
		Order("settled_on DESC, created_at DESC").
		Find(&settlements).Error
	if err != nil {
		return nil, nil, err
	}

	var group models.Group
	if err := s.db.Select("id, base_currency").Where("id = ?", groupID).First(&group).Error; err != nil {
		return nil, nil, err
	}
	groupBaseCurrency := effectiveGroupBaseCurrency(group)

	payload := make([]groupSettlement, 0, len(settlements))
	for _, settlement := range settlements {
		payload = append(payload, groupSettlement{
			ID:                  settlement.ID,
			FromUserID:          settlement.FromUserID,
			FromName:            settlement.FromUser.Name,
			ToUserID:            settlement.ToUserID,
			ToName:              settlement.ToUser.Name,
			AmountCents:         settlement.AmountCents,
			BaseCurrency:        groupBaseCurrency,
			OriginalAmountCents: effectiveOriginalAmount(settlement.AmountCents, settlement.OriginalAmountCents),
			OriginalCurrency:    effectiveOriginalCurrency(groupBaseCurrency, settlement.OriginalCurrency),
			FXRate:              effectiveFXRate(settlement.FXRate),
			SettledOn:           settlement.SettledOn.Format("2006-01-02"),
			CreatedAt:           settlement.CreatedAt,
		})
	}

	return settlements, payload, nil
}

func toGroupExpenseSplits(splits []models.ExpenseSplit) []groupExpenseSplit {
	result := make([]groupExpenseSplit, 0, len(splits))
	for _, split := range splits {
		result = append(result, groupExpenseSplit{
			UserID:      split.UserID,
			UserName:    split.User.Name,
			AmountCents: split.AmountCents,
		})
	}

	sort.SliceStable(result, func(i, j int) bool {
		return result[i].UserName < result[j].UserName
	})
	return result
}

func effectiveExpenseOwnerID(expense models.Expense) uuid.UUID {
	if expense.OwnerUserID != nil && *expense.OwnerUserID != uuid.Nil {
		return *expense.OwnerUserID
	}
	return expense.CreatedByUserID
}

func effectiveExpenseOwnerName(expense models.Expense) string {
	if expense.OwnerUser != nil && expense.OwnerUser.Name != "" {
		return expense.OwnerUser.Name
	}
	return expense.CreatedByUser.Name
}

func effectiveExpenseCategory(expense models.Expense) string {
	category := normalizeExpenseCategory(expense.Category)
	if category == "" {
		return "uncategorized"
	}
	return category
}

func effectiveExpenseSplitMode(expense models.Expense) string {
	mode := normalizeSplitMode(expense.SplitMode)
	if mode == "" {
		return "equal"
	}
	return mode
}

func (s *Server) groupMemberMap(groupID uuid.UUID) (map[uuid.UUID]groupMember, error) {
	members, _, err := s.loadGroupMembers(groupID)
	if err != nil {
		return nil, err
	}

	result := make(map[uuid.UUID]groupMember, len(members))
	for _, member := range members {
		result[member.ID] = member
	}
	return result, nil
}

func (s *Server) resolveExpenseSplits(
	memberMap map[uuid.UUID]groupMember,
	totalAmount int64,
	splitMode string,
	participantIDs []uuid.UUID,
	inputSplits []expenseSplitInput,
) ([]uuid.UUID, []int64, error) {
	switch splitMode {
	case "equal":
		dedupedParticipants := dedupeUUIDs(participantIDs)
		if len(dedupedParticipants) == 0 {
			return nil, nil, huma.Error400BadRequest("at least one participant is required")
		}
		for _, participantID := range dedupedParticipants {
			if _, ok := memberMap[participantID]; !ok {
				return nil, nil, huma.Error400BadRequest("all participants must be group members")
			}
		}
		return dedupedParticipants, equalSplits(totalAmount, dedupedParticipants), nil
	case "custom":
		participants, splitAmounts, err := resolveCustomSplits(memberMap, totalAmount, inputSplits)
		if err != nil {
			return nil, nil, err
		}
		return participants, splitAmounts, nil
	default:
		return nil, nil, huma.Error400BadRequest("splitMode must be equal or custom")
	}
}

func (s *Server) groupCounts(groupID uuid.UUID) (int64, int64, error) {
	var memberCount int64
	if err := s.db.Model(&models.Membership{}).Where("group_id = ?", groupID).Count(&memberCount).Error; err != nil {
		return 0, 0, err
	}

	var expenseCount int64
	if err := s.db.Model(&models.Expense{}).Where("group_id = ? AND archived_at IS NULL", groupID).Count(&expenseCount).Error; err != nil {
		return 0, 0, err
	}

	return memberCount, expenseCount, nil
}

type settlementAllocationPlanRow struct {
	ExpenseID    uuid.UUID
	ObligationID uuid.UUID
	AmountCents  int64
}

func (s *Server) planSettlementAllocations(groupID, fromUserID, toUserID uuid.UUID, amountCents int64) ([]settlementAllocationPlanRow, error) {
	var expenses []models.Expense
	if err := s.db.
		Preload("Splits").
		Where("group_id = ? AND paid_by_user_id = ? AND archived_at IS NULL", groupID, toUserID).
		Order("incurred_on ASC, created_at ASC").
		Find(&expenses).Error; err != nil {
		return nil, huma.Error500InternalServerError("unable to load expenses for settlement allocation")
	}

	allocationTotals, err := s.loadExpenseAllocationTotals(s.db, expenseIDs(expenses))
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load existing settlement allocations")
	}
	obligationIDsByExpenseID, err := s.loadExpenseSplitObligationIDs(expenseIDs(expenses), fromUserID, toUserID)
	if err != nil {
		return nil, huma.Error500InternalServerError("unable to load settlement obligations")
	}

	remainingAmount := amountCents
	plan := make([]settlementAllocationPlanRow, 0)

	for _, expense := range expenses {
		splitAmount := splitAmountForUser(expense.Splits, fromUserID)
		if splitAmount <= 0 {
			continue
		}

		allocatedAmount := allocationTotals[expense.ID][fromUserID]
		outstandingAmount := splitAmount - allocatedAmount
		if outstandingAmount <= 0 {
			continue
		}

		appliedAmount := minInt64(outstandingAmount, remainingAmount)
		obligationID, ok := obligationIDsByExpenseID[expense.ID]
		if !ok {
			return nil, huma.Error500InternalServerError("unable to match settlement obligation")
		}
		plan = append(plan, settlementAllocationPlanRow{
			ExpenseID:    expense.ID,
			ObligationID: obligationID,
			AmountCents:  appliedAmount,
		})
		remainingAmount -= appliedAmount

		if remainingAmount == 0 {
			break
		}
	}

	if len(plan) == 0 {
		return nil, huma.Error400BadRequest("there are no unsettled expenses owed to this member")
	}
	if remainingAmount > 0 {
		return nil, huma.Error400BadRequest("settlement exceeds the unsettled amount owed to this member")
	}

	return plan, nil
}

func (s *Server) planExpenseSettlementAllocation(
	groupID, fromUserID, toUserID, expenseID uuid.UUID,
	amountCents int64,
	expenses []models.Expense,
) (settlementAllocationPlanRow, error) {
	if amountCents <= 0 {
		return settlementAllocationPlanRow{}, huma.Error400BadRequest("amount must be greater than zero")
	}

	var targetExpense *models.Expense
	for i := range expenses {
		expense := &expenses[i]
		if expense.ID == expenseID {
			targetExpense = expense
			break
		}
	}
	if targetExpense == nil {
		return settlementAllocationPlanRow{}, huma.Error400BadRequest("expense is not available for settlement")
	}
	if targetExpense.GroupID != groupID || targetExpense.PaidByUserID != toUserID || targetExpense.ArchivedAt != nil {
		return settlementAllocationPlanRow{}, huma.Error400BadRequest("expense is not available for settlement")
	}

	allocationTotals, err := s.loadExpenseAllocationTotals(s.db, []uuid.UUID{expenseID})
	if err != nil {
		return settlementAllocationPlanRow{}, huma.Error500InternalServerError("unable to load existing settlement allocations")
	}
	obligationIDsByExpenseID, err := s.loadExpenseSplitObligationIDs([]uuid.UUID{expenseID}, fromUserID, toUserID)
	if err != nil {
		return settlementAllocationPlanRow{}, huma.Error500InternalServerError("unable to load settlement obligations")
	}

	splitAmount := splitAmountForUser(targetExpense.Splits, fromUserID)
	if splitAmount <= 0 {
		return settlementAllocationPlanRow{}, huma.Error400BadRequest("you do not owe any amount on this expense")
	}

	allocatedAmount := allocationTotals[expenseID][fromUserID]
	outstandingAmount := splitAmount - allocatedAmount
	if outstandingAmount <= 0 {
		return settlementAllocationPlanRow{}, huma.Error400BadRequest("expense is already fully settled")
	}
	if amountCents != outstandingAmount {
		return settlementAllocationPlanRow{}, huma.Error400BadRequest("settlement amount must exactly match the remaining amount for this expense")
	}
	obligationID, ok := obligationIDsByExpenseID[expenseID]
	if !ok {
		return settlementAllocationPlanRow{}, huma.Error500InternalServerError("unable to match settlement obligation")
	}

	return settlementAllocationPlanRow{
		ExpenseID:    expenseID,
		ObligationID: obligationID,
		AmountCents:  amountCents,
	}, nil
}

type openExpenseObligationRow struct {
	ObligationID uuid.UUID
	ExpenseID    uuid.UUID
	FromUserID   uuid.UUID
	ToUserID     uuid.UUID
	AmountCents  int64
}

type settlementPathEdge struct {
	FromUserID uuid.UUID
	ToUserID   uuid.UUID
}

type nettedSettlementPathFlow struct {
	Path        []settlementPathEdge
	AmountCents int64
}

type settlementPairKey struct {
	FromUserID uuid.UUID
	ToUserID   uuid.UUID
}

func (s *Server) planNettedSettlementAllocations(
	groupID, fromUserID, toUserID uuid.UUID, amountCents int64,
) (int64, []settlementAllocationPlanRow, []nettedSettlementPathFlow, error) {
	openObligations, err := s.loadOpenExpenseSplitObligations(groupID)
	if err != nil {
		return 0, nil, nil, huma.Error500InternalServerError("unable to load obligations for simplified settlement")
	}

	edgeAmounts := make(map[uuid.UUID]map[uuid.UUID]int64)
	rowsByPair := make(map[settlementPairKey][]*openExpenseObligationRow)
	for index := range openObligations {
		row := &openObligations[index]
		if row.AmountCents <= 0 {
			continue
		}
		adjustSimplifiedDebtEdge(edgeAmounts, row.FromUserID, row.ToUserID, row.AmountCents)
		pairKey := settlementPairKey{FromUserID: row.FromUserID, ToUserID: row.ToUserID}
		rowsByPair[pairKey] = append(rowsByPair[pairKey], row)
	}

	reducedEdgeAmounts := cloneDebtGraph(edgeAmounts)
	reduceSimplifiedDebtGraph(reducedEdgeAmounts)

	simplifiedAmount := simplifiedDebtAmount(reducedEdgeAmounts, fromUserID, toUserID)
	if simplifiedAmount <= 0 {
		return 0, nil, nil, huma.Error400BadRequest("there is no simplified debt owed to this member")
	}
	if amountCents != simplifiedAmount {
		return simplifiedAmount, nil, nil, huma.Error400BadRequest("settlement amount must exactly match the simplified debt owed to this member")
	}

	remainingAmount := amountCents
	plan := make([]settlementAllocationPlanRow, 0)
	pathFlows := make([]nettedSettlementPathFlow, 0)
	for remainingAmount > 0 {
		path := findSettlementPath(edgeAmounts, fromUserID, toUserID)
		if len(path) == 0 {
			return simplifiedAmount, nil, nil, huma.Error409Conflict("simplified debt changed before the settlement could be recorded")
		}

		flowAmount := remainingAmount
		for _, pathEdge := range path {
			flowAmount = minInt64(flowAmount, simplifiedDebtAmount(edgeAmounts, pathEdge.FromUserID, pathEdge.ToUserID))
		}
		if flowAmount <= 0 {
			return simplifiedAmount, nil, nil, huma.Error409Conflict("simplified debt changed before the settlement could be recorded")
		}

		pathCopy := append([]settlementPathEdge(nil), path...)
		pathFlows = append(pathFlows, nettedSettlementPathFlow{
			Path:        pathCopy,
			AmountCents: flowAmount,
		})

		for _, pathEdge := range path {
			pairKey := settlementPairKey{FromUserID: pathEdge.FromUserID, ToUserID: pathEdge.ToUserID}
			edgePlan, allocationErr := allocateSettlementFlow(rowsByPair[pairKey], flowAmount)
			if allocationErr != nil {
				return simplifiedAmount, nil, nil, huma.Error409Conflict("simplified debt changed before the settlement could be recorded")
			}
			plan = append(plan, edgePlan...)
			adjustSimplifiedDebtEdge(edgeAmounts, pathEdge.FromUserID, pathEdge.ToUserID, -flowAmount)
		}

		remainingAmount -= flowAmount
	}

	return simplifiedAmount, combineSettlementPlanRows(plan), pathFlows, nil
}

func (s *Server) loadOpenExpenseSplitObligations(groupID uuid.UUID) ([]openExpenseObligationRow, error) {
	rows := make([]openExpenseObligationRow, 0)
	err := s.db.Table("obligations").
		Select(`
			obligations.id AS obligation_id,
			obligations.source_expense_id AS expense_id,
			obligations.from_user_id,
			obligations.to_user_id,
			obligations.amount_cents - COALESCE(SUM(settlement_applications.amount_cents), 0) AS amount_cents
		`).
		Joins("JOIN expenses ON expenses.id = obligations.source_expense_id").
		Joins("LEFT JOIN settlement_applications ON settlement_applications.obligation_id = obligations.id").
		Where("obligations.group_id = ? AND obligations.source_type = ? AND expenses.archived_at IS NULL", groupID, models.ObligationSourceExpenseSplit).
		Group("obligations.id, obligations.source_expense_id, obligations.from_user_id, obligations.to_user_id, obligations.amount_cents, expenses.incurred_on, expenses.created_at").
		Having("obligations.amount_cents - COALESCE(SUM(settlement_applications.amount_cents), 0) > 0").
		Order("expenses.incurred_on ASC, expenses.created_at ASC, obligations.id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func cloneDebtGraph(edgeAmounts map[uuid.UUID]map[uuid.UUID]int64) map[uuid.UUID]map[uuid.UUID]int64 {
	cloned := make(map[uuid.UUID]map[uuid.UUID]int64, len(edgeAmounts))
	for fromUserID, targets := range edgeAmounts {
		clonedTargets := make(map[uuid.UUID]int64, len(targets))
		for toUserID, amountCents := range targets {
			clonedTargets[toUserID] = amountCents
		}
		cloned[fromUserID] = clonedTargets
	}
	return cloned
}

func findSettlementPath(edgeAmounts map[uuid.UUID]map[uuid.UUID]int64, fromUserID, toUserID uuid.UUID) []settlementPathEdge {
	if fromUserID == toUserID {
		return nil
	}

	queue := []uuid.UUID{fromUserID}
	visited := map[uuid.UUID]struct{}{fromUserID: {}}
	previous := make(map[uuid.UUID]uuid.UUID)

	for len(queue) > 0 {
		currentUserID := queue[0]
		queue = queue[1:]

		targets := make([]uuid.UUID, 0, len(edgeAmounts[currentUserID]))
		for nextUserID, amountCents := range edgeAmounts[currentUserID] {
			if amountCents <= 0 {
				continue
			}
			targets = append(targets, nextUserID)
		}
		sort.Slice(targets, func(i, j int) bool {
			return targets[i].String() < targets[j].String()
		})

		for _, nextUserID := range targets {
			if _, ok := visited[nextUserID]; ok {
				continue
			}
			visited[nextUserID] = struct{}{}
			previous[nextUserID] = currentUserID
			if nextUserID == toUserID {
				path := make([]settlementPathEdge, 0)
				cursor := toUserID
				for cursor != fromUserID {
					prevUserID := previous[cursor]
					path = append(path, settlementPathEdge{
						FromUserID: prevUserID,
						ToUserID:   cursor,
					})
					cursor = prevUserID
				}
				for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
					path[left], path[right] = path[right], path[left]
				}
				return path
			}
			queue = append(queue, nextUserID)
		}
	}

	return nil
}

func allocateSettlementFlow(rows []*openExpenseObligationRow, amountCents int64) ([]settlementAllocationPlanRow, error) {
	remainingAmount := amountCents
	plan := make([]settlementAllocationPlanRow, 0)
	for _, row := range rows {
		if row.AmountCents <= 0 {
			continue
		}
		appliedAmount := minInt64(row.AmountCents, remainingAmount)
		if appliedAmount <= 0 {
			continue
		}
		plan = append(plan, settlementAllocationPlanRow{
			ExpenseID:    row.ExpenseID,
			ObligationID: row.ObligationID,
			AmountCents:  appliedAmount,
		})
		row.AmountCents -= appliedAmount
		remainingAmount -= appliedAmount
		if remainingAmount == 0 {
			return plan, nil
		}
	}

	if remainingAmount > 0 {
		return nil, fmt.Errorf("insufficient open obligation flow")
	}
	return plan, nil
}

func combineSettlementPlanRows(plan []settlementAllocationPlanRow) []settlementAllocationPlanRow {
	if len(plan) == 0 {
		return nil
	}

	type combinedRow struct {
		index int
		row   settlementAllocationPlanRow
	}

	rowByObligationID := make(map[uuid.UUID]*combinedRow, len(plan))
	obligationOrder := make([]uuid.UUID, 0, len(plan))
	for _, planRow := range plan {
		if existing := rowByObligationID[planRow.ObligationID]; existing != nil {
			existing.row.AmountCents += planRow.AmountCents
			continue
		}
		rowCopy := planRow
		rowByObligationID[planRow.ObligationID] = &combinedRow{row: rowCopy}
		obligationOrder = append(obligationOrder, planRow.ObligationID)
	}

	combined := make([]settlementAllocationPlanRow, 0, len(obligationOrder))
	for _, obligationID := range obligationOrder {
		combined = append(combined, rowByObligationID[obligationID].row)
	}
	return combined
}

func buildNettedReimbursementLedgerRows(
	settlementID, groupID, payerUserID uuid.UUID,
	pathFlows []nettedSettlementPathFlow,
) ([]models.Obligation, []models.SettlementApplication) {
	if len(pathFlows) == 0 {
		return nil, nil
	}

	obligations := make([]models.Obligation, 0)
	applications := make([]models.SettlementApplication, 0)
	for _, pathFlow := range pathFlows {
		if len(pathFlow.Path) <= 1 || pathFlow.AmountCents <= 0 {
			continue
		}

		currentReimbursementIndex := -1
		lastPathEdge := pathFlow.Path[len(pathFlow.Path)-1]
		obligations = append(obligations, models.Obligation{
			GroupID:            groupID,
			FromUserID:         lastPathEdge.FromUserID,
			ToUserID:           payerUserID,
			SourceType:         models.ObligationSourceReimbursement,
			SourceSettlementID: &settlementID,
			AmountCents:        pathFlow.AmountCents,
		})
		currentReimbursementIndex = len(obligations) - 1

		for pathIndex := len(pathFlow.Path) - 2; pathIndex >= 0; pathIndex-- {
			applications = append(applications, models.SettlementApplication{
				SettlementID: settlementID,
				AmountCents:  pathFlow.AmountCents,
			})
			applications[len(applications)-1].ObligationID = obligations[currentReimbursementIndex].ID

			if pathIndex == 0 {
				break
			}

			pathEdge := pathFlow.Path[pathIndex]
			obligations = append(obligations, models.Obligation{
				GroupID:            groupID,
				FromUserID:         pathEdge.FromUserID,
				ToUserID:           payerUserID,
				SourceType:         models.ObligationSourceReimbursement,
				SourceSettlementID: &settlementID,
				AmountCents:        pathFlow.AmountCents,
			})
			currentReimbursementIndex = len(obligations) - 1
		}
	}

	return obligations, applications
}

func (s *Server) loadExpenseSplitObligationIDs(expenseIDs []uuid.UUID, fromUserID, toUserID uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	result := make(map[uuid.UUID]uuid.UUID)
	if len(expenseIDs) == 0 {
		return result, nil
	}

	var obligations []models.Obligation
	if err := s.db.
		Where(
			"source_type = ? AND source_expense_id IN ? AND from_user_id = ? AND to_user_id = ?",
			models.ObligationSourceExpenseSplit,
			expenseIDs,
			fromUserID,
			toUserID,
		).
		Find(&obligations).Error; err != nil {
		return nil, err
	}

	for _, obligation := range obligations {
		if obligation.SourceExpenseID == nil {
			continue
		}
		result[*obligation.SourceExpenseID] = obligation.ID
	}

	return result, nil
}

func createExpenseSplitObligations(tx *gorm.DB, expense models.Expense, splits []models.ExpenseSplit) error {
	obligations := make([]models.Obligation, 0, len(splits))
	for _, split := range splits {
		if split.UserID == expense.PaidByUserID || split.AmountCents <= 0 {
			continue
		}

		expenseID := expense.ID
		obligations = append(obligations, models.Obligation{
			GroupID:         expense.GroupID,
			FromUserID:      split.UserID,
			ToUserID:        expense.PaidByUserID,
			SourceType:      models.ObligationSourceExpenseSplit,
			SourceExpenseID: &expenseID,
			AmountCents:     split.AmountCents,
		})
	}

	if len(obligations) == 0 {
		return nil
	}

	return tx.Create(&obligations).Error
}

func settlementAllocationsFromPlan(settlementID uuid.UUID, plan []settlementAllocationPlanRow) []models.SettlementAllocation {
	if len(plan) == 0 {
		return nil
	}

	amountByExpenseID := make(map[uuid.UUID]int64)
	expenseOrder := make([]uuid.UUID, 0, len(plan))
	for _, planRow := range plan {
		if planRow.ExpenseID == uuid.Nil || planRow.AmountCents <= 0 {
			continue
		}
		if _, ok := amountByExpenseID[planRow.ExpenseID]; !ok {
			expenseOrder = append(expenseOrder, planRow.ExpenseID)
		}
		amountByExpenseID[planRow.ExpenseID] += planRow.AmountCents
	}

	allocations := make([]models.SettlementAllocation, 0, len(expenseOrder))
	for _, expenseID := range expenseOrder {
		allocations = append(allocations, models.SettlementAllocation{
			SettlementID: settlementID,
			ExpenseID:    expenseID,
			AmountCents:  amountByExpenseID[expenseID],
		})
	}
	return allocations
}

func (s *Server) loadExpenseAllocationTotals(tx *gorm.DB, expenseIDs []uuid.UUID) (map[uuid.UUID]map[uuid.UUID]int64, error) {
	result := make(map[uuid.UUID]map[uuid.UUID]int64, len(expenseIDs))
	if len(expenseIDs) == 0 {
		return result, nil
	}

	type allocationRow struct {
		ExpenseID   uuid.UUID
		FromUserID  uuid.UUID
		AmountCents int64
	}

	var rows []allocationRow
	if err := tx.Table("settlement_applications").
		Select("obligations.source_expense_id AS expense_id, obligations.from_user_id, SUM(settlement_applications.amount_cents) AS amount_cents").
		Joins("JOIN obligations ON obligations.id = settlement_applications.obligation_id").
		Where("obligations.source_type = ? AND obligations.source_expense_id IN ?", models.ObligationSourceExpenseSplit, expenseIDs).
		Group("obligations.source_expense_id, obligations.from_user_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	for _, row := range rows {
		if _, ok := result[row.ExpenseID]; !ok {
			result[row.ExpenseID] = make(map[uuid.UUID]int64)
		}
		result[row.ExpenseID][row.FromUserID] = row.AmountCents
	}

	return result, nil
}

func buildCurrentUserBalanceRows(
	currentUserID uuid.UUID,
	expenses []models.Expense,
	settlements []models.Settlement,
	openObligations []openExpenseObligationRow,
) ([]currentUserBalanceRow, []currentUserBalanceRow, []currentUserHistoryRow, []openExpensePaymentRow, []settleUpPayeeRow, []settleUpExpenseRow) {
	expenseByID := expensesByID(expenses)
	userNames := userNamesByID(expenses, settlements)

	payments := buildCurrentUserPayments(currentUserID, expenseByID, userNames, openObligations)
	outgoingSettlements, settleUpPayees, settleUpExpenses := buildCurrentUserSettlements(
		currentUserID,
		expenseByID,
		userNames,
		openObligations,
	)
	history := buildCurrentUserHistory(currentUserID, settlements)
	openExpensePayments := buildOpenExpensePaymentRows(currentUserID, expenseByID, userNames, openObligations)
	return payments, outgoingSettlements, history, openExpensePayments, settleUpPayees, settleUpExpenses
}

func simplifiedSettleToRows(
	currentUserID uuid.UUID,
	members []app.MemberSnapshot,
	openObligations []openExpenseObligationRow,
) []simplifiedSettleToRow {
	edgeAmounts := make(map[uuid.UUID]map[uuid.UUID]int64)
	userNames := make(map[uuid.UUID]string)
	for _, member := range members {
		userNames[member.ID] = member.Name
	}

	for _, row := range openObligations {
		if row.AmountCents <= 0 {
			continue
		}
		adjustSimplifiedDebtEdge(edgeAmounts, row.FromUserID, row.ToUserID, row.AmountCents)
	}

	reduceSimplifiedDebtGraph(edgeAmounts)

	targets := make([]uuid.UUID, 0, len(edgeAmounts[currentUserID]))
	for toUserID, amountCents := range edgeAmounts[currentUserID] {
		if amountCents <= 0 {
			continue
		}
		targets = append(targets, toUserID)
	}
	sort.Slice(targets, func(i, j int) bool {
		leftName := userNames[targets[i]]
		rightName := userNames[targets[j]]
		if leftName != rightName {
			return leftName < rightName
		}
		return targets[i].String() < targets[j].String()
	})

	rows := make([]simplifiedSettleToRow, 0, len(targets))
	for _, toUserID := range targets {
		rows = append(rows, simplifiedSettleToRow{
			FromUserID:  currentUserID,
			FromName:    userNames[currentUserID],
			ToUserID:    toUserID,
			ToName:      userNames[toUserID],
			AmountCents: edgeAmounts[currentUserID][toUserID],
		})
	}

	return rows
}

func expensesByID(expenses []models.Expense) map[uuid.UUID]models.Expense {
	result := make(map[uuid.UUID]models.Expense, len(expenses))
	for _, expense := range expenses {
		result[expense.ID] = expense
	}
	return result
}

func userNamesByID(expenses []models.Expense, settlements []models.Settlement) map[uuid.UUID]string {
	result := make(map[uuid.UUID]string)
	for _, expense := range expenses {
		result[expense.PaidByUserID] = expense.PaidByUser.Name
		result[effectiveExpenseOwnerID(expense)] = effectiveExpenseOwnerName(expense)
		for _, split := range expense.Splits {
			result[split.UserID] = split.User.Name
		}
	}
	for _, settlement := range settlements {
		result[settlement.FromUserID] = settlement.FromUser.Name
		result[settlement.ToUserID] = settlement.ToUser.Name
	}
	return result
}

func reduceSimplifiedDebtGraph(edgeAmounts map[uuid.UUID]map[uuid.UUID]int64) {
	cancelReciprocalDebtEdges(edgeAmounts)

	for {
		reduced := false
		nodeIDs := simplifiedDebtNodeIDs(edgeAmounts)

		for _, intermediaryID := range nodeIDs {
			for _, fromUserID := range nodeIDs {
				if fromUserID == intermediaryID || simplifiedDebtAmount(edgeAmounts, fromUserID, intermediaryID) <= 0 {
					continue
				}

				for _, toUserID := range nodeIDs {
					if toUserID == intermediaryID || toUserID == fromUserID {
						continue
					}

					flowAmount := minInt64(
						simplifiedDebtAmount(edgeAmounts, fromUserID, intermediaryID),
						simplifiedDebtAmount(edgeAmounts, intermediaryID, toUserID),
					)
					if flowAmount <= 0 {
						continue
					}

					adjustSimplifiedDebtEdge(edgeAmounts, fromUserID, intermediaryID, -flowAmount)
					adjustSimplifiedDebtEdge(edgeAmounts, intermediaryID, toUserID, -flowAmount)
					adjustSimplifiedDebtEdge(edgeAmounts, fromUserID, toUserID, flowAmount)
					cancelReciprocalDebtPair(edgeAmounts, fromUserID, toUserID)

					reduced = true
					break
				}
				if reduced {
					break
				}
			}
			if reduced {
				break
			}
		}

		if !reduced {
			return
		}
	}
}

func cancelReciprocalDebtEdges(edgeAmounts map[uuid.UUID]map[uuid.UUID]int64) {
	nodeIDs := simplifiedDebtNodeIDs(edgeAmounts)
	for leftIndex := 0; leftIndex < len(nodeIDs); leftIndex++ {
		for rightIndex := leftIndex + 1; rightIndex < len(nodeIDs); rightIndex++ {
			cancelReciprocalDebtPair(edgeAmounts, nodeIDs[leftIndex], nodeIDs[rightIndex])
		}
	}
}

func cancelReciprocalDebtPair(edgeAmounts map[uuid.UUID]map[uuid.UUID]int64, leftUserID, rightUserID uuid.UUID) {
	cancelAmount := minInt64(
		simplifiedDebtAmount(edgeAmounts, leftUserID, rightUserID),
		simplifiedDebtAmount(edgeAmounts, rightUserID, leftUserID),
	)
	if cancelAmount <= 0 {
		return
	}

	adjustSimplifiedDebtEdge(edgeAmounts, leftUserID, rightUserID, -cancelAmount)
	adjustSimplifiedDebtEdge(edgeAmounts, rightUserID, leftUserID, -cancelAmount)
}

func simplifiedDebtNodeIDs(edgeAmounts map[uuid.UUID]map[uuid.UUID]int64) []uuid.UUID {
	nodeSet := make(map[uuid.UUID]struct{})
	for fromUserID, targets := range edgeAmounts {
		for toUserID, amountCents := range targets {
			if amountCents <= 0 {
				continue
			}
			nodeSet[fromUserID] = struct{}{}
			nodeSet[toUserID] = struct{}{}
		}
	}

	nodeIDs := make([]uuid.UUID, 0, len(nodeSet))
	for userID := range nodeSet {
		nodeIDs = append(nodeIDs, userID)
	}
	sort.Slice(nodeIDs, func(i, j int) bool {
		return nodeIDs[i].String() < nodeIDs[j].String()
	})
	return nodeIDs
}

func simplifiedDebtAmount(edgeAmounts map[uuid.UUID]map[uuid.UUID]int64, fromUserID, toUserID uuid.UUID) int64 {
	if targets := edgeAmounts[fromUserID]; targets != nil {
		return targets[toUserID]
	}
	return 0
}

func adjustSimplifiedDebtEdge(edgeAmounts map[uuid.UUID]map[uuid.UUID]int64, fromUserID, toUserID uuid.UUID, deltaCents int64) {
	if fromUserID == toUserID || deltaCents == 0 {
		return
	}

	targets := edgeAmounts[fromUserID]
	if targets == nil {
		if deltaCents < 0 {
			return
		}
		targets = make(map[uuid.UUID]int64)
		edgeAmounts[fromUserID] = targets
	}

	targets[toUserID] += deltaCents
	if targets[toUserID] <= 0 {
		delete(targets, toUserID)
	}
	if len(targets) == 0 {
		delete(edgeAmounts, fromUserID)
	}
}

func buildCurrentUserPayments(
	currentUserID uuid.UUID,
	expenseByID map[uuid.UUID]models.Expense,
	userNames map[uuid.UUID]string,
	openObligations []openExpenseObligationRow,
) []currentUserBalanceRow {
	type paymentAccumulator struct {
		who         string
		amountCents int64
		expenses    map[string]struct{}
	}

	rowsByUserID := make(map[uuid.UUID]*paymentAccumulator)
	for _, row := range openObligations {
		if row.ToUserID != currentUserID || row.AmountCents <= 0 {
			continue
		}

		accumulator := rowsByUserID[row.FromUserID]
		if accumulator == nil {
			accumulator = &paymentAccumulator{
				who:      userNames[row.FromUserID],
				expenses: make(map[string]struct{}),
			}
			rowsByUserID[row.FromUserID] = accumulator
		}

		accumulator.amountCents += row.AmountCents
		if expense, ok := expenseByID[row.ExpenseID]; ok {
			accumulator.expenses[expense.Description] = struct{}{}
		}
	}

	rows := make([]currentUserBalanceRow, 0, len(rowsByUserID))
	for _, row := range rowsByUserID {
		rows = append(rows, currentUserBalanceRow{
			Who:         row.who,
			AmountCents: row.amountCents,
			Expense:     joinSortedNames(row.expenses),
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].AmountCents == rows[j].AmountCents {
			return rows[i].Who < rows[j].Who
		}
		return rows[i].AmountCents > rows[j].AmountCents
	})

	return rows
}

func buildCurrentUserSettlements(
	currentUserID uuid.UUID,
	expenseByID map[uuid.UUID]models.Expense,
	userNames map[uuid.UUID]string,
	openObligations []openExpenseObligationRow,
) ([]currentUserBalanceRow, []settleUpPayeeRow, []settleUpExpenseRow) {
	type settlementAccumulator struct {
		userID      uuid.UUID
		who         string
		amountCents int64
		expenses    map[string]struct{}
	}

	rowsByUserID := make(map[uuid.UUID]*settlementAccumulator)
	expenseRows := make([]settleUpExpenseRow, 0)
	for _, row := range openObligations {
		if row.FromUserID != currentUserID || row.AmountCents <= 0 {
			continue
		}

		accumulator := rowsByUserID[row.ToUserID]
		if accumulator == nil {
			accumulator = &settlementAccumulator{
				userID:   row.ToUserID,
				who:      userNames[row.ToUserID],
				expenses: make(map[string]struct{}),
			}
			rowsByUserID[row.ToUserID] = accumulator
		}

		accumulator.amountCents += row.AmountCents
		expenseName := ""
		if expense, ok := expenseByID[row.ExpenseID]; ok {
			expenseName = expense.Description
			accumulator.expenses[expense.Description] = struct{}{}
		}
		expenseRows = append(expenseRows, settleUpExpenseRow{
			ExpenseID:   row.ExpenseID,
			ToUserID:    row.ToUserID,
			ToName:      userNames[row.ToUserID],
			Expense:     expenseName,
			AmountCents: row.AmountCents,
		})
	}

	rows := make([]currentUserBalanceRow, 0, len(rowsByUserID))
	payees := make([]settleUpPayeeRow, 0, len(rowsByUserID))
	for _, row := range rowsByUserID {
		rows = append(rows, currentUserBalanceRow{
			Who:         row.who,
			AmountCents: row.amountCents,
			Expense:     joinSortedNames(row.expenses),
		})
		payees = append(payees, settleUpPayeeRow{
			UserID:      row.userID,
			Who:         row.who,
			AmountCents: row.amountCents,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].AmountCents == rows[j].AmountCents {
			return rows[i].Who < rows[j].Who
		}
		return rows[i].AmountCents > rows[j].AmountCents
	})

	sort.Slice(payees, func(i, j int) bool {
		if payees[i].AmountCents == payees[j].AmountCents {
			return payees[i].Who < payees[j].Who
		}
		return payees[i].AmountCents > payees[j].AmountCents
	})

	sort.Slice(expenseRows, func(i, j int) bool {
		if expenseRows[i].ToName == expenseRows[j].ToName {
			if expenseRows[i].AmountCents == expenseRows[j].AmountCents {
				return expenseRows[i].Expense < expenseRows[j].Expense
			}
			return expenseRows[i].AmountCents > expenseRows[j].AmountCents
		}
		return expenseRows[i].ToName < expenseRows[j].ToName
	})

	return rows, payees, expenseRows
}

func buildCurrentUserHistory(currentUserID uuid.UUID, settlements []models.Settlement) []currentUserHistoryRow {
	type historyWithSort struct {
		row       currentUserHistoryRow
		settledOn time.Time
		createdAt time.Time
	}

	rows := make([]historyWithSort, 0)
	for _, settlement := range settlements {
		switch {
		case settlement.FromUserID == currentUserID:
			rows = append(rows, historyWithSort{
				row: currentUserHistoryRow{
					Action:      "Paid To",
					Who:         settlement.ToUser.Name,
					AmountCents: settlement.AmountCents,
					Expense:     expenseDescriptionsForSettlement(settlement),
				},
				settledOn: settlement.SettledOn,
				createdAt: settlement.CreatedAt,
			})
		case settlement.ToUserID == currentUserID:
			rows = append(rows, historyWithSort{
				row: currentUserHistoryRow{
					Action:      "Settled From",
					Who:         settlement.FromUser.Name,
					AmountCents: settlement.AmountCents,
					Expense:     expenseDescriptionsForSettlement(settlement),
				},
				settledOn: settlement.SettledOn,
				createdAt: settlement.CreatedAt,
			})
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].settledOn.Equal(rows[j].settledOn) {
			return rows[i].createdAt.After(rows[j].createdAt)
		}
		return rows[i].settledOn.After(rows[j].settledOn)
	})

	result := make([]currentUserHistoryRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.row)
	}
	return result
}

func settlementAllocationTotalsByExpenseAndUser(settlements []models.Settlement) map[uuid.UUID]map[uuid.UUID]int64 {
	result := make(map[uuid.UUID]map[uuid.UUID]int64)
	for _, settlement := range settlements {
		if len(settlement.Applications) > 0 {
			for _, application := range settlement.Applications {
				if application.Obligation.SourceType != models.ObligationSourceExpenseSplit || application.Obligation.SourceExpenseID == nil {
					continue
				}
				expenseID := *application.Obligation.SourceExpenseID
				if _, ok := result[expenseID]; !ok {
					result[expenseID] = make(map[uuid.UUID]int64)
				}
				result[expenseID][application.Obligation.FromUserID] += application.AmountCents
			}
			continue
		}
		for _, allocation := range settlement.Allocations {
			if _, ok := result[allocation.ExpenseID]; !ok {
				result[allocation.ExpenseID] = make(map[uuid.UUID]int64)
			}
			result[allocation.ExpenseID][settlement.FromUserID] += allocation.AmountCents
		}
	}
	return result
}

func calculateBalancesFromOpenObligations(
	members []app.MemberSnapshot,
	openObligations []openExpenseObligationRow,
) ([]app.MemberBalance, []app.TransferSuggestion) {
	summaries := make(map[uuid.UUID]*app.MemberBalance, len(members))
	for _, member := range members {
		memberCopy := member
		summaries[member.ID] = &app.MemberBalance{
			UserID: memberCopy.ID,
			Name:   memberCopy.Name,
			Email:  memberCopy.Email,
		}
	}

	for _, obligation := range openObligations {
		if obligation.AmountCents <= 0 {
			continue
		}
		if creditor, ok := summaries[obligation.ToUserID]; ok {
			creditor.PaidCents += obligation.AmountCents
			creditor.NetCents += obligation.AmountCents
		}
		if debtor, ok := summaries[obligation.FromUserID]; ok {
			debtor.OwesCents += obligation.AmountCents
			debtor.NetCents -= obligation.AmountCents
		}
	}

	balances := make([]app.MemberBalance, 0, len(summaries))
	for _, member := range members {
		balances = append(balances, *summaries[member.ID])
	}

	sort.Slice(balances, func(i, j int) bool {
		if balances[i].NetCents == balances[j].NetCents {
			return balances[i].Name < balances[j].Name
		}
		return balances[i].NetCents > balances[j].NetCents
	})

	type ledgerEntry struct {
		UserID uuid.UUID
		Name   string
		Amount int64
	}

	creditors := make([]ledgerEntry, 0)
	debtors := make([]ledgerEntry, 0)
	for _, balance := range balances {
		switch {
		case balance.NetCents > 0:
			creditors = append(creditors, ledgerEntry{UserID: balance.UserID, Name: balance.Name, Amount: balance.NetCents})
		case balance.NetCents < 0:
			debtors = append(debtors, ledgerEntry{UserID: balance.UserID, Name: balance.Name, Amount: -balance.NetCents})
		}
	}

	sort.Slice(creditors, func(i, j int) bool { return creditors[i].Amount > creditors[j].Amount })
	sort.Slice(debtors, func(i, j int) bool { return debtors[i].Amount > debtors[j].Amount })

	transfers := make([]app.TransferSuggestion, 0)
	creditorIndex := 0
	debtorIndex := 0
	for creditorIndex < len(creditors) && debtorIndex < len(debtors) {
		creditor := &creditors[creditorIndex]
		debtor := &debtors[debtorIndex]

		amount := minInt64(creditor.Amount, debtor.Amount)
		transfers = append(transfers, app.TransferSuggestion{
			FromUserID:  debtor.UserID,
			FromName:    debtor.Name,
			ToUserID:    creditor.UserID,
			ToName:      creditor.Name,
			AmountCents: amount,
		})

		creditor.Amount -= amount
		debtor.Amount -= amount
		if creditor.Amount == 0 {
			creditorIndex++
		}
		if debtor.Amount == 0 {
			debtorIndex++
		}
	}

	return balances, transfers
}

func buildOpenExpensePaymentRows(
	currentUserID uuid.UUID,
	expenseByID map[uuid.UUID]models.Expense,
	userNames map[uuid.UUID]string,
	openObligations []openExpenseObligationRow,
) []openExpensePaymentRow {
	rows := make([]openExpensePaymentRow, 0)
	outstandingByExpenseID := make(map[uuid.UUID]int64)
	for _, row := range openObligations {
		outstandingByExpenseID[row.ExpenseID] += row.AmountCents
	}

	for _, row := range openObligations {
		if row.AmountCents <= 0 {
			continue
		}
		expense, ok := expenseByID[row.ExpenseID]
		if !ok {
			continue
		}
		canDelete := effectiveExpenseOwnerID(expense) == currentUserID &&
			expenseDeleteAllowed(expense, outstandingByExpenseID[row.ExpenseID], totalExpenseOwedByOthers(expense))
		rows = append(rows, openExpensePaymentRow{
			ExpenseID:   row.ExpenseID,
			Expense:     expense.Description,
			Who:         userNames[row.FromUserID],
			AmountCents: row.AmountCents,
			OwnerUserID: effectiveExpenseOwnerID(expense),
			CanDelete:   canDelete,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Expense == rows[j].Expense {
			if rows[i].ExpenseID != rows[j].ExpenseID {
				return rows[i].ExpenseID.String() < rows[j].ExpenseID.String()
			}
			if rows[i].AmountCents == rows[j].AmountCents {
				return rows[i].Who < rows[j].Who
			}
			return rows[i].AmountCents > rows[j].AmountCents
		}
		return rows[i].Expense < rows[j].Expense
	})

	return rows
}

func totalExpenseOwedByOthers(expense models.Expense) int64 {
	var total int64
	for _, split := range expense.Splits {
		if split.UserID == expense.PaidByUserID {
			continue
		}
		total += split.AmountCents
	}
	return total
}

func totalSettlementAllocationAmount(allocationTotals map[uuid.UUID]int64) int64 {
	var total int64
	for _, amountCents := range allocationTotals {
		total += amountCents
	}
	return total
}

func expenseDescriptionsForSettlement(settlement models.Settlement) string {
	names := make(map[string]struct{}, len(settlement.Allocations))
	for _, allocation := range settlement.Allocations {
		names[allocation.Expense.Description] = struct{}{}
	}
	return joinSortedNames(names)
}

func joinSortedNames(values map[string]struct{}) string {
	if len(values) == 0 {
		return "-"
	}

	names := make([]string, 0, len(values))
	for value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		names = append(names, value)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func buildExpenseReportDocument(
	group models.Group,
	expenses []models.Expense,
	settlements []models.Settlement,
) expenseReportDocument {
	allocationTotals := settlementAllocationTotalsByExpenseAndUser(settlements)
	latestPaymentDates := latestSettlementDatesByExpenseAndUser(settlements)

	report := expenseReportDocument{
		Group: expenseReportGroupSummary{
			Name:         group.Name,
			Status:       expenseReportGroupStatus(group),
			CreatedAt:    group.CreatedAt,
			BaseCurrency: effectiveGroupBaseCurrency(group),
		},
		Expenses: make([]expenseReportExpenseRow, 0, len(expenses)),
	}

	for _, expense := range expenses {
		expenseStatus := "Closed"
		if expenseOutstandingBalance(expense, allocationTotals[expense.ID]) > 0 {
			report.Group.ActiveExpenseCount++
			expenseStatus = "Open"
		} else {
			report.Group.ClosedExpenseCount++
			report.Group.GroupExpenditureCents += expense.AmountCents
		}

		report.Expenses = append(report.Expenses, expenseReportExpenseRow{
			Name:        expense.Description,
			Category:    expenseCategoryLabel(effectiveExpenseCategory(expense)),
			ExpenseType: expenseTypeLabel(expense.ExpenseType),
			PayByDate:   expenseReportPayByDate(expense),
			Status:      expenseStatus,
			AmountCents: expense.AmountCents,
			CreatedAt:   expense.CreatedAt,
			Owner:       effectiveExpenseOwnerName(expense),
			PaidBy:      expense.PaidByUser.Name,
			SplitWith:   expenseReportSplitWith(expense, allocationTotals[expense.ID], latestPaymentDates[expense.ID]),
		})
	}

	sort.Slice(report.Expenses, func(i, j int) bool {
		if report.Expenses[i].CreatedAt.Equal(report.Expenses[j].CreatedAt) {
			return report.Expenses[i].Name < report.Expenses[j].Name
		}
		return report.Expenses[i].CreatedAt.After(report.Expenses[j].CreatedAt)
	})

	return report
}

func expenseReportGroupStatus(group models.Group) string {
	if group.ArchivedAt != nil {
		return "Archived"
	}
	return "Active"
}

func expenseReportPayByDate(expense models.Expense) string {
	if normalizeExpenseType(expense.ExpenseType) != models.ExpenseTypeMonthly {
		return "N/A"
	}
	dueDate := expense.IncurredOn.UTC()
	dueDayOfMonth := dueDate.Day()
	if expense.RecurringTemplate != nil && expense.RecurringTemplate.DueDayOfMonth > 0 {
		dueDayOfMonth = expense.RecurringTemplate.DueDayOfMonth
	}
	shiftedDate, err := retrofitMonthlyPayByDate(expense.CreatedAt.UTC(), dueDate, dueDayOfMonth)
	if err == nil {
		dueDate = shiftedDate
	}
	return dueDate.Format("2006-01-02")
}

func expenseReportSplitWith(
	expense models.Expense,
	allocationTotals map[uuid.UUID]int64,
	latestPaymentDates map[uuid.UUID]time.Time,
) []string {
	rows := make([]string, 0)
	for _, split := range expense.Splits {
		if split.UserID == expense.PaidByUserID || split.AmountCents <= 0 {
			continue
		}
		name := strings.TrimSpace(split.User.Name)
		if name == "" {
			continue
		}
		if allocationTotals[split.UserID] >= split.AmountCents {
			if paidOn, ok := latestPaymentDates[split.UserID]; ok && !paidOn.IsZero() {
				rows = append(rows, fmt.Sprintf("%s [%s]", name, paidOn.Format("2006-01-02")))
				continue
			}
		}
		rows = append(rows, fmt.Sprintf("%s [ToPay]", name))
	}
	sort.Strings(rows)
	return rows
}

func latestSettlementDatesByExpenseAndUser(settlements []models.Settlement) map[uuid.UUID]map[uuid.UUID]time.Time {
	result := make(map[uuid.UUID]map[uuid.UUID]time.Time)
	for _, settlement := range settlements {
		if len(settlement.Applications) > 0 {
			for _, application := range settlement.Applications {
				if application.Obligation.SourceType != models.ObligationSourceExpenseSplit || application.Obligation.SourceExpenseID == nil {
					continue
				}
				expenseID := *application.Obligation.SourceExpenseID
				if _, ok := result[expenseID]; !ok {
					result[expenseID] = make(map[uuid.UUID]time.Time)
				}
				current := result[expenseID][application.Obligation.FromUserID]
				if current.IsZero() || settlement.SettledOn.After(current) {
					result[expenseID][application.Obligation.FromUserID] = settlement.SettledOn
				}
			}
			continue
		}
		for _, allocation := range settlement.Allocations {
			if _, ok := result[allocation.ExpenseID]; !ok {
				result[allocation.ExpenseID] = make(map[uuid.UUID]time.Time)
			}
			current := result[allocation.ExpenseID][settlement.FromUserID]
			if current.IsZero() || settlement.SettledOn.After(current) {
				result[allocation.ExpenseID][settlement.FromUserID] = settlement.SettledOn
			}
		}
	}
	return result
}

func sortedNames(values map[string]struct{}) []string {
	if len(values) == 0 {
		return []string{}
	}

	names := make([]string, 0, len(values))
	for value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		names = append(names, value)
	}
	sort.Strings(names)
	return names
}

func marshalExpenseReportCSV(report expenseReportDocument) ([]byte, error) {
	buffer := &bytes.Buffer{}
	writer := csv.NewWriter(buffer)

	rows := [][]string{
		{"Field", "Value"},
		{"Group Name", report.Group.Name},
		{"Group Status", report.Group.Status},
		{"Created At", report.Group.CreatedAt.Format("2006-01-02")},
		{"Base Currency", report.Group.BaseCurrency},
		{"Active Expenses", fmt.Sprintf("%d", report.Group.ActiveExpenseCount)},
		{"Closed Expenses", fmt.Sprintf("%d", report.Group.ClosedExpenseCount)},
		{"Group Expenditure", reportAmountString(report.Group.GroupExpenditureCents)},
		{},
		{"Expense Name", "Expense Category", "Expense Type", "PayByDate", "Expense Status", "Amount", "Created At", "Owner", "Paid By", "Split With"},
	}

	for _, expense := range report.Expenses {
		rows = append(rows, []string{
			expense.Name,
			expense.Category,
			expense.ExpenseType,
			expense.PayByDate,
			expense.Status,
			reportAmountString(expense.AmountCents),
			expense.CreatedAt.Format("2006-01-02"),
			expense.Owner,
			expense.PaidBy,
			strings.Join(expense.SplitWith, ", "),
		})
	}

	if err := writer.WriteAll(rows); err != nil {
		return nil, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

func reportAmountString(amountCents int64) string {
	return fmt.Sprintf("%.2f", float64(amountCents)/100)
}

func marshalExpenseReportJSON(report expenseReportDocument) ([]byte, error) {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func sanitizeReportFilename(name string) string {
	sanitized := strings.ToLower(strings.TrimSpace(name))
	sanitized = strings.ReplaceAll(sanitized, " ", "-")
	sanitized = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-':
			return r
		default:
			return -1
		}
	}, sanitized)
	sanitized = strings.Trim(sanitized, "-")
	if sanitized == "" {
		return "group"
	}
	return sanitized
}

func expenseReportDownloadFilename(filenameBase, format string, generatedAt time.Time) string {
	return fmt.Sprintf(
		"%s-%s-expense-report.%s",
		filenameBase,
		generatedAt.UTC().Format("20060102-150405"),
		format,
	)
}

func writeHTTPAPIError(w http.ResponseWriter, err error) {
	var statusErr interface {
		GetStatus() int
		Error() string
	}
	if errors.As(err, &statusErr) {
		http.Error(w, statusErr.Error(), statusErr.GetStatus())
		return
	}
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func requireUser(ctx context.Context) (*models.User, error) {
	user, ok := ctx.Value(currentUserKey).(*models.User)
	if !ok || user == nil {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	return user, nil
}

func currentSession(ctx context.Context) *models.Session {
	session, _ := ctx.Value(currentSessionKey).(*models.Session)
	return session
}

func toUserResponse(user models.User) userResponse {
	return userResponse{
		ID:    user.ID,
		Name:  user.Name,
		Email: user.Email,
	}
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func expenseIDs(expenses []models.Expense) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(expenses))
	for _, expense := range expenses {
		result = append(result, expense.ID)
	}
	return result
}

func participantIDsFromSplits(splits []models.ExpenseSplit) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(splits))
	for _, split := range splits {
		result = append(result, split.UserID)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].String() < result[j].String()
	})
	return result
}

func participantNamesFromSplits(splits []models.ExpenseSplit) []string {
	result := make([]string, 0, len(splits))
	for _, split := range splits {
		result = append(result, split.User.Name)
	}
	sort.Strings(result)
	return result
}

func splitAmountForUser(splits []models.ExpenseSplit, userID uuid.UUID) int64 {
	for _, split := range splits {
		if split.UserID == userID {
			return split.AmountCents
		}
	}
	return 0
}

func expenseOutstandingBalance(expense models.Expense, allocationsByDebtor map[uuid.UUID]int64) int64 {
	var outstanding int64
	for _, split := range expense.Splits {
		if split.UserID == expense.PaidByUserID {
			continue
		}
		remaining := split.AmountCents - allocationsByDebtor[split.UserID]
		if remaining > 0 {
			outstanding += remaining
		}
	}
	return outstanding
}

func expenseDeleteAllowed(expense models.Expense, outstandingAmount, totalOwedByOthers int64) bool {
	if normalizeExpenseType(expense.ExpenseType) == models.ExpenseTypeMonthly {
		return outstandingAmount == 0
	}
	return outstandingAmount == 0 || outstandingAmount == totalOwedByOthers
}

func outstandingExpenseAmount(paidByUserID uuid.UUID, participantIDs []uuid.UUID, splitAmounts []int64) int64 {
	var outstanding int64
	for index, participantID := range participantIDs {
		if participantID == paidByUserID {
			continue
		}
		outstanding += splitAmounts[index]
	}
	return outstanding
}

func dedupeUUIDs(values []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(values))
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		if value == uuid.Nil {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].String() < result[j].String()
	})
	return result
}

func equalSplits(total int64, participants []uuid.UUID) []int64 {
	if len(participants) == 0 {
		return nil
	}

	base := total / int64(len(participants))
	remainder := total % int64(len(participants))
	result := make([]int64, len(participants))
	for index := range participants {
		result[index] = base
		if int64(index) < remainder {
			result[index]++
		}
	}
	return result
}

func resolveCustomSplits(
	memberMap map[uuid.UUID]groupMember,
	totalAmount int64,
	inputSplits []expenseSplitInput,
) ([]uuid.UUID, []int64, error) {
	if len(inputSplits) == 0 {
		return nil, nil, huma.Error400BadRequest("at least one custom split is required")
	}

	type splitRow struct {
		userID      uuid.UUID
		amountCents int64
	}

	rows := make([]splitRow, 0, len(inputSplits))
	seen := make(map[uuid.UUID]struct{}, len(inputSplits))
	var totalSplitAmount int64

	for _, inputSplit := range inputSplits {
		if inputSplit.UserID == uuid.Nil {
			return nil, nil, huma.Error400BadRequest("custom split users are required")
		}
		if _, ok := memberMap[inputSplit.UserID]; !ok {
			return nil, nil, huma.Error400BadRequest("all custom split users must be group members")
		}
		if inputSplit.AmountCents <= 0 {
			return nil, nil, huma.Error400BadRequest("custom split amounts must be greater than zero")
		}
		if _, exists := seen[inputSplit.UserID]; exists {
			return nil, nil, huma.Error400BadRequest("custom split users must be unique")
		}

		seen[inputSplit.UserID] = struct{}{}
		rows = append(rows, splitRow{
			userID:      inputSplit.UserID,
			amountCents: inputSplit.AmountCents,
		})
		totalSplitAmount += inputSplit.AmountCents
	}

	if totalSplitAmount != totalAmount {
		return nil, nil, huma.Error400BadRequest("custom split amounts must add up to the full expense amount")
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].userID.String() < rows[j].userID.String()
	})

	participants := make([]uuid.UUID, 0, len(rows))
	splitAmounts := make([]int64, 0, len(rows))
	for _, row := range rows {
		participants = append(participants, row.userID)
		splitAmounts = append(splitAmounts, row.amountCents)
	}

	return participants, splitAmounts, nil
}

func convertCustomSplitAmounts(originalAmounts []int64, originalTotal int64, baseTotal int64) []int64 {
	if len(originalAmounts) == 0 {
		return nil
	}
	if originalTotal <= 0 || baseTotal <= 0 {
		return make([]int64, len(originalAmounts))
	}

	type remainderRow struct {
		index     int
		remainder int64
	}

	converted := make([]int64, len(originalAmounts))
	remainders := make([]remainderRow, 0, len(originalAmounts))
	var allocated int64

	for index, originalAmount := range originalAmounts {
		product := originalAmount * baseTotal
		converted[index] = product / originalTotal
		remainders = append(remainders, remainderRow{
			index:     index,
			remainder: product % originalTotal,
		})
		allocated += converted[index]
	}

	remaining := baseTotal - allocated
	sort.SliceStable(remainders, func(i, j int) bool {
		if remainders[i].remainder == remainders[j].remainder {
			return remainders[i].index < remainders[j].index
		}
		return remainders[i].remainder > remainders[j].remainder
	})

	for index := int64(0); index < remaining; index++ {
		converted[remainders[index].index]++
	}

	return converted
}

func parseOptionalDBTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value.String)
		if err == nil {
			parsed = parsed.UTC()
			return &parsed, nil
		}
	}

	return nil, fmt.Errorf("parse database time %q: unsupported format", value.String)
}

func normalizeExpenseCategory(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func expenseCategoryLabel(value string) string {
	switch normalizeExpenseCategory(value) {
	case "food":
		return "Food"
	case "transport":
		return "Transport"
	case "accommodation":
		return "Accommodation"
	case "entertainment":
		return "Entertainment"
	case "rent":
		return "Rent"
	case "subscription":
		return "Subscription"
	default:
		return "Uncategorized"
	}
}

func isValidExpenseCategory(value string) bool {
	switch value {
	case "food", "transport", "accommodation", "entertainment", "rent", "subscription":
		return true
	default:
		return false
	}
}

func normalizeExpenseType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return models.ExpenseTypeOneTime
	}
	return normalized
}

func isValidExpenseType(value string) bool {
	return value == models.ExpenseTypeOneTime || value == models.ExpenseTypeMonthly
}

func expenseTypeLabel(value string) string {
	if normalizeExpenseType(value) == models.ExpenseTypeMonthly {
		return "Monthly"
	}
	return "One-time"
}

func resolveBrowserLocation(value string) (*time.Location, string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.UTC, "UTC", nil
	}

	location, err := time.LoadLocation(trimmed)
	if err != nil {
		return nil, "", fmt.Errorf("load browser time zone: %w", err)
	}
	return location, trimmed, nil
}

func monthKeyForTime(value time.Time) string {
	year, month, _ := value.Date()
	return fmt.Sprintf("%04d-%02d", year, int(month))
}

func recurringTemplateStartMonth(createdAt time.Time, dueDayOfMonth int, location *time.Location) (string, error) {
	localCreatedAt := createdAt.In(location)
	currentMonth := monthKeyForTime(localCreatedAt)
	year, month, _ := localCreatedAt.Date()
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, location).Day()
	effectiveDueDay := dueDayOfMonth
	if effectiveDueDay > lastDay {
		effectiveDueDay = lastDay
	}
	if localCreatedAt.Day() > effectiveDueDay {
		return nextMonthKey(currentMonth)
	}
	return currentMonth, nil
}

func retrofitMonthlyPayByDate(createdAt, dueDate time.Time, dueDayOfMonth int) (time.Time, error) {
	retrofittedDate := dueDate.UTC()
	for payByDateIsEarlierThanCreatedAt(retrofittedDate, createdAt.UTC()) {
		nextMonth, err := nextMonthKey(monthKeyForTime(retrofittedDate))
		if err != nil {
			return time.Time{}, err
		}
		retrofittedDate, err = recurringOccurrenceDateUTC(nextMonth, dueDayOfMonth, time.UTC)
		if err != nil {
			return time.Time{}, err
		}
	}
	return retrofittedDate, nil
}

func payByDateIsEarlierThanCreatedAt(payByDate, createdAt time.Time) bool {
	payYear, payMonth, payDay := payByDate.UTC().Date()
	createYear, createMonth, createDay := createdAt.UTC().Date()

	if payYear != createYear {
		return payYear < createYear || (payYear == createYear && payMonth < createMonth)
	}
	if payMonth != createMonth {
		return payMonth < createMonth
	}
	return payDay < createDay
}

func parseMonthKey(value string) (int, time.Month, error) {
	parsed, err := time.Parse("2006-01", value)
	if err != nil {
		return 0, 0, err
	}
	year, month, _ := parsed.Date()
	return year, month, nil
}

func monthKeyLessOrEqual(left, right string) bool {
	leftYear, leftMonth, err := parseMonthKey(left)
	if err != nil {
		return false
	}
	rightYear, rightMonth, err := parseMonthKey(right)
	if err != nil {
		return false
	}
	if leftYear != rightYear {
		return leftYear < rightYear
	}
	return leftMonth <= rightMonth
}

func nextMonthKey(value string) (string, error) {
	year, month, err := parseMonthKey(value)
	if err != nil {
		return "", err
	}
	nextYear, nextMonth := year, month+1
	if nextMonth > time.December {
		nextYear++
		nextMonth = time.January
	}
	return fmt.Sprintf("%04d-%02d", nextYear, int(nextMonth)), nil
}

func recurringOccurrenceDateUTC(monthKey string, dueDayOfMonth int, location *time.Location) (time.Time, error) {
	year, month, err := parseMonthKey(monthKey)
	if err != nil {
		return time.Time{}, err
	}
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, location).Day()
	day := dueDayOfMonth
	if day > lastDay {
		day = lastDay
	}
	return time.Date(year, month, day, 0, 0, 0, 0, location).UTC(), nil
}

func extractExpenseSplitAmounts(splits []models.ExpenseSplit) []int64 {
	amounts := make([]int64, 0, len(splits))
	for _, split := range splits {
		amounts = append(amounts, split.AmountCents)
	}
	return amounts
}

func normalizeSplitMode(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isValidSplitMode(value string) bool {
	return value == "equal" || value == "custom"
}

func normalizeSettlementKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return ""
	case models.SettlementKindDirectExpense:
		return models.SettlementKindDirectExpense
	case models.SettlementKindNetted:
		return models.SettlementKindNetted
	default:
		return ""
	}
}

var supportedCurrencyCodes = map[string]struct{}{
	"AUD": {},
	"CAD": {},
	"CHF": {},
	"CNY": {},
	"EUR": {},
	"GBP": {},
	"INR": {},
	"JPY": {},
	"NZD": {},
	"USD": {},
}

func normalizeCurrencyCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func normalizedGroupCurrency(value string) string {
	normalized := normalizeCurrencyCode(value)
	if normalized == "" {
		return "USD"
	}
	return normalized
}

func isValidCurrencyCode(value string) bool {
	_, ok := supportedCurrencyCodes[value]
	return ok
}

func effectiveGroupBaseCurrency(group models.Group) string {
	baseCurrency := normalizeCurrencyCode(group.BaseCurrency)
	if !isValidCurrencyCode(baseCurrency) {
		return "USD"
	}
	return baseCurrency
}

func effectiveOriginalAmount(baseAmountCents, originalAmountCents int64) int64 {
	if originalAmountCents > 0 {
		return originalAmountCents
	}
	return baseAmountCents
}

func effectiveOriginalCurrency(baseCurrency, originalCurrency string) string {
	normalized := normalizeCurrencyCode(originalCurrency)
	if isValidCurrencyCode(normalized) {
		return normalized
	}
	return baseCurrency
}

func effectiveFXRate(rate float64) float64 {
	if rate > 0 {
		return rate
	}
	return 1
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func randomToken(byteLength int) string {
	buffer := make([]byte, byteLength)
	if _, err := rand.Read(buffer); err != nil {
		panic(fmt.Sprintf("generate random token: %v", err))
	}
	return hex.EncodeToString(buffer)
}

func hashToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}
