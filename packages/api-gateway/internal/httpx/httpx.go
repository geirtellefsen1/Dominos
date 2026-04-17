package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type ErrorBody struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Details any    `json:"details,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json", "err", err)
	}
}

func WriteError(w http.ResponseWriter, status int, code, msg string, details any) {
	WriteJSON(w, status, ErrorBody{Error: code, Message: msg, Details: details})
}

func DecodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// DevPrincipal reads X-Dominion-Dev-Principal for phase 1 local dev only.
// Phase 2 replaces this with the OIDC / mTLS auth middleware.
func DevPrincipal(r *http.Request) string {
	if p := r.Header.Get("X-Dominion-Dev-Principal"); p != "" {
		return p
	}
	return "dev:anonymous"
}
