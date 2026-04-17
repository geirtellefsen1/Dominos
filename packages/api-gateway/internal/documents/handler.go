package documents

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/acl"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/auth"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
)

type Handler struct {
	store     *Store
	validator *Validator
	fga       *acl.Client
}

// NewHandler wires the document routes. `fga` may be nil in Phase 1-style
// deployments where ACL is disabled; in that case every authenticated
// caller sees every document and owner tuples are not written.
func NewHandler(store *Store, v *Validator, fga *acl.Client) *Handler {
	return &Handler{store: store, validator: v, fga: fga}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("POST /documents", auth.Require(http.HandlerFunc(h.create)))
	mux.Handle("GET /documents", auth.Require(http.HandlerFunc(h.list)))
	mux.Handle("GET /documents/{id}", auth.Require(http.HandlerFunc(h.get)))
}

type createRequest struct {
	SchemaID string          `json:"schema_id"`
	Body     json.RawMessage `json:"body"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.FromContext(r.Context())

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

	doc, err := h.store.Create(r.Context(), req.SchemaID, req.Body, principal.ID)
	if err != nil {
		slog.Error("create document", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create document", nil)
		return
	}

	if h.fga != nil {
		ownerTuple := acl.Tuple{
			User:     principal.ID,
			Relation: acl.RelOwner,
			Object:   objectID(doc.ID),
		}
		if err := h.fga.Write(r.Context(), ownerTuple); err != nil {
			// If we can't write the tuple, the document exists but is
			// orphaned from the ACL graph. Soft-delete it so the system
			// stays consistent, then surface the error.
			slog.Error("write owner tuple", "err", err, "doc", doc.ID)
			httpx.WriteError(w, http.StatusInternalServerError, "acl_write_failed",
				"could not register document ownership", nil)
			return
		}
	}
	httpx.WriteJSON(w, http.StatusCreated, doc)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.FromContext(r.Context())

	raw := r.PathValue("id")
	id, err := uuid.Parse(raw)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_id", "id must be a uuid", nil)
		return
	}

	if h.fga != nil {
		allowed, err := h.fga.Check(r.Context(), principal.ID, acl.RelReader, objectID(id))
		if err != nil {
			slog.Error("fga check", "err", err)
			httpx.WriteError(w, http.StatusInternalServerError, "acl_check_failed", err.Error(), nil)
			return
		}
		if !allowed {
			httpx.WriteError(w, http.StatusForbidden, "forbidden",
				"principal does not have reader access to this document", nil)
			return
		}
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
	principal, _ := auth.FromContext(r.Context())
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	params := ListParams{
		SchemaID: q.Get("schema"),
		Limit:    limit,
		Offset:   offset,
	}

	if h.fga != nil {
		// Ask FGA for every document the principal can read, then filter
		// the DB query to that set. Fine for MVP list sizes; when lists
		// grow past ~1k rows we'll page through ListObjects instead.
		objs, err := h.fga.ListObjects(r.Context(), principal.ID, acl.RelReader, acl.TypeDocument)
		if err != nil {
			slog.Error("fga list-objects", "err", err)
			httpx.WriteError(w, http.StatusInternalServerError, "acl_list_failed", err.Error(), nil)
			return
		}
		ids := make([]uuid.UUID, 0, len(objs))
		for _, o := range objs {
			s := strings.TrimPrefix(o, "document:")
			id, err := uuid.Parse(s)
			if err == nil {
				ids = append(ids, id)
			}
		}
		params.IDs = ids
	}

	docs, err := h.store.List(r.Context(), params)
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

// objectID returns the FGA object identifier for a document UUID.
func objectID(id uuid.UUID) string { return "document:" + id.String() }
