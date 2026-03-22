package db

import (
	"fmt"
	"time"

	"sharetab/service/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func Open(databaseDSN string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(databaseDSN), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("database handle: %w", err)
	}

	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	if err := migrate(db); err != nil {
		return nil, err
	}

	return db, nil
}

func migrate(db *gorm.DB) error {
	preMigrationStatements := []string{
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS base_currency varchar(3)`,
		`UPDATE groups SET base_currency = 'USD' WHERE base_currency IS NULL OR base_currency = ''`,
		`ALTER TABLE groups ALTER COLUMN base_currency SET DEFAULT 'USD'`,
		`ALTER TABLE groups ALTER COLUMN base_currency SET NOT NULL`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS archived_by_user_id uuid`,
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS archived_at timestamptz`,
		`ALTER TABLE expenses ADD COLUMN IF NOT EXISTS original_amount_cents bigint`,
		`UPDATE expenses SET original_amount_cents = amount_cents WHERE original_amount_cents IS NULL OR original_amount_cents <= 0`,
		`ALTER TABLE expenses ALTER COLUMN original_amount_cents SET DEFAULT 0`,
		`ALTER TABLE expenses ALTER COLUMN original_amount_cents SET NOT NULL`,
		`ALTER TABLE expenses ADD COLUMN IF NOT EXISTS original_currency varchar(3)`,
		`UPDATE expenses SET original_currency = 'USD' WHERE original_currency IS NULL OR original_currency = ''`,
		`ALTER TABLE expenses ALTER COLUMN original_currency SET DEFAULT 'USD'`,
		`ALTER TABLE expenses ALTER COLUMN original_currency SET NOT NULL`,
		`ALTER TABLE expenses ADD COLUMN IF NOT EXISTS fx_rate double precision`,
		`UPDATE expenses SET fx_rate = 1 WHERE fx_rate IS NULL OR fx_rate <= 0`,
		`ALTER TABLE expenses ALTER COLUMN fx_rate SET DEFAULT 1`,
		`ALTER TABLE expenses ALTER COLUMN fx_rate SET NOT NULL`,
		`ALTER TABLE expenses ADD COLUMN IF NOT EXISTS fx_source varchar(120)`,
		`UPDATE expenses SET fx_source = 'legacy' WHERE fx_source IS NULL OR fx_source = ''`,
		`ALTER TABLE expenses ALTER COLUMN fx_source SET DEFAULT 'manual'`,
		`ALTER TABLE expenses ALTER COLUMN fx_source SET NOT NULL`,
		`ALTER TABLE expenses ADD COLUMN IF NOT EXISTS fx_fetched_at timestamptz`,
		`UPDATE expenses SET fx_fetched_at = COALESCE(created_at, NOW()) WHERE fx_fetched_at IS NULL`,
		`ALTER TABLE expenses ALTER COLUMN fx_fetched_at SET DEFAULT NOW()`,
		`ALTER TABLE expenses ALTER COLUMN fx_fetched_at SET NOT NULL`,
		`ALTER TABLE expenses ADD COLUMN IF NOT EXISTS archived_by_user_id uuid`,
		`ALTER TABLE expenses ADD COLUMN IF NOT EXISTS archived_at timestamptz`,
		`ALTER TABLE expenses ADD COLUMN IF NOT EXISTS expense_type varchar(32)`,
		`UPDATE expenses SET expense_type = 'one-time' WHERE expense_type IS NULL OR expense_type = ''`,
		`ALTER TABLE expenses ALTER COLUMN expense_type SET DEFAULT 'one-time'`,
		`ALTER TABLE expenses ALTER COLUMN expense_type SET NOT NULL`,
		`ALTER TABLE expenses ADD COLUMN IF NOT EXISTS recurring_template_id uuid`,
		`ALTER TABLE expenses ADD COLUMN IF NOT EXISTS occurrence_month varchar(7)`,
		`ALTER TABLE settlements ADD COLUMN IF NOT EXISTS original_amount_cents bigint`,
		`UPDATE settlements SET original_amount_cents = amount_cents WHERE original_amount_cents IS NULL OR original_amount_cents <= 0`,
		`ALTER TABLE settlements ALTER COLUMN original_amount_cents SET DEFAULT 0`,
		`ALTER TABLE settlements ALTER COLUMN original_amount_cents SET NOT NULL`,
		`ALTER TABLE settlements ADD COLUMN IF NOT EXISTS original_currency varchar(3)`,
		`UPDATE settlements SET original_currency = 'USD' WHERE original_currency IS NULL OR original_currency = ''`,
		`ALTER TABLE settlements ALTER COLUMN original_currency SET DEFAULT 'USD'`,
		`ALTER TABLE settlements ALTER COLUMN original_currency SET NOT NULL`,
		`ALTER TABLE settlements ADD COLUMN IF NOT EXISTS fx_rate double precision`,
		`UPDATE settlements SET fx_rate = 1 WHERE fx_rate IS NULL OR fx_rate <= 0`,
		`ALTER TABLE settlements ALTER COLUMN fx_rate SET DEFAULT 1`,
		`ALTER TABLE settlements ALTER COLUMN fx_rate SET NOT NULL`,
		`ALTER TABLE settlements ADD COLUMN IF NOT EXISTS fx_source varchar(120)`,
		`UPDATE settlements SET fx_source = 'legacy' WHERE fx_source IS NULL OR fx_source = ''`,
		`ALTER TABLE settlements ALTER COLUMN fx_source SET DEFAULT 'manual'`,
		`ALTER TABLE settlements ALTER COLUMN fx_source SET NOT NULL`,
		`ALTER TABLE settlements ADD COLUMN IF NOT EXISTS fx_fetched_at timestamptz`,
		`UPDATE settlements SET fx_fetched_at = COALESCE(created_at, NOW()) WHERE fx_fetched_at IS NULL`,
		`ALTER TABLE settlements ALTER COLUMN fx_fetched_at SET DEFAULT NOW()`,
		`ALTER TABLE settlements ALTER COLUMN fx_fetched_at SET NOT NULL`,
		`ALTER TABLE settlements ADD COLUMN IF NOT EXISTS kind varchar(32)`,
		`UPDATE settlements SET kind = 'direct_expense' WHERE kind IS NULL OR kind = ''`,
		`ALTER TABLE settlements ALTER COLUMN kind SET DEFAULT 'direct_expense'`,
		`ALTER TABLE settlements ALTER COLUMN kind SET NOT NULL`,
	}

	for _, statement := range preMigrationStatements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("run pre-migration statement: %w", err)
		}
	}

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
		return fmt.Errorf("auto migrate: %w", err)
	}

	statements := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_memberships_group_user ON memberships (group_id, user_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_invitations_group_email_pending ON invitations (group_id, email) WHERE status = 'pending'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_recurring_expense_splits_template_user ON recurring_expense_splits (recurring_template_id, user_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_expenses_recurring_template_month ON expenses (recurring_template_id, occurrence_month) WHERE recurring_template_id IS NOT NULL AND occurrence_month IS NOT NULL AND occurrence_month <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_group_messages_group_created_at ON group_messages (group_id, created_at DESC, id DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_expense_splits_expense_user ON expense_splits (expense_id, user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_settlements_group_settled_on ON settlements (group_id, settled_on DESC, created_at DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_settlement_allocations_settlement_expense ON settlement_allocations (settlement_id, expense_id)`,
		`CREATE INDEX IF NOT EXISTS idx_obligations_group_parties ON obligations (group_id, from_user_id, to_user_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_obligations_expense_split ON obligations (source_type, source_expense_id, from_user_id, to_user_id) WHERE source_type = 'expense_split' AND source_expense_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_settlement_applications_settlement_obligation ON settlement_applications (settlement_id, obligation_id)`,
		`CREATE INDEX IF NOT EXISTS idx_settlement_applications_obligation ON settlement_applications (obligation_id)`,
	}

	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("run migration statement: %w", err)
		}
	}

	if err := backfillObligationLedger(db); err != nil {
		return err
	}

	return nil
}

