package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds application configuration for all local services.
type Config struct {
	// Database
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string
	DBSSLMode  string

	// RabbitMQ
	RabbitMQURL string // amqp://user:pass@host:5672/

	// MinIO (S3-compatible object store)
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioBucket    string
	MinioUseSSL    bool

	// Fraud rules
	RulesFile string // path to rules.yaml

	// Replay service
	IngestURL  string
	CSVFile    string
	RatePerSec int

	// Application
	Environment string
	LogLevel    string
}

// LoadFromEnv loads configuration from environment variables.
func LoadFromEnv() (*Config, error) {
	cfg := &Config{
		DBHost:         getEnv("DB_HOST", ""),
		DBPort:         getEnv("DB_PORT", "5432"),
		DBName:         getEnv("DB_NAME", "fluxa"),
		DBUser:         getEnv("DB_USER", ""),
		DBPassword:     getEnv("DB_PASSWORD", ""),
		DBSSLMode:      getEnv("DB_SSL_MODE", "disable"),
		RabbitMQURL:    getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		MinioEndpoint:  getEnv("MINIO_ENDPOINT", "localhost:9000"),
		MinioAccessKey: getEnv("MINIO_ACCESS_KEY", "minioadmin"),
		MinioSecretKey: getEnv("MINIO_SECRET_KEY", "minioadmin123"),
		MinioBucket:    getEnv("MINIO_BUCKET", "fluxa-events"),
		MinioUseSSL:    getEnv("MINIO_USE_SSL", "false") == "true",
		RulesFile:      getEnv("RULES_FILE", "/app/rules.yaml"),
		IngestURL:      getEnv("INGEST_URL", "http://localhost:8080"),
		CSVFile:        getEnv("CSV_FILE", "/data/transactions.csv"),
		RatePerSec:     parseIntEnv("RATE_PER_SEC", 200),
		Environment:    getEnv("ENVIRONMENT", "local"),
		LogLevel:       getEnv("LOG_LEVEL", "info"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// Validate checks that required DB fields are present.
// Other fields have sensible defaults and are optional.
func (c *Config) Validate() error {
	if c.DBHost == "" {
		return fmt.Errorf("DB_HOST is required")
	}
	if c.DBUser == "" {
		return fmt.Errorf("DB_USER is required")
	}
	if c.DBPassword == "" {
		return fmt.Errorf("DB_PASSWORD is required")
	}
	return nil
}

// DSN returns the PostgreSQL connection string.
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

func parseIntEnv(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultValue
}
