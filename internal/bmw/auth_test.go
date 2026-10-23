package bmw

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestPollTokenRetriesServerErrors(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporary BMW error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access",
			"refresh_token": "refresh",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	auth := NewAuth("client-id", filepath.Join(t.TempDir(), "session.json"), func(string, string) {})
	auth.authServer = server.URL
	auth.http = server.Client()

	session, err := auth.pollToken(
		context.Background(),
		"verifier",
		"device-code",
		time.Millisecond,
		time.Now().Add(time.Second),
	)
	if err != nil {
		t.Fatalf("pollToken returned an error: %v", err)
	}
	if session.AccessToken != "access" {
		t.Fatalf("unexpected access token %q", session.AccessToken)
	}
	if requests.Load() != 2 {
		t.Fatalf("expected 2 token requests, got %d", requests.Load())
	}
}

func TestPollTokenWaitsWhileAuthorizationPending(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(oauthError{Error: "authorization_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	auth := NewAuth("client-id", filepath.Join(t.TempDir(), "session.json"), func(string, string) {})
	auth.authServer = server.URL
	auth.http = server.Client()

	if _, err := auth.pollToken(
		context.Background(),
		"verifier",
		"device-code",
		time.Millisecond,
		time.Now().Add(time.Second),
	); err != nil {
		t.Fatalf("pollToken returned an error: %v", err)
	}
}

func TestPollTokenDetectsAuthorizationPoisonedAfterServerError(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporary BMW error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(oauthError{
			Error:            "access_denied",
			ErrorDescription: "The user has declined authorization",
		})
	}))
	defer server.Close()

	auth := NewAuth("client-id", filepath.Join(t.TempDir(), "session.json"), func(string, string) {})
	auth.authServer = server.URL
	auth.http = server.Client()

	_, err := auth.pollToken(
		context.Background(),
		"verifier",
		"device-code",
		time.Millisecond,
		time.Now().Add(time.Second),
	)
	if !errors.Is(err, errBMWAuthorizationPoisoned) {
		t.Fatalf("expected poisoned authorization error, got %v", err)
	}
}
