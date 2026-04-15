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
			name: "valid config",
			cfg: &Config{
				DBHost:     "localhost",
				DBUser:     "user",
				DBPassword: "password",
			},
			wantErr: false,
		},
		{
			name: "missing DB host",
			cfg: &Config{
				DBUser:     "user",
				DBPassword: "password",
			},
			wantErr: true,
		},
		{
			name: "missing DB user",
			cfg: &Config{
				DBHost:     "localhost",
				DBPassword: "password",
			},
			wantErr: true,
		},
		{
			name: "missing DB password",
			cfg: &Config{
				DBHost: "localhost",
				DBUser: "user",
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
	envVars := []string{"DB_HOST", "DB_USER", "DB_PASSWORD"}
	origEnv := map[string]string{}
	for _, key := range envVars {
		origEnv[key] = os.Getenv(key)
		defer os.Setenv(key, origEnv[key])
	}

	os.Setenv("DB_HOST", "localhost")
	os.Setenv("DB_USER", "testuser")
	os.Setenv("DB_PASSWORD", "testpass")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}

	if cfg.DBHost != "localhost" {
		t.Errorf("LoadFromEnv() DBHost = %v, want localhost", cfg.DBHost)
	}
	if cfg.DBUser != "testuser" {
		t.Errorf("LoadFromEnv() DBUser = %v, want testuser", cfg.DBUser)
	}
	if cfg.RabbitMQURL != "amqp://guest:guest@localhost:5672/" {
		t.Errorf("LoadFromEnv() RabbitMQURL = %v, want default amqp URL", cfg.RabbitMQURL)
	}
}
