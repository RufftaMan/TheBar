package main

// Single-password auth. Kept self-contained so it can later be swapped for
// proper SSO without touching the rest of the app.
//
// First run: no password exists yet and the web UI offers a setup screen
// (only reachable from the local network, see setupBlockedReason). Afterwards
// the password can be changed from the settings, or on the server with
// `thebar passwd`. THEBAR_PASSWORD in the environment overrides everything.
//
// The session cookie is stateless: expiry + HMAC(secret, expiry + password
// fingerprint). Changing the password therefore logs everyone out.

import (
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

const (
	cookieName     = "thebar_session"
	sessionLife    = 30 * 24 * time.Hour
	minPasswordLen = 8
	maxPasswordLen = 72 // bcrypt only looks at the first 72 bytes
)

// loginFailDelay slows down guessing; tests shorten it.
var loginFailDelay = time.Second

type Auth struct {
	disabled     bool
	plain        string // THEBAR_PASSWORD, if set
	hashPath     string
	secret       []byte
	allowedHosts map[string]bool // extra host names accepted during first-run setup

	pwMu     sync.Mutex // serialises setup and password changes
	mu       sync.Mutex
	failures map[string][]time.Time
}

func NewAuth(dataDir string) (*Auth, error) {
	a := &Auth{
		disabled:     strings.EqualFold(os.Getenv("THEBAR_AUTH"), "off"),
		plain:        os.Getenv("THEBAR_PASSWORD"),
		hashPath:     filepath.Join(dataDir, "password.hash"),
		failures:     map[string][]time.Time{},
		allowedHosts: map[string]bool{},
	}
	for _, h := range strings.Split(os.Getenv("THEBAR_ALLOWED_HOSTS"), ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			a.allowedHosts[h] = true
		}
	}
	// older versions generated a password and left it in a file: don't keep it around
	os.Remove(filepath.Join(dataDir, "initial-password.txt"))

	secPath := filepath.Join(dataDir, "secret.key")
	sec, err := os.ReadFile(secPath)
	if err != nil || len(sec) < 32 {
		sec = make([]byte, 32)
		if _, err := rand.Read(sec); err != nil {
			return nil, err
		}
		if err := os.WriteFile(secPath, sec, 0o600); err != nil {
			return nil, err
		}
	}
	a.secret = sec
	switch {
	case a.disabled:
		log.Printf("WARNING: authentication is disabled (THEBAR_AUTH=off)")
	case !a.Configured():
		log.Printf("TheBar isn't set up yet: open it in a browser on your local network to choose a password " +
			"(or run: docker exec -it thebar thebar passwd)")
	}
	return a, nil
}

// Configured tells whether a password exists (or auth is off / set by env).
func (a *Auth) Configured() bool {
	if a.disabled || a.plain != "" {
		return true
	}
	b, err := os.ReadFile(a.hashPath)
	return err == nil && len(strings.TrimSpace(string(b))) > 0
}

// ManagedByEnv is true when THEBAR_PASSWORD decides the password.
func (a *Auth) ManagedByEnv() bool { return a.plain != "" }

func validatePassword(pw string) error {
	switch {
	case len(pw) < minPasswordLen:
		return fmt.Errorf("The password must be at least %d characters long.", minPasswordLen)
	case len(pw) > maxPasswordLen:
		return fmt.Errorf("The password can be at most %d bytes long.", maxPasswordLen)
	}
	return nil
}

