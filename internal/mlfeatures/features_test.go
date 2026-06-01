package mlfeatures

import (
	"context"
	"testing"
	"time"

	"github.com/fluxa/fluxa/internal/domain"
)

// fakeQ returns distinct counts per window so the test can assert correct wiring,
// and a fixed prev event 15s before the as-of timestamp.
type fakeQ struct{ noPrev bool }

func (q fakeQ) CountUserEventsAsOf(_ string, _ time.Time, windowSeconds int) (int, error) {
	switch windowSeconds {
	case Win60s:
		return 1, nil
	case Win1h:
		return 5, nil
	case Win24h:
		return 20, nil
	}
	return 0, nil
}

func (q fakeQ) UserAmountStatsAsOf(_ string, asOf time.Time, _ int) (float64, float64, time.Time, error) {
	if q.noPrev {
		return 0, 0, time.Time{}, nil
	}
	return 1234.5, 999.0, asOf.Add(-15 * time.Second), nil
}

func TestBuildMapsFeatures(t *testing.T) {
	ev := &domain.Event{
		UserID: "u9", Amount: 500.0, Currency: "USD", Merchant: "Amazon Marketplace",
		Timestamp: time.Unix(1_000_000, 0).UTC(),
		Metadata:  map[string]interface{}{"product_code": "W", "card_network": "visa", "email_domain": "gmail.com"},
	}
	f, err := Build(context.Background(), ev, fakeQ{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if f.Amount != 500.0 {
		t.Errorf("amount: got %v", f.Amount)
	}
	if f.VelocityCount60s != 1 || f.VelocityCount3600s != 5 || f.VelocityCount86400s != 20 {
		t.Errorf("velocity windows mis-wired: %+v", f)
	}
	if f.UserAmtSum3600s != 1234.5 || f.UserAmtMax3600s != 999.0 {
		t.Errorf("amount stats: %+v", f)
	}
	if f.SecsSincePrevEvent != 15 {
		t.Errorf("secs since prev: got %v want 15", f.SecsSincePrevEvent)
	}
	if f.Currency != "usd" || f.Merchant != "amazon marketplace" {
		t.Errorf("normalization: cur=%q merch=%q", f.Currency, f.Merchant)
	}
	if f.ProductCode != "W" || f.CardNetwork != "visa" || f.EmailDomain != "gmail.com" {
		t.Errorf("metadata extraction: %+v", f)
	}
}

func TestBuildNoPriorEventAndMissingMetadata(t *testing.T) {
	ev := &domain.Event{
		UserID: "u", Amount: 10, Currency: "usd", Merchant: "m",
		Timestamp: time.Unix(2_000_000, 0).UTC(), Metadata: nil,
	}
	f, err := Build(context.Background(), ev, fakeQ{noPrev: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if f.SecsSincePrevEvent != -1 {
		t.Errorf("no prior event: got %v want -1", f.SecsSincePrevEvent)
	}
	if f.ProductCode != "" || f.EmailDomain != "" {
		t.Errorf("missing metadata should yield empty strings: %+v", f)
	}
}

func TestHeaderRowAlignment(t *testing.T) {
	f := Features{Amount: 1, Currency: "usd", Merchant: "m"}
	row := f.Row("0")
	if len(row) != len(Header())+1 {
		t.Errorf("Row length %d != len(Header())+1 (%d)", len(row), len(Header())+1)
	}
}
