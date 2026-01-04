package config

import (
	"os"
	"testing"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "valid config with password",
			cfg: &Config{
				SQSQueueURL:  "https://sqs.us-east-1.amazonaws.com/123456789/queue",
				S3BucketName: "test-bucket",
				DBHost:       "localhost",
				DBUser:       "user",
				DBPassword:   "password",
			},
			wantErr: false,
		},
		{
			name: "valid config with secret ARN",
			cfg: &Config{
				SQSQueueURL:         "https://sqs.us-east-1.amazonaws.com/123456789/queue",
				S3BucketName:        "test-bucket",
				DBHost:              "localhost",
				DBUser:              "user",
				DBPasswordSecretARN: "arn:aws:secretsmanager:us-east-1:123456789:secret:db-password",
			},
			wantErr: false,
		},
		{
			name: "missing SQS queue URL",
			cfg: &Config{
				S3BucketName: "test-bucket",
				DBHost:       "localhost",
				DBUser:       "user",
				DBPassword:   "password",
			},
			wantErr: true,
		},
		{
			name: "missing S3 bucket name",
			cfg: &Config{
				SQSQueueURL: "https://sqs.us-east-1.amazonaws.com/123456789/queue",
				DBHost:      "localhost",
				DBUser:      "user",
				DBPassword:  "password",
			},
			wantErr: true,
		},
		{
			name: "missing DB host",
			cfg: &Config{
				SQSQueueURL:  "https://sqs.us-east-1.amazonaws.com/123456789/queue",
				S3BucketName: "test-bucket",
				DBUser:       "user",
				DBPassword:   "password",
			},
			wantErr: true,
		},
		{
			name: "missing DB user",
			cfg: &Config{
				SQSQueueURL:  "https://sqs.us-east-1.amazonaws.com/123456789/queue",
				S3BucketName: "test-bucket",
				DBHost:       "localhost",
				DBPassword:   "password",
			},
			wantErr: true,
		},
		{
			name: "missing both password and secret ARN",
			cfg: &Config{
				SQSQueueURL:  "https://sqs.us-east-1.amazonaws.com/123456789/queue",
				S3BucketName: "test-bucket",
				DBHost:       "localhost",
				DBUser:       "user",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_DSN(t *testing.T) {
	cfg := &Config{
		DBHost:     "localhost",
		DBPort:     "5432",
		DBUser:     "testuser",
		DBPassword: "testpass",
		DBName:     "testdb",
		DBSSLMode:  "require",
	}

	dsn := cfg.DSN()
	expected := "host=localhost port=5432 user=testuser password=testpass dbname=testdb sslmode=require"

	if dsn != expected {
		t.Errorf("Config.DSN() = %v, want %v", dsn, expected)
	}
}

func TestLoadFromEnv(t *testing.T) {
	// Save original env vars
	origEnv := map[string]string{}
	envVars := []string{
		"SQS_QUEUE_URL",
		"S3_BUCKET_NAME",
		"DB_HOST",
		"DB_USER",
		"DB_PASSWORD",
	}

	for _, key := range envVars {
		origEnv[key] = os.Getenv(key)
		defer os.Setenv(key, origEnv[key])
	}

	// Set test env vars
	os.Setenv("SQS_QUEUE_URL", "https://sqs.us-east-1.amazonaws.com/123456789/queue")
	os.Setenv("S3_BUCKET_NAME", "test-bucket")
	os.Setenv("DB_HOST", "localhost")
	os.Setenv("DB_USER", "testuser")
	os.Setenv("DB_PASSWORD", "testpass")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	if cfg.SQSQueueURL != "https://sqs.us-east-1.amazonaws.com/123456789/queue" {
		t.Errorf("LoadFromEnv() SQSQueueURL = %v, want https://sqs.us-east-1.amazonaws.com/123456789/queue", cfg.SQSQueueURL)
	}

	if cfg.DBHost != "localhost" {
		t.Errorf("LoadFromEnv() DBHost = %v, want localhost", cfg.DBHost)
	}
}
