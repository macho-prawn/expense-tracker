package app

import (
	"testing"
	"time"

	"sharetab/service/internal/models"

	"github.com/google/uuid"
)

func TestCalculateBalancesCreatesSettlementSuggestions(t *testing.T) {
	aliceID := uuid.New()
	bobID := uuid.New()
	charlieID := uuid.New()

	members := []MemberSnapshot{
		{ID: aliceID, Name: "Alice", Email: "alice@example.com"},
		{ID: bobID, Name: "Bob", Email: "bob@example.com"},
		{ID: charlieID, Name: "Charlie", Email: "charlie@example.com"},
	}

	expenses := []models.Expense{
		{
			PaidByUserID: aliceID,
			AmountCents:  9000,
			IncurredOn:   time.Now(),
			Splits: []models.ExpenseSplit{
				{UserID: aliceID, AmountCents: 3000},
				{UserID: bobID, AmountCents: 3000},
				{UserID: charlieID, AmountCents: 3000},
			},
		},
		{
			PaidByUserID: bobID,
			AmountCents:  3000,
			IncurredOn:   time.Now(),
			Splits: []models.ExpenseSplit{
				{UserID: bobID, AmountCents: 1500},
				{UserID: charlieID, AmountCents: 1500},
			},
		},
	}

	balances, transfers := CalculateBalances(members, expenses, nil)
	if len(balances) != 3 {
		t.Fatalf("expected 3 balances, got %d", len(balances))
	}

	if len(transfers) != 2 {
		t.Fatalf("expected 2 transfer suggestions, got %d", len(transfers))
	}

	if transfers[0].ToUserID != aliceID {
		t.Fatalf("expected largest creditor to be Alice, got %s", transfers[0].ToUserID)
	}
}

func TestCalculateBalancesAccountsForSettlements(t *testing.T) {
	aliceID := uuid.New()
	bobID := uuid.New()

	members := []MemberSnapshot{
		{ID: aliceID, Name: "Alice", Email: "alice@example.com"},
		{ID: bobID, Name: "Bob", Email: "bob@example.com"},
	}

	expenses := []models.Expense{
		{
			PaidByUserID: aliceID,
			AmountCents:  6000,
			IncurredOn:   time.Now(),
			Splits: []models.ExpenseSplit{
				{UserID: aliceID, AmountCents: 3000},
				{UserID: bobID, AmountCents: 3000},
			},
		},
	}

	settlements := []models.Settlement{
		{
			FromUserID:  bobID,
			ToUserID:    aliceID,
			AmountCents: 1000,
			SettledOn:   time.Now(),
		},
	}

	balances, transfers := CalculateBalances(members, expenses, settlements)
	if len(transfers) != 1 {
		t.Fatalf("expected 1 transfer after settlement, got %d", len(transfers))
	}
	if transfers[0].FromUserID != bobID || transfers[0].ToUserID != aliceID {
		t.Fatalf("expected Bob to still owe Alice, got %+v", transfers[0])
	}
	if transfers[0].AmountCents != 2000 {
		t.Fatalf("expected remaining transfer of 2000 cents, got %d", transfers[0].AmountCents)
	}

	if balances[0].NetCents != 2000 || balances[1].NetCents != -2000 {
		t.Fatalf("unexpected balances after settlement: %+v", balances)
	}
}
