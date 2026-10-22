package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() { loginFailDelay = 0 }

func newAuth(t *testing.T, env map[string]string) (*Auth, string) {
	t.Helper()
	for _, k := range []string{"THEBAR_AUTH", "THEBAR_PASSWORD", "THEBAR_ALLOWED_HOSTS"} {
		t.Setenv(k, env[k])
	}
	dir := t.TempDir()
	a, err := NewAuth(dir)
	if err != nil {
		t.Fatal(err)
	}
	return a, dir
}

// call runs a handler like the browser would: from addr, to host, same-origin.
func call(h http.Handler, method, path, body, addr, host string, hdr map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = addr
	r.Host = host
	r.Header.Set("X-TheBar", "1")
	for k, v := range hdr {
		if v == "" {
			r.Header.Del(k)
		} else {
			r.Header.Set(k, v)
		}
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func sessionCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == cookieName && c.Value != "" {
			return c
		}
	}
	return nil
}

func (a *Auth) handlers() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", csrfGuard(a.HandleLogin))
	mux.HandleFunc("POST /api/setup", csrfGuard(a.HandleSetup))
	api := http.NewServeMux()
	api.HandleFunc("POST /api/password", a.HandleChangePassword)
	api.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("pong")) })
	mux.Handle("/api/", a.Middleware(api))
	return mux
}

const lan = "192.168.1.20:51000"

func TestFirstRunSetup(t *testing.T) {
	a, dir := newAuth(t, nil)
	h := a.handlers()
	if a.Configured() {
		t.Fatal("a fresh install must not be configured")
	}
	if w := call(h, "GET", "/api/ping", "", lan, "192.168.1.10:8800", nil); w.Code != 401 {
		t.Fatalf("API before setup: %d, want 401", w.Code)
	}
	if w := call(h, "POST", "/api/login", `{"password":"anything at all"}`, lan, "192.168.1.10:8800", nil); w.Code != 409 {
		t.Fatalf("login before setup: %d, want 409", w.Code)
	}

	w := call(h, "POST", "/api/setup", `{"password":"correct horse"}`, lan, "192.168.1.10:8800", nil)
	if w.Code != 200 {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	c := sessionCookie(w)
	if c == nil {
		t.Fatal("setup did not log the user in")
	}
	if !a.Configured() {
		t.Fatal("not configured after setup")
	}
	if w := call(h, "GET", "/api/ping", "", lan, "192.168.1.10:8800", nil, c); w.Code != 200 {
		t.Fatalf("API with the new session: %d", w.Code)
	}
	// the hash on disk is a bcrypt hash, readable only by us, and not the password
	b, _ := os.ReadFile(filepath.Join(dir, "password.hash"))
	if strings.Contains(string(b), "correct horse") || !strings.HasPrefix(string(b), "$2") {
		t.Fatalf("password.hash doesn't look like a bcrypt hash: %q", b)
	}
	if st, _ := os.Stat(filepath.Join(dir, "password.hash")); st.Mode().Perm() != 0o600 {
		t.Fatalf("password.hash mode %v, want 0600", st.Mode().Perm())
	}
	// setup is a one-time thing
	if w := call(h, "POST", "/api/setup", `{"password":"another password"}`, lan, "192.168.1.10:8800", nil); w.Code != 409 {
		t.Fatalf("second setup: %d, want 409", w.Code)
	}
	// and the password works for logging in
	if w := call(h, "POST", "/api/login", `{"password":"correct horse"}`, lan, "192.168.1.10:8800", nil); w.Code != 200 {
		t.Fatalf("login: %d", w.Code)
	}
}

func TestSetupOnlyForTheLocalOwner(t *testing.T) {
	good := `{"password":"correct horse"}`
	cases := []struct {
		name, addr, host string
		hdr              map[string]string
		env              map[string]string
		want             int
	}{
		{"IP address on the LAN", "192.168.1.20:5000", "192.168.1.10:8800", nil, nil, 200},
		{"localhost", "127.0.0.1:5000", "localhost:8800", nil, nil, 200},
		{"IPv6 loopback", "[::1]:5000", "[::1]:8800", nil, nil, 200},
		{"Tailscale address", "100.101.102.103:5000", "100.64.0.5:8800", nil, nil, 200},
		{"local name", "10.0.0.7:5000", "thebar.local:8800", nil, nil, 200},
		{"single-label name", "10.0.0.7:5000", "homeserver:8800", nil, nil, 200},
		{"name allowed by the owner", "10.0.0.7:5000", "bar.example.org", nil, map[string]string{"THEBAR_ALLOWED_HOSTS": "bar.example.org"}, 200},

		{"client from the internet", "8.8.8.8:5000", "192.168.1.10:8800", nil, nil, 403},
		{"public IPv6 client", "[2a02:168::1]:5000", "192.168.1.10:8800", nil, nil, 403},
		{"public host name (DNS rebinding)", "192.168.1.20:5000", "evil.example.com:8800", nil, nil, 403},
		{"behind a reverse proxy", "172.18.0.1:5000", "192.168.1.10:8800", map[string]string{"X-Forwarded-For": "203.0.113.9"}, nil, 403},
		{"behind a proxy (Forwarded)", "172.18.0.1:5000", "192.168.1.10:8800", map[string]string{"Forwarded": "for=203.0.113.9"}, nil, 403},
		{"cross-site request", "192.168.1.20:5000", "192.168.1.10:8800", map[string]string{"Sec-Fetch-Site": "cross-site"}, nil, 403},
		{"no custom header (CSRF)", "192.168.1.20:5000", "192.168.1.10:8800", map[string]string{"X-TheBar": ""}, nil, 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newAuth(t, tc.env)
			w := call(a.handlers(), "POST", "/api/setup", good, tc.addr, tc.host, tc.hdr)
			if w.Code != tc.want {
				t.Fatalf("got %d (%s), want %d", w.Code, strings.TrimSpace(w.Body.String()), tc.want)
			}
			if tc.want != 200 && a.Configured() {
				t.Fatal("a refused setup must not set a password")
			}
		})
	}
}

