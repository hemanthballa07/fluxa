package db

import (
	"fmt"
	"testing"
	"time"

	"github.com/fluxa/fluxa/internal/domain"
	_ "github.com/lib/pq"
)

func getTestDB(t *testing.T) *Client {
	t.Helper()
	dsn := "host=localhost port=5432 user=fluxa_user password=fluxa_password dbname=fluxa sslmode=disable"
	client, err := NewClient(dsn, 5)
	if err != nil {
		t.Skipf("Skipping integration test: failed to connect to DB: %v", err)
	}
	return client
}

// seedEventAndFlag inserts one event + one fraud flag with a controllable flagged_at.
func seedEventAndFlag(t *testing.T, client *Client, suffix string, amount float64, flaggedAt time.Time) *domain.FraudEvent {
	t.Helper()

	eventID := fmt.Sprintf("test-db-sse-%s", suffix)
	userID := "test-user-sse"
	merchant := "TestMerchant"
	currency := "USD"

	event := &domain.Event{
		EventID:   eventID,
		UserID:    userID,
		Amount:    amount,
		Currency:  currency,
		Merchant:  merchant,
		Timestamp: flaggedAt,
	}
	if err := client.InsertEvent(event, "corr-sse-"+suffix, domain.PayloadModeInline, nil); err != nil {
		t.Fatalf("seedEventAndFlag: InsertEvent failed: %v", err)
	}

	flagID := fmt.Sprintf("test-flag-sse-%s", suffix)
	flag := &domain.FraudFlag{
		FlagID:    flagID,
		EventID:   eventID,
		UserID:    userID,
		RuleName:  "amount_threshold",
		RuleValue: fmt.Sprintf("amount=%.2f > threshold=500.00", amount),
		FlaggedAt: flaggedAt,
	}
	if err := client.InsertFraudFlag(flag); err != nil {
		t.Fatalf("seedEventAndFlag: InsertFraudFlag failed: %v", err)
	}

	return &domain.FraudEvent{
		FlagID:    flagID,
		EventID:   eventID,
		UserID:    userID,
		Amount:    amount,
		Currency:  currency,
		Merchant:  merchant,
		RuleName:  flag.RuleName,
		RuleValue: flag.RuleValue,
		FlaggedAt: flaggedAt,
	}
}

func cleanupSSETestData(t *testing.T, client *Client) {
	t.Helper()
	_, _ = client.GetDB().Exec("DELETE FROM fraud_flags WHERE flag_id LIKE 'test-flag-sse-%'")
	_, _ = client.GetDB().Exec("DELETE FROM events WHERE event_id LIKE 'test-db-sse-%'")
}

func TestGetRecentFraudEvents_ReturnsNewestFirst(t *testing.T) {
	client := getTestDB(t)
	defer client.Close()
	cleanupSSETestData(t, client)

	base := time.Now().UTC().Truncate(time.Millisecond)
	older := seedEventAndFlag(t, client, "old", 600.00, base.Add(-10*time.Second))
	newer := seedEventAndFlag(t, client, "new", 1200.00, base.Add(-1*time.Second))

	events, err := client.GetRecentFraudEvents(10)
	if err != nil {
		t.Fatalf("GetRecentFraudEvents: %v", err)
	}

	idx := func(flagID string) int {
		for i, e := range events {
			if e.FlagID == flagID {
				return i
			}
		}
		return -1
	}

	iNewer := idx(newer.FlagID)
	iOlder := idx(older.FlagID)

	if iNewer == -1 || iOlder == -1 {
		t.Fatalf("Expected both seeded flags in result; newer=%d older=%d", iNewer, iOlder)
	}
	if iNewer >= iOlder {
		t.Errorf("Expected newer flag (idx %d) before older flag (idx %d) in DESC result", iNewer, iOlder)
	}
}

