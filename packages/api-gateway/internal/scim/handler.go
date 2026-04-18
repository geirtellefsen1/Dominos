package scim

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/auth"
)

type Handler struct {
	store    *Store
	sessions *auth.SessionStore
	token    string
}

func NewHandler(store *Store, sessions *auth.SessionStore, token string) *Handler {
	return &Handler{store: store, sessions: sessions, token: token}
}

func (h *Handler) Register(mux *http.ServeMux) {
	// SCIM uses its own bearer (DOMINION_SCIM_TOKEN) so Entra's
	// provisioning agent doesn't need the more-privileged admin
	// bearer. auth.RequireBearer attaches an admin:scim principal to
	// the context on success so the audit tap records the SCIM call.
	scimAuth := auth.RequireBearer(h.token, "scim")
	mux.Handle("POST /scim/v2/Users", scimAuth(http.HandlerFunc(h.create)))
	mux.Handle("GET /scim/v2/Users", scimAuth(http.HandlerFunc(h.list)))
	mux.Handle("GET /scim/v2/Users/{id}", scimAuth(http.HandlerFunc(h.get)))
	mux.Handle("PATCH /scim/v2/Users/{id}", scimAuth(http.HandlerFunc(h.patch)))
	mux.Handle("PUT /scim/v2/Users/{id}", scimAuth(http.HandlerFunc(h.put)))
	mux.Handle("DELETE /scim/v2/Users/{id}", scimAuth(http.HandlerFunc(h.delete)))
}

// --- handlers ------------------------------------------------------------

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in User
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error(), "invalidSyntax")
		return
	}
	if in.UserName == "" {
		writeErr(w, http.StatusBadRequest, "userName is required", "invalidValue")
		return
	}
	email := primaryEmail(in)
	if email == "" {
		email = in.UserName
	}
	row, err := h.store.Create(r.Context(), in.ExternalID, email, in.DisplayName, boolOr(in.Active, true))
	if err != nil {
		slog.Error("scim create", "err", err)
		writeErr(w, http.StatusConflict, err.Error(), "uniqueness")
		return
	}
	writeUser(w, http.StatusCreated, row, r)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id", "invalidValue")
		return
	}
	row, err := h.store.GetByID(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found", "")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error(), "")
		return
	}
	writeUser(w, http.StatusOK, row, r)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("filter")
	var rows []*Row
	var err error

	if userName, ok := parseUserNameEq(filter); ok {
		rows, err = h.store.ListByUserName(r.Context(), userName)
	} else if filter == "" {
		rows = nil // MVP: empty unfiltered listing
	} else {
		writeErr(w, http.StatusBadRequest, "only `userName eq \"...\"` is supported", "invalidFilter")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error(), "")
		return
	}

	resources := make([]User, 0, len(rows))
	for _, row := range rows {
		resources = append(resources, toSCIM(row, r))
	}
	resp := ListResponse{
		Schemas:      []string{SchemaListResp},
		TotalResults: len(resources),
		Resources:    resources,
		StartIndex:   1,
		ItemsPerPage: len(resources),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id", "invalidValue")
		return
	}
	var req PatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json", "invalidSyntax")
		return
	}

	nextActive, touched, err := applyActivePatch(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error(), "invalidValue")
		return
	}
	if !touched {
		// Other ops are out of scope for phase 2; return current state unchanged.
		row, err := h.store.GetByID(r.Context(), id)
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not found", "")
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error(), "")
			return
		}
		writeUser(w, http.StatusOK, row, r)
		return
	}

	row, err := h.store.SetActive(r.Context(), id, nextActive)
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found", "")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error(), "")
		return
	}
	if !nextActive {
		if _, err := h.sessions.RevokeAllForUser(r.Context(), id); err != nil {
			slog.Error("revoke sessions on scim deactivate", "err", err)
		}
	}
	writeUser(w, http.StatusOK, row, r)
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id", "invalidValue")
		return
	}
	var in User
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json", "invalidSyntax")
		return
	}
	email := primaryEmail(in)
	if email == "" {
		email = in.UserName
	}
	active := boolOr(in.Active, true)
	row, err := h.store.Replace(r.Context(), id, in.ExternalID, email, in.DisplayName, active)
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found", "")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error(), "")
		return
	}
	if !active {
		if _, err := h.sessions.RevokeAllForUser(r.Context(), id); err != nil {
			slog.Error("revoke sessions on scim replace", "err", err)
		}
	}
	writeUser(w, http.StatusOK, row, r)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad id", "invalidValue")
		return
	}
	// DELETE is treated as deactivate in SCIM-over-SQL, not a hard delete.
	if _, err := h.store.SetActive(r.Context(), id, false); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not found", "")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error(), "")
		return
	}
	if _, err := h.sessions.RevokeAllForUser(r.Context(), id); err != nil {
		slog.Error("revoke sessions on scim delete", "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers -------------------------------------------------------------

func primaryEmail(u User) string {
	if len(u.Emails) == 0 {
		return ""
	}
	for _, e := range u.Emails {
		if e.Primary {
			return strings.ToLower(e.Value)
		}
	}
	return strings.ToLower(u.Emails[0].Value)
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// parseUserNameEq accepts `userName eq "alice@example.com"` (quotes optional).
func parseUserNameEq(filter string) (string, bool) {
	f := strings.TrimSpace(filter)
	low := strings.ToLower(f)
	const prefix = "username eq "
	if !strings.HasPrefix(low, prefix) {
		return "", false
	}
	val := strings.TrimSpace(f[len(prefix):])
	if unq, err := strconv.Unquote(val); err == nil {
		return strings.ToLower(unq), true
	}
	return strings.ToLower(val), true
}

// applyActivePatch returns (nextActive, touched, err) for a PATCH request
// that toggles `active`. Everything else is a no-op in phase 2.
func applyActivePatch(req PatchRequest) (bool, bool, error) {
	for _, op := range req.Operations {
		path := strings.ToLower(op.Path)
		if path != "active" && op.Path != "" {
			continue
		}
		opName := strings.ToLower(op.Op)
		if opName != "replace" && opName != "add" {
			continue
		}
		switch v := op.Value.(type) {
		case bool:
			return v, true, nil
		case map[string]any:
			if a, ok := v["active"]; ok {
				if b, ok := a.(bool); ok {
					return b, true, nil
				}
			}
		case string:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return false, false, errors.New("active must be boolean")
			}
			return b, true, nil
		}
	}
	return true, false, nil
}

func toSCIM(r *Row, req *http.Request) User {
	loc := ""
	if req != nil {
		loc = baseURL(req) + "/scim/v2/Users/" + r.ID.String()
	}
	active := r.Active
	return User{
		Schemas:     []string{SchemaUser},
		ID:          r.ID.String(),
		ExternalID:  r.ExternalID,
		UserName:    r.Email,
		DisplayName: r.DisplayName,
		Active:      &active,
		Emails:      []Email{{Value: r.Email, Primary: true}},
		Meta:        &Meta{ResourceType: "User", Location: loc},
	}
}

func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func writeUser(w http.ResponseWriter, status int, row *Row, r *http.Request) {
	writeJSON(w, status, toSCIM(row, r))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, detail, scimType string) {
	writeJSON(w, status, ErrorResponse{
		Schemas:  []string{SchemaError},
		Status:   strconv.Itoa(status),
		Detail:   detail,
		SCIMType: scimType,
	})
}
