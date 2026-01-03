package config

import (
	"fmt"
	"os"
)

// Config holds application configuration
type Config struct {
	// SQS Configuration
	SQSQueueURL string
	SQSDLQURL   string

	// S3 Configuration
	S3BucketName string

	// Database Configuration
	DBHost              string
	DBPort              string
	DBName              string
	DBUser              string
	DBPassword          string
	DBPasswordSecretARN string
	DBSSLMode           string

	// SNS Configuration
	SNSTopicARN string

	// Application Configuration
	Environment string
	LogLevel    string
}

// LoadFromEnv loads configuration from environment variables
func LoadFromEnv() (*Config, error) {
	cfg := &Config{
		SQSQueueURL:         getEnv("SQS_QUEUE_URL", ""),
		SQSDLQURL:           getEnv("SQS_DLQ_URL", ""),
		S3BucketName:        getEnv("S3_BUCKET_NAME", ""),
		DBHost:              getEnv("DB_HOST", ""),
		DBPort:              getEnv("DB_PORT", "5432"),
		DBName:              getEnv("DB_NAME", "fluxa"),
		DBUser:              getEnv("DB_USER", ""),
		DBPassword:          getEnv("DB_PASSWORD", ""),
		DBPasswordSecretARN: getEnv("DB_PASSWORD_SECRET_ARN", ""),
		DBSSLMode:           getEnv("DB_SSL_MODE", "require"),
		SNSTopicARN:         getEnv("SNS_TOPIC_ARN", ""),
		Environment:         getEnv("ENVIRONMENT", "dev"),
		LogLevel:            getEnv("LOG_LEVEL", "info"),
	}

	// Load DB password from Secrets Manager if secret ARN is provided
	if cfg.DBPasswordSecretARN != "" && cfg.DBPassword == "" {
		password, err := LoadDBPasswordFromSecret(cfg.DBPasswordSecretARN)
		if err != nil {
			return nil, fmt.Errorf("failed to load DB password from secret: %w", err)
		}
		cfg.DBPassword = password
	}

	// Validate required fields
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// Validate checks that required configuration is present
func (c *Config) Validate() error {
	required := map[string]string{
		"SQS_QUEUE_URL":  c.SQSQueueURL,
		"S3_BUCKET_NAME": c.S3BucketName,
		"DB_HOST":        c.DBHost,
		"DB_USER":        c.DBUser,
	}

	// Password must be set either directly or via secret ARN
	if c.DBPassword == "" && c.DBPasswordSecretARN == "" {
		return fmt.Errorf("DB_PASSWORD or DB_PASSWORD_SECRET_ARN is required")
	}

	for name, value := range required {
		if value == "" {
			return fmt.Errorf("%s is required", name)
		}
	}

	return nil
}

// DSN returns the PostgreSQL connection string
func (c *Config) DSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
