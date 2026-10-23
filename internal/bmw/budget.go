package bmw

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

const dailyRequestLimit = 50

type requestState struct {
	Day         string    `json:"day"`
	Count       int       `json:"count"`
	Failures    int       `json:"failures"`
	LastError   string    `json:"last_error,omitempty"`
	NextAllowed time.Time `json:"next_allowed"`
}

type requestBudget struct {
	mu      sync.Mutex
	path    string
	backoff time.Duration
}

type requestDeferred struct {
	Until  time.Time
	Reason string
}

func (e *requestDeferred) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("%s; BMW REST requests paused until %s", e.Reason, e.Until.UTC().Format(time.RFC3339))
	}
	return fmt.Sprintf("BMW REST requests paused until %s", e.Until.UTC().Format(time.RFC3339))
}

func nextUTCDay(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
}

func (b *requestBudget) load(now time.Time) (requestState, error) {
	var s requestState
	data, err := os.ReadFile(b.path)
	if err != nil && !os.IsNotExist(err) {
		return s, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &s); err != nil {
			return s, fmt.Errorf("read BMW request budget: %w", err)
		}
	}
	day := now.UTC().Format("2006-01-02")
	if s.Day != day {
		s.Day = day
		s.Count = 0
	}
	return s, nil
}

func budgetDeadline(s requestState, now time.Time) time.Time {
	until := s.NextAllowed
	if s.Count >= dailyRequestLimit && nextUTCDay(now).After(until) {
		until = nextUTCDay(now)
	}
	return until
}

// check and reserve are separate so expired sessions are not refreshed during a cooldown.
func (b *requestBudget) check(now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, err := b.load(now)
	if err != nil {
		return err
	}
	if until := budgetDeadline(s, now); until.After(now) {
		return &requestDeferred{Until: until, Reason: s.LastError}
	}
	return nil
}

func (b *requestBudget) reserve(now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, err := b.load(now)
	if err != nil {
		return err
	}
	if until := budgetDeadline(s, now); until.After(now) {
		return &requestDeferred{Until: until, Reason: s.LastError}
	}
	s.Count++
	// Persist before sending: a process restart cannot erase an attempted request.
	return writeJSON(b.path, &s)
}

func retryAfter(value string, now time.Time) time.Time {
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return date
	}
	return time.Time{}
}

func (b *requestBudget) record(response *http.Response, requestErr error, now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, err := b.load(now)
	if err != nil {
		return err
	}
	switch {
	case response != nil && response.StatusCode == http.StatusTooManyRequests:
		s.LastError = "BMW REST: " + response.Status
		s.NextAllowed = retryAfter(response.Header.Get("Retry-After"), now)
		if s.NextAllowed.IsZero() {
			s.NextAllowed = nextUTCDay(now)
		}
	case requestErr != nil || response != nil && response.StatusCode >= 500:
		s.Failures++
		s.LastError = "BMW REST transport request failed"
		if response != nil {
			s.LastError = "BMW REST: " + response.Status
		}
		base := b.backoff
		if base <= 0 {
			base = 5 * time.Minute
		}
		delay := min(base*time.Duration(1<<min(s.Failures-1, 6)), 6*time.Hour)
		s.NextAllowed = now.Add(delay)
		if response != nil {
			if until := retryAfter(response.Header.Get("Retry-After"), now); until.After(s.NextAllowed) {
				s.NextAllowed = until
			}
		}
	default:
		s.Failures = 0
		s.LastError = ""
		s.NextAllowed = time.Time{}
	}
	return writeJSON(b.path, &s)
}

func (b *requestBudget) next(now time.Time) time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, err := b.load(now)
	if err != nil {
		return now.Add(time.Hour)
	}
	return budgetDeadline(s, now)
}
