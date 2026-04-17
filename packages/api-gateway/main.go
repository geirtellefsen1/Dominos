package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/auth"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/db"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/documents"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/scim"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	addr := envOr("DOMINION_GATEWAY_ADDR", ":3000")
	dsn := envOr("DOMINION_DATABASE_URL",
		"postgres://dominion:dominion_dev@localhost:5432/dominion?sslmode=disable")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		slog.Error("db connect", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		slog.Error("db migrate", "err", err)
		os.Exit(1)
	}

	// --- auth / identity ---
	jwtSecret := loadSessionSecret()
	users := auth.NewUserStore(pool)
	sessions := auth.NewSessionStore(pool, jwtSecret)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)

	// OIDC (optional in dev; log a warning if unconfigured)
	if issuer := os.Getenv("DOMINION_OIDC_ISSUER"); issuer != "" {
		oidcClient, err := auth.NewOIDC(ctx, auth.OIDCConfig{
			IssuerURL:    issuer,
			ClientID:     os.Getenv("DOMINION_OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("DOMINION_OIDC_CLIENT_SECRET"),
			RedirectURL:  envOr("DOMINION_OIDC_REDIRECT_URL", "http://localhost:3000/auth/callback"),
		}, users, sessions)
		if err != nil {
			slog.Error("oidc init", "err", err)
			os.Exit(1)
		}
		oidcClient.Register(mux)
	} else {
		slog.Warn("DOMINION_OIDC_ISSUER not set; /auth/login disabled until configured")
	}

	// Document store (phase 1). Uses attached principal if available.
	docStore := documents.NewStore(pool)
	validator := documents.NewValidator(docStore)
	docHandler := documents.NewHandler(docStore, validator)
	docHandler.Register(mux)

	// /me (phase 2) — requires auth.
	mux.Handle("GET /me", auth.Require(auth.MeHandler(users)))

	// SCIM (phase 2). Disabled if no bearer token is configured.
	scimStore := scim.NewStore(pool)
	scimToken := os.Getenv("DOMINION_SCIM_TOKEN")
	if scimToken == "" {
		slog.Warn("DOMINION_SCIM_TOKEN not set; SCIM endpoints will reject all requests")
	}
	scimHandler := scim.NewHandler(scimStore, sessions, scimToken)
	scimHandler.Register(mux)

	// Wrap everything in the auth.Attach middleware so any route can read a
	// Principal from context.
	handler := auth.Attach(sessions)(mux)

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("dominion api gateway listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadSessionSecret reads DOMINION_SESSION_SECRET or generates an ephemeral
// one with a loud warning (sessions won't survive a restart).
func loadSessionSecret() []byte {
	if s := os.Getenv("DOMINION_SESSION_SECRET"); s != "" {
		return []byte(s)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic("cannot generate session secret: " + err.Error())
	}
	slog.Warn("DOMINION_SESSION_SECRET not set; generated ephemeral secret (sessions will not survive restart)",
		"secret_preview", base64.RawURLEncoding.EncodeToString(buf[:4])+"...")
	return buf
}
