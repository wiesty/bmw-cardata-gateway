package bmw

import (
	"context"
	"fmt"
	"log"
	"time"
)

// StartPoller keeps the last successful cache and persists attempts and retry deadlines.
func StartPoller(ctx context.Context, client *Client, vin, containerID string, interval time.Duration, cache *Cache, statePath string) {
	s, err := loadState(statePath)
	if err != nil {
		log.Printf("[poller] could not restore state: %v", err)
		s = &state{}
	}
	if s.VIN != vin {
		s = &state{}
	}
	status := s.Polling
	if s.CachedData != nil {
		cache.Set(*s.CachedData)
		if status.LastSuccess == nil {
			stamp := s.CachedData.LastUpdate
			status.LastSuccess = &stamp
		}
	}
	next := time.Now()
	if s.ContainerID == containerID {
		if status.NextPoll != nil {
			next = *status.NextPoll
		} else if !s.LastPollTime.IsZero() {
			next = s.LastPollTime.Add(interval)
		}
	}
	if until := client.budget.next(time.Now()); until.After(next) {
		next = until
	}
	if next.Before(time.Now()) {
		next = time.Now()
	}
	status.State = "waiting"
	if err := client.budget.check(time.Now()); err != nil {
		status.LastError = err.Error()
	}
	if status.LastError != "" {
		status.State = "degraded"
	}
	status.NextPoll = pollTime(next)
	cache.setPollStatus(status)
	persistPollState(statePath, vin, containerID, cache, status)
	for {
		log.Printf("[poller] next poll at %s", next.Local().Format(time.RFC3339))
		timer := time.NewTimer(max(time.Until(next), 0))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return
		}
		attempt := time.Now().UTC()
		next = attempt.Add(interval)
		status.State = "polling"
		status.LastAttempt = &attempt
		status.NextPoll = pollTime(next)
		cache.setPollStatus(status)
		persistPollState(statePath, vin, containerID, cache, status)
		pollCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		err := pollOnce(pollCtx, client, vin, containerID, cache)
		cancel()
		if ctx.Err() != nil {
			status.State = "waiting"
			cache.setPollStatus(status)
			persistPollState(statePath, vin, containerID, cache, status)
			return
		}
		finished := time.Now().UTC()
		if err == nil {
			status.State = "waiting"
			status.LastSuccess = &finished
			status.LastError = ""
			status.ConsecutiveFailures = 0
			next = finished.Add(interval)
			log.Printf("[poll] ok; next regular poll in %s", interval)
		} else {
			status.State = "degraded"
			status.LastError = err.Error()
			status.ConsecutiveFailures++
			delay := max(interval, min(5*time.Minute*time.Duration(1<<min(status.ConsecutiveFailures-1, 6)), 6*time.Hour))
			next = finished.Add(delay)
			log.Printf("[poll] failed; keeping cache: %v", err)
		}
		if until := client.budget.next(finished); until.After(next) {
			next = until
		}
		status.NextPoll = pollTime(next)
		cache.setPollStatus(status)
		persistPollState(statePath, vin, containerID, cache, status)
	}
}

func persistPollState(path, vin, containerID string, cache *Cache, status PollStatus) {
	s, err := loadState(path)
	if err != nil {
		log.Printf("[poller] could not load state for persistence: %v", err)
		return
	}
	s.VIN = vin
	s.ContainerID = containerID
	s.Polling = status
	s.CachedData = cache.Get()
	if s.CachedData != nil {
		s.LastPollTime = s.CachedData.LastUpdate
	}
	if err := saveState(path, s); err != nil {
		log.Printf("[poller] could not persist cache/schedule: %v", err)
	}
}

func pollOnce(ctx context.Context, client *Client, vin, containerID string, cache *Cache) error {
	previous := cache.Get()
	var basicData map[string]any
	if previous != nil {
		basicData = previous.BasicData
	}
	if basicData == nil {
		var err error
		basicData, err = client.getBasicData(ctx, vin)
		if err != nil {
			log.Printf("[poll] basic metadata unavailable: %v", err)
		}
	}
	data, err := client.getTelematicData(ctx, vin, containerID)
	if err != nil {
		return err
	}
	usable := false
	for _, entry := range data {
		if entry.Value != nil && *entry.Value != "" {
			usable = true
			break
		}
	}
	if !usable {
		return fmt.Errorf("BMW returned no usable telemetry; previous cache retained")
	}
	merged := make(map[string]TelematicEntry)
	if previous != nil {
		for key, entry := range previous.Telematics {
			merged[key] = entry
		}
	}
	for key, entry := range data {
		if entry.Value != nil && *entry.Value != "" {
			merged[key] = entry
		}
	}
	value := mapVehicleData(merged, basicData, time.Now())
	if previous != nil && getFloatPtr(merged, "vehicle.vehicle.travelledDistance") == nil {
		value.MileageKm = previous.MileageKm
	}
	cache.Set(value)
	return nil
}

func pollTime(value time.Time) *time.Time { return &value }
