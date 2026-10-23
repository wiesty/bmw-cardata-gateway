package bmw

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func savedTestVehicle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := saveState(filepath.Join(dir, "state.json"), &state{
		VIN: "TEST", ContainerID: "container", ContainerName: containerName,
		LastPollTime: time.Now().Add(-2 * time.Hour), CachedData: &VehicleData{MileageKm: 1234, BasicData: map[string]any{"modelName": "Synthetic vehicle"}},
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBootstrap502UsesSavedVehicle(t *testing.T) {
	dir := savedTestVehicle(t)
	requests := 0
	client := testClient(t, func(r *http.Request) (int, string) {
		requests++
		if r.URL.Path != "/customers/vehicles/mappings" {
			t.Fatalf("unexpected request %s", r.URL)
		}
		return http.StatusBadGateway, "upstream failure"
	})
	vins, containerID, err := BootstrapMulti(context.Background(), client, dir, nil)
	if err != nil || len(vins) != 1 || vins[0] != "TEST" || containerID != "container" || requests != 1 {
		t.Fatalf("bootstrap = %v, %q, %v; requests=%d", vins, containerID, err, requests)
	}
	cache := NewCache()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	StartPoller(ctx, client, "TEST", containerID, time.Hour, cache, filepath.Join(dir, "state.json"))
	if cache.Get() == nil || cache.Get().MileageKm != 1234 {
		t.Fatal("older cached telemetry was not restored")
	}
}

func TestBootstrapDoesNotHidePermanentDiscoveryErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{403, "forbidden"}, {401, "unauthorized"}, {200, `[]`}, {200, `[{"vin":"OTHER","mappingType":"SECONDARY"}]`}} {
		t.Run(fmt.Sprint(tc.status, tc.body), func(t *testing.T) {
			client := testClient(t, func(*http.Request) (int, string) { return tc.status, tc.body })
			if _, _, err := BootstrapMulti(context.Background(), client, savedTestVehicle(t), nil); err == nil {
				t.Fatal("permanent discovery failure fell back to cached vehicle")
			}
		})
	}
}

func TestBootstrapRetries502WithoutSavedVehicle(t *testing.T) {
	requests := 0
	client := testClient(t, func(r *http.Request) (int, string) {
		requests++
		if requests == 1 {
			return 502, "upstream failure"
		}
		if r.URL.Path == "/customers/vehicles/mappings" {
			return 200, `[{"vin":"TEST","mappingType":"PRIMARY"}]`
		}
		if r.URL.Path == "/customers/containers" && r.Method == http.MethodGet {
			return 200, fmt.Sprintf(`{"containers":[{"containerId":"container","name":%q}]}`, containerName)
		}
		t.Fatalf("unexpected request %s %s", r.Method, r.URL)
		return 500, ""
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	vins, id, err := bootstrapWithRetry(ctx, client, t.TempDir(), nil, time.Millisecond)
	if err != nil || len(vins) != 1 || vins[0] != "TEST" || id != "container" || requests != 3 {
		t.Fatalf("bootstrap = %v, %q, %v; requests=%d", vins, id, err, requests)
	}
}

func TestBootstrapRetryStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := testClient(t, func(*http.Request) (int, string) { cancel(); return 503, "unavailable" })
	_, _, err := bootstrapWithRetry(ctx, client, t.TempDir(), nil, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestBootstrapExplicitVehicleSkipsDiscovery(t *testing.T) {
	client := testClient(t, func(*http.Request) (int, string) { t.Fatal("unexpected BMW request"); return 500, "" })
	vins, id, err := BootstrapMulti(context.Background(), client, savedTestVehicle(t), []string{"TEST"})
	if err != nil || len(vins) != 1 || id != "container" {
		t.Fatalf("bootstrap = %v, %q, %v", vins, id, err)
	}
}
