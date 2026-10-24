package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed all:web/dist
var webFS embed.FS

var version = "dev"

type App struct {
	d       *Docker
	hub     *Hub
	inv     *Inventory
	stats   *Stats
	host    *Host
	store   *Store
	auth    *Auth
	jobs    *Jobs
	compose *Compose
	icons   *Icons
	proxy   *Proxy
	updates *Updates
	self    string
	static  map[string]*asset
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	dataDir := env("THEBAR_DATA", "/data")
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "passwd":
			cmdPasswd(dataDir)
			return
		case "version":
			println(version)
			return
		case "health":
			os.Exit(cmdHealth())
		}
	}
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		log.Fatal(err)
	}

	a := &App{self: os.Getenv("THEBAR_SELF")}
	a.d = NewDocker(env("THEBAR_DOCKER_SOCK", "/var/run/docker.sock"))
	a.hub = NewHub()
	a.inv = NewInventory(a.d, a.hub, a.self)
	a.stats = NewStats(a.d, a.inv, a.hub)
	a.host = NewHost(a.hub, env("THEBAR_DISKS", "System=/"))
	a.store = NewStore(dataDir)
	a.jobs = NewJobs(a.hub)
	a.compose = NewCompose(a.inv, a.jobs, dataDir)
	a.icons = NewIcons(dataDir, a.inv)
	a.proxy = NewProxy(a.inv)
	a.updates = NewUpdates(a.d, a.inv)
	var err error
	if a.auth, err = NewAuth(dataDir); err != nil {
		log.Fatal(err)
	}
	a.inv.enrich = a.enrich
	a.inv.onCreate = a.updates.RefreshLocal
	a.inv.onPull = a.updates.RefreshLocal
	if err := a.loadStatic(); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go a.inv.Run(ctx)
	go a.stats.Run(ctx)
	go a.stats.RunDiskUsage(ctx)
	go a.host.Run(ctx)
	go a.compose.RunDrift(ctx)
	go a.icons.RunIndex(ctx)
	go a.proxy.Run(ctx)
	go a.updates.Run(ctx)

	listen := env("THEBAR_LISTEN", "0.0.0.0:8800")
	network := "tcp"
	if h, _, err := net.SplitHostPort(listen); err == nil && (h == "0.0.0.0" || (net.ParseIP(h) != nil && net.ParseIP(h).To4() != nil)) {
		network = "tcp4" // 0.0.0.0 means IPv4 only; don't silently expose on public IPv6
	}
	ln, err := net.Listen(network, listen)
	if err != nil {
		log.Fatal(err)
	}
	// no WriteTimeout: the event and log streams stay open for hours
	srv := &http.Server{
		Handler:           a.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    32 << 10,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	log.Printf("TheBar %s listening on %s (%s)", version, listen, network)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// cmdHealth is used by the container healthcheck.
func cmdHealth() int {
	host, port, err := net.SplitHostPort(env("THEBAR_LISTEN", "0.0.0.0:8800"))
	if err != nil {
		return 1
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + net.JoinHostPort(host, port) + "/favicon.svg")
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return 1
	}
	return 0
}

// enrich adds derived info to a freshly inspected container.
func (a *App) enrich(c *Container) {
	c.WebPort = webPort(c)
	if c.WebPort > 0 {
		c.WebScheme = "http"
		if c.App != nil && c.App.Scheme != "" {
			c.WebScheme = c.App.Scheme
		}
		for _, p := range c.Ports {
			if p.Host == c.WebPort && (p.Container == 443 || p.Container == 8443 || p.Container == 9443) {
				c.WebScheme = "https"
			}
		}
		if c.NetworkMode == "host" && (c.WebPort == 443 || c.WebPort == 8443 || c.WebPort == 9443) {
			c.WebScheme = "https"
		}
	}
	c.Icon = a.icons.Resolve(c)
	c.Domains = a.proxy.Domains(c)
	if s := a.updates.Status(c); s != "" {
		c.Update = s
	}
	if c.Project != "" {
		c.Drift = false
		for _, s := range a.compose.Drift(c.Project) {
			if s == c.Service {
				c.Drift = true
			}
		}
	}
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	api := http.NewServeMux()

	mux.HandleFunc("GET /{$}", a.serveIndex)
	mux.HandleFunc("GET /assets/", a.serveStatic)
	mux.HandleFunc("GET /favicon.svg", a.serveStatic)
	// logos: only for logged-in users (they trigger downloads and fetches from inside the LAN)
	mux.HandleFunc("GET /icons/di/", a.loggedIn(a.icons.ServeDI))
	mux.HandleFunc("GET /icons/fav/", a.loggedIn(a.icons.ServeFavicon))
	mux.HandleFunc("POST /api/login", csrfGuard(a.auth.HandleLogin))
	mux.HandleFunc("POST /api/logout", csrfGuard(a.auth.HandleLogout))
	mux.HandleFunc("POST /api/setup", csrfGuard(a.auth.HandleSetup))

	api.HandleFunc("GET /api/init", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, a.initPayload(r)) })
	api.HandleFunc("GET /api/events", a.serveEvents)
	api.HandleFunc("PUT /api/layout", a.putLayout)
	api.HandleFunc("POST /api/password", a.auth.HandleChangePassword)
	api.HandleFunc("POST /api/containers/{id}/{action}", a.containerAction)
	api.HandleFunc("GET /api/containers/{id}/logs", a.serveLogs)
	api.HandleFunc("GET /api/compose/{project}", a.getCompose)
	api.HandleFunc("PUT /api/compose/{project}/file", a.putComposeFile)
	api.HandleFunc("GET /api/compose/{project}/backups", a.getBackups)
	api.HandleFunc("GET /api/compose/{project}/backup/{name}", a.getBackup)
	api.HandleFunc("POST /api/compose/{project}/action", a.composeAction)
	api.HandleFunc("GET /api/ports", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, UsedPorts(a.inv)) })
	api.HandleFunc("GET /api/icons", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, a.icons.Search(r.URL.Query().Get("q"))) })
	api.HandleFunc("POST /api/updates/check", func(w http.ResponseWriter, r *http.Request) {
		a.updates.CheckNow()
		writeJSON(w, map[string]bool{"ok": true})
	})
	api.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, a.jobs.List()) })
	mux.Handle("/api/", a.auth.Middleware(api))

	return securityHeaders(mux)
}

