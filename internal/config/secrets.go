package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
)

// GetSecretValue retrieves a secret value from AWS Secrets Manager
func GetSecretValue(secretARN string) (string, error) {
	sess, err := session.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create AWS session: %w", err)
	}

	svc := secretsmanager.New(sess)
	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretARN),
	}

	result, err := svc.GetSecretValue(input)
	if err != nil {
		return "", fmt.Errorf("failed to get secret value: %w", err)
	}

	// If SecretString is available, use it directly
	if result.SecretString != nil {
		return *result.SecretString, nil
	}

	// Otherwise, decode from SecretBinary
	return string(result.SecretBinary), nil
}

// LoadDBPasswordFromSecret loads DB password from Secrets Manager or environment variable
func LoadDBPasswordFromSecret(secretARN string) (string, error) {
	// Try environment variable first (for local development)
	if password := os.Getenv("DB_PASSWORD"); password != "" {
		return password, nil
	}

	// Fetch from Secrets Manager
	if secretARN == "" {
		return "", fmt.Errorf("DB_PASSWORD_SECRET_ARN not set")
	}

	// For Secrets Manager, the secret might be a JSON object or plain string
	secretValue, err := GetSecretValue(secretARN)
	if err != nil {
		return "", err
	}

	// Try to parse as JSON (in case it's stored as {"password": "value"})
	var secretJSON map[string]string
	if err := json.Unmarshal([]byte(secretValue), &secretJSON); err == nil {
		if password, ok := secretJSON["password"]; ok {
			return password, nil
		}
		if password, ok := secretJSON["DB_PASSWORD"]; ok {
			return password, nil
		}
	}

	// Otherwise, treat as plain string
	return secretValue, nil
}