func writePasswordHash(path, pw string) error {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, h, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// fingerprint identifies the current password without revealing it.
func (a *Auth) fingerprint() string {
	src := []byte(a.plain)
	if a.plain == "" {
		src, _ = os.ReadFile(a.hashPath)
	}
	sum := sha256.Sum256(src)
	return hex.EncodeToString(sum[:8])
}

func (a *Auth) check(pw string) bool {
	if a.plain != "" {
		return subtle.ConstantTimeCompare([]byte(pw), []byte(a.plain)) == 1
	}
	h, err := os.ReadFile(a.hashPath) // read each time so `thebar passwd` applies without restart
	if err != nil {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(strings.TrimSpace(string(h))), []byte(pw)) == nil
}

func (a *Auth) sign(exp int64) string {
	m := hmac.New(sha256.New, a.secret)
	fmt.Fprintf(m, "v1|%d|%s", exp, a.fingerprint())
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (a *Auth) Valid(r *http.Request) bool {
	if a.disabled {
		return true
	}
	if !a.Configured() {
		return false
	}
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	expS, sig, ok := strings.Cut(c.Value, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(expS, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(a.sign(exp)))
}

func (a *Auth) setCookie(w http.ResponseWriter, r *http.Request) {
	exp := time.Now().Add(sessionLife).Unix()
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    fmt.Sprintf("%d.%s", exp, a.sign(exp)),
		Path:     "/",
		Expires:  time.Unix(exp, 0),
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// tooManyFailures reports whether this client has used up its attempts.
func (a *Auth) tooManyFailures(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	var recent []time.Time
	for _, t := range a.failures[ip] {
		if time.Since(t) < 15*time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		delete(a.failures, ip)
	} else {
		a.failures[ip] = recent
	}
	return len(recent) >= 10
}

func (a *Auth) recordFailure(ip string) {
	a.mu.Lock()
	a.failures[ip] = append(a.failures[ip], time.Now())
	a.mu.Unlock()
	time.Sleep(loginFailDelay)
}

func (a *Auth) clearFailures(ip string) {
	a.mu.Lock()
	delete(a.failures, ip)
	a.mu.Unlock()
}

func (a *Auth) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if !a.Configured() {
		jsonError(w, 409, "TheBar isn't set up yet.")
		return
	}
	ip := clientIP(r)
	if a.tooManyFailures(ip) {
		jsonError(w, 429, "Too many attempts. Try again in a few minutes.")
		return
	}
	var body struct{ Password string }
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "bad request")
		return
	}
	if !a.check(body.Password) {
		a.recordFailure(ip)
		jsonError(w, 401, "Wrong password")
		return
	}
	a.clearFailures(ip)
	a.setCookie(w, r)
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *Auth) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, map[string]bool{"ok": true})
}

// HandleSetup lets the first visitor choose the password.
func (a *Auth) HandleSetup(w http.ResponseWriter, r *http.Request) {
	if a.Configured() {
		jsonError(w, 409, "TheBar is already set up.")
		return
	}
	if reason := a.setupBlockedReason(r); reason != "" {
		jsonError(w, 403, reason)
		return
	}
	ip := clientIP(r)
	if a.tooManyFailures(ip) {
		jsonError(w, 429, "Too many attempts. Try again in a few minutes.")
		return
	}
	var body struct{ Password string }
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "bad request")
		return
	}
	if err := validatePassword(body.Password); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	a.pwMu.Lock()
	defer a.pwMu.Unlock()
	if a.Configured() { // somebody else was faster
		jsonError(w, 409, "TheBar is already set up.")
		return
	}
	if err := writePasswordHash(a.hashPath, body.Password); err != nil {
		log.Printf("setup: %v", err)
		jsonError(w, 500, "Could not save the password.")
		return
	}
	log.Printf("Password set up from %s", ip)
	a.setCookie(w, r)
	writeJSON(w, map[string]bool{"ok": true})
}

// HandleChangePassword changes the password for a logged-in user. Everybody
// else is logged out (their cookies stop matching); this browser gets a new one.
func (a *Auth) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	switch {
	case a.disabled:
		jsonError(w, 409, "Authentication is turned off.")
		return
	case a.plain != "":
		jsonError(w, 409, "The password is set by THEBAR_PASSWORD in the environment. Change it there.")
		return
	}
	ip := clientIP(r)
	if a.tooManyFailures(ip) {
		jsonError(w, 429, "Too many attempts. Try again in a few minutes.")
		return
	}
	var body struct{ Current, Next string }
	if err := readJSON(r, &body); err != nil {
		jsonError(w, 400, "bad request")
		return
	}
	if err := validatePassword(body.Next); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	a.pwMu.Lock()
	defer a.pwMu.Unlock()
	if !a.check(body.Current) {
		a.recordFailure(ip)
		// not 401: that means "not logged in" and makes the web app reload
		jsonError(w, 403, "The current password is wrong.")
		return
	}
	if err := writePasswordHash(a.hashPath, body.Next); err != nil {
		log.Printf("change password: %v", err)
		jsonError(w, 500, "Could not save the password.")
		return
	}
	a.clearFailures(ip)
	log.Printf("Password changed from %s", ip)
	a.setCookie(w, r)
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- first-run setup safety ----
//
// Whoever reaches the setup screen first chooses the password, so it's limited
// to what an owner at home looks like: a direct connection from the local
// network, addressed by IP or a local name. That keeps out the internet, a
// reverse proxy in front of TheBar, and DNS-rebinding web pages (which arrive
// under a public host name).

