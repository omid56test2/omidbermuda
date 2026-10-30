package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"omid-iran-bermuda/internal/gateway"
	"omid-iran-bermuda/internal/managed"
	"omid-iran-bermuda/internal/supervisor"
)

func main() {
	port := env("PORT", "8080")
	adminToken := os.Getenv("ADMIN_TOKEN")
	if len(adminToken) < 32 {
		log.Fatal("ADMIN_TOKEN must be set to at least 32 characters")
	}
	configPath := env("XRAY_CONFIG", "./data/xray-generated.json")
	store, err := managed.StoreFromEnv()
	if err != nil {
		log.Fatalf("initialize managed data: %v", err)
	}
	if err := store.WriteXrayConfig(configPath); err != nil {
		log.Fatalf("generate Xray config: %v", err)
	}
	sup := supervisor.New(os.Getenv("XRAY_BIN"), configPath)
	var syncMu sync.Mutex
	var gw *gateway.Gateway
	onConfigChanged := func() error {
		syncMu.Lock()
		defer syncMu.Unlock()
		if err := store.WriteXrayConfig(configPath); err != nil {
			return err
		}
		if gw != nil {
			gw.Refresh()
		}
		return sup.Restart(7 * time.Second)
	}
	gw = gateway.NewManaged(store, os.Getenv("TRUSTED_PROXY_CIDRS"))
	mux := http.NewServeMux()
	(&managed.HTTPHandler{Store: store, AdminToken: adminToken, PublicBaseURL: os.Getenv("PUBLIC_BASE_URL"), OnConfigChanged: onConfigChanged}).Register(mux)
	mux.Handle("/connect/", gw)
	mux.HandleFunc("GET /assets/bermuda-qr.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, "./web/bermuda-qr.js")
	})
	collector := managed.NewUsageCollector(store, os.Getenv("XRAY_BIN"), managed.DefaultXrayStatsAPIAddress, onConfigChanged)
	mux.HandleFunc("GET /admin/usage", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, adminToken) {
			http.Error(w, "unauthorized", 401)
			return
		}
		write(w, 200, collector.Snapshot())
	})
	startedAt := time.Now().UTC()
	mux.HandleFunc("GET /admin/system", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, adminToken) {
			http.Error(w, "unauthorized", 401)
			return
		}
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		write(w, 200, map[string]any{
			"uptime_seconds":     int64(time.Since(startedAt).Seconds()),
			"goroutines":         runtime.NumGoroutine(),
			"memory_alloc_bytes": mem.Alloc,
			"memory_sys_bytes":   mem.Sys,
			"gateway":            gw.Snapshot(),
			"xray":               sup.Snapshot(),
			"usage":              collector.Snapshot(),
		})
	})
	mux.HandleFunc("GET /admin/backup", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, adminToken) {
			http.Error(w, "unauthorized", 401)
			return
		}
		backup, err := store.ExportBackup()
		if err != nil {
			http.Error(w, "backup failed", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="omid-iran-bermuda-backup.json"`)
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(backup)
	})
	mux.HandleFunc("POST /admin/restore", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, adminToken) {
			write(w, 401, map[string]string{"detail": "unauthorized"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			write(w, 413, map[string]string{"detail": "backup upload is too large or unreadable"})
			return
		}
		if err := store.RestoreBackup(data); err != nil {
			write(w, 400, map[string]string{"detail": err.Error()})
			return
		}
		if err := onConfigChanged(); err != nil {
			write(w, 500, map[string]any{"restored": true, "config_sync": "failed", "error": err.Error()})
			return
		}
		write(w, 200, map[string]any{"restored": true, "config_sync": "updated"})
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"status": "ok", "service": "OMID-IRAN BERMUDA"})
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		s := sup.Snapshot()
		code := 200
		if !s.Running {
			code = 503
		}
		write(w, code, map[string]any{"ready": s.Running, "xray": s})
	})
	mux.HandleFunc("POST /admin/xray/rebuild", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, adminToken) {
			http.Error(w, "unauthorized", 401)
			return
		}
		if err := onConfigChanged(); err != nil {
			http.Error(w, "rebuild failed: "+err.Error(), 500)
			return
		}
		write(w, 200, map[string]string{"status": "config rebuilt", "path": configPath})
	})
	mux.HandleFunc("GET /admin/xray", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, adminToken) {
			http.Error(w, "unauthorized", 401)
			return
		}
		write(w, 200, sup.Snapshot())
	})
	mux.HandleFunc("POST /admin/xray/start", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, adminToken) {
			http.Error(w, "unauthorized", 401)
			return
		}
		if err := sup.Start(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		write(w, 202, map[string]string{"status": "starting"})
	})
	mux.HandleFunc("POST /admin/xray/stop", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, adminToken) {
			http.Error(w, "unauthorized", 401)
			return
		}
		if err := sup.Stop(8 * time.Second); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		write(w, 200, map[string]string{"status": "stopped"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "./web/index.html")
	})
	listenAddr := env("LISTEN_ADDR", ":"+port)
	srv := &http.Server{Addr: listenAddr, Handler: securityHeaders(mux), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go collector.Run(ctx)
	if strings.EqualFold(os.Getenv("XRAY_AUTORESTART"), "true") {
		go func() {
			backoff := 2 * time.Second
			consecutiveFailures := 0
			lastUnexpected := 0
			var stableSince time.Time
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					snap := sup.Snapshot()
					if snap.Running {
						if stableSince.IsZero() {
							stableSince = time.Now()
						}
						if time.Since(stableSince) >= 30*time.Second {
							backoff = 2 * time.Second
							lastUnexpected = snap.UnexpectedExits
						}
						continue
					}
					stableSince = time.Time{}
					if !sup.ShouldRestart() {
						continue
					}
					if snap.UnexpectedExits > lastUnexpected {
						lastUnexpected = snap.UnexpectedExits
						consecutiveFailures++
						backoff = recoveryBackoff(consecutiveFailures)
					}
					timer := time.NewTimer(backoff)
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
					if !sup.ShouldRestart() {
						continue
					}
					if err := sup.Start(); err != nil {
						log.Printf("xray auto-restart failed: %v", err)
						consecutiveFailures++
						backoff = recoveryBackoff(consecutiveFailures)
					}
				}
			}
		}()
	}
	if os.Getenv("XRAY_AUTOSTART") == "true" {
		if err := sup.Start(); err != nil {
			log.Printf("xray autostart failed: %v", err)
		}
	}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
		_ = sup.Stop(7 * time.Second)
	}()
	log.Printf("OMID-IRAN BERMUDA listening on %s", listenAddr)
	certFile, keyFile := os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE")
	var serveErr error
	if certFile != "" && keyFile != "" {
		serveErr = srv.ListenAndServeTLS(certFile, keyFile)
	} else {
		serveErr = srv.ListenAndServe()
	}
	if serveErr != nil && serveErr != http.ErrServerClosed {
		log.Fatal(serveErr)
	}
}
func recoveryBackoff(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	d := 2 * time.Second
	for i := 1; i < failures && d < 60*time.Second; i++ {
		d *= 2
	}
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

func authorized(r *http.Request, token string) bool {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if token == "" || len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(parts[1]), []byte(token)) == 1
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'; object-src 'none'")
		next.ServeHTTP(w, r)
	})
}
