package bmw

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultAuthServer     = "https://customer.bmwgroup.com"
	defaultAuthScopes     = "authenticate_user openid cardata:api:read"
	deviceCodeGrantType   = "urn:ietf:params:oauth:grant-type:device_code"
	refreshTokenGrantType = "refresh_token"
)

var errBMWAuthorizationPoisoned = errors.New("BMW failed while finalizing device authorization")

type authSession struct {
	ClientID     string    `json:"client_id"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (s *authSession) validFor(clientID string) bool {
	return s != nil &&
		strings.EqualFold(s.ClientID, clientID) &&
		s.AccessToken != "" &&
		time.Now().Add(30*time.Second).Before(s.ExpiresAt)
}

type oauthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// Auth implements BMW's OAuth device-code flow with PKCE and persistent refresh tokens.
type Auth struct {
	clientID    string
	sessionPath string
	promptURI   func(verificationURI, userCode string)
	authServer  string
	scopes      string
	http        *http.Client
	mu          sync.Mutex
	cached      *authSession
}

func NewAuth(clientID, sessionPath string, promptURI func(string, string)) *Auth {
	authServer := strings.TrimRight(os.Getenv("BMW_AUTH_SERVER"), "/")
	if authServer == "" {
		authServer = defaultAuthServer
	}
	scopes := strings.TrimSpace(os.Getenv("BMW_AUTH_SCOPES"))
	if scopes == "" {
		scopes = defaultAuthScopes
	}
	return &Auth{
		clientID:    clientID,
		sessionPath: sessionPath,
		promptURI:   promptURI,
		authServer:  authServer,
		scopes:      scopes,
		http:        &http.Client{Timeout: 30 * time.Second},
	}
}

// Token returns a valid access token, refreshing or authenticating when necessary.
func (a *Auth) Token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	session, err := a.loadSession()
	if err == nil && session.validFor(a.clientID) {
		return session.AccessToken, nil
	}

	if err == nil && session != nil &&
		strings.EqualFold(session.ClientID, a.clientID) &&
		session.RefreshToken != "" {
		refreshed, refreshErr := a.refresh(ctx, session.RefreshToken)
		if refreshErr == nil {
			return refreshed.AccessToken, nil
		}
		log.Printf("[auth] token refresh failed, starting device authorization: %v", refreshErr)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		session, err = a.deviceCodeFlow(ctx)
		if err == nil {
			return session.AccessToken, nil
		}
		if !errors.Is(err, errBMWAuthorizationPoisoned) || attempt == 2 {
			return "", err
		}
		log.Printf("[auth] BMW invalidated the approved device code after a server error; requesting one fresh code")
	}
	return "", errors.New("BMW authentication failed")
}

func (a *Auth) deviceCodeFlow(ctx context.Context) (*authSession, error) {
	verifier, challenge, err := pkceS256()
	if err != nil {
		return nil, fmt.Errorf("generate PKCE challenge: %w", err)
	}

	resp, err := a.postForm(ctx, a.authServer+"/gcdm/oauth/device/code", url.Values{
		"client_id":             {a.clientID},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"response_type":         {"device_code"},
		"scope":                 {a.scopes},
	})
	if err != nil {
		return nil, fmt.Errorf("request device code: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read device code response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, responseError("device code request", resp.Status, body)
	}

	var device struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &device); err != nil {
		return nil, fmt.Errorf("decode device code response: %w", err)
	}
	if device.DeviceCode == "" || device.UserCode == "" || device.VerificationURI == "" {
		return nil, errors.New("BMW returned an incomplete device code response")
	}

	verificationURI := device.VerificationURIComplete
	if verificationURI == "" {
		verificationURI = device.VerificationURI
	}
	a.promptURI(verificationURI, device.UserCode)

	pollInterval := time.Duration(device.Interval) * time.Second
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	expiresIn := time.Duration(device.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = 10 * time.Minute
	}

	return a.pollToken(ctx, verifier, device.DeviceCode, pollInterval, time.Now().Add(expiresIn))
}

func (a *Auth) pollToken(
	ctx context.Context,
	verifier string,
	deviceCode string,
	pollInterval time.Duration,
	expiresAt time.Time,
) (*authSession, error) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	pollAttempt := 0
	sawServerError := false
	for time.Now().Before(expiresAt) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
		pollAttempt++

		resp, err := a.postForm(ctx, a.authServer+"/gcdm/oauth/token", url.Values{
			"client_id":     {a.clientID},
			"code_verifier": {verifier},
			"device_code":   {deviceCode},
			"grant_type":    {deviceCodeGrantType},
		})
		if err != nil {
			log.Printf("[auth] token poll attempt %d failed, retrying: %v", pollAttempt, err)
			timer.Reset(pollInterval)
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil {
			log.Printf("[auth] token poll attempt %d response could not be read, retrying: %v", pollAttempt, readErr)
			timer.Reset(pollInterval)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			session, err := a.decodeSession(body)
			if err != nil {
				return nil, err
			}
			if err := a.saveSession(session); err != nil {
				return nil, fmt.Errorf("save BMW session: %w", err)
			}
			log.Printf("[auth] authentication successful")
			return session, nil
		}

		var oauthErr oauthError
		_ = json.Unmarshal(body, &oauthErr)
		log.Printf(
			"[auth] token poll attempt %d: status=%s oauth_error=%s",
			pollAttempt,
			resp.Status,
			logValue(oauthErr.Error),
		)
		switch oauthErr.Error {
		case "authorization_pending":
			// Normal until the user approves the request.
		case "slow_down":
			pollInterval += 5 * time.Second
		case "access_denied":
			if sawServerError {
				return nil, fmt.Errorf(
					"%w: BMW returned a server error and then marked the same code as declined",
					errBMWAuthorizationPoisoned,
				)
			}
			return nil, responseError("device authorization denied", resp.Status, body)
		case "expired_token":
			return nil, errors.New("BMW device code expired")
		default:
			if resp.StatusCode >= 500 {
				sawServerError = true
				log.Printf("[auth] BMW token endpoint returned %s, retrying", resp.Status)
			} else if resp.StatusCode != http.StatusForbidden {
				return nil, responseError("token request rejected", resp.Status, body)
			}
		}

		timer.Reset(pollInterval)
	}
	return nil, errors.New("BMW device authorization expired")
}

func (a *Auth) refresh(ctx context.Context, refreshToken string) (*authSession, error) {
	resp, err := a.postForm(ctx, a.authServer+"/gcdm/oauth/token", url.Values{
		"client_id":     {a.clientID},
		"refresh_token": {refreshToken},
		"grant_type":    {refreshTokenGrantType},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, responseError("refresh token request", resp.Status, body)
	}
	session, err := a.decodeSession(body)
	if err != nil {
		return nil, err
	}
	if session.RefreshToken == "" {
		session.RefreshToken = refreshToken
	}
	if err := a.saveSession(session); err != nil {
		return nil, err
	}
	return session, nil
}

func (a *Auth) decodeSession(body []byte) (*authSession, error) {
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if token.AccessToken == "" {
		return nil, errors.New("BMW token response did not contain an access token")
	}
	return &authSession{
		ClientID:     a.clientID,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(token.ExpiresIn) * time.Second),
	}, nil
}

func (a *Auth) loadSession() (*authSession, error) {
	if a.cached != nil {
		return a.cached, nil
	}
	data, err := os.ReadFile(a.sessionPath)
	if err != nil {
		return nil, err
	}
	var session authSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	a.cached = &session
	return &session, nil
}

func (a *Auth) saveSession(session *authSession) error {
	if err := os.MkdirAll(filepath.Dir(a.sessionPath), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(a.sessionPath, data, 0600); err != nil {
		return err
	}
	a.cached = session
	return nil
}

func (a *Auth) postForm(ctx context.Context, endpoint string, values url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return a.http.Do(req)
}

func responseError(operation, status string, body []byte) error {
	var oauthErr oauthError
	if json.Unmarshal(body, &oauthErr) == nil && oauthErr.Error != "" {
		if oauthErr.ErrorDescription != "" {
			return fmt.Errorf("%s: %s: %s (%s)", operation, oauthErr.Error, oauthErr.ErrorDescription, status)
		}
		return fmt.Errorf("%s: %s (%s)", operation, oauthErr.Error, status)
	}
	message := strings.TrimSpace(string(body))
	if len(message) > 300 {
		message = message[:300] + "…"
	}
	if message == "" {
		return fmt.Errorf("%s: %s", operation, status)
	}
	return fmt.Errorf("%s: %s: %s", operation, status, message)
}

func logValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func pkceS256() (verifier, challenge string, err error) {
	randomBytes := make([]byte, 64)
	if _, err = rand.Read(randomBytes); err != nil {
		return "", "", err
	}
	verifier = strings.TrimRight(base64.URLEncoding.EncodeToString(randomBytes), "=")
	sum := sha256.Sum256([]byte(verifier))
	challenge = strings.TrimRight(base64.URLEncoding.EncodeToString(sum[:]), "=")
	return verifier, challenge, nil
}
