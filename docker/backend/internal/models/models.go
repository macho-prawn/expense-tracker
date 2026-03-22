package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	SettlementKindDirectExpense = "direct_expense"
	SettlementKindNetted        = "netted"

	ObligationSourceExpenseSplit = "expense_split"
	ObligationSourceReimbursement = "reimbursement"

	ExpenseTypeOneTime = "one-time"
	ExpenseTypeMonthly = "monthly"
)

type User struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name         string    `gorm:"size:120;not null"`
	Email        string    `gorm:"size:255;not null;uniqueIndex"`
	PasswordHash string    `gorm:"size:255;not null"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

func (u *User) BeforeCreate(_ *gorm.DB) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}

type Session struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index"`
	User      User      `gorm:"foreignKey:UserID"`
	TokenHash string    `gorm:"size:64;not null;uniqueIndex"`
	ExpiresAt time.Time `gorm:"not null;index"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

func (s *Session) BeforeCreate(_ *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}

type Group struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name            string    `gorm:"size:160;not null"`
	BaseCurrency    string    `gorm:"size:3;not null;default:USD"`
	CreatedByUserID uuid.UUID `gorm:"type:uuid;not null;index"`
	ArchivedByUserID *uuid.UUID `gorm:"type:uuid;index"`
	ArchivedByUser  *User      `gorm:"foreignKey:ArchivedByUserID"`
	ArchivedAt      *time.Time `gorm:"index"`
	CreatedAt       time.Time `gorm:"not null"`
	UpdatedAt       time.Time `gorm:"not null;index"`
}

func (g *Group) BeforeCreate(_ *gorm.DB) error {
	if g.ID == uuid.Nil {
		g.ID = uuid.New()
	}
	return nil
}

type Membership struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	GroupID   uuid.UUID `gorm:"type:uuid;not null;index"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index"`
	Role      string    `gorm:"size:32;not null;default:member"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

func (m *Membership) BeforeCreate(_ *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

type Invitation struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey"`
	GroupID         uuid.UUID  `gorm:"type:uuid;not null;index"`
	InvitedByUserID uuid.UUID  `gorm:"type:uuid;not null;index"`
	AcceptedByUserID *uuid.UUID `gorm:"type:uuid;index"`
	Email           string     `gorm:"size:255;not null;index"`
	Token           string     `gorm:"size:64;not null;uniqueIndex"`
	Status          string     `gorm:"size:32;not null;index"`
	AcceptedAt      *time.Time
	CreatedAt       time.Time `gorm:"not null"`
	UpdatedAt       time.Time `gorm:"not null"`
}

func (i *Invitation) BeforeCreate(_ *gorm.DB) error {
	if i.ID == uuid.Nil {
		i.ID = uuid.New()
	}
	return nil
}

type Expense struct {
	ID              uuid.UUID      `gorm:"type:uuid;primaryKey"`
	GroupID         uuid.UUID      `gorm:"type:uuid;not null;index"`
	Description     string         `gorm:"size:240;not null"`
	Category        string         `gorm:"size:32;index"`
	ExpenseType     string         `gorm:"size:32;not null;default:one-time;index"`
	RecurringTemplateID *uuid.UUID `gorm:"type:uuid;index"`
	RecurringTemplate   *RecurringExpenseTemplate `gorm:"foreignKey:RecurringTemplateID"`
	OccurrenceMonth string        `gorm:"size:7;index"`
	AmountCents     int64          `gorm:"not null"`
	OriginalAmountCents int64      `gorm:"not null;default:0"`
	OriginalCurrency string        `gorm:"size:3;not null;default:USD"`
	FXRate         float64         `gorm:"not null;default:1"`
	FXSource       string          `gorm:"size:120;not null;default:manual"`
	FXFetchedAt    time.Time       `gorm:"not null"`
	SplitMode       string         `gorm:"size:16;not null;default:equal"`
	PaidByUserID    uuid.UUID      `gorm:"type:uuid;not null;index"`
	PaidByUser      User           `gorm:"foreignKey:PaidByUserID"`
	OwnerUserID     *uuid.UUID     `gorm:"type:uuid;index"`
	OwnerUser       *User          `gorm:"foreignKey:OwnerUserID"`
	ArchivedByUserID *uuid.UUID    `gorm:"type:uuid;index"`
	ArchivedByUser  *User          `gorm:"foreignKey:ArchivedByUserID"`
	ArchivedAt      *time.Time     `gorm:"index"`
	CreatedByUserID uuid.UUID      `gorm:"type:uuid;not null;index"`
	CreatedByUser   User           `gorm:"foreignKey:CreatedByUserID"`
	IncurredOn      time.Time      `gorm:"type:date;not null;index"`
	Splits          []ExpenseSplit `gorm:"foreignKey:ExpenseID"`
	CreatedAt       time.Time      `gorm:"not null;index"`
	UpdatedAt       time.Time      `gorm:"not null"`
}

