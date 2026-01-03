package queue

import (
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/fluxa/fluxa/internal/models"
)

const (
	maxPayloadSizeBytes = 256 * 1024 // 256KB
)

// Client wraps SQS operations
type Client struct {
	sqsClient *sqs.SQS
	queueURL  string
}

// NewClient creates a new SQS client
func NewClient(queueURL string) (*Client, error) {
	sess, err := session.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	return &Client{
		sqsClient: sqs.New(sess),
		queueURL:  queueURL,
	}, nil
}

// SendEventMessage sends an event message to SQS
func (c *Client) SendEventMessage(msg *models.SQSEventMessage) error {
	bodyBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	input := &sqs.SendMessageInput{
		QueueUrl:    aws.String(c.queueURL),
		MessageBody: aws.String(string(bodyBytes)),
		MessageAttributes: map[string]*sqs.MessageAttributeValue{
			"correlation_id": {
				DataType:    aws.String("String"),
				StringValue: aws.String(msg.CorrelationID),
			},
			"event_id": {
				DataType:    aws.String("String"),
				StringValue: aws.String(msg.EventID),
			},
		},
	}

	_, err = c.sqsClient.SendMessage(input)
	if err != nil {
		return fmt.Errorf("failed to send message to SQS: %w", err)
	}

	return nil
}

// ShouldUseS3 checks if payload size exceeds threshold
func ShouldUseS3(payloadSize int) bool {
	return payloadSize > maxPayloadSizeBytes
}

// ReceiveMessages receives messages from SQS queue
func (c *Client) ReceiveMessages(maxMessages int64, visibilityTimeout int64) ([]*sqs.Message, error) {
	input := &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(c.queueURL),
		MaxNumberOfMessages: aws.Int64(maxMessages),
		VisibilityTimeout:   aws.Int64(visibilityTimeout),
		MessageAttributeNames: []*string{
			aws.String("All"),
		},
	}

	result, err := c.sqsClient.ReceiveMessage(input)
	if err != nil {
		return nil, fmt.Errorf("failed to receive messages: %w", err)
	}

	return result.Messages, nil
}

// DeleteMessage deletes a message from the queue
func (c *Client) DeleteMessage(receiptHandle string) error {
	input := &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(c.queueURL),
		ReceiptHandle: aws.String(receiptHandle),
	}

	_, err := c.sqsClient.DeleteMessage(input)
	if err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}

	return nil
}

// ParseSQSEventMessage parses an SQS message body into SQSEventMessage
func ParseSQSEventMessage(body string) (*models.SQSEventMessage, error) {
	var msg models.SQSEventMessage
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	if msg.EventID == "" {
		return nil, fmt.Errorf("event_id is required")
	}
	if msg.CorrelationID == "" {
		return nil, fmt.Errorf("correlation_id is required")
	}
	if msg.PayloadSHA256 == "" {
		return nil, fmt.Errorf("payload_sha256 is required")
	}

	return &msg, nil
}
