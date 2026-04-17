package graph

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/auth"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
)

// Handler serves the user-facing OAuth flow plus a dev-only fake inject
// endpoint so the smoke tests can exercise the ingestion pipeline
// without a live M365 tenant.
type Handler struct {
	store      *Store
	client     *Client
	ingester   *Ingester
	adminToken string
	// DevSimulate enables POST /admin/connectors/graph/simulate. Gated by
	// DOMINION_DEV_GRAPH_SIMULATE=true — must be off in production.
	devSimulate bool
}

func NewHandler(store *Store, client *Client, ingester *Ingester, adminToken string, devSimulate bool) *Handler {
	return &Handler{
		store: store, client: client, ingester: ingester,
		adminToken: adminToken, devSimulate: devSimulate,
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /connectors/graph/connect", auth.Require(http.HandlerFunc(h.connect)))
	mux.HandleFunc("GET /connectors/graph/callback", h.callback)
	if h.devSimulate {
		mux.Handle("POST /admin/connectors/graph/simulate", h.adminAuth(http.HandlerFunc(h.simulate)))
	}
}

// --- OAuth connect / callback ---------------------------------------------

const (
	graphStateCookie = "dominion_graph_state"
	graphUserCookie  = "dominion_graph_user"
)

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	if h.client == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "graph_disabled",
			"DOMINION_GRAPH_CLIENT_ID is not configured", nil)
		return
	}
	p, _ := auth.FromContext(r.Context())
	if p.Kind != "user" {
		httpx.WriteError(w, http.StatusForbidden, "forbidden",
			"only user principals can connect a mailbox", nil)
		return
	}
	state, err := randomToken()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	setShortCookie(w, graphStateCookie, state)
	setShortCookie(w, graphUserCookie, p.ID)
	http.Redirect(w, r, h.client.AuthorizeURL(state), http.StatusFound)
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	if h.client == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "graph_disabled", "", nil)
		return
	}
	q := r.URL.Query()
	stateParam, code := q.Get("state"), q.Get("code")
	if stateParam == "" || code == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "missing state or code", nil)
		return
	}
	stateCk, err := r.Cookie(graphStateCookie)
	if err != nil || stateCk.Value == "" || stateCk.Value != stateParam {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_state", "state mismatch", nil)
		return
	}
	userCk, err := r.Cookie(graphUserCookie)
	if err != nil || !strings.HasPrefix(userCk.Value, "user:") {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_user", "missing user cookie", nil)
		return
	}
	userID, err := uuid.Parse(strings.TrimPrefix(userCk.Value, "user:"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_user", err.Error(), nil)
		return
	}

	tok, err := h.client.ExchangeCode(r.Context(), code)
	if err != nil {
		slog.Error("graph exchange code", "err", err)
		httpx.WriteError(w, http.StatusBadGateway, "graph_exchange_failed", err.Error(), nil)
		return
	}
	if tok.RefreshToken == "" {
		httpx.WriteError(w, http.StatusBadGateway, "graph_no_refresh",
			"Microsoft did not return a refresh token; ensure offline_access is in the requested scopes", nil)
		return
	}
	me, err := h.client.Me(r.Context(), tok.AccessToken)
	if err != nil {
		httpx.WriteError(w, http.StatusBadGateway, "graph_me_failed", err.Error(), nil)
		return
	}
	mailbox := me.Mail
	if mailbox == "" {
		mailbox = me.UserPrincipalName
	}
	if _, err := h.store.Upsert(r.Context(), userID, mailbox, tok.RefreshToken); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	clearShortCookie(w, graphStateCookie)
	clearShortCookie(w, graphUserCookie)

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"status":  "connected",
		"mailbox": mailbox,
	})
}

// --- dev simulate ---------------------------------------------------------

type simulateRequest struct {
	UserID      uuid.UUID `json:"user_id"`
	MailboxUser string    `json:"mailbox_user"`
	Message     Message   `json:"message"`
}

func (h *Handler) adminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.adminToken == "" {
			httpx.WriteError(w, http.StatusServiceUnavailable, "admin_disabled", "", nil)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(h.adminToken)) != 1 {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// simulate runs a fake Graph message through the real ingest pipeline so
// the smoke test can exercise owner-tuple write, PA reader grant, audit
// entry, and email.v1 schema validation without a live M365 tenant.
func (h *Handler) simulate(w http.ResponseWriter, r *http.Request) {
	var req simulateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if req.UserID == uuid.Nil || req.MailboxUser == "" || req.Message.ID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request",
			"user_id, mailbox_user, and message.id are required", nil)
		return
	}
	body := MessageToEmailBody(req.Message, req.MailboxUser)
	doc, err := h.ingester.Ingest(r.Context(), req.UserID, body)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "ingest_failed", err.Error(), nil)
		return
	}
	if doc == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "duplicate"})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, doc)
}

// --- helpers --------------------------------------------------------------

func randomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func setShortCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: 600,
	})
}
func clearShortCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// avoid unused-import warnings in some build configs
var _ = errors.New
