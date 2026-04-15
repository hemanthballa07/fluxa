package ports

import "context"

// Storage abstracts object store operations (MinIO or S3-compatible).
type Storage interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
}
