package main

// Logos: dashboard-icons (homarr-labs) via jsDelivr, cached on disk.
// Fallback: the app's own favicon, fetched from its web UI.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const iconCDN = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons"

type Icons struct {
	dir string
	inv *Inventory

	mu     sync.RWMutex
	index  map[string]string // slug -> preferred ext (svg, png, webp)
	neg    map[string]time.Time
	fetch  sync.Mutex
	client *http.Client
}

func NewIcons(dataDir string, inv *Inventory) *Icons {
	ic := &Icons{
		dir: filepath.Join(dataDir, "icons"), inv: inv,
		index: map[string]string{}, neg: map[string]time.Time{},
		client: &http.Client{Timeout: 15 * time.Second},
	}
	os.MkdirAll(filepath.Join(ic.dir, "di"), 0o755)
	os.MkdirAll(filepath.Join(ic.dir, "fav"), 0o755)
	ic.loadIndex()
	return ic
}

func (ic *Icons) indexPath() string { return filepath.Join(ic.dir, "tree.json") }

func (ic *Icons) loadIndex() bool {
	b, err := os.ReadFile(ic.indexPath())
	if err != nil {
		return false
	}
	var tree map[string][]string
	if json.Unmarshal(b, &tree) != nil {
		return false
	}
	idx := map[string]string{}
	for _, ext := range []string{"webp", "png", "svg"} { // later wins: svg preferred
		for _, f := range tree[ext] {
			idx[strings.TrimSuffix(f, "."+ext)] = ext
		}
	}
	ic.mu.Lock()
	ic.index = idx
	ic.mu.Unlock()
	return len(idx) > 0
}

// RunIndex keeps the icon index fresh (weekly).
func (ic *Icons) RunIndex(ctx context.Context) {
	for {
		st, err := os.Stat(ic.indexPath())
		if err != nil || time.Since(st.ModTime()) > 7*24*time.Hour {
			if err := ic.downloadIndex(ctx); err != nil {
				log.Printf("icon index: %v", err)
			} else if ic.loadIndex() {
				ic.inv.Refresh()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(6 * time.Hour):
		}
	}
}

func (ic *Icons) downloadIndex(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", iconCDN+"/tree.json", nil)
	resp, err := ic.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil || !json.Valid(b) {
		return fmt.Errorf("bad index")
	}
	tmp := ic.indexPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, ic.indexPath())
}

func (ic *Icons) lookup(slug string) (string, bool) {
	ic.mu.RLock()
	defer ic.mu.RUnlock()
	ext, ok := ic.index[slug]
	return ext, ok
}

// Search lists icon slugs matching q (for the icon picker).
func (ic *Icons) Search(q string) []map[string]string {
	q = strings.ToLower(strings.TrimSpace(q))
	ic.mu.RLock()
	var hits []string
	for slug := range ic.index {
		if q == "" || strings.Contains(slug, q) {
			hits = append(hits, slug)
		}
	}
	ic.mu.RUnlock()
	sort.Slice(hits, func(i, j int) bool {
		pi, pj := strings.HasPrefix(hits[i], q), strings.HasPrefix(hits[j], q)
		if pi != pj {
			return pi
		}
		if len(hits[i]) != len(hits[j]) {
			return len(hits[i]) < len(hits[j])
		}
		return hits[i] < hits[j]
	})
	if len(hits) > 48 {
		hits = hits[:48]
	}
	out := make([]map[string]string, 0, len(hits))
	for _, h := range hits {
		ext, _ := ic.lookup(h)
		out = append(out, map[string]string{"slug": h, "url": "/icons/di/" + h + "." + ext})
	}
	return out
}

