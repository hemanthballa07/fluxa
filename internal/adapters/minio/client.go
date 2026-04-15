package minioadapter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client wraps MinIO operations and implements ports.Storage.
type Client struct {
	mc         *minio.Client
	bucketName string
}

// NewClient creates a MinIO client and ensures the bucket exists.
func NewClient(endpoint, accessKey, secretKey, bucketName string, useSSL bool) (*Client, error) {
	mc, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio: failed to create client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := mc.BucketExists(ctx, bucketName)
	if err != nil {
		return nil, fmt.Errorf("minio: failed to check bucket %q: %w", bucketName, err)
	}
	if !exists {
		if err := mc.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("minio: failed to create bucket %q: %w", bucketName, err)
		}
	}

	return &Client{mc: mc, bucketName: bucketName}, nil
}

// Put stores data at the given key (path within the bucket).
func (c *Client) Put(ctx context.Context, key string, data []byte) error {
	_, err := c.mc.PutObject(ctx, c.bucketName, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/json",
	})
	if err != nil {
		return fmt.Errorf("minio: put %q: %w", key, err)
	}
	return nil
}

// Get retrieves the object stored at key.
func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	obj, err := c.mc.GetObject(ctx, c.bucketName, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("minio: get %q: %w", key, err)
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("minio: read %q: %w", key, err)
	}
	return data, nil
}
