package domain

// RulesConfig maps exactly to rules.yaml.
// All fields have zero values that disable the corresponding rule if not set.
type RulesConfig struct {
	AmountThreshold       float64  `yaml:"amount_threshold"`
	VelocityWindowSeconds int      `yaml:"velocity_window_seconds"`
	VelocityMaxCount      int      `yaml:"velocity_max_count"`
	BlockedMerchants      []string `yaml:"blocked_merchants"`
	HighRiskCurrencies    []string `yaml:"high_risk_currencies"`
}
