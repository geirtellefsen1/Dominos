package documents

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
)

type Handler struct {
	store     *Store
	validator *Validator
}

func NewHandler(store *Store, v *Validator) *Handler {
	return &Handler{store: store, validator: v}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /documents", h.create)
	mux.HandleFunc("GET /documents", h.list)
	mux.HandleFunc("GET /documents/{id}", h.get)
}

type createRequest struct {
	SchemaID string          `json:"schema_id"`
	Body     json.RawMessage `json:"body"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if req.SchemaID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "schema_id is required", nil)
		return
	}
	if len(req.Body) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "body is required", nil)
		return
	}

	issues, err := h.validator.Validate(r.Context(), req.SchemaID, req.Body)
	if err != nil {
		if errors.Is(err, ErrSchemaNotFound) {
			httpx.WriteError(w, http.StatusBadRequest, "unknown_schema", "no schema registered with id "+req.SchemaID, nil)
			return
		}
		slog.Error("validate", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "validation failed", nil)
		return
	}
	if len(issues) > 0 {
		httpx.WriteError(w, http.StatusBadRequest, "schema_violation", "document does not satisfy "+req.SchemaID, issues)
		return
	}

	doc, err := h.store.Create(r.Context(), req.SchemaID, req.Body, httpx.DevPrincipal(r))
	if err != nil {
		slog.Error("create document", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create document", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, doc)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("id")
	id, err := uuid.Parse(raw)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_id", "id must be a uuid", nil)
		return
	}
	doc, err := h.store.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "document not found", nil)
		return
	}
	if err != nil {
		slog.Error("get document", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get document", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, doc)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	docs, err := h.store.List(r.Context(), ListParams{
		SchemaID: q.Get("schema"),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		slog.Error("list documents", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list documents", nil)
		return
	}
	if docs == nil {
		docs = []*Document{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": docs})
}
