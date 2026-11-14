package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"townsquare/internal/config"
	"townsquare/internal/store"
	"townsquare/internal/web"
)

func main() {
	cfg := config.Load()

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer st.Close()
	_ = st.CleanupSessions()
	_ = st.CleanupPasswordResets()

	n, err := st.CountUsers()
	if err != nil {
		log.Fatalf("count users: %v", err)
	}
	if n == 0 && !cfg.AllowSetup {
		log.Fatal("no users exist and TS_ALLOW_SETUP is false; set TS_ALLOW_SETUP=true to create the first account")
	}
	if strings.HasPrefix(cfg.BaseURL, "https://") && !cfg.SecureCookies {
		log.Printf("warning: TS_BASE_URL is HTTPS but TS_SECURE_COOKIES is false; cookies will not be marked Secure")
	}
	if cfg.BaseURL == "" && !cfg.SecureCookies {
		log.Printf("warning: TS_SECURE_COOKIES is false; set it to true when the public URL is HTTPS")
	}

	srv, err := web.New(cfg, st)
	if err != nil {
		log.Fatalf("setup web server: %v", err)
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "auto-detected per request"
	}
	go func() {
		log.Printf("%s %s listening on %s (base URL %s)", cfg.SiteName, web.Version, cfg.Addr, baseURL)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
