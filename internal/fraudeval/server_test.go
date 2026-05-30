package fraudeval

import (
	"context"
	"database/sql"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	prommetrics "github.com/fluxa/fluxa/internal/adapters/prometheus"
	"github.com/fluxa/fluxa/internal/db"
	"github.com/fluxa/fluxa/internal/fraud"
	fraudv1 "github.com/fluxa/fluxa/internal/grpc/fraud/v1"
	"github.com/fluxa/fluxa/internal/logging"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const bufSize = 1024 * 1024

// NewMetrics registers collectors on the global default Prometheus registry,
// so calling it twice in one process panics with "duplicate metrics collector
// registration". Tests in this package share one instance to stay collision-free.
var (
	sharedTestMetricsOnce  sync.Once
	sharedTestMetricsValue *prommetrics.Metrics
)

func sharedTestMetrics() *prommetrics.Metrics {
	sharedTestMetricsOnce.Do(func() {
		sharedTestMetricsValue = prommetrics.NewMetrics("fraud-grpc-test")
	})
	return sharedTestMetricsValue
}

func getTestDB(t *testing.T) (*sql.DB, *db.Client) {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set, skipping integration test")
	}

	dbConn, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := dbConn.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	client, err := db.NewClient(dsn, 5)
	if err != nil {
		t.Fatalf("db.NewClient: %v", err)
	}

	t.Cleanup(func() {
		bg := context.Background()
		if _, err := dbConn.ExecContext(bg, "DELETE FROM fraud_flags WHERE event_id LIKE 'test-grpc-%'"); err != nil {
			t.Logf("cleanup fraud_flags: %v", err)
		}
		if _, err := dbConn.ExecContext(bg, "DELETE FROM idempotency_keys WHERE event_id LIKE 'test-grpc-%'"); err != nil {
			t.Logf("cleanup idempotency_keys: %v", err)
		}
		if _, err := dbConn.ExecContext(bg, "DELETE FROM events WHERE event_id LIKE 'test-grpc-%'"); err != nil {
			t.Logf("cleanup events: %v", err)
		}
		_ = dbConn.Close()
		_ = client.Close()
	})

	return dbConn, client
}

func newTestServer(t *testing.T) (fraudv1.FraudEvalClient, *db.Client, *sql.DB, func()) {
	t.Helper()

	dbConn, dbClient := getTestDB(t)

	rulesPath, err := filepath.Abs("../../rules.yaml")
	if err != nil {
		t.Fatalf("rules path: %v", err)
	}
	logger := logging.NewLogger("fraud-grpc-test", "test")
	engine, err := fraud.NewEngine(rulesPath, logger)
	if err != nil {
		t.Fatalf("fraud.NewEngine: %v", err)
	}
	metrics := sharedTestMetrics()
	srv := NewServer(engine, dbClient, metrics, logger, "fluxa-rules-v1.0")

	lis := bufconn.Listen(bufSize)
	grpcServer := grpc.NewServer()
	fraudv1.RegisterFraudEvalServer(grpcServer, srv)
	go func() { _ = grpcServer.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}

	cleanup := func() {
		_ = conn.Close()
		grpcServer.Stop()
		_ = lis.Close()
	}

	return fraudv1.NewFraudEvalClient(conn), dbClient, dbConn, cleanup
}

func mkReq(eventID, merchant, currency string, amount float64) *fraudv1.EvaluateRequest {
	return &fraudv1.EvaluateRequest{
		EventId:         eventID,
		UserId:          "u-" + eventID,
		Amount:          amount,
		Currency:        currency,
		Merchant:        merchant,
		TransactionTime: timestamppb.New(time.Now().UTC().Add(-1 * time.Minute)),
	}
}

