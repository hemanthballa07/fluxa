package storage

import (
	"bytes"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

// Client wraps S3 operations
type Client struct {
	s3Client  *s3.S3
	bucketName string
}

// NewClient creates a new S3 client
func NewClient(bucketName string) (*Client, error) {
	sess, err := session.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	return &Client{
		s3Client:   s3.New(sess),
		bucketName: bucketName,
	}, nil
}

// PutPayload stores a payload in S3 with key format: raw/YYYY-MM-DD/event_id.json
func (c *Client) PutPayload(eventID string, payload []byte) (string, error) {
	dateStr := time.Now().UTC().Format("2006-01-02")
	key := fmt.Sprintf("raw/%s/%s.json", dateStr, eventID)

	input := &s3.PutObjectInput{
		Bucket:      aws.String(c.bucketName),
		Key:         aws.String(key),
		Body:        bytes.NewReader(payload),
		ContentType: aws.String("application/json"),
	}

	_, err := c.s3Client.PutObject(input)
	if err != nil {
		return "", fmt.Errorf("failed to put object to S3: %w", err)
	}

	return key, nil
}

// GetPayload retrieves a payload from S3
func (c *Client) GetPayload(key string) ([]byte, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(c.bucketName),
		Key:    aws.String(key),
	}

	result, err := c.s3Client.GetObject(input)
	if err != nil {
		return nil, fmt.Errorf("failed to get object from S3: %w", err)
	}
	defer result.Body.Close()

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(result.Body); err != nil {
		return nil, fmt.Errorf("failed to read object body: %w", err)
	}

	return buf.Bytes(), nil
}