// csp: only our own scripts run; styles may be inline (Svelte and the editor set
// style attributes); images may come from anywhere because a card's logo can be
// any URL (they can't run code in an <img>).
const csp = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: https: http:; connect-src 'self'; font-src 'self'; " +
	"base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy", csp)
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Cross-Origin-Opener-Policy", "same-origin")
		hd.Set("Cross-Origin-Resource-Policy", "same-origin")
		hd.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		h.ServeHTTP(w, r)
	})
}

// loggedIn wraps a handler that is only for logged-in users but lives outside /api/.
func (a *App) loggedIn(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.auth.Valid(r) {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}
}

// millis is a Unix time in milliseconds, 0 for "never" (the zero time isn't 1970).
func millis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func (a *App) initPayload(r *http.Request) map[string]any {
	if !a.auth.Valid(r) {
		return map[string]any{"auth": false, "setup": !a.auth.Configured()}
	}
	layout, rev := a.store.Get()
	return map[string]any{
		"auth":            true,
		"authEnabled":     !a.auth.disabled,
		"passwordManaged": a.auth.ManagedByEnv(),
		"version":         version,
		"containers":      a.inv.List(),
		"history":         a.stats.History(),
		"host":            a.host.Info(),
		"layout":          layout,
		"rev":             rev,
		"jobs":            a.jobs.List(),
		"updatesChecked":  millis(a.updates.LastCheck()),
	}
}

func (a *App) serveEvents(w http.ResponseWriter, r *http.Request) {
	a.hub.Serve(w, r, func() []byte {
		// a reconnecting browser gets a fresh snapshot first
		var b bytes.Buffer
		b.Write(sseMsg("containers", a.inv.List()))
		b.Write(sseMsg("host", a.host.Info()))
		return b.Bytes()
	})
}

func (a *App) putLayout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Layout json.RawMessage `json:"layout"`
		Rev    int64           `json:"rev"`
		Origin string          `json:"origin"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	rev, err := a.store.Put(body.Layout, body.Rev)
	if errors.Is(err, errConflict) {
		cur, curRev := a.store.Get()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(409)
		json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "layout": cur, "rev": curRev})
		return
	}
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	a.hub.Broadcast("layout", map[string]any{"layout": body.Layout, "rev": rev, "origin": body.Origin})
	writeJSON(w, map[string]any{"rev": rev})
}

func (a *App) containerAction(w http.ResponseWriter, r *http.Request) {
	c := a.inv.Get(r.PathValue("id"))
	if c == nil {
		jsonError(w, 404, "no such container")
		return
	}
	action := r.PathValue("action")
	if c.Self && action != "restart" {
		jsonError(w, 400, "TheBar can't stop itself from here")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	if err := a.d.Action(ctx, c.ID, action); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	a.inv.Refresh()
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) serveLogs(w http.ResponseWriter, r *http.Request) {
	c := a.inv.Get(r.PathValue("id"))
	if c == nil {
		jsonError(w, 404, "no such container")
		return
	}
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tail <= 0 || tail > 5000 {
		tail = 300
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	type line struct {
		S int    `json:"s"`
		T string `json:"t"`
	}
	ch := make(chan line, 1024)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		err := a.d.Logs(ctx, c.ID, tail, true, c.Tty, func(s byte, t string) {
			select {
			case ch <- line{int(s), stripANSI(t)}:
			case <-ctx.Done():
			}
		})
		if err != nil && ctx.Err() == nil && !errors.Is(err, io.EOF) {
			ch <- line{2, "[thebar] log stream ended: " + err.Error()}
		}
		close(ch)
	}()
	// batch lines so a big tail doesn't mean thousands of tiny writes
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	var batch []line
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		if _, err := w.Write(sseMsg("lines", batch)); err != nil {
			return false
		}
		fl.Flush()
		batch = batch[:0]
		return true
	}
	for {
		select {
		case l, ok := <-ch:
			if !ok {
				flush()
				w.Write(sseMsg("end", true))
				fl.Flush()
				return
			}
			batch = append(batch, l)
			if len(batch) >= 500 && !flush() {
				return
			}
		case <-tick.C:
			if !flush() {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (a *App) getCompose(w http.ResponseWriter, r *http.Request) {
	res, err := a.compose.Read(r.PathValue("project"))
	if err != nil {
		jsonError(w, 404, err.Error())
		return
	}
	writeJSON(w, res)
}

func (a *App) putComposeFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Mtime   int64  `json:"mtime"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	res, err := a.compose.Save(r.Context(), r.PathValue("project"), body.Path, body.Content, body.Mtime)
	if err != nil {
		code := 400
		if errors.Is(err, errStale) {
			code = 409
		}
		jsonError(w, code, err.Error())
		return
	}
	writeJSON(w, res)
}

