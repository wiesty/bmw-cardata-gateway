package bmw

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// state persists VIN, container ID, last poll time, and cached vehicle data across restarts.
type state struct {
	VIN           string       `json:"vin,omitempty"`
	ContainerID   string       `json:"container_id"`
	ContainerName string       `json:"container_name,omitempty"`
	LastPollTime  time.Time    `json:"last_poll_time,omitempty"`
	CachedData    *VehicleData `json:"cached_data,omitempty"`
}

func loadState(path string) (*state, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &state{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s state
	return &s, json.Unmarshal(data, &s)
}

func saveState(path string, s *state) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// Bootstrap loads or discovers a single VIN and container ID, persisting them to state.json.
// Kept for backward compatibility with single-vehicle mode.
func Bootstrap(ctx context.Context, client *Client, dataDir string) (vin, containerID string, err error) {
	vins, cID, err := BootstrapMulti(ctx, client, dataDir, nil)
	if err != nil {
		return "", "", err
	}
	return vins[0], cID, nil
}

// BootstrapMulti discovers/validates VINs and ensures the shared container exists.
// If explicitVINs is non-empty, those VINs are used instead of auto-discovery.
// Container info is persisted to state.json (shared); per-VIN poll state uses state_<VIN>.json.
func BootstrapMulti(ctx context.Context, client *Client, dataDir string, explicitVINs []string) (vins []string, containerID string, err error) {
	statePath := filepath.Join(dataDir, "state.json")
	s, err := loadState(statePath)
	if err != nil {
		return nil, "", fmt.Errorf("load state: %w", err)
	}

	if len(explicitVINs) > 0 {
		vins = explicitVINs
	} else {
		vin, err := discoverVIN(ctx, client)
		if err != nil {
			return nil, "", fmt.Errorf("discover VIN: %w", err)
		}
		log.Printf("[bmw] discovered VIN: %s", vin)
		vins = []string{vin}
	}

	if s.ContainerID == "" || s.ContainerName != containerName {
		s.LastPollTime = time.Time{}
		s.ContainerID = ""
		s.ContainerID, err = ensureContainer(ctx, client)
		if err != nil {
			return nil, "", fmt.Errorf("ensure container: %w", err)
		}
		s.ContainerName = containerName
		log.Printf("[bmw] using container: %s (%s)", s.ContainerID, containerName)
	}

	// Persist VIN in state.json for single-vehicle restarts (no BMW_VINS set).
	if len(vins) == 1 {
		if s.VIN != vins[0] {
			s.LastPollTime = time.Time{}
			s.CachedData = nil
		}
		s.VIN = vins[0]
	}

	if err := saveState(statePath, s); err != nil {
		log.Printf("[bmw] warning: could not save state: %v", err)
	}

	return vins, s.ContainerID, nil
}

func discoverVIN(ctx context.Context, client *Client) (string, error) {
	mappings, err := client.getMappings(ctx)
	if err != nil {
		return "", err
	}
	if len(mappings) == 0 {
		return "", fmt.Errorf("no vehicles found in account")
	}
	for _, m := range mappings {
		if m.MappingType != nil && *m.MappingType == "PRIMARY" && m.Vin != nil {
			return *m.Vin, nil
		}
	}
	return "", fmt.Errorf("no primary vehicle found in account; check the primary user assignment in My BMW")
}

func ensureContainer(ctx context.Context, client *Client) (string, error) {
	id, err := findContainer(ctx, client)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}
	return client.createContainer(ctx, containerName, allDescriptors)
}

func findContainer(ctx context.Context, client *Client) (string, error) {
	list, err := client.listContainers(ctx)
	if err != nil {
		return "", err
	}
	for _, c := range list {
		if c.Name != nil && *c.Name == containerName && c.ContainerId != nil {
			return *c.ContainerId, nil
		}
	}
	return "", nil
}

