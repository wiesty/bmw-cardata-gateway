package bmw

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestTemporaryRefreshFailureDoesNotStartDeviceAuthorization(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/gcdm/oauth/token" {
			t.Errorf("unexpected device authorization %s", r.URL.Path)
		}
		http.Error(w, "temporary upstream failure", http.StatusBadGateway)
	}))
	defer server.Close()
	auth := NewAuth("synthetic-client", filepath.Join(t.TempDir(), "session.json"), func(string, string) { t.Error("temporary failure prompted a new login") })
	auth.authServer = server.URL
	auth.http = server.Client()
	session := &authSession{ClientID: "synthetic-client", AccessToken: "expired-test-token", RefreshToken: "synthetic-refresh-token", ExpiresAt: time.Now().Add(-time.Hour)}
	if err := auth.saveSession(session); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Token(context.Background()); err == nil || !isTransientError(err) {
		t.Fatalf("temporary refresh failure not propagated: %v", err)
	}
	if requests != 1 || auth.cached.RefreshToken != "synthetic-refresh-token" {
		t.Fatal("refresh session was lost or new authorization started")
	}
}
