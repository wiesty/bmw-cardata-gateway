package bmw

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func syntheticCache() *Cache {
	cache := NewCache()
	value := "1234"
	stamp := "2000-01-01T00:00:00Z"
	cache.Set(mapVehicleData(map[string]TelematicEntry{"vehicle.vehicle.travelledDistance": {Value: &value, Timestamp: &stamp}}, map[string]any{"modelName": "Synthetic vehicle"}, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)))
	return cache
}

func TestFailedAndEmptyPollsPreserveCache(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{502, "gateway error"}, {200, `{"telematicData":{}}`}, {200, `{"telematicData":{"missing":{"value":null}}}`}} {
		t.Run(fmt.Sprint(tc.status, tc.body), func(t *testing.T) {
			client := testClient(t, func(*http.Request) (int, string) { return tc.status, tc.body })
			cache := syntheticCache()
			previous := cache.Get()
			if err := pollOnce(context.Background(), client, "TEST", "container", cache); err == nil {
				t.Fatal("empty/failed poll accepted")
			}
			if cache.Get() != previous {
				t.Fatal("successful cache was replaced after failed poll")
			}
		})
	}
}

func TestPartialUpdatesKeepEarlierValuesAndSourceTimestamps(t *testing.T) {
	client := testClient(t, func(*http.Request) (int, string) {
		return 200, `{"telematicData":{"vehicle.drivetrain.fuelSystem.level":{"value":"25","timestamp":"2000-01-02T00:00:00Z"}}}`
	})
	cache := syntheticCache()
	if err := pollOnce(context.Background(), client, "TEST", "container", cache); err != nil {
		t.Fatal(err)
	}
	if cache.Get().MileageKm != 1234 || *cache.Get().FuelLevelPct != 25 || *cache.Get().Telematics["vehicle.vehicle.travelledDistance"].Timestamp != "2000-01-01T00:00:00Z" {
		t.Fatal("partial poll lost cached values or their age")
	}
}

func TestPollingContinuesAcrossMultipleIntervals(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	requests := 0
	client := testClient(t, func(*http.Request) (int, string) {
		requests++
		if requests == 3 {
			cancel()
		}
		return 200, fmt.Sprintf(`{"telematicData":{"vehicle.vehicle.travelledDistance":{"value":"%d"}}}`, requests)
	})
	cache := syntheticCache()
	path := filepath.Join(t.TempDir(), "state.json")
	if err := saveState(path, &state{VIN: "TEST", ContainerID: "container", CachedData: cache.Get()}); err != nil {
		t.Fatal(err)
	}
	StartPoller(ctx, client, "TEST", "container", time.Millisecond, cache, path)
	if requests != 3 || cache.Get().MileageKm != 3 {
		t.Fatalf("polling stopped: requests=%d", requests)
	}
	saved, err := loadState(path)
	if err != nil || saved.Polling.LastAttempt == nil || saved.Polling.LastSuccess == nil || saved.Polling.NextPoll == nil {
		t.Fatalf("poll diagnostics were not persisted: %v", err)
	}
}

func TestRestartHonorsPersistedFailedAttemptSchedule(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	next := time.Now().Add(2 * time.Hour)
	cache := syntheticCache()
	path := filepath.Join(t.TempDir(), "state.json")
	if err := saveState(path, &state{VIN: "TEST", ContainerID: "container", CachedData: cache.Get(), Polling: PollStatus{NextPoll: &next, LastError: "synthetic upstream error", ConsecutiveFailures: 2}}); err != nil {
		t.Fatal(err)
	}
	client := testClient(t, func(*http.Request) (int, string) { t.Fatal("restart ignored saved cooldown"); return 500, "" })
	StartPoller(ctx, client, "TEST", "container", time.Hour, cache, path)
	if cache.PollStatus().NextPoll == nil || !cache.PollStatus().NextPoll.Equal(next) || cache.PollStatus().LastError == "" {
		t.Fatal("saved cooldown/error was lost")
	}
}

func TestFailedPollPersistsPauseAndRestartDoesNotRequestAgain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	requests := 0
	client := testClient(t, func(*http.Request) (int, string) { requests++; return 502, "synthetic upstream failure" })
	cache := syntheticCache()
	path := filepath.Join(t.TempDir(), "state.json")
	if err := saveState(path, &state{VIN: "TEST", ContainerID: "container", CachedData: cache.Get()}); err != nil {
		t.Fatal(err)
	}
	StartPoller(ctx, client, "TEST", "container", time.Hour, cache, path)
	saved, err := loadState(path)
	if err != nil || saved.Polling.LastError == "" || saved.Polling.ConsecutiveFailures != 1 || saved.Polling.NextPoll == nil || !saved.Polling.NextPoll.After(time.Now()) {
		t.Fatalf("failed poll did not persist its retry deadline: %+v, %v", saved, err)
	}
	if requests != 1 {
		t.Fatalf("failure caused %d immediate requests", requests)
	}
	restartCtx, stop := context.WithCancel(context.Background())
	stop()
	StartPoller(restartCtx, client, "TEST", "container", time.Hour, NewCache(), path)
	if requests != 1 {
		t.Fatal("restart bypassed the persisted retry deadline")
	}
}

func TestDiscoveryCooldownIsVisibleInRestoredPollStatus(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := testClient(t, func(*http.Request) (int, string) { t.Fatal("unexpected REST request during cooldown"); return 500, "" })
	client.budget.backoff = 5 * time.Minute
	now := time.Now()
	response := &http.Response{StatusCode: 502, Status: "502 Bad Gateway", Header: make(http.Header)}
	if err := client.budget.record(response, nil, now); err != nil {
		t.Fatal(err)
	}
	cache := syntheticCache()
	path := filepath.Join(t.TempDir(), "state.json")
	if err := saveState(path, &state{VIN: "TEST", ContainerID: "container", CachedData: cache.Get()}); err != nil {
		t.Fatal(err)
	}
	StartPoller(ctx, client, "TEST", "container", time.Hour, cache, path)
	if cache.PollStatus().State != "degraded" || cache.PollStatus().LastError == "" {
		t.Fatalf("startup outage not visible: %+v", cache.PollStatus())
	}
}
