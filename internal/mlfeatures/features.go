// Package mlfeatures is the single source of truth for ML fraud-scorer feature
// computation. The same Build() is used online (engine serving path) and by the
// offline batch export, which guarantees train/serve feature parity (no skew).
package mlfeatures

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/fluxa/fluxa/internal/domain"
)

// Aggregate window sizes (seconds). Single source of truth for online + export.
const (
	Win60s = 60
	Win1h  = 3600
	Win24h = 86400
)

// FeatureQuerier is the narrow DB surface the feature builder needs. *db.Client
// satisfies it; tests use a fake. Mirrors the engine's VelocityQuerier pattern.
type FeatureQuerier interface {
	CountUserEventsAsOf(userID string, asOf time.Time, windowSeconds int) (int, error)
	UserAmountStatsAsOf(userID string, asOf time.Time, windowSeconds int) (sum, max float64, prevTs time.Time, err error)
}

// Features is the ordered feature vector for one event. Field order matches Header().
type Features struct {
	Amount              float64
	VelocityCount60s    int
	VelocityCount3600s  int
	VelocityCount86400s int
	SecsSincePrevEvent  float64 // -1 when the user has no prior event
	UserAmtSum3600s     float64
	UserAmtMax3600s     float64
	Currency            string
	ProductCode         string
	CardNetwork         string
	Merchant            string
	EmailDomain         string
}

// Build computes the feature vector for one event using transaction-time,
// point-in-time aggregates (over ev.Timestamp), so values are reproducible
// offline (export) and online (serving).
func Build(ctx context.Context, ev *domain.Event, q FeatureQuerier) (Features, error) {
	_ = ctx
	f := Features{
		Amount:   ev.Amount,
		Currency: strings.ToLower(ev.Currency),
		Merchant: strings.ToLower(ev.Merchant),
	}
	var err error
	if f.VelocityCount60s, err = q.CountUserEventsAsOf(ev.UserID, ev.Timestamp, Win60s); err != nil {
		return f, err
	}
	if f.VelocityCount3600s, err = q.CountUserEventsAsOf(ev.UserID, ev.Timestamp, Win1h); err != nil {
		return f, err
	}
	if f.VelocityCount86400s, err = q.CountUserEventsAsOf(ev.UserID, ev.Timestamp, Win24h); err != nil {
		return f, err
	}
	sum, max, prevTs, err := q.UserAmountStatsAsOf(ev.UserID, ev.Timestamp, Win1h)
	if err != nil {
		return f, err
	}
	f.UserAmtSum3600s, f.UserAmtMax3600s = sum, max
	if prevTs.IsZero() {
		f.SecsSincePrevEvent = -1
	} else {
		f.SecsSincePrevEvent = ev.Timestamp.Sub(prevTs).Seconds()
	}
	f.ProductCode = metaStr(ev.Metadata, "product_code")
	f.CardNetwork = metaStr(ev.Metadata, "card_network")
	f.EmailDomain = metaStr(ev.Metadata, "email_domain")
	return f, nil
}

// metaStr safely reads a string value from Event.Metadata (map[string]interface{}).
func metaStr(m map[string]interface{}, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

// Header is the ordered raw-feature contract: the CSV export writes Header()+["label"],
// and the model's encoder maps these raw values to its encoded column order.
func Header() []string {
	return []string{
		"amount", "velocity_60s", "velocity_3600s", "velocity_86400s",
		"secs_since_prev", "user_amt_sum_3600s", "user_amt_max_3600s",
		"currency", "product_code", "card_network", "merchant", "email_domain",
	}
}

// Row formats the features (Header() order) plus the label as CSV string fields.
func (f Features) Row(label string) []string {
	ff := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return []string{
		ff(f.Amount),
		strconv.Itoa(f.VelocityCount60s),
		strconv.Itoa(f.VelocityCount3600s),
		strconv.Itoa(f.VelocityCount86400s),
		ff(f.SecsSincePrevEvent),
		ff(f.UserAmtSum3600s),
		ff(f.UserAmtMax3600s),
		f.Currency, f.ProductCode, f.CardNetwork, f.Merchant, f.EmailDomain,
		label,
	}
}