func TestGetRecentFraudEvents_RespectsLimit(t *testing.T) {
	client := getTestDB(t)
	defer client.Close()
	cleanupSSETestData(t, client)

	base := time.Now().UTC().Truncate(time.Millisecond)
	for i := 0; i < 5; i++ {
		seedEventAndFlag(t, client, fmt.Sprintf("lim%d", i), float64(600+i*100), base.Add(time.Duration(i)*time.Second))
	}

	events, err := client.GetRecentFraudEvents(3)
	if err != nil {
		t.Fatalf("GetRecentFraudEvents: %v", err)
	}
	if len(events) > 3 {
		t.Errorf("Expected at most 3 events with limit=3, got %d", len(events))
	}
}

func TestGetRecentFraudEvents_JoinsEventFields(t *testing.T) {
	client := getTestDB(t)
	defer client.Close()
	cleanupSSETestData(t, client)

	ts := time.Now().UTC().Truncate(time.Millisecond)
	want := seedEventAndFlag(t, client, "join", 750.00, ts)

	events, err := client.GetRecentFraudEvents(100)
	if err != nil {
		t.Fatalf("GetRecentFraudEvents: %v", err)
	}

	var got *domain.FraudEvent
	for _, e := range events {
		if e.FlagID == want.FlagID {
			got = e
			break
		}
	}
	if got == nil {
		t.Fatal("Seeded flag not found in result")
	}

	if got.Amount != want.Amount {
		t.Errorf("Amount: got %.2f, want %.2f", got.Amount, want.Amount)
	}
	if got.Currency != want.Currency {
		t.Errorf("Currency: got %q, want %q", got.Currency, want.Currency)
	}
	if got.Merchant != want.Merchant {
		t.Errorf("Merchant: got %q, want %q", got.Merchant, want.Merchant)
	}
	if got.RuleName != want.RuleName {
		t.Errorf("RuleName: got %q, want %q", got.RuleName, want.RuleName)
	}
	if got.UserID != want.UserID {
		t.Errorf("UserID: got %q, want %q", got.UserID, want.UserID)
	}
}

func TestGetFraudEventsSince_ReturnsOnlyNewer(t *testing.T) {
	client := getTestDB(t)
	defer client.Close()
	cleanupSSETestData(t, client)

	base := time.Now().UTC().Truncate(time.Millisecond)
	seedEventAndFlag(t, client, "since-before", 600.00, base.Add(-5*time.Second))
	after := seedEventAndFlag(t, client, "since-after", 700.00, base.Add(1*time.Second))

	events, err := client.GetFraudEventsSince(base)
	if err != nil {
		t.Fatalf("GetFraudEventsSince: %v", err)
	}

	foundBefore := false
	foundAfter := false
	for _, e := range events {
		switch e.FlagID {
		case "test-flag-sse-since-before":
			foundBefore = true
		case after.FlagID:
			foundAfter = true
		}
	}

	if foundBefore {
		t.Error("Expected flag from before 'since' to be excluded")
	}
	if !foundAfter {
		t.Error("Expected flag from after 'since' to be included")
	}
}

func TestGetFraudEventsSince_ReturnsOldestFirst(t *testing.T) {
	client := getTestDB(t)
	defer client.Close()
	cleanupSSETestData(t, client)

	base := time.Now().UTC().Truncate(time.Millisecond)
	older := seedEventAndFlag(t, client, "asc-old", 600.00, base.Add(1*time.Second))
	newer := seedEventAndFlag(t, client, "asc-new", 800.00, base.Add(3*time.Second))

	events, err := client.GetFraudEventsSince(base)
	if err != nil {
		t.Fatalf("GetFraudEventsSince: %v", err)
	}

	idx := func(flagID string) int {
		for i, e := range events {
			if e.FlagID == flagID {
				return i
			}
		}
		return -1
	}

	iOlder := idx(older.FlagID)
	iNewer := idx(newer.FlagID)

	if iOlder == -1 || iNewer == -1 {
		t.Fatalf("Expected both seeded flags in result; older=%d newer=%d", iOlder, iNewer)
	}
	if iOlder >= iNewer {
		t.Errorf("Expected older flag (idx %d) before newer flag (idx %d) in ASC result", iOlder, iNewer)
	}
}