func TestPasswordRules(t *testing.T) {
	a, _ := newAuth(t, nil)
	h := a.handlers()
	for name, pw := range map[string]string{
		"empty":     "",
		"too short": "1234567",
		"too long":  strings.Repeat("a", 73),
	} {
		w := call(h, "POST", "/api/setup", `{"password":"`+pw+`"}`, lan, "192.168.1.10:8800", nil)
		if w.Code != 400 {
			t.Errorf("%s: got %d, want 400", name, w.Code)
		}
	}
	if a.Configured() {
		t.Fatal("a rejected password must not be saved")
	}
	// the limits themselves are fine
	if w := call(h, "POST", "/api/setup", `{"password":"`+strings.Repeat("é", 36)+`"}`, lan, "192.168.1.10:8800", nil); w.Code != 200 {
		t.Fatalf("72 bytes of unicode: %d %s", w.Code, w.Body)
	}
}

func setupDone(t *testing.T, env map[string]string) (*Auth, http.Handler, *http.Cookie) {
	t.Helper()
	a, _ := newAuth(t, env)
	h := a.handlers()
	w := call(h, "POST", "/api/setup", `{"password":"first password"}`, lan, "192.168.1.10:8800", nil)
	if w.Code != 200 {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	return a, h, sessionCookie(w)
}

func TestChangePassword(t *testing.T) {
	a, h, c1 := setupDone(t, nil)
	// a second device logged in earlier
	other := sessionCookie(call(h, "POST", "/api/login", `{"password":"first password"}`, "192.168.1.30:1", "192.168.1.10:8800", nil))
	do := func(body string, c *http.Cookie) *httptest.ResponseRecorder {
		return call(h, "POST", "/api/password", body, lan, "192.168.1.10:8800", nil, c)
	}

	if w := do(`{"current":"wrong one","next":"second password"}`, c1); w.Code != 403 {
		t.Fatalf("wrong current password: %d, want 403 (401 would mean \"not logged in\")", w.Code)
	}
	if w := do(`{"current":"first password","next":"short"}`, c1); w.Code != 400 {
		t.Fatalf("weak new password: %d, want 400", w.Code)
	}
	if w := call(h, "POST", "/api/password", `{"current":"first password","next":"second password"}`, lan, "192.168.1.10:8800", nil); w.Code != 401 {
		t.Fatalf("not logged in: %d, want 401", w.Code)
	}
	if !a.check("first password") {
		t.Fatal("failed attempts must not change the password")
	}

	w := do(`{"current":"first password","next":"second password"}`, c1)
	if w.Code != 200 {
		t.Fatalf("change: %d %s", w.Code, w.Body)
	}
	fresh := sessionCookie(w)
	if fresh == nil {
		t.Fatal("this browser must stay logged in (new cookie expected)")
	}
	if w := call(h, "GET", "/api/ping", "", lan, "192.168.1.10:8800", nil, fresh); w.Code != 200 {
		t.Fatalf("new session: %d", w.Code)
	}
	for name, c := range map[string]*http.Cookie{"the old session": c1, "the other device": other} {
		if w := call(h, "GET", "/api/ping", "", lan, "192.168.1.10:8800", nil, c); w.Code != 401 {
			t.Errorf("%s still works after a password change (%d)", name, w.Code)
		}
	}
	if w := call(h, "POST", "/api/login", `{"password":"first password"}`, lan, "192.168.1.10:8800", nil); w.Code != 401 {
		t.Errorf("old password still logs in (%d)", w.Code)
	}
	if w := call(h, "POST", "/api/login", `{"password":"second password"}`, lan, "192.168.1.10:8800", nil); w.Code != 200 {
		t.Errorf("new password doesn't log in (%d)", w.Code)
	}
}

func TestChangePasswordIsNotPossibleWhenSetByEnvironment(t *testing.T) {
	a, _ := newAuth(t, map[string]string{"THEBAR_PASSWORD": "from the environment"})
	h := a.handlers()
	if a.Configured() != true || !a.ManagedByEnv() {
		t.Fatal("env password should count as configured")
	}
	c := sessionCookie(call(h, "POST", "/api/login", `{"password":"from the environment"}`, lan, "192.168.1.10:8800", nil))
	if c == nil {
		t.Fatal("login with the env password failed")
	}
	if w := call(h, "POST", "/api/password", `{"current":"from the environment","next":"something else"}`, lan, "192.168.1.10:8800", nil, c); w.Code != 409 {
		t.Fatalf("got %d, want 409", w.Code)
	}
	if w := call(h, "POST", "/api/setup", `{"password":"another password"}`, lan, "192.168.1.10:8800", nil); w.Code != 409 {
		t.Fatalf("setup with env password: %d, want 409", w.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	a, h, _ := setupDone(t, nil)
	_ = a
	for i := 0; i < 10; i++ {
		if w := call(h, "POST", "/api/login", `{"password":"nope nope"}`, "192.168.1.99:1", "192.168.1.10:8800", nil); w.Code != 401 {
			t.Fatalf("attempt %d: %d", i+1, w.Code)
		}
	}
	// now even the right password is refused for this client, for a while ...
	if w := call(h, "POST", "/api/login", `{"password":"first password"}`, "192.168.1.99:1", "192.168.1.10:8800", nil); w.Code != 429 {
		t.Fatalf("after 10 failures: %d, want 429", w.Code)
	}
	// ... but not for everyone else
	if w := call(h, "POST", "/api/login", `{"password":"first password"}`, "192.168.1.50:1", "192.168.1.10:8800", nil); w.Code != 200 {
		t.Fatalf("another client: %d, want 200", w.Code)
	}
}

func TestChangePasswordAttemptsAreRateLimited(t *testing.T) {
	_, h, c := setupDone(t, nil)
	for i := 0; i < 10; i++ {
		call(h, "POST", "/api/password", `{"current":"guess","next":"second password"}`, lan, "192.168.1.10:8800", nil, c)
	}
	if w := call(h, "POST", "/api/password", `{"current":"first password","next":"second password"}`, lan, "192.168.1.10:8800", nil, c); w.Code != 429 {
		t.Fatalf("got %d, want 429", w.Code)
	}
}

func TestLegacyGeneratedPasswordFileIsRemoved(t *testing.T) {
	t.Setenv("THEBAR_AUTH", "")
	t.Setenv("THEBAR_PASSWORD", "")
	dir := t.TempDir()
	legacy := filepath.Join(dir, "initial-password.txt")
	os.WriteFile(legacy, []byte("old-secret\n"), 0o600)
	if _, err := NewAuth(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("initial-password.txt (plain text) should be deleted on start")
	}
}

func TestSessionCookieIsHardened(t *testing.T) {
	_, h, c := setupDone(t, nil)
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie flags: HttpOnly=%v SameSite=%v", c.HttpOnly, c.SameSite)
	}
	if c.Expires.Before(time.Now().Add(29 * 24 * time.Hour)) {
		t.Fatal("session should last about 30 days")
	}
	// over HTTPS (behind a proxy) the cookie is Secure
	w := call(h, "POST", "/api/login", `{"password":"first password"}`, lan, "192.168.1.10:8800", map[string]string{"X-Forwarded-Proto": "https"})
	if sc := sessionCookie(w); sc == nil || !sc.Secure {
		t.Fatal("cookie must be Secure when served over HTTPS")
	}
}

func TestPrivateAddressDetection(t *testing.T) {
	for ip, want := range map[string]bool{
		"127.0.0.1": true, "::1": true, "10.1.2.3": true, "172.16.0.1": true, "172.31.255.255": true, "192.168.0.1": true,
		"169.254.1.1": true, "fe80::1%eth0": true, "fd12:3456::1": true, "100.64.0.1": true, "::ffff:192.168.1.5": true,
		"8.8.8.8": false, "172.32.0.1": false, "100.128.0.1": false, "2001:4860:4860::8888": false, "": false, "not an ip": false,
	} {
		if got := isLocalAddr(ip); got != want {
			t.Errorf("isLocalAddr(%q) = %v, want %v", ip, got, want)
		}
	}
}
