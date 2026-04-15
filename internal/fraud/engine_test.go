package fraud

import (
	"errors"
	"testing"
	"time"

	"github.com/fluxa/fluxa/internal/domain"
	"github.com/fluxa/fluxa/internal/logging"
)

// mockQuerier implements VelocityQuerier without a real database.
type mockQuerier struct {
	count int
	err   error
}

func (m *mockQuerier) CountRecentEvents(_ string, _ int) (int, error) {
	return m.count, m.err
}

func newTestEngine(rules domain.RulesConfig) *Engine {
	return &Engine{
		rules:  rules,
		logger: logging.NewLogger("test", "test"),
	}
}

func baseEvent() *domain.Event {
	return &domain.Event{
		EventID:   "evt-001",
		UserID:    "user-001",
		Amount:    100.00,
		Currency:  "USD",
		Merchant:  "acme",
		Timestamp: time.Now(),
	}
}

func flagNames(flags []domain.FraudFlag) []string {
	names := make([]string, len(flags))
	for i, f := range flags {
		names[i] = f.RuleName
	}
	return names
}

func containsRule(flags []domain.FraudFlag, rule string) bool {
	for _, f := range flags {
		if f.RuleName == rule {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Rule 1: amount_threshold
// ---------------------------------------------------------------------------

func TestAmountThreshold(t *testing.T) {
	rules := domain.RulesConfig{AmountThreshold: 10000.00}
	engine := newTestEngine(rules)
	noopDB := &mockQuerier{}

	tests := []struct {
		name    string
		amount  float64
		wantHit bool
	}{
		{"above threshold fires", 15000.00, true},
		{"equal to threshold does not fire", 10000.00, false},
		{"below threshold does not fire", 5000.00, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			evt := baseEvent()
			evt.Amount = tc.amount
			flags, err := engine.Evaluate(evt, noopDB)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := containsRule(flags, "amount_threshold")
			if got != tc.wantHit {
				t.Errorf("amount=%.2f: got hit=%v, want hit=%v", tc.amount, got, tc.wantHit)
			}
		})
	}
}

func TestAmountThresholdDisabled(t *testing.T) {
	engine := newTestEngine(domain.RulesConfig{AmountThreshold: 0})
	evt := baseEvent()
	evt.Amount = 999999.00
	flags, err := engine.Evaluate(evt, &mockQuerier{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsRule(flags, "amount_threshold") {
		t.Error("amount_threshold fired but rule is disabled (threshold=0)")
	}
}

// ---------------------------------------------------------------------------
// Rule 2: velocity
// ---------------------------------------------------------------------------

func TestVelocity(t *testing.T) {
	rules := domain.RulesConfig{VelocityWindowSeconds: 60, VelocityMaxCount: 5}
	engine := newTestEngine(rules)

	tests := []struct {
		name    string
		count   int
		wantHit bool
	}{
		{"count equals max fires", 5, true},
		{"count exceeds max fires", 10, true},
		{"count below max does not fire", 4, false},
		{"zero count does not fire", 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flags, err := engine.Evaluate(baseEvent(), &mockQuerier{count: tc.count})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := containsRule(flags, "velocity")
			if got != tc.wantHit {
				t.Errorf("count=%d: got hit=%v, want hit=%v", tc.count, got, tc.wantHit)
			}
		})
	}
}

func TestVelocityDBError(t *testing.T) {
	engine := newTestEngine(domain.RulesConfig{VelocityWindowSeconds: 60, VelocityMaxCount: 5})
	db := &mockQuerier{err: errors.New("db unavailable")}
	flags, err := engine.Evaluate(baseEvent(), db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsRule(flags, "velocity") {
		t.Error("velocity fired on DB error; engine should log and skip")
	}
}

func TestVelocityDisabled(t *testing.T) {
	engine := newTestEngine(domain.RulesConfig{VelocityWindowSeconds: 0, VelocityMaxCount: 0})
	flags, err := engine.Evaluate(baseEvent(), &mockQuerier{count: 9999})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsRule(flags, "velocity") {
		t.Error("velocity fired but rule is disabled (window/max both 0)")
	}
}

// ---------------------------------------------------------------------------
// Rule 3: blocked_merchant
// ---------------------------------------------------------------------------

func TestBlockedMerchant(t *testing.T) {
	rules := domain.RulesConfig{BlockedMerchants: []string{"bad-corp", "sketchy-mart"}}
	engine := newTestEngine(rules)
	noopDB := &mockQuerier{}

	tests := []struct {
		name     string
		merchant string
		wantHit  bool
	}{
		{"exact match first entry fires", "bad-corp", true},
		{"exact match second entry fires", "sketchy-mart", true},
		{"unblocked merchant does not fire", "acme", false},
		{"partial match does not fire", "bad-corp-extra", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			evt := baseEvent()
			evt.Merchant = tc.merchant
			flags, err := engine.Evaluate(evt, noopDB)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := containsRule(flags, "blocked_merchant")
			if got != tc.wantHit {
				t.Errorf("merchant=%q: got hit=%v, want hit=%v", tc.merchant, got, tc.wantHit)
			}
		})
	}
}

func TestBlockedMerchantEmptyList(t *testing.T) {
	engine := newTestEngine(domain.RulesConfig{})
	evt := baseEvent()
	evt.Merchant = "anything"
	flags, err := engine.Evaluate(evt, &mockQuerier{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsRule(flags, "blocked_merchant") {
		t.Error("blocked_merchant fired with empty blocked list")
	}
}

// ---------------------------------------------------------------------------
// Rule 4: high_risk_currency
// ---------------------------------------------------------------------------

func TestHighRiskCurrency(t *testing.T) {
	rules := domain.RulesConfig{HighRiskCurrencies: []string{"XMR", "ZEC"}}
	engine := newTestEngine(rules)
	noopDB := &mockQuerier{}

	tests := []struct {
		name     string
		currency string
		wantHit  bool
	}{
		{"XMR fires", "XMR", true},
		{"ZEC fires", "ZEC", true},
		{"USD does not fire", "USD", false},
		{"lowercase xmr does not fire (case-sensitive)", "xmr", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			evt := baseEvent()
			evt.Currency = tc.currency
			flags, err := engine.Evaluate(evt, noopDB)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := containsRule(flags, "high_risk_currency")
			if got != tc.wantHit {
				t.Errorf("currency=%q: got hit=%v, want hit=%v", tc.currency, got, tc.wantHit)
			}
		})
	}
}

func TestHighRiskCurrencyEmptyList(t *testing.T) {
	engine := newTestEngine(domain.RulesConfig{})
	evt := baseEvent()
	evt.Currency = "XMR"
	flags, err := engine.Evaluate(evt, &mockQuerier{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsRule(flags, "high_risk_currency") {
		t.Error("high_risk_currency fired with empty high-risk list")
	}
}

// ---------------------------------------------------------------------------
// Multiple rules firing simultaneously
// ---------------------------------------------------------------------------

func TestMultipleRulesFire(t *testing.T) {
	engine := newTestEngine(domain.RulesConfig{
		AmountThreshold:       10000.00,
		VelocityWindowSeconds: 60,
		VelocityMaxCount:      5,
		BlockedMerchants:      []string{"bad-corp"},
		HighRiskCurrencies:    []string{"XMR"},
	})

	evt := &domain.Event{
		EventID:   "evt-multi",
		UserID:    "user-001",
		Amount:    15000.00,
		Currency:  "XMR",
		Merchant:  "bad-corp",
		Timestamp: time.Now(),
	}

	flags, err := engine.Evaluate(evt, &mockQuerier{count: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(flags) != 4 {
		t.Errorf("expected 4 flags, got %d: %v", len(flags), flagNames(flags))
	}
	for _, rule := range []string{"amount_threshold", "velocity", "blocked_merchant", "high_risk_currency"} {
		if !containsRule(flags, rule) {
			t.Errorf("expected rule %q to fire but it did not", rule)
		}
	}
}

func TestFlagFieldsPopulated(t *testing.T) {
	engine := newTestEngine(domain.RulesConfig{AmountThreshold: 10000.00})
	evt := baseEvent()
	evt.Amount = 20000.00

	flags, err := engine.Evaluate(evt, &mockQuerier{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(flags) != 1 {
		t.Fatalf("expected 1 flag, got %d", len(flags))
	}
	f := flags[0]
	if f.FlagID == "" {
		t.Error("FlagID is empty")
	}
	if f.EventID != evt.EventID {
		t.Errorf("EventID: got %q, want %q", f.EventID, evt.EventID)
	}
	if f.UserID != evt.UserID {
		t.Errorf("UserID: got %q, want %q", f.UserID, evt.UserID)
	}
	if f.FlaggedAt.IsZero() {
		t.Error("FlaggedAt is zero")
	}
}
