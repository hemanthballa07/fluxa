// Command export-features iterates labeled events and writes a CSV of ML features
// + label, using the SAME mlfeatures.Build as the online serving path (train/serve
// parity, H2). Output rows preserve ts order so the trainer's temporal split is valid.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/fluxa/fluxa/internal/db"
	"github.com/fluxa/fluxa/internal/domain"
	"github.com/fluxa/fluxa/internal/mlfeatures"
)

const (
	workers    = 12
	outPath    = "ml/data/features.csv"
	defaultDSN = "host=localhost port=5432 user=fluxa_user password=fluxa_password dbname=fluxa sslmode=disable"
)

type eventRow struct {
	ev    domain.Event
	label string
}

func main() {
	dsn := os.Getenv("EXPORT_DSN")
	if dsn == "" {
		dsn = defaultDSN
	}
	client, err := db.NewClient(dsn, workers+4)
	if err != nil {
		fatalf("connect: %v", err)
	}
	defer client.Close()

	// 1. Load all labeled events in ts order (kept in memory; ~216k small rows).
	rows, err := client.GetDB().Query(
		`SELECT user_id, amount, currency, merchant, ts, metadata_json
		   FROM events
		  WHERE metadata_json ->> 'is_fraud_ground_truth' IS NOT NULL
		  ORDER BY ts`)
	if err != nil {
		fatalf("query events: %v", err)
	}
	var events []eventRow
	for rows.Next() {
		var (
			userID, currency, merchant string
			amount                     float64
			ts                         time.Time
			metaRaw                    []byte
		)
		if err := rows.Scan(&userID, &amount, &currency, &merchant, &ts, &metaRaw); err != nil {
			fatalf("scan: %v", err)
		}
		meta := map[string]interface{}{}
		_ = json.Unmarshal(metaRaw, &meta)
		label, _ := meta["is_fraud_ground_truth"].(string)
		if label == "" {
			label = "0"
		}
		events = append(events, eventRow{
			ev: domain.Event{
				UserID: userID, Amount: amount, Currency: currency,
				Merchant: merchant, Timestamp: ts, Metadata: meta,
			},
			label: label,
		})
	}
	if err := rows.Err(); err != nil {
		fatalf("rows: %v", err)
	}
	rows.Close()
	fmt.Printf("loaded %d labeled events; building features with %d workers...\n", len(events), workers)

	// 2. Build features concurrently; results[i] preserves ts order.
	results := make([][]string, len(events))
	var wg sync.WaitGroup
	jobs := make(chan int, workers*4)
	var built int64
	var mu sync.Mutex
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				f, err := mlfeatures.Build(context.Background(), &events[i].ev, client)
				if err != nil {
					fatalf("build idx %d: %v", i, err)
				}
				results[i] = f.Row(events[i].label)
				mu.Lock()
				built++
				if built%20000 == 0 {
					fmt.Printf("  built %d/%d\n", built, len(events))
				}
				mu.Unlock()
			}
		}()
	}
	for i := range events {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	// 3. Write CSV (header + ts-ordered rows).
	if err := os.MkdirAll("ml/data", 0o755); err != nil {
		fatalf("mkdir: %v", err)
	}
	f, err := os.Create(outPath)
	if err != nil {
		fatalf("create %s: %v", outPath, err)
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	if err := cw.Write(append(mlfeatures.Header(), "label")); err != nil {
		fatalf("write header: %v", err)
	}
	for _, r := range results {
		if err := cw.Write(r); err != nil {
			fatalf("write row: %v", err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		fatalf("flush: %v", err)
	}
	fmt.Printf("wrote %d rows to %s\n", len(results), outPath)
}

func fatalf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