// Resolve picks the logo URL for a container.
func (ic *Icons) Resolve(c *Container) string {
	var cands []string
	if v := c.Labels["thebar.icon"]; v != "" {
		if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
			return v
		}
		cands = append(cands, v)
	}
	if c.App != nil && c.App.Icon != "" {
		cands = append(cands, c.App.Icon)
	}
	slug := imageSlug(c.Image)
	cands = append(cands, c.AppKey, slug, c.Service, strings.TrimSuffix(slug, "-server"), strings.TrimSuffix(slug, "-app"))
	for _, s := range cands {
		if s == "" {
			continue
		}
		if ext, ok := ic.lookup(s); ok {
			return "/icons/di/" + s + "." + ext
		}
	}
	if webPort(c) > 0 {
		return "/icons/fav/" + url.PathEscape(c.Name)
	}
	return ""
}

// webPort is the host port where the container's web UI is reachable, 0 if none.
// usualWebPorts, in order of preference, when several candidates are left.
var usualWebPorts = []int{80, 8080, 3000, 5000, 8000, 443, 8443}

func pickPort(ports []int) int {
	for _, want := range usualWebPorts {
		for _, p := range ports {
			if p == want {
				return p
			}
		}
	}
	lowest := 0
	for _, p := range ports {
		if lowest == 0 || p < lowest {
			lowest = p
		}
	}
	return lowest
}

// hostModePort finds the web UI of a container on host networking. Docker
// publishes nothing there, so there are no port bindings to read. Instead:
// a known app's usual port, else the ports its image declares (EXPOSE) that
// something really listens on. A stopped container has no sockets to check, so
// its first declared port is used and its card keeps its link.
func hostModePort(c *Container) int {
	if c.Self {
		return 0 // no link from this dashboard to itself
	}
	if c.App != nil && c.App.Port > 0 {
		return c.App.Port
	}
	if len(c.Exposed) == 0 {
		return 0
	}
	if c.State != "running" {
		return pickPort(c.Exposed)
	}
	up := listeningTCP()
	var live []int
	for _, p := range c.Exposed {
		if up[p] {
			live = append(live, p)
		}
	}
	return pickPort(live)
}

// listeningTCP is the set of TCP ports something listens on right now (TheBar
// runs with host networking, so it sees the host's sockets). A variable so
// tests can replace it.
var listeningTCP = func() map[int]bool {
	m := map[int]bool{}
	for key := range listeningPorts() {
		if ps, proto, _ := strings.Cut(key, "/"); proto == "tcp" {
			if n, err := strconv.Atoi(ps); err == nil {
				m[n] = true
			}
		}
	}
	return m
}

func webPort(c *Container) int {
	// the label wins in every mode: for anything detection can't work out
	if n, err := strconv.Atoi(c.Labels["thebar.port"]); err == nil && n > 0 && n < 65536 {
		return n
	}
	if c.NetworkMode == "host" {
		return hostModePort(c)
	}
	var tcp []Port
	for _, p := range c.Ports {
		if p.Proto == "tcp" {
			tcp = append(tcp, p)
		}
	}
	if len(tcp) == 0 {
		return 0
	}
	if c.App != nil && c.App.Port > 0 {
		for _, p := range tcp {
			if p.Container == c.App.Port {
				return p.Host
			}
		}
	}
	for _, want := range usualWebPorts {
		for _, p := range tcp {
			if p.Container == want {
				return p.Host
			}
		}
	}
	return tcp[0].Host
}

var safeSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*\.(svg|png|webp)$`)

func iconHeaders(w http.ResponseWriter, ctype string) {
	w.Header().Set("Content-Type", ctype)
	// third-party SVGs: never let them run scripts on our origin
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

var extType = map[string]string{"svg": "image/svg+xml", "png": "image/png", "webp": "image/webp"}

// ServeDI serves /icons/di/<slug>.<ext>, downloading on first use.
func (ic *Icons) ServeDI(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/icons/di/")
	if !safeSlug.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	ext := name[strings.LastIndex(name, ".")+1:]
	slug := strings.TrimSuffix(name, "."+ext)
	path := filepath.Join(ic.dir, "di", name)
	if _, err := os.Stat(path); err != nil {
		if have, ok := ic.lookup(slug); !ok || have != ext {
			http.NotFound(w, r)
			return
		}
		if err := ic.download(r.Context(), ext+"/"+name, path); err != nil {
			log.Printf("icon %s: %v", name, err)
			http.Error(w, "icon unavailable", 502)
			return
		}
	}
	iconHeaders(w, extType[ext])
	w.Header().Set("Cache-Control", "public, max-age=604800")
	http.ServeFile(w, r, path)
}

func (ic *Icons) download(ctx context.Context, rel, dst string) error {
	ic.fetch.Lock()
	defer ic.fetch.Unlock()
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", iconCDN+"/"+rel, nil)
	resp, err := ic.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

var linkIcon = regexp.MustCompile(`(?is)<link\b[^>]*>`)
var attr = regexp.MustCompile(`(?is)\b(rel|href)\s*=\s*["']([^"']*)["']`)

// ServeFavicon serves /icons/fav/<container>, scraping the app's web UI once.
func (ic *Icons) ServeFavicon(w http.ResponseWriter, r *http.Request) {
	name, _ := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/icons/fav/"))
	c := ic.inv.Get(name)
	if c == nil || strings.ContainsAny(name, "/\\") {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(ic.dir, "fav", name)
	if b, err := os.ReadFile(path); err == nil {
		ctype, _ := os.ReadFile(path + ".type")
		iconHeaders(w, string(ctype))
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(b)
		return
	}
	ic.mu.RLock()
	failedAt, failed := ic.neg[name]
	ic.mu.RUnlock()
	if failed && time.Since(failedAt) < time.Hour {
		http.NotFound(w, r)
		return
	}
	port := webPort(c)
	if port == 0 {
		http.NotFound(w, r)
		return
	}
	b, ctype, err := ic.scrapeFavicon(r.Context(), fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err != nil {
		ic.mu.Lock()
		ic.neg[name] = time.Now()
		ic.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	os.WriteFile(path, b, 0o644)
	os.WriteFile(path+".type", []byte(ctype), 0o644)
	iconHeaders(w, ctype)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(b)
}

func (ic *Icons) scrapeFavicon(ctx context.Context, base string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	get := func(u string, limit int64) ([]byte, string, error) {
		req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
		resp, err := ic.client.Do(req)
		if err != nil {
			return nil, "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, "", fmt.Errorf("status %d", resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
		return b, resp.Header.Get("Content-Type"), err
	}
	var cands []string
	if html, _, err := get(base, 512<<10); err == nil {
		best := map[string]string{}
		for _, tag := range linkIcon.FindAllString(string(html), -1) {
			var rel, href string
			for _, m := range attr.FindAllStringSubmatch(tag, -1) {
				if strings.EqualFold(m[1], "rel") {
					rel = strings.ToLower(m[2])
				} else {
					href = m[2]
				}
			}
			if href == "" || !strings.Contains(rel, "icon") {
				continue
			}
			if strings.Contains(rel, "apple-touch") {
				best["apple"] = href
			} else if _, ok := best["icon"]; !ok || strings.HasSuffix(href, ".svg") {
				best["icon"] = href
			}
		}
		for _, k := range []string{"apple", "icon"} {
			if h, ok := best[k]; ok {
				cands = append(cands, h)
			}
		}
	}
	cands = append(cands, "/favicon.ico")
	baseURL, _ := url.Parse(base)
	for _, h := range cands {
		ref, err := url.Parse(h)
		if err != nil {
			continue
		}
		u := baseURL.ResolveReference(ref)
		if u.Host != baseURL.Host { // only fetch from the app itself
			continue
		}
		b, ctype, err := get(u.String(), 1<<20)
		if err != nil || len(b) == 0 {
			continue
		}
		sniff := http.DetectContentType(b)
		switch {
		case strings.HasPrefix(sniff, "image/"):
			return b, sniff, nil
		case strings.Contains(ctype, "svg") || strings.Contains(string(b[:min(len(b), 512)]), "<svg"):
			return b, "image/svg+xml", nil
		}
	}
	return nil, "", fmt.Errorf("no favicon")
}