func TestEvaluateTransaction_Allow(t *testing.T) {
	client, _, dbConn, cleanup := newTestServer(t)
	defer cleanup()

	id := "test-grpc-allow-" + uuid.NewString()
	resp, err := client.EvaluateTransaction(context.Background(), mkReq(id, "Tiny Cafe", "USD", 100))
	if err != nil {
		t.Fatalf("EvaluateTransaction: %v", err)
	}
	if resp.GetDecision() != fraudv1.Decision_DECISION_ALLOW {
		t.Errorf("decision = %v, want DECISION_ALLOW", resp.GetDecision())
	}
	if len(resp.GetFlags()) != 0 {
		t.Errorf("flags = %d, want 0", len(resp.GetFlags()))
	}
	var count int
	if err := dbConn.QueryRow("SELECT COUNT(*) FROM events WHERE event_id=$1", id).Scan(&count); err != nil {
		t.Fatalf("query events: %v", err)
	}
	if count != 1 {
		t.Errorf("events row count = %d, want 1", count)
	}
}

func TestEvaluateTransaction_FlagAmountThreshold(t *testing.T) {
	client, _, dbConn, cleanup := newTestServer(t)
	defer cleanup()

	id := "test-grpc-amount-" + uuid.NewString()
	resp, err := client.EvaluateTransaction(context.Background(), mkReq(id, "Tiny Cafe", "USD", 15000))
	if err != nil {
		t.Fatalf("EvaluateTransaction: %v", err)
	}
	if resp.GetDecision() != fraudv1.Decision_DECISION_FLAG {
		t.Errorf("decision = %v, want DECISION_FLAG", resp.GetDecision())
	}
	if len(resp.GetFlags()) != 1 || resp.GetFlags()[0].GetRuleName() != "amount_threshold" {
		t.Errorf("flags = %+v, want one amount_threshold flag", resp.GetFlags())
	}
	var count int
	if err := dbConn.QueryRow("SELECT COUNT(*) FROM fraud_flags WHERE event_id=$1 AND rule_name='amount_threshold'", id).Scan(&count); err != nil {
		t.Fatalf("query fraud_flags: %v", err)
	}
	if count != 1 {
		t.Errorf("fraud_flags row count = %d, want 1", count)
	}
}

func TestEvaluateTransaction_FlagBlockedMerchant(t *testing.T) {
	client, _, _, cleanup := newTestServer(t)
	defer cleanup()

	id := "test-grpc-merchant-" + uuid.NewString()
	resp, err := client.EvaluateTransaction(context.Background(), mkReq(id, "Amazon Marketplace", "USD", 50))
	if err != nil {
		t.Fatalf("EvaluateTransaction: %v", err)
	}
	if len(resp.GetFlags()) != 1 || resp.GetFlags()[0].GetRuleName() != "blocked_merchant" {
		t.Errorf("flags = %+v, want one blocked_merchant flag", resp.GetFlags())
	}
}

func TestEvaluateTransaction_FlagHighRiskCurrency(t *testing.T) {
	client, _, _, cleanup := newTestServer(t)
	defer cleanup()

	id := "test-grpc-currency-" + uuid.NewString()
	resp, err := client.EvaluateTransaction(context.Background(), mkReq(id, "Tiny Cafe", "XMR", 50))
	if err != nil {
		t.Fatalf("EvaluateTransaction: %v", err)
	}
	if len(resp.GetFlags()) != 1 || resp.GetFlags()[0].GetRuleName() != "high_risk_currency" {
		t.Errorf("flags = %+v, want one high_risk_currency flag", resp.GetFlags())
	}
}

func TestEvaluateTransaction_MultipleFlags(t *testing.T) {
	client, _, _, cleanup := newTestServer(t)
	defer cleanup()

	id := "test-grpc-multi-" + uuid.NewString()
	resp, err := client.EvaluateTransaction(context.Background(), mkReq(id, "Amazon Marketplace", "USD", 15000))
	if err != nil {
		t.Fatalf("EvaluateTransaction: %v", err)
	}
	if len(resp.GetFlags()) != 2 {
		t.Errorf("flags = %d, want 2", len(resp.GetFlags()))
	}
	names := map[string]bool{}
	for _, f := range resp.GetFlags() {
		names[f.GetRuleName()] = true
	}
	if !names["amount_threshold"] || !names["blocked_merchant"] {
		t.Errorf("expected amount_threshold + blocked_merchant, got %v", names)
	}
}