// StartPoller starts the polling loop for a single VIN.
// statePath is the full path to the state file used to persist poll time and cached data.
// On restart it checks the last poll time and reuses cached data if the interval hasn't elapsed.
func StartPoller(ctx context.Context, client *Client, vin, containerID string, interval time.Duration, cache *Cache, statePath string) {
	s, _ := loadState(statePath)

	var basicData map[string]any
	if s != nil && s.VIN == vin && s.CachedData != nil {
		basicData = s.CachedData.BasicData
	}
	if basicData == nil {
		var err error
		basicData, err = client.getBasicData(ctx, vin)
		if err != nil {
			log.Printf("[bmw] basic data unavailable for VIN=%s: %v", vin, err)
		}
	}

	delay := time.Duration(0)
	if s != nil && s.VIN == vin && s.ContainerID == containerID && s.CachedData != nil && !s.LastPollTime.IsZero() {
		elapsed := time.Since(s.LastPollTime)
		if elapsed < interval {
			cache.Set(*s.CachedData)
			delay = interval - elapsed
			log.Printf("[poller] VIN=%s cached data loaded (polled %v ago), next poll in %v",
				vin, elapsed.Round(time.Second), delay.Round(time.Second))
		}
	}

	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return
	}

	poll(ctx, client, vin, containerID, cache, statePath, basicData)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			poll(ctx, client, vin, containerID, cache, statePath, basicData)
		case <-ctx.Done():
			return
		}
	}
}

func poll(ctx context.Context, client *Client, vin, containerID string, cache *Cache, statePath string, basicData map[string]any) {
	data, err := client.getTelematicData(ctx, vin, containerID)
	if err != nil {
		log.Printf("[poll] VIN=%s error: %v", vin, err)
		return
	}
	if data == nil {
		log.Printf("[poll] VIN=%s empty response", vin)
		return
	}

	d := mapVehicleData(data, basicData, time.Now())

	cache.Set(d)

	// Persist so restarts can skip a poll if interval hasn't elapsed.
	if s, err := loadState(statePath); err == nil {
		s.VIN = vin
		s.ContainerID = containerID
		s.LastPollTime = d.LastUpdate
		s.CachedData = &d
		if err := saveState(statePath, s); err != nil {
			log.Printf("[poll] VIN=%s warning: could not save state: %v", vin, err)
		}
	}

	log.Printf("[poll] ok — VIN=%s", vin)
}

func getFloat(e map[string]TelematicEntry, key string) float64 {
	v := getFloatPtr(e, key)
	if v == nil {
		return 0
	}
	return *v
}

func getFloatPtr(e map[string]TelematicEntry, key string) *float64 {
	entry, ok := e[key]
	if !ok || entry.Value == nil || *entry.Value == "" {
		return nil
	}
	v, err := strconv.ParseFloat(*entry.Value, 64)
	if err != nil {
		return nil
	}
	return &v
}

func getBoolPtr(e map[string]TelematicEntry, key string) *bool {
	entry, ok := e[key]
	if !ok || entry.Value == nil || *entry.Value == "" {
		return nil
	}
	// BMW reports door, hood and trunk sensors as OPEN/CLOSED.
	if strings.HasSuffix(key, ".isOpen") {
		switch *entry.Value {
		case "OPEN":
			v := true
			return &v
		case "CLOSED":
			v := false
			return &v
		}
	}
	if *entry.Value == "ASN_isTrue" {
		v := true
		return &v
	}
	if *entry.Value == "ASN_isFalse" {
		v := false
		return &v
	}
	v, err := strconv.ParseBool(*entry.Value)
	if err != nil {
		return nil
	}
	return &v
}

func getStringPtr(e map[string]TelematicEntry, key string) *string {
	entry, ok := e[key]
	if !ok || entry.Value == nil || *entry.Value == "" {
		return nil
	}
	return entry.Value
}

func floatOr(v *float64, fallback float64) float64 {
	if v == nil {
		return fallback
	}
	return *v
}
