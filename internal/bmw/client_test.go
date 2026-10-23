package bmw

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testClient(t *testing.T, handler func(*http.Request) (int, string)) *Client {
	t.Helper()
	auth := NewAuth("test-client", filepath.Join(t.TempDir(), "session.json"), func(string, string) {
		t.Fatal("unexpected device authorization")
	})
	auth.cached = &authSession{ClientID: "test-client", AccessToken: "test-token", ExpiresAt: time.Now().Add(time.Hour)}
	client := NewClient(auth)
	client.budget.backoff = time.Millisecond
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("x-version") != apiVersion {
			t.Error("missing BMW API headers")
		}
		code, body := handler(r)
		return &http.Response{StatusCode: code, Status: http.StatusText(code), Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	return client
}

func TestClientDecodesBMWResponses(t *testing.T) {
	client := testClient(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/customers/vehicles/mappings":
			return 200, `[{"vin":"TEST","mappingType":"PRIMARY"}]`
		case "/customers/containers":
			return 200, `{"containers":[{"containerId":"container","name":"bridge"}]}`
		case "/customers/vehicles/TEST/telematicData":
			if r.URL.Query().Get("containerId") != "container" {
				t.Error("missing container ID")
			}
			return 200, `{"telematicData":{"vehicle.vehicle.travelledDistance":{"value":"1234","unit":"km"}}}`
		default:
			t.Fatalf("unexpected request %s", r.URL)
			return 404, ""
		}
	})
	ctx := context.Background()
	mappings, err := client.getMappings(ctx)
	if err != nil || len(mappings) != 1 || mappings[0].Vin == nil || *mappings[0].Vin != "TEST" {
		t.Fatalf("mappings = %v, err = %v", mappings, err)
	}
	containers, err := client.listContainers(ctx)
	if err != nil || len(containers) != 1 || containers[0].ContainerId == nil || *containers[0].ContainerId != "container" {
		t.Fatalf("containers = %v, err = %v", containers, err)
	}
	data, err := client.getTelematicData(ctx, "TEST", "container")
	if err != nil || getFloat(data, "vehicle.vehicle.travelledDistance") != 1234 {
		t.Fatalf("telematic data = %v, err = %v", data, err)
	}
}

func TestClientRejectsMalformedResponses(t *testing.T) {
	client := testClient(t, func(*http.Request) (int, string) { return 200, "{" })
	ctx := context.Background()
	if _, err := client.getMappings(ctx); err == nil {
		t.Error("accepted malformed mappings")
	}
	if _, err := client.listContainers(ctx); err == nil {
		t.Error("accepted malformed containers")
	}
	if _, err := client.getTelematicData(ctx, "TEST", "container"); err == nil {
		t.Error("accepted malformed telemetry")
	}
}

func TestContainerFailuresDoNotTriggerMoreRequests(t *testing.T) {
	for _, operation := range []string{"list", "create"} {
		t.Run(operation, func(t *testing.T) {
			requests := 0
			client := testClient(t, func(*http.Request) (int, string) {
				requests++
				return 403, `{"error":"access_denied"}`
			})
			var err error
			if operation == "list" {
				_, err = ensureContainer(context.Background(), client)
			} else {
				_, err = client.createContainer(context.Background(), "bridge", allDescriptors)
			}
			if err == nil || requests != 1 {
				t.Fatalf("err = %v, requests = %d; want one failed request", err, requests)
			}
		})
	}
}

func TestBootstrapDiscoversReplacementVehicle(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	statePath := filepath.Join(dataDir, "state.json")
	oldState := &state{VIN: "OLD", ContainerID: "container", ContainerName: containerName,
		LastPollTime: time.Now(), CachedData: &VehicleData{MileageKm: 99999}}
	if err := saveState(statePath, oldState); err != nil {
		t.Fatal(err)
	}
	client := testClient(t, func(r *http.Request) (int, string) {
		if r.URL.Path != "/customers/vehicles/mappings" {
			t.Fatalf("unexpected request %s", r.URL)
		}
		return 200, `[{"vin":"OTHER","mappingType":"SECONDARY"},{"vin":"TEST","mappingType":"PRIMARY"}]`
	})
	vins, containerID, err := BootstrapMulti(context.Background(), client, dataDir, nil)
	if err != nil || len(vins) != 1 || vins[0] != "TEST" || containerID != "container" {
		t.Fatalf("bootstrap = %v, %q, %v", vins, containerID, err)
	}
	s, err := loadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if s.VIN != "TEST" || s.CachedData != nil || !s.LastPollTime.IsZero() {
		t.Fatalf("old vehicle cache retained: %+v", s)
	}
}

func TestDiscoverVINRequiresPrimaryVehicle(t *testing.T) {
	client := testClient(t, func(*http.Request) (int, string) {
		return 200, `[{"vin":"OTHER","mappingType":"SECONDARY"}]`
	})
	if _, err := discoverVIN(context.Background(), client); err == nil {
		t.Fatal("selected secondary vehicle")
	}
}
