package bmw

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

type apiError struct {
	operation  string
	statusCode int
	status     string
}

func newAPIError(operation string, response *http.Response) error {
	return &apiError{operation: operation, statusCode: response.StatusCode, status: response.Status}
}

func (e *apiError) Error() string { return fmt.Sprintf("%s: %s", e.operation, e.status) }

func isTransientError(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	var deferred *requestDeferred
	if errors.As(err, &deferred) {
		return true
	}
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return apiErr.statusCode == http.StatusRequestTimeout || apiErr.statusCode == http.StatusTooManyRequests || apiErr.statusCode >= 500
	}
	var operationErr *net.OpError
	var dnsErr *net.DNSError
	var networkErr net.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.As(err, &operationErr) || errors.As(err, &dnsErr) ||
		(errors.As(err, &networkErr) && networkErr.Timeout()) || errors.Is(err, io.EOF)
}

// BootstrapMulti discovers the current vehicle and prepares its container.
// Temporary BMW failures retry in-process; permanent errors remain actionable.
func BootstrapMulti(ctx context.Context, client *Client, dataDir string, explicitVINs []string) ([]string, string, error) {
	return bootstrapWithRetry(ctx, client, dataDir, explicitVINs, 5*time.Minute)
}

func bootstrapWithRetry(ctx context.Context, client *Client, dataDir string, explicitVINs []string, delay time.Duration) ([]string, string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		vins, containerID, err := bootstrapMulti(ctx, client, dataDir, explicitVINs)
		if err == nil || !isTransientError(err) {
			return vins, containerID, err
		}
		if until := client.budget.next(time.Now()); until.After(time.Now().Add(delay)) {
			delay = time.Until(until)
		}
		log.Printf("[bmw] startup temporarily unavailable; retrying in %s: %v", delay, err)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, "", ctx.Err()
		case <-timer.C:
		}
		delay = min(delay*2, 6*time.Hour)
	}
}
