package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"
	"unicode/utf8"
)

//go:embed assets/favicon.ico
var assetsFS embed.FS

const (
	defaultPort = "9090"
	maxNameLen  = 50
)

var (
	defaultVersion = "1.0.0"
	startedAt      = time.Now()
)

var indexTmpl = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Hello World</title>
<link rel="icon" type="image/x-icon" href="/favicon.ico">
<style>
:root { color-scheme: light dark; }
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; }
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Oxygen, Ubuntu, Cantarell, "Helvetica Neue", Arial, sans-serif;
  background: #f7f7f8;
  color: #111;
  min-height: 100vh;
  display: flex;
  flex-direction: column;
}
main {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 2rem;
  text-align: center;
}
h1 {
  font-size: clamp(2rem, 6vw, 3.5rem);
  margin: 0 0 0.5rem;
  letter-spacing: -0.02em;
}
p.sub { margin: 0; color: #555; font-size: 1.1rem; }
footer {
  padding: 1rem;
  text-align: center;
  font-size: 0.85rem;
  color: #666;
  border-top: 1px solid rgba(0,0,0,0.08);
}
@media (prefers-color-scheme: dark) {
  body { background: #0e0f12; color: #f2f2f3; }
  p.sub { color: #bbb; }
  footer { color: #9aa0a6; border-top-color: rgba(255,255,255,0.08); }
}
</style>
</head>
<body>
<main>
  <h1>Hello, {{.Name}}!</h1>
  <p class="sub">Welcome to a tiny Go web app.</p>
</main>
<footer>
  <span>v{{.Version}}</span> &middot; <span>uptime {{.Uptime}}</span>
</footer>
</body>
</html>
`))

var notFoundTmpl = template.Must(template.New("404").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Page not found</title>
<link rel="icon" type="image/x-icon" href="/favicon.ico">
<style>
:root { color-scheme: light dark; }
html, body { margin: 0; padding: 0; }
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  background: #f7f7f8;
  color: #111;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  text-align: center;
}
main { padding: 2rem; }
h1 { font-size: 2.25rem; margin: 0 0 0.5rem; }
p { color: #555; }
a { color: #0b66d2; text-decoration: none; }
a:hover { text-decoration: underline; }
@media (prefers-color-scheme: dark) {
  body { background: #0e0f12; color: #f2f2f3; }
  p { color: #bbb; }
  a { color: #6ea8ff; }
}
</style>
</head>
<body>
<main>
  <h1>404 &mdash; Page not found</h1>
  <p>The page you requested does not exist.</p>
  <p><a href="/">&larr; Back to home</a></p>
</main>
</body>
</html>
`))

type indexData struct {
	Name    string
	Version string
	Uptime  string
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (sw *statusWriter) WriteHeader(code int) {
	if sw.wroteHeader {
		return
	}
	sw.status = code
	sw.wroteHeader = true
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	if !sw.wroteHeader {
		sw.status = http.StatusOK
		sw.wroteHeader = true
	}
	return sw.ResponseWriter.Write(b)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Strict-Transport-Security", "max-age=63072000")
		next.ServeHTTP(w, r)
	})
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if rec := recover(); rec != nil {
				if !sw.wroteHeader {
					sw.WriteHeader(http.StatusInternalServerError)
				}
				log.Printf("%s %s %s %d %s panic=%v",
					time.Now().UTC().Format(time.RFC3339),
					r.Method, r.URL.Path, http.StatusInternalServerError,
					time.Since(start), rec)
				return
			}
			fmt.Printf("%s %s %s %d %s\n",
				time.Now().UTC().Format(time.RFC3339),
				r.Method, r.URL.Path, sw.status, time.Since(start))
		}()
		next.ServeHTTP(sw, r)
	})
}

func normalizeName(raw string) string {
	n := strings.TrimSpace(raw)
	if n == "" {
		return "World"
	}
	if utf8.RuneCountInString(n) > maxNameLen {
		runes := []rune(n)
		n = string(runes[:maxNameLen])
	}
	return n
}

func version() string {
	if v := strings.TrimSpace(os.Getenv("VERSION")); v != "" {
		return v
	}
	return defaultVersion
}

func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	s := int((d % time.Minute) / time.Second)
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		handleNotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := normalizeName(r.URL.Query().Get("name"))
	data := indexData{
		Name:    name,
		Version: version(),
		Uptime:  formatUptime(time.Since(startedAt)),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTmpl.Execute(w, data); err != nil {
		log.Printf("render index error: %v", err)
	}
}

func handleFavicon(w http.ResponseWriter, r *http.Request) {
	data, err := assetsFS.ReadFile("assets/favicon.ico")
	if err != nil {
		http.Error(w, "favicon unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(data)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"uptimeSeconds": time.Since(startedAt).Seconds(),
		"version":       version(),
		"startedAt":     startedAt.UTC().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

func handleNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_ = notFoundTmpl.Execute(w, nil)
}

func buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/favicon.ico", handleFavicon)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/status", handleStatus)
	return mux
}

func port() string {
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		return p
	}
	return defaultPort
}

func main() {
	addr := ":" + port()
	srv := &http.Server{
		Addr:              addr,
		Handler:           requestLogger(securityHeaders(buildMux())),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscallTerm)
		sig := <-sigCh
		log.Printf("received signal %v, shutting down...", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown error: %v", err)
		}
		close(idleConnsClosed)
	}()

	log.Printf("hello-web v%s listening on %s", version(), addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
	<-idleConnsClosed
	log.Printf("server stopped cleanly")
}
