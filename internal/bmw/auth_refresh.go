package bmw

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

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
		var oauthErr oauthError
		_ = json.Unmarshal(body, &oauthErr)
		if resp.StatusCode == http.StatusBadRequest && (oauthErr.Error == "invalid_grant" || oauthErr.Error == "invalid_token") {
			return nil, fmt.Errorf("%w: %s", errRefreshRejected, oauthErr.Error)
		}
		return nil, newAPIError("refresh token request", resp)
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
