package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	oidclib "github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
)

type OIDCConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

type OIDC struct {
	cfg      OIDCConfig
	provider *oidclib.Provider
	verifier *oidclib.IDTokenVerifier
	oauth    *oauth2.Config
	users    *UserStore
	sessions *SessionStore
}

func NewOIDC(ctx context.Context, cfg OIDCConfig, users *UserStore, sessions *SessionStore) (*OIDC, error) {
	if cfg.IssuerURL == "" || cfg.ClientID == "" || cfg.RedirectURL == "" {
		return nil, errors.New("oidc: IssuerURL, ClientID, RedirectURL are required")
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidclib.ScopeOpenID, "email", "profile"}
	}
	provider, err := oidclib.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover issuer: %w", err)
	}
	return &OIDC{
		cfg:      cfg,
		provider: provider,
		verifier: provider.Verifier(&oidclib.Config{ClientID: cfg.ClientID}),
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       cfg.Scopes,
		},
		users:    users,
		sessions: sessions,
	}, nil
}

func (o *OIDC) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", o.login)
	mux.HandleFunc("GET /auth/callback", o.callback)
	mux.HandleFunc("POST /auth/logout", o.logout)
}

// --- login ---------------------------------------------------------------

const (
	stateCookie    = "dominion_oidc_state"
	nonceCookie    = "dominion_oidc_nonce"
	returnToCookie = "dominion_oidc_return_to"
)

func randomToken(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (o *OIDC) login(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken(16)
	if err != nil {
		httpx.WriteError(w, 500, "internal_error", "state", nil)
		return
	}
	nonce, err := randomToken(16)
	if err != nil {
		httpx.WriteError(w, 500, "internal_error", "nonce", nil)
		return
	}
	setShortCookie(w, r, stateCookie, state)
	setShortCookie(w, r, nonceCookie, nonce)
	if rt := r.URL.Query().Get("return_to"); rt != "" {
		setShortCookie(w, r, returnToCookie, rt)
	}
	authURL := o.oauth.AuthCodeURL(state, oidclib.Nonce(nonce))
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (o *OIDC) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	stateParam := q.Get("state")
	code := q.Get("code")
	if stateParam == "" || code == "" {
		httpx.WriteError(w, 400, "invalid_request", "missing state or code", nil)
		return
	}

	stateCk, err := r.Cookie(stateCookie)
	if err != nil || stateCk.Value == "" || stateCk.Value != stateParam {
		httpx.WriteError(w, 400, "invalid_state", "state mismatch", nil)
		return
	}
	nonceCk, err := r.Cookie(nonceCookie)
	if err != nil || nonceCk.Value == "" {
		httpx.WriteError(w, 400, "invalid_nonce", "missing nonce cookie", nil)
		return
	}

	tok, err := o.oauth.Exchange(r.Context(), code)
	if err != nil {
		slog.Error("oidc exchange", "err", err)
		httpx.WriteError(w, 502, "oidc_exchange_failed", err.Error(), nil)
		return
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		httpx.WriteError(w, 502, "oidc_no_id_token", "provider returned no id_token", nil)
		return
	}
	idTok, err := o.verifier.Verify(r.Context(), rawID)
	if err != nil {
		httpx.WriteError(w, 401, "oidc_invalid_id_token", err.Error(), nil)
		return
	}
	if idTok.Nonce != nonceCk.Value {
		httpx.WriteError(w, 401, "oidc_invalid_nonce", "nonce mismatch", nil)
		return
	}

	var claims struct {
		Sub               string `json:"sub"`
		Oid               string `json:"oid"`
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
	}
	if err := idTok.Claims(&claims); err != nil {
		httpx.WriteError(w, 500, "oidc_claims_decode", err.Error(), nil)
		return
	}
	externalID := claims.Oid
	if externalID == "" {
		externalID = claims.Sub
	}
	email := claims.Email
	if email == "" {
		email = claims.PreferredUsername
	}
	if email == "" {
		httpx.WriteError(w, 400, "oidc_missing_email", "id_token has no email claim", nil)
		return
	}

	u, err := o.users.UpsertFromOIDC(r.Context(), externalID, strings.ToLower(email), claims.Name)
	if err != nil {
		slog.Error("upsert user", "err", err)
		httpx.WriteError(w, 500, "internal_error", "failed to upsert user", nil)
		return
	}
	if !u.Active {
		httpx.WriteError(w, 403, "user_inactive", "user is deactivated", nil)
		return
	}

	signed, _, err := o.sessions.Create(r.Context(), u.ID, DefaultSessionTTL, r.UserAgent(), clientIP(r))
	if err != nil {
		slog.Error("create session", "err", err)
		httpx.WriteError(w, 500, "internal_error", "failed to create session", nil)
		return
	}
	setSessionCookie(w, r, signed, DefaultSessionTTL)
	clearShortCookie(w, r, stateCookie)
	clearShortCookie(w, r, nonceCookie)

	returnTo := "/me"
	if rt, err := r.Cookie(returnToCookie); err == nil && isSafeReturnTo(rt.Value) {
		returnTo = rt.Value
	}
	clearShortCookie(w, r, returnToCookie)

	// If the caller is a JSON client (e.g. a smoke test), return JSON instead
	// of a redirect.
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user": u,
			"session_cookie": signed,
		})
		return
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (o *OIDC) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" {
		if info, err := o.sessions.Verify(r.Context(), c.Value); err == nil {
			_ = o.sessions.RevokeByID(r.Context(), info.SessionID)
		}
	}
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// --- cookies -------------------------------------------------------------

// secureCookies reports whether Set-Cookie responses should carry the
// Secure attribute. True when the current request arrived over TLS
// (r.TLS != nil), or when the operator sets DOMINION_COOKIES_SECURE=true
// for deployments behind a TLS-terminating reverse proxy.
func secureCookies(r *http.Request) bool {
	if r != nil && r.TLS != nil {
		return true
	}
	return strings.EqualFold(os.Getenv("DOMINION_COOKIES_SECURE"), "true")
}

func setShortCookie(w http.ResponseWriter, r *http.Request, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookies(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
}
func clearShortCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secureCookies(r), SameSite: http.SameSiteLaxMode,
	})
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookies(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
	})
}
func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secureCookies(r), SameSite: http.SameSiteLaxMode,
	})
}

func isSafeReturnTo(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "" && u.Host == "" && strings.HasPrefix(u.Path, "/")
}

func clientIP(r *http.Request) string {
	if x := r.Header.Get("X-Forwarded-For"); x != "" {
		if i := strings.Index(x, ","); i >= 0 {
			return strings.TrimSpace(x[:i])
		}
		return strings.TrimSpace(x)
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	return host
}

// avoid unused-import warning in environments where uuid isn't used by other files
var _ = uuid.Nil