func backfillObligationLedger(db *gorm.DB) error {
	if err := db.Model(&models.Settlement{}).
		Where("kind = '' OR kind IS NULL").
		Update("kind", models.SettlementKindDirectExpense).Error; err != nil {
		return fmt.Errorf("backfill settlement kinds: %w", err)
	}

	var expenses []models.Expense
	if err := db.Preload("Splits").Find(&expenses).Error; err != nil {
		return fmt.Errorf("load expenses for obligation backfill: %w", err)
	}

	type obligationKey struct {
		expenseID   string
		fromUserID  string
		toUserID    string
		sourceType  string
	}

	var existingObligations []models.Obligation
	if err := db.Where("source_type = ?", models.ObligationSourceExpenseSplit).Find(&existingObligations).Error; err != nil {
		return fmt.Errorf("load obligations for backfill: %w", err)
	}

	existingByKey := make(map[obligationKey]models.Obligation, len(existingObligations))
	for _, obligation := range existingObligations {
		if obligation.SourceExpenseID == nil {
			continue
		}
		existingByKey[obligationKey{
			expenseID:  obligation.SourceExpenseID.String(),
			fromUserID: obligation.FromUserID.String(),
			toUserID:   obligation.ToUserID.String(),
			sourceType: obligation.SourceType,
		}] = obligation
	}

	newObligations := make([]models.Obligation, 0)
	for _, expense := range expenses {
		expenseID := expense.ID
		for _, split := range expense.Splits {
			if split.UserID == expense.PaidByUserID || split.AmountCents <= 0 {
				continue
			}
			key := obligationKey{
				expenseID:  expense.ID.String(),
				fromUserID: split.UserID.String(),
				toUserID:   expense.PaidByUserID.String(),
				sourceType: models.ObligationSourceExpenseSplit,
			}
			if _, exists := existingByKey[key]; exists {
				continue
			}
			newObligations = append(newObligations, models.Obligation{
				GroupID:         expense.GroupID,
				FromUserID:      split.UserID,
				ToUserID:        expense.PaidByUserID,
				SourceType:      models.ObligationSourceExpenseSplit,
				SourceExpenseID: &expenseID,
				AmountCents:     split.AmountCents,
				CreatedAt:       expense.CreatedAt,
				UpdatedAt:       expense.UpdatedAt,
			})
		}
	}
	if len(newObligations) > 0 {
		if err := db.Create(&newObligations).Error; err != nil {
			return fmt.Errorf("create obligations during backfill: %w", err)
		}
		existingObligations = append(existingObligations, newObligations...)
	}

	obligationByKey := make(map[obligationKey]models.Obligation, len(existingObligations))
	for _, obligation := range existingObligations {
		if obligation.SourceExpenseID == nil {
			continue
		}
		obligationByKey[obligationKey{
			expenseID:  obligation.SourceExpenseID.String(),
			fromUserID: obligation.FromUserID.String(),
			toUserID:   obligation.ToUserID.String(),
			sourceType: obligation.SourceType,
		}] = obligation
	}

	var settlements []models.Settlement
	if err := db.Preload("Allocations").Find(&settlements).Error; err != nil {
		return fmt.Errorf("load settlements for application backfill: %w", err)
	}

	var existingApplications []models.SettlementApplication
	if err := db.Find(&existingApplications).Error; err != nil {
		return fmt.Errorf("load settlement applications for backfill: %w", err)
	}
	type applicationKey struct {
		settlementID string
		obligationID string
	}
	existingApplicationsByKey := make(map[applicationKey]struct{}, len(existingApplications))
	for _, application := range existingApplications {
		existingApplicationsByKey[applicationKey{
			settlementID: application.SettlementID.String(),
			obligationID: application.ObligationID.String(),
		}] = struct{}{}
	}

	newApplications := make([]models.SettlementApplication, 0)
	for _, settlement := range settlements {
		for _, allocation := range settlement.Allocations {
			obligation, exists := obligationByKey[obligationKey{
				expenseID:  allocation.ExpenseID.String(),
				fromUserID: settlement.FromUserID.String(),
				toUserID:   settlement.ToUserID.String(),
				sourceType: models.ObligationSourceExpenseSplit,
			}]
			if !exists {
				continue
			}

			key := applicationKey{
				settlementID: settlement.ID.String(),
				obligationID: obligation.ID.String(),
			}
			if _, exists := existingApplicationsByKey[key]; exists {
				continue
			}

			newApplications = append(newApplications, models.SettlementApplication{
				SettlementID: settlement.ID,
				ObligationID: obligation.ID,
				AmountCents:  allocation.AmountCents,
				CreatedAt:    settlement.CreatedAt,
				UpdatedAt:    settlement.UpdatedAt,
			})
			existingApplicationsByKey[key] = struct{}{}
		}
	}
	if len(newApplications) > 0 {
		if err := db.Create(&newApplications).Error; err != nil {
			return fmt.Errorf("create settlement applications during backfill: %w", err)
		}
	}

	return nil
}
