package bmw

import "time"

// PollStatus distinguishes gateway requests from vehicle-source timestamps.
type PollStatus struct {
	State               string     `json:"state"`
	LastAttempt         *time.Time `json:"last_attempt,omitempty"`
	LastSuccess         *time.Time `json:"last_success,omitempty"`
	NextPoll            *time.Time `json:"next_poll,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
}

func (c *Cache) PollStatus() PollStatus          { c.mu.RLock(); defer c.mu.RUnlock(); return c.polling }
func (c *Cache) setPollStatus(status PollStatus) { c.mu.Lock(); c.polling = status; c.mu.Unlock() }
