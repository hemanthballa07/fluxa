// Package scorer is the gRPC client adapter to the Python ML scorer service.
// It implements fraud.Scorer; the engine calls it best-effort (fail-open).
package scorer

import (
	"context"
	"time"

	scorerv1 "github.com/fluxa/fluxa/internal/grpc/scorer/v1"
	"github.com/fluxa/fluxa/internal/mlfeatures"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client wraps the generated ScorerClient with a per-call timeout.
type Client struct {
	conn    *grpc.ClientConn
	client  scorerv1.ScorerClient
	timeout time.Duration
}

// NewClient dials endpoint (e.g. "ml-scorer:9097") lazily. timeout bounds each Score call.
func NewClient(endpoint string, timeout time.Duration) (*Client, error) {
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, client: scorerv1.NewScorerClient(conn), timeout: timeout}, nil
}

// Close releases the underlying connection.
func (c *Client) Close() error { return c.conn.Close() }

// Score sends the features to the scorer service and returns (score, modelVersion, err).
// On any transport/timeout error the engine fails open to rules-only.
func (c *Client) Score(ctx context.Context, f mlfeatures.Features) (float64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.client.Score(ctx, &scorerv1.ScoreRequest{
		Amount:           f.Amount,
		Velocity_60S:     int32(f.VelocityCount60s),
		Velocity_3600S:   int32(f.VelocityCount3600s),
		Velocity_86400S:  int32(f.VelocityCount86400s),
		SecsSincePrev:    f.SecsSincePrevEvent,
		UserAmtSum_3600S: f.UserAmtSum3600s,
		UserAmtMax_3600S: f.UserAmtMax3600s,
		Currency:         f.Currency,
		ProductCode:      f.ProductCode,
		CardNetwork:      f.CardNetwork,
		Merchant:         f.Merchant,
		EmailDomain:      f.EmailDomain,
	})
	if err != nil {
		return 0, "", err
	}
	return resp.MlScore, resp.ModelVersion, nil
}