func (a *App) getBackups(w http.ResponseWriter, r *http.Request) {
	list, err := a.compose.Backups(r.PathValue("project"), r.URL.Query().Get("path"))
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	if list == nil {
		list = []Backup{}
	}
	writeJSON(w, list)
}

func (a *App) getBackup(w http.ResponseWriter, r *http.Request) {
	s, err := a.compose.ReadBackup(r.PathValue("project"), r.PathValue("name"))
	if err != nil {
		jsonError(w, 404, err.Error())
		return
	}
	writeJSON(w, map[string]string{"content": s})
}

func (a *App) composeAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action   string   `json:"action"`
		Services []string `json:"services"`
	}
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	project := r.PathValue("project")
	if body.Action == "stop" {
		for _, c := range a.inv.List() {
			if c.Self && c.Project == project && (len(body.Services) == 0 || contains(body.Services, c.Service)) {
				jsonError(w, 400, "that would stop TheBar itself")
				return
			}
		}
	}
	job, err := a.compose.Action(r.Context(), project, body.Action, body.Services)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	writeJSON(w, job)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---- static files ----

type asset struct {
	body, gz []byte
	ctype    string
	etag     string
}

func (a *App) loadStatic() error {
	a.static = map[string]*asset{}
	sub, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		return err
	}
	return fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(path.Base(p), ".") {
			return err
		}
		b, err := fs.ReadFile(sub, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		as := &asset{body: b, ctype: mime.TypeByExtension(path.Ext(p)), etag: `"` + hex.EncodeToString(sum[:8]) + `"`}
		if len(b) > 1024 {
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			zw.Write(b)
			zw.Close()
			as.gz = buf.Bytes()
		}
		a.static["/"+p] = as
		return nil
	})
}

func (a *App) writeAsset(w http.ResponseWriter, r *http.Request, as *asset, body []byte) {
	if as.ctype != "" {
		w.Header().Set("Content-Type", as.ctype)
	}
	w.Header().Set("Vary", "Accept-Encoding")
	if body == nil && as.gz != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(as.gz)
		return
	}
	if body == nil {
		body = as.body
	}
	w.Write(body)
}

func (a *App) serveStatic(w http.ResponseWriter, r *http.Request) {
	as, ok := a.static[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // hashed names
	} else {
		w.Header().Set("Cache-Control", "no-cache") // unhashed names (favicon): revalidate via ETag
	}
	w.Header().Set("ETag", as.etag)
	if r.Header.Get("If-None-Match") == as.etag {
		w.WriteHeader(304)
		return
	}
	a.writeAsset(w, r, as, nil)
}

// serveIndex inlines the initial data so the page renders without waiting
// for a second request.
func (a *App) serveIndex(w http.ResponseWriter, r *http.Request) {
	as, ok := a.static["/index.html"]
	if !ok {
		http.Error(w, "frontend not built", 500)
		return
	}
	init, _ := json.Marshal(a.initPayload(r)) // json.Marshal escapes <, > and &
	// a JSON data block is never executed, so the CSP can forbid inline scripts
	page := bytes.Replace(as.body, []byte("<!--INIT-->"), []byte(`<script id="init" type="application/json">`+string(init)+`</script>`), 1)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && len(page) > 1024 {
		w.Header().Set("Content-Encoding", "gzip")
		zw, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
		zw.Write(page)
		zw.Close()
		return
	}
	w.Write(page)
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v)
}
