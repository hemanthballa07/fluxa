package fraud

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/fluxa/fluxa/internal/domain"
	"github.com/fluxa/fluxa/internal/logging"
	"github.com/fluxa/fluxa/internal/mlfeatures"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// VelocityQuerier is a narrow DB interface so the engine does not depend on *db.Client directly.
type VelocityQuerier interface {
	CountRecentEvents(userID string, windowSeconds int) (int, error)
}

// Scorer returns an ML fraud score for an event's features. Implemented by the gRPC
// scorer client adapter; a no-op/fake is used when the scorer is unavailable or in tests.
type Scorer interface {
	Score(ctx context.Context, f mlfeatures.Features) (score float64, modelVersion string, err error)
}

// EvalQuerier is what EvaluateWithScorer needs: rules velocity + ML feature aggregates.
type EvalQuerier interface {
	VelocityQuerier
	mlfeatures.FeatureQuerier
}

// Engine evaluates fraud rules against an event.
type Engine struct {
	rules  domain.RulesConfig
	logger *logging.Logger
	// Tau is the ML blend threshold: an "ml_risk" flag is appended when score >= Tau.
	// Zero disables the ml_risk flag (the score is still computed and returned).
	Tau float64
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

// EvaluateWithScorer runs the rules (via Evaluate), then builds features and calls the
// ML scorer, blending: an "ml_risk" flag is appended when score >= Tau. Fail-open: any
// feature-build or scorer error returns the rules-only flags with modelVersion
// "unavailable" and score 0 — the fraud decision is never blocked by the scorer.
func (e *Engine) EvaluateWithScorer(ctx context.Context, event *domain.Event, q EvalQuerier, scorer Scorer) (flags []domain.FraudFlag, score float64, modelVersion string, err error) {
	flags, _ = e.Evaluate(event, q)
	if scorer == nil {
		return flags, 0, "unavailable", nil
	}
	f, ferr := mlfeatures.Build(ctx, event, q)
	if ferr != nil {
		e.logger.Error("ML feature build failed; rules-only", ferr, map[string]interface{}{"event_id": event.EventID})
		return flags, 0, "unavailable", nil
	}
	s, ver, serr := scorer.Score(ctx, f)
	if serr != nil {
		e.logger.Warn("ML scorer unavailable; rules-only", map[string]interface{}{"event_id": event.EventID, "error": serr.Error()})
		return flags, 0, "unavailable", nil
	}
	if e.Tau > 0 && s >= e.Tau {
		flags = append(flags, domain.FraudFlag{
			FlagID:    uuid.New().String(),
			EventID:   event.EventID,
			UserID:    event.UserID,
			RuleName:  "ml_risk",
			RuleValue: fmt.Sprintf("ml_score=%.4f >= tau=%.4f (model=%s)", s, e.Tau, ver),
			FlaggedAt: time.Now().UTC(),
		})
	}
	return flags, s, ver, nil
}