func (e *Expense) BeforeCreate(_ *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

type RecurringExpenseTemplate struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey"`
	GroupID            uuid.UUID `gorm:"type:uuid;not null;index"`
	Group              Group     `gorm:"foreignKey:GroupID"`
	Description        string    `gorm:"size:240;not null"`
	Category           string    `gorm:"size:32;index"`
	AmountCents        int64     `gorm:"not null"`
	OriginalAmountCents int64    `gorm:"not null;default:0"`
	OriginalCurrency   string    `gorm:"size:3;not null;default:USD"`
	FXRate             float64   `gorm:"not null;default:1"`
	FXSource           string    `gorm:"size:120;not null;default:manual"`
	FXFetchedAt        time.Time `gorm:"not null"`
	SplitMode          string    `gorm:"size:16;not null;default:equal"`
	OwnerUserID        uuid.UUID `gorm:"type:uuid;not null;index"`
	OwnerUser          User      `gorm:"foreignKey:OwnerUserID"`
	DueDayOfMonth      int       `gorm:"not null"`
	TimeZone           string    `gorm:"size:120;not null"`
	StartMonth         string    `gorm:"size:7;not null"`
	CreatedByUserID    uuid.UUID `gorm:"type:uuid;not null;index"`
	CreatedByUser      User      `gorm:"foreignKey:CreatedByUserID"`
	Splits             []RecurringExpenseSplit `gorm:"foreignKey:RecurringTemplateID"`
	CreatedAt          time.Time `gorm:"not null;index"`
	UpdatedAt          time.Time `gorm:"not null"`
}

func (r *RecurringExpenseTemplate) BeforeCreate(_ *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return nil
}

type RecurringExpenseSplit struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey"`
	RecurringTemplateID uuid.UUID `gorm:"type:uuid;not null;index"`
	RecurringTemplate  RecurringExpenseTemplate `gorm:"foreignKey:RecurringTemplateID"`
	UserID             uuid.UUID `gorm:"type:uuid;not null;index"`
	User               User      `gorm:"foreignKey:UserID"`
	AmountCents        int64     `gorm:"not null"`
	CreatedAt          time.Time `gorm:"not null"`
	UpdatedAt          time.Time `gorm:"not null"`
}

func (r *RecurringExpenseSplit) BeforeCreate(_ *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return nil
}

type Settlement struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	GroupID         uuid.UUID `gorm:"type:uuid;not null;index"`
	FromUserID      uuid.UUID `gorm:"type:uuid;not null;index"`
	FromUser        User      `gorm:"foreignKey:FromUserID"`
	ToUserID        uuid.UUID `gorm:"type:uuid;not null;index"`
	ToUser          User      `gorm:"foreignKey:ToUserID"`
	Kind            string    `gorm:"size:32;not null;default:direct_expense;index"`
	AmountCents     int64     `gorm:"not null"`
	OriginalAmountCents int64 `gorm:"not null;default:0"`
	OriginalCurrency string   `gorm:"size:3;not null;default:USD"`
	FXRate         float64    `gorm:"not null;default:1"`
	FXSource       string     `gorm:"size:120;not null;default:manual"`
	FXFetchedAt    time.Time  `gorm:"not null"`
	SettledOn       time.Time `gorm:"type:date;not null;index"`
	Allocations     []SettlementAllocation `gorm:"foreignKey:SettlementID"`
	Applications    []SettlementApplication `gorm:"foreignKey:SettlementID"`
	CreatedByUserID uuid.UUID `gorm:"type:uuid;not null;index"`
	CreatedByUser   User      `gorm:"foreignKey:CreatedByUserID"`
	CreatedAt       time.Time `gorm:"not null;index"`
	UpdatedAt       time.Time `gorm:"not null"`
}