func TestGetFraudEventsSince_EmptyWhenNoneNewer(t *testing.T) {
	client := getTestDB(t)
	defer client.Close()
	cleanupSSETestData(t, client)

	future := time.Now().UTC().Add(1 * time.Hour)
	events, err := client.GetFraudEventsSince(future)
	if err != nil {
		t.Fatalf("GetFraudEventsSince: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("Expected 0 events for future since, got %d", len(events))
	}
}

// seedEventTS inserts a labeled event with a controllable user_id + ts for the
// ML feature point-in-time aggregate tests (Task 2/3).
func seedEventTS(t *testing.T, c *Client, idSuffix, userID string, amount float64, ts time.Time) {
	t.Helper()
	ev := &domain.Event{
		EventID:   "mlfeat-" + idSuffix,
		UserID:    userID,
		Amount:    amount,
		Currency:  "USD",
		Merchant:  "TestMerchant",
		Timestamp: ts,
		Metadata:  map[string]interface{}{"is_fraud_ground_truth": "0"},
	}
	if err := c.InsertEvent(ev, "corr-mlfeat-"+idSuffix, domain.PayloadModeInline, nil); err != nil {
		t.Fatalf("seedEventTS InsertEvent: %v", err)
	}
}

func TestCountUserEventsAsOf(t *testing.T) {
	c := getTestDB(t)
	defer c.Close()
	base := time.Now().UTC().Truncate(time.Second)
	u := fmt.Sprintf("mlfeat-cnt-%d", base.UnixNano())
	seedEventTS(t, c, "c1-"+u, u, 10.0, base)
	seedEventTS(t, c, "c2-"+u, u, 20.0, base.Add(30*time.Second))
	seedEventTS(t, c, "c3-"+u, u, 30.0, base.Add(90*time.Second))

	// window=100s as-of base+90s => ts > base-10s => all three.
	if n, err := c.CountUserEventsAsOf(u, base.Add(90*time.Second), 100); err != nil {
		t.Fatalf("CountUserEventsAsOf(100): %v", err)
	} else if n != 3 {
		t.Errorf("window=100: expected 3, got %d", n)
	}
	// window=50s as-of base+90s => ts > base+40s => only c3.
	if n, err := c.CountUserEventsAsOf(u, base.Add(90*time.Second), 50); err != nil {
		t.Fatalf("CountUserEventsAsOf(50): %v", err)
	} else if n != 1 {
		t.Errorf("window=50: expected 1, got %d", n)
	}
}

func TestUserAmountStatsAsOf(t *testing.T) {
	c := getTestDB(t)
	defer c.Close()
	base := time.Now().UTC().Truncate(time.Second)
	u := fmt.Sprintf("mlfeat-amt-%d", base.UnixNano())
	seedEventTS(t, c, "a1-"+u, u, 100.0, base)
	seedEventTS(t, c, "a2-"+u, u, 300.0, base.Add(10*time.Second))

	sum, max, prevTs, err := c.UserAmountStatsAsOf(u, base.Add(10*time.Second), 3600)
	if err != nil {
		t.Fatalf("UserAmountStatsAsOf: %v", err)
	}
	if sum != 400.0 {
		t.Errorf("expected sum 400, got %v", sum)
	}
	if max != 300.0 {
		t.Errorf("expected max 300, got %v", max)
	}
	// prev event strictly before base+10s is a1 at base.
	if d := prevTs.Sub(base); d < -time.Second || d > time.Second {
		t.Errorf("expected prevTs ~base, got %v (base %v)", prevTs, base)
	}
}
