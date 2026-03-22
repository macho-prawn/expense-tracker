package app

import (
	"sort"

	"sharetab/service/internal/models"

	"github.com/google/uuid"
)

type MemberSnapshot struct {
	ID    uuid.UUID
	Name  string
	Email string
}

type MemberBalance struct {
	UserID      uuid.UUID `json:"userId"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	PaidCents   int64     `json:"paidCents"`
	OwesCents   int64     `json:"owesCents"`
	NetCents    int64     `json:"netCents"`
}

type TransferSuggestion struct {
	FromUserID  uuid.UUID `json:"fromUserId"`
	FromName    string    `json:"fromName"`
	ToUserID    uuid.UUID `json:"toUserId"`
	ToName      string    `json:"toName"`
	AmountCents int64     `json:"amountCents"`
}

func CalculateBalances(
	members []MemberSnapshot,
	expenses []models.Expense,
	settlements []models.Settlement,
) ([]MemberBalance, []TransferSuggestion) {
	summaries := make(map[uuid.UUID]*MemberBalance, len(members))
	for _, member := range members {
		memberCopy := member
		summaries[member.ID] = &MemberBalance{
			UserID: memberCopy.ID,
			Name:   memberCopy.Name,
			Email:  memberCopy.Email,
		}
	}

	for _, expense := range expenses {
		if payer, ok := summaries[expense.PaidByUserID]; ok {
			payer.PaidCents += expense.AmountCents
			payer.NetCents += expense.AmountCents
		}

		for _, split := range expense.Splits {
			if debtor, ok := summaries[split.UserID]; ok {
				debtor.OwesCents += split.AmountCents
				debtor.NetCents -= split.AmountCents
			}
		}
	}

	for _, settlement := range settlements {
		if payer, ok := summaries[settlement.FromUserID]; ok {
			payer.NetCents += settlement.AmountCents
		}
		if recipient, ok := summaries[settlement.ToUserID]; ok {
			recipient.NetCents -= settlement.AmountCents
		}
	}

	balances := make([]MemberBalance, 0, len(summaries))
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

	var creditors []ledgerEntry
	var debtors []ledgerEntry

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

	transfers := make([]TransferSuggestion, 0)
	creditorIndex := 0
	debtorIndex := 0

	for creditorIndex < len(creditors) && debtorIndex < len(debtors) {
		creditor := &creditors[creditorIndex]
		debtor := &debtors[debtorIndex]

		amount := minInt64(creditor.Amount, debtor.Amount)
		transfers = append(transfers, TransferSuggestion{
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

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