func (a *Auth) setupBlockedReason(r *http.Request) string {
	for _, h := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Real-Ip", "Forwarded"} {
		if r.Header.Get(h) != "" {
			return "First-time setup has to be done directly, not through a reverse proxy. Open TheBar by its IP address, e.g. http://192.168.1.10:8800."
		}
	}
	if !isLocalAddr(clientIP(r)) {
		return "First-time setup is only allowed from your local network."
	}
	if !a.isLocalHost(r.Host) {
		return "For the first-time setup, open TheBar by its IP address (e.g. http://192.168.1.10:8800) or a local name. " +
			"To allow another name, list it in THEBAR_ALLOWED_HOSTS."
	}
	return ""
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10") // Tailscale and carrier-grade NAT

func isLocalAddr(s string) bool {
	if i := strings.IndexByte(s, '%'); i >= 0 { // IPv6 zone
		s = s[:i]
	}
	ip, err := netip.ParseAddr(s)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip)
}

func (a *Auth) isLocalHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "" {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true // typed as an IP address: not a name an attacker controls
	}
	if a.allowedHosts[host] || !strings.Contains(host, ".") {
		return true // "localhost", "server", ...
	}
	for _, suf := range []string{".local", ".lan", ".home", ".internal", ".localdomain", ".home.arpa", ".localhost"} {
		if strings.HasSuffix(host, suf) {
			return true
		}
	}
	return false
}

// csrfGuard rejects cross-site requests: state-changing calls must carry a
// custom header (which browsers only send same-origin or after a CORS
// preflight we never grant) and must not be flagged cross-site by the browser.
func csrfGuard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if blocked(r) {
			jsonError(w, 403, "missing request header")
			return
		}
		next(w, r)
	}
}

func blocked(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return false
	}
	return r.Header.Get("X-TheBar") != "1" || r.Header.Get("Sec-Fetch-Site") == "cross-site"
}

// Middleware protects API routes. Non-GET requests must also carry a custom
// header, which browsers won't send cross-site without CORS (CSRF guard).
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.Valid(r) {
			jsonError(w, 401, "login required")
			return
		}
		if blocked(r) {
			jsonError(w, 403, "missing request header")
			return
		}
		// slide the session: refresh the cookie when it's past half its life
		if c, err := r.Cookie(cookieName); err == nil && !a.disabled {
			if expS, _, ok := strings.Cut(c.Value, "."); ok {
				if exp, _ := strconv.ParseInt(expS, 10, 64); time.Until(time.Unix(exp, 0)) < sessionLife/2 {
					a.setCookie(w, r)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// cmdPasswd implements `thebar passwd`: sets (or resets) the password from the
// command line. Works for first-time setup too, and is the way back in when
// the password is forgotten.
func cmdPasswd(dataDir string) {
	read := func(prompt string) string {
		fmt.Fprint(os.Stderr, prompt)
		if term.IsTerminal(int(os.Stdin.Fd())) {
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				os.Exit(1)
			}
			return string(b)
		}
		sc := bufio.NewScanner(os.Stdin)
		if !sc.Scan() {
			os.Exit(1)
		}
		return sc.Text()
	}
	pw := read("New password: ")
	if err := validatePassword(pw); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if term.IsTerminal(int(os.Stdin.Fd())) && read("Repeat it: ") != pw {
		fmt.Fprintln(os.Stderr, "The two passwords don't match.")
		os.Exit(1)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := writePasswordHash(filepath.Join(dataDir, "password.hash"), pw); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Remove(filepath.Join(dataDir, "initial-password.txt"))
	fmt.Fprintln(os.Stderr, "Password updated. All existing sessions are logged out.")
	if os.Getenv("THEBAR_PASSWORD") != "" {
		fmt.Fprintln(os.Stderr, "Note: THEBAR_PASSWORD is set in the environment and takes precedence. Remove it to use this password.")
	}
}
