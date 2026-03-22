package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type FXQuote struct {
	BaseAmountCents int64
	Rate            float64
	Source          string
	FetchedAt       time.Time
}

type CurrencyConverter interface {
	Convert(ctx context.Context, amountCents int64, fromCurrency, toCurrency string) (FXQuote, error)
}

type FXClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewFXClient(baseURL string, httpClient *http.Client) *FXClient {
	client := httpClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &FXClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: client,
	}
}

func (c *FXClient) Convert(ctx context.Context, amountCents int64, fromCurrency, toCurrency string) (FXQuote, error) {
	from := strings.ToUpper(strings.TrimSpace(fromCurrency))
	to := strings.ToUpper(strings.TrimSpace(toCurrency))
	now := time.Now().UTC()

	if amountCents <= 0 {
		return FXQuote{}, fmt.Errorf("amount must be greater than zero")
	}

	if from == "" || to == "" {
		return FXQuote{}, fmt.Errorf("currency codes are required")
	}

	if from == to {
		return FXQuote{
			BaseAmountCents: amountCents,
			Rate:            1,
			Source:          "identity",
			FetchedAt:       now,
		}, nil
	}

	amount := float64(amountCents) / 100
	requestURL, err := url.Parse(c.baseURL + "/latest")
	if err != nil {
		return FXQuote{}, fmt.Errorf("build fx request: %w", err)
	}

	query := requestURL.Query()
	query.Set("amount", fmt.Sprintf("%.2f", amount))
	query.Set("from", from)
	query.Set("to", to)
	requestURL.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return FXQuote{}, fmt.Errorf("create fx request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return FXQuote{}, fmt.Errorf("request fx quote: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return FXQuote{}, fmt.Errorf("fx quote request failed with status %d", response.StatusCode)
	}

	var payload struct {
		Date  string             `json:"date"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return FXQuote{}, fmt.Errorf("decode fx quote: %w", err)
	}

	convertedAmount, ok := payload.Rates[to]
	if !ok || convertedAmount <= 0 {
		return FXQuote{}, fmt.Errorf("fx quote missing %s rate", to)
	}

	rate := convertedAmount / amount
	if rate <= 0 {
		return FXQuote{}, fmt.Errorf("fx quote returned invalid rate")
	}

	if payload.Date != "" {
		parsedDate, err := time.Parse("2006-01-02", payload.Date)
		if err == nil {
			now = parsedDate.UTC()
		}
	}

	return FXQuote{
		BaseAmountCents: int64(math.Round(convertedAmount * 100)),
		Rate:            rate,
		Source:          "frankfurter",
		FetchedAt:       now,
	}, nil
}
