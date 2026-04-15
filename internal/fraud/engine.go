package fraud

import (
	"fmt"
	"os"
	"time"

	"github.com/fluxa/fluxa/internal/domain"
	"github.com/fluxa/fluxa/internal/logging"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// VelocityQuerier is a narrow DB interface so the engine does not depend on *db.Client directly.
type VelocityQuerier interface {
	CountRecentEvents(userID string, windowSeconds int) (int, error)
}

// Engine evaluates fraud rules against an event.
type Engine struct {
	rules  domain.RulesConfig
	logger *logging.Logger
}

// NewEngine reads rulesFilePath (YAML), parses it into RulesConfig, and returns an Engine.
func NewEngine(rulesFilePath string, logger *logging.Logger) (*Engine, error) {
	data, err := os.ReadFile(rulesFilePath)
	if err != nil {
		return nil, fmt.Errorf("fraud: read rules file %q: %w", rulesFilePath, err)
	}

	var rules domain.RulesConfig
	if err := yaml.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("fraud: parse rules file %q: %w", rulesFilePath, err)
	}

	logger.Info("Loaded fraud rules", map[string]interface{}{
		"amount_threshold":        rules.AmountThreshold,
		"velocity_window_seconds": rules.VelocityWindowSeconds,
		"velocity_max_count":      rules.VelocityMaxCount,
		"blocked_merchants":       len(rules.BlockedMerchants),
		"high_risk_currencies":    len(rules.HighRiskCurrencies),
	})

	return &Engine{rules: rules, logger: logger}, nil
}

// Evaluate runs all rules against event. All matching rules produce flags (not first-match).
// db is used only for the velocity check query.
func (e *Engine) Evaluate(event *domain.Event, db VelocityQuerier) ([]domain.FraudFlag, error) {
	now := time.Now().UTC()
	var flags []domain.FraudFlag

	// Rule 1: amount threshold
	if e.rules.AmountThreshold > 0 && event.Amount > e.rules.AmountThreshold {
		flags = append(flags, domain.FraudFlag{
			FlagID:    uuid.New().String(),
			EventID:   event.EventID,
			UserID:    event.UserID,
			RuleName:  "amount_threshold",
			RuleValue: fmt.Sprintf("amount=%.2f > threshold=%.2f", event.Amount, e.rules.AmountThreshold),
			FlaggedAt: now,
		})
	}

	// Rule 2: velocity check
	if e.rules.VelocityWindowSeconds > 0 && e.rules.VelocityMaxCount > 0 {
		count, err := db.CountRecentEvents(event.UserID, e.rules.VelocityWindowSeconds)
		if err != nil {
			// Log and skip this rule rather than failing the whole pipeline
			e.logger.Error("Velocity check query failed", err, map[string]interface{}{
				"user_id": event.UserID,
			})
		} else if count >= e.rules.VelocityMaxCount {
			flags = append(flags, domain.FraudFlag{
				FlagID:    uuid.New().String(),
				EventID:   event.EventID,
				UserID:    event.UserID,
				RuleName:  "velocity",
				RuleValue: fmt.Sprintf("count=%d in %ds >= max=%d", count, e.rules.VelocityWindowSeconds, e.rules.VelocityMaxCount),
				FlaggedAt: now,
			})
		}
	}

	// Rule 3: blocked merchant
	for _, blocked := range e.rules.BlockedMerchants {
		if event.Merchant == blocked {
			flags = append(flags, domain.FraudFlag{
				FlagID:    uuid.New().String(),
				EventID:   event.EventID,
				UserID:    event.UserID,
				RuleName:  "blocked_merchant",
				RuleValue: fmt.Sprintf("merchant=%q is blocked", event.Merchant),
				FlaggedAt: now,
			})
			break
		}
	}

	// Rule 4: high-risk currency
	for _, currency := range e.rules.HighRiskCurrencies {
		if event.Currency == currency {
			flags = append(flags, domain.FraudFlag{
				FlagID:    uuid.New().String(),
				EventID:   event.EventID,
				UserID:    event.UserID,
				RuleName:  "high_risk_currency",
				RuleValue: fmt.Sprintf("currency=%q is high-risk", event.Currency),
				FlaggedAt: now,
			})
			break
		}
	}

	return flags, nil
}
