package bmw

import (
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestBudgetSurvivesRestartAndResetsAtUTCMidnight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.json")
	now := time.Date(2000, 1, 1, 23, 0, 0, 0, time.UTC)
	budget := &requestBudget{path: path}
	for range dailyRequestLimit {
		if err := budget.reserve(now); err != nil {
			t.Fatal(err)
		}
	}
	restarted := &requestBudget{path: path}
	var paused *requestDeferred
	if err := restarted.reserve(now); !errors.As(err, &paused) || !paused.Until.Equal(nextUTCDay(now)) {
		t.Fatalf("expected saved quota pause, got %v", err)
	}
	if err := restarted.reserve(nextUTCDay(now)); err != nil {
		t.Fatalf("quota did not reset: %v", err)
	}
}

func TestSharedBudgetCapsConcurrentVehicleRequests(t *testing.T) {
	budget := &requestBudget{path: filepath.Join(t.TempDir(), "requests.json")}
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for range 80 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if budget.reserve(time.Now()) == nil {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != dailyRequestLimit {
		t.Fatalf("allowed %d requests", allowed.Load())
	}
}

func TestCooldownSurvivesRestartAndHonorsRetryAfter(t *testing.T) {
	now := time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		status int
		retry  string
		until  time.Time
	}{
		{"gateway", 502, "", now.Add(5 * time.Minute)},
		{"quota", 429, "", nextUTCDay(now)},
		{"seconds", 429, "7200", now.Add(2 * time.Hour)},
		{"date", 503, now.Add(3 * time.Hour).Format(http.TimeFormat), now.Add(3 * time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "requests.json")
			budget := &requestBudget{path: path}
			if err := budget.reserve(now); err != nil {
				t.Fatal(err)
			}
			response := &http.Response{StatusCode: tc.status, Header: make(http.Header)}
			response.Header.Set("Retry-After", tc.retry)
			if err := budget.record(response, nil, now); err != nil {
				t.Fatal(err)
			}
			restarted := &requestBudget{path: path}
			var paused *requestDeferred
			if err := restarted.check(now); !errors.As(err, &paused) || !paused.Until.Equal(tc.until) {
				t.Fatalf("unexpected cooldown: %v", err)
			}
		})
	}
}
