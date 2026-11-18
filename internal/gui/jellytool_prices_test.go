package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// pricesFrom points the price reads at a fake site that answers each model
// after hold, and counts the asks per model.
func pricesFrom(t *testing.T, hold func() time.Duration) (asked func(string) int) {
	t.Helper()
	var mu sync.Mutex
	n := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ModelCode string }
		json.NewDecoder(r.Body).Decode(&in)
		mu.Lock()
		n[in.ModelCode]++
		mu.Unlock()
		select {
		case <-time.After(hold()):
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{"prices":[{"usageField":"inputTokens","priceUnit":"PER_1K_TOKENS","discountUnitPrice":0.001},{"usageField":"outputTokens","priceUnit":"PER_1K_TOKENS","discountUnitPrice":0.002}]}}`)
	}))
	t.Cleanup(srv.Close)
	url, first := jellytoolPriceURL, jellytoolPriceFirstWait
	t.Cleanup(func() {
		jellytoolPriceURL, jellytoolPriceFirstWait = url, first
		waitPrices(t)
		jellytoolPrices.Lock()
		jellytoolPrices.at, jellytoolPrices.data = time.Time{}, map[string]jellytoolPrice{}
		jellytoolPrices.Unlock()
	})
	jellytoolPriceURL, jellytoolPriceFirstWait = srv.URL, 5*time.Second
	jellytoolPrices.Lock()
	jellytoolPrices.at, jellytoolPrices.data = time.Time{}, map[string]jellytoolPrice{}
	jellytoolPrices.Unlock()
	return func(id string) int { mu.Lock(); defer mu.Unlock(); return n[id] }
}

// waitPrices waits for the read in flight, if any, to end.
func waitPrices(t *testing.T) {
	t.Helper()
	jellytoolPrices.Lock()
	r := jellytoolPrices.reading
	jellytoolPrices.Unlock()
	if r == nil {
		return
	}
	select {
	case <-r:
	case <-time.After(time.Minute):
		t.Fatal("the price read never ended")
	}
}

// Asks that come together share one read, each model asked once.
func TestJellytoolPricesOneReadAtATime(t *testing.T) {
	asked := pricesFrom(t, func() time.Duration { return 50 * time.Millisecond })
	ids := []string{"gpt-a", "kimi-b"}
	var wg sync.WaitGroup
	got := make([]map[string]jellytoolPrice, 6)
	for i := range got {
		wg.Add(1)
		go func() { defer wg.Done(); got[i] = jellytoolPriceData(context.Background(), ids) }()
	}
	wg.Wait()
	waitPrices(t)
	for _, id := range ids {
		if n := asked(id); n != 1 {
			t.Errorf("%s asked %d times", id, n)
		}
	}
	if p := jellytoolPriceData(context.Background(), ids)["gpt-a"]; p.Input != 1 || p.Output != 2 {
		t.Fatalf("prices kept: %+v", p)
	}
}

// A page closed before the prices came back leaves the read going on, and
// what it reads is kept; nothing half read is kept as fresh.
func TestJellytoolPricesOutliveTheRequest(t *testing.T) {
	pricesFrom(t, func() time.Duration { return 200 * time.Millisecond })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	jellytoolPriceData(ctx, []string{"gpt-a"})
	if d := time.Since(start); d > time.Second {
		t.Fatalf("a closed page waited %v", d)
	}
	waitPrices(t)
	jellytoolPrices.Lock()
	p := jellytoolPrices.data["gpt-a"]
	jellytoolPrices.Unlock()
	if p.Input != 1 {
		t.Fatalf("the read for a closed page kept nothing: %+v", p)
	}
}

// Prices kept but stale answer at once, and are read again behind.
func TestJellytoolStalePricesAnswerAtOnce(t *testing.T) {
	slow := false
	var mu sync.Mutex
	asked := pricesFrom(t, func() time.Duration {
		mu.Lock()
		defer mu.Unlock()
		if slow {
			return 2 * time.Second
		}
		return 0
	})
	jellytoolPriceData(context.Background(), []string{"gpt-a"})
	waitPrices(t)
	mu.Lock()
	slow = true
	mu.Unlock()
	jellytoolPrices.Lock()
	jellytoolPrices.at = time.Now().Add(-time.Hour)
	jellytoolPrices.Unlock()
	start := time.Now()
	got := jellytoolPriceData(context.Background(), []string{"gpt-a"})
	if d := time.Since(start); d > time.Second || got["gpt-a"].Input != 1 {
		t.Fatalf("stale prices took %v: %+v", d, got)
	}
	waitPrices(t)
	if n := asked("gpt-a"); n != 2 {
		t.Fatalf("stale prices were read %d times, want 2", n)
	}
}