func TestEvaluateTransaction_IdempotentEventInsert(t *testing.T) {
	client, _, dbConn, cleanup := newTestServer(t)
	defer cleanup()

	id := "test-grpc-idem-" + uuid.NewString()
	if _, err := client.EvaluateTransaction(context.Background(), mkReq(id, "Tiny Cafe", "USD", 100)); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := client.EvaluateTransaction(context.Background(), mkReq(id, "Tiny Cafe", "USD", 100)); err != nil {
		t.Fatalf("second call: %v", err)
	}
	var count int
	if err := dbConn.QueryRow("SELECT COUNT(*) FROM events WHERE event_id=$1", id).Scan(&count); err != nil {
		t.Fatalf("query events: %v", err)
	}
	if count != 1 {
		t.Errorf("events row count = %d, want 1", count)
	}
}

func TestEvaluateTransaction_InvalidArgument_EmptyEventID(t *testing.T) {
	client, _, _, cleanup := newTestServer(t)
	defer cleanup()

	req := mkReq("", "Tiny Cafe", "USD", 100)
	_, err := client.EvaluateTransaction(context.Background(), req)
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestEvaluateTransaction_InvalidArgument_EmptyUserID(t *testing.T) {
	client, _, _, cleanup := newTestServer(t)
	defer cleanup()

	req := mkReq("test-grpc-bad-user-"+uuid.NewString(), "Tiny Cafe", "USD", 100)
	req.UserId = ""
	_, err := client.EvaluateTransaction(context.Background(), req)
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestEvaluateTransaction_InvalidArgument_NegativeAmount(t *testing.T) {
	client, _, _, cleanup := newTestServer(t)
	defer cleanup()

	req := mkReq("test-grpc-neg-"+uuid.NewString(), "Tiny Cafe", "USD", -1)
	_, err := client.EvaluateTransaction(context.Background(), req)
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestEvaluateTransaction_InvalidArgument_NoTimestamp(t *testing.T) {
	client, _, _, cleanup := newTestServer(t)
	defer cleanup()

	req := mkReq("test-grpc-nots-"+uuid.NewString(), "Tiny Cafe", "USD", 100)
	req.TransactionTime = nil
	_, err := client.EvaluateTransaction(context.Background(), req)
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestEvaluateTransaction_LatencyAndEvaluatedBy(t *testing.T) {
	client, _, _, cleanup := newTestServer(t)
	defer cleanup()

	id := "test-grpc-meta-" + uuid.NewString()
	resp, err := client.EvaluateTransaction(context.Background(), mkReq(id, "Tiny Cafe", "USD", 100))
	if err != nil {
		t.Fatalf("EvaluateTransaction: %v", err)
	}
	if resp.GetLatencyMs() <= 0 || resp.GetLatencyMs() >= 1000 {
		t.Errorf("latency_ms = %f, want (0, 1000)", resp.GetLatencyMs())
	}
	if resp.GetEvaluatedBy() != "fluxa-rules-v1.0" {
		t.Errorf("evaluated_by = %q, want fluxa-rules-v1.0", resp.GetEvaluatedBy())
	}
}

func TestProtoToEventConversion(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	req := &fraudv1.EvaluateRequest{
		EventId:         "e1",
		UserId:          "u1",
		Amount:          12.5,
		Currency:        "USD",
		Merchant:        "acme",
		TransactionTime: timestamppb.New(now),
		Metadata:        map[string]string{"channel": "mobile"},
	}
	got := protoToEvent(req)
	if got.EventID != "e1" || got.UserID != "u1" || got.Amount != 12.5 || got.Currency != "USD" || got.Merchant != "acme" {
		t.Errorf("scalar fields wrong: %+v", got)
	}
	if !got.Timestamp.Equal(now) {
		t.Errorf("timestamp = %v, want %v", got.Timestamp, now)
	}
	if v, ok := got.Metadata["channel"]; !ok || v != "mobile" {
		t.Errorf("metadata channel = %v ok=%v, want mobile/true", v, ok)
	}

	req2 := &fraudv1.EvaluateRequest{EventId: "e2", TransactionTime: timestamppb.Now()}
	got2 := protoToEvent(req2)
	if got2.Currency != "" || got2.Metadata != nil {
		t.Errorf("empty req → got %+v", got2)
	}
}