func (s *Settlement) BeforeCreate(_ *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}

type SettlementAllocation struct {
	ID           uuid.UUID  `gorm:"type:uuid;primaryKey"`
	SettlementID uuid.UUID  `gorm:"type:uuid;not null;index"`
	Settlement   Settlement `gorm:"foreignKey:SettlementID"`
	ExpenseID    uuid.UUID  `gorm:"type:uuid;not null;index"`
	Expense      Expense    `gorm:"foreignKey:ExpenseID"`
	AmountCents  int64      `gorm:"not null"`
	CreatedAt    time.Time  `gorm:"not null"`
	UpdatedAt    time.Time  `gorm:"not null"`
}

func (a *SettlementAllocation) BeforeCreate(_ *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}

type Obligation struct {
	ID                 uuid.UUID   `gorm:"type:uuid;primaryKey"`
	GroupID            uuid.UUID   `gorm:"type:uuid;not null;index"`
	FromUserID         uuid.UUID   `gorm:"type:uuid;not null;index"`
	FromUser           User        `gorm:"foreignKey:FromUserID"`
	ToUserID           uuid.UUID   `gorm:"type:uuid;not null;index"`
	ToUser             User        `gorm:"foreignKey:ToUserID"`
	SourceType         string      `gorm:"size:32;not null;index"`
	SourceExpenseID    *uuid.UUID  `gorm:"type:uuid;index"`
	SourceExpense      *Expense    `gorm:"foreignKey:SourceExpenseID"`
	SourceSettlementID *uuid.UUID  `gorm:"type:uuid;index"`
	SourceSettlement   *Settlement `gorm:"foreignKey:SourceSettlementID"`
	AmountCents        int64       `gorm:"not null"`
	CreatedAt          time.Time   `gorm:"not null;index"`
	UpdatedAt          time.Time   `gorm:"not null"`
}

func (o *Obligation) BeforeCreate(_ *gorm.DB) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	return nil
}

type SettlementApplication struct {
	ID           uuid.UUID  `gorm:"type:uuid;primaryKey"`
	SettlementID uuid.UUID  `gorm:"type:uuid;not null;index"`
	Settlement   Settlement `gorm:"foreignKey:SettlementID"`
	ObligationID uuid.UUID  `gorm:"type:uuid;not null;index"`
	Obligation   Obligation `gorm:"foreignKey:ObligationID"`
	AmountCents  int64      `gorm:"not null"`
	CreatedAt    time.Time  `gorm:"not null"`
	UpdatedAt    time.Time  `gorm:"not null"`
}

func (a *SettlementApplication) BeforeCreate(_ *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}

type GroupMessage struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	GroupID   uuid.UUID `gorm:"type:uuid;not null;index"`
	Group     Group     `gorm:"foreignKey:GroupID"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index"`
	User      User      `gorm:"foreignKey:UserID"`
	Body      string    `gorm:"size:2000;not null"`
	CreatedAt time.Time `gorm:"not null;index"`
	UpdatedAt time.Time `gorm:"not null"`
}

func (m *GroupMessage) BeforeCreate(_ *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

type ExpenseSplit struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	ExpenseID   uuid.UUID `gorm:"type:uuid;not null;index"`
	UserID      uuid.UUID `gorm:"type:uuid;not null;index"`
	User        User      `gorm:"foreignKey:UserID"`
	AmountCents int64     `gorm:"not null"`
	CreatedAt   time.Time `gorm:"not null"`
	UpdatedAt   time.Time `gorm:"not null"`
}

func (s *ExpenseSplit) BeforeCreate(_ *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}
