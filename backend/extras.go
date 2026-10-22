package main

import (
	"context"
	"log"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Nginx Proxy Manager: map proxy hosts (domains) to containers by reading the
// nginx configs NPM generates. No API credentials needed.

type proxyHost struct {
	Domains []string
	Server  string
	Port    int
	HTTPS   bool
}

type Proxy struct {
	inv   *Inventory
	mu    sync.RWMutex
	hosts []proxyHost
}

func NewProxy(inv *Inventory) *Proxy { return &Proxy{inv: inv} }

var (
	reServerName = regexp.MustCompile(`(?m)^\s*server_name\s+([^;]+);`)
	reSet        = regexp.MustCompile(`(?m)^\s*set\s+\$(server|port|forward_scheme)\s+"?([^";]+)"?;`)
	reSSL        = regexp.MustCompile(`(?m)^\s*listen\s+443\s+ssl`)
)

func (p *Proxy) confDir() string {
	if d := os.Getenv("THEBAR_NPM_DIR"); d != "" {
		return d
	}
	for _, c := range p.inv.List() {
		if !strings.Contains(c.Image, "nginx-proxy-manager") {
			continue
		}
		in, err := p.inv.d.Inspect(context.Background(), c.ID)
		if err != nil {
			continue
		}
		for _, m := range in.Mounts {
			if m.Destination == "/data" && m.Source != "" {
				return filepath.Join(m.Source, "nginx", "proxy_host")
			}
		}
	}
	return ""
}

func (p *Proxy) Run(ctx context.Context) {
	time.Sleep(3 * time.Second)
	for {
		dir := p.confDir()
		var hosts []proxyHost
		if dir != "" {
			files, _ := filepath.Glob(filepath.Join(dir, "*.conf"))
			for _, f := range files {
				b, err := os.ReadFile(f)
				if err != nil {
					continue
				}
				s := string(b)
				var h proxyHost
				if m := reServerName.FindStringSubmatch(s); m != nil {
					h.Domains = strings.Fields(m[1])
				}
				for _, m := range reSet.FindAllStringSubmatch(s, -1) {
					switch m[1] {
					case "server":
						h.Server = m[2]
					case "port":
						h.Port, _ = strconv.Atoi(m[2])
					}
				}
				h.HTTPS = reSSL.MatchString(s)
				if len(h.Domains) > 0 && h.Server != "" {
					hosts = append(hosts, h)
				}
			}
		}
		p.mu.Lock()
		changed := len(hosts) != len(p.hosts)
		p.hosts = hosts
		p.mu.Unlock()
		if changed {
			p.inv.Refresh()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
}

var hostAddrs = func() map[string]bool {
	m := map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true, "host.docker.internal": true}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ip, _, err := net.ParseCIDR(a.String()); err == nil {
				m[ip.String()] = true
			}
		}
	}
	if h, err := os.Hostname(); err == nil {
		m[h] = true
	}
	return m
}()

// Domains returns proxy URLs pointing at this container.
func (p *Proxy) Domains(c *Container) []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var out []string
	for _, h := range p.hosts {
		match := false
		if hostAddrs[h.Server] {
			if c.NetworkMode == "host" {
				match = webPort(c) == h.Port
			} else {
				for _, pt := range c.Ports {
					if pt.Host == h.Port && pt.Proto == "tcp" {
						match = true
					}
				}
			}
		} else {
			names := append([]string{c.Name, c.Service}, c.Aliases...)
			for _, n := range names {
				if n != "" && n == h.Server {
					match = true
				}
			}
		}
		if !match {
			continue
		}
		scheme := "http://"
		if h.HTTPS {
			scheme = "https://"
		}
		for _, d := range h.Domains {
			if !strings.HasPrefix(d, "*") {
				out = append(out, scheme+d)
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Image updates: compare the local image digest with the registry's.

type Updates struct {
	d      *Docker
	inv    *Inventory
	mu     sync.RWMutex
	byRef  map[string]string // image ref -> available | current | local
	imgID  map[string]string // image ref -> local image ID
	remote map[string]string // image ref -> registry digest from the last online check
	last   time.Time
	kick   chan struct{} // full check (asks the registries)
	local  chan struct{} // local re-check only (after pulls / recreates)
}

func NewUpdates(d *Docker, inv *Inventory) *Updates {
	return &Updates{
		d: d, inv: inv,
		byRef: map[string]string{}, imgID: map[string]string{}, remote: map[string]string{},
		kick: make(chan struct{}, 1), local: make(chan struct{}, 1),
	}
}

var hexID = regexp.MustCompile(`^(sha256:)?[0-9a-f]{12,64}$`)

// Status for one container: "available" (registry has newer), "recreate"
// (newer image already pulled, container still on the old one), "current",
// "local" (built locally / not from a registry), or "" (unknown yet).
func (u *Updates) Status(c *Container) string {
	u.mu.RLock()
	defer u.mu.RUnlock()
	st := u.byRef[c.Image]
	if st == "" {
		return ""
	}
	if st != "local" && u.imgID[c.Image] != "" && u.imgID[c.Image] != c.ImageID {
		return "recreate"
	}
	return st
}

func (u *Updates) CheckNow() {
	select {
	case u.kick <- struct{}{}:
	default:
	}
}

// RefreshLocal re-evaluates against local images only, without asking the
// registries. Called when containers are (re)created, e.g. after an update,
// so badges don't stay stale until the next full check.
func (u *Updates) RefreshLocal() {
	select {
	case u.local <- struct{}{}:
	default:
	}
}

func (u *Updates) LastCheck() time.Time {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.last
}

func (u *Updates) Run(ctx context.Context) {
	select {
	case <-time.After(time.Minute):
	case <-u.kick:
	case <-ctx.Done():
		return
	}
	online := true
	for {
		u.check(ctx, online)
		select {
		case <-ctx.Done():
			return
		case <-u.kick:
			online = true
		case <-u.local:
			time.Sleep(2 * time.Second) // let a burst of recreates settle
			select {
			case <-u.local:
			default:
			}
			online = false
		case <-time.After(12 * time.Hour):
			online = true
		}
	}
}

func (u *Updates) check(ctx context.Context, online bool) {
	refs := map[string]bool{}
	for _, c := range u.inv.List() {
		refs[c.Image] = true
	}
	u.mu.RLock()
	remote := make(map[string]string, len(u.remote))
	for k, v := range u.remote {
		remote[k] = v
	}
	prevID := make(map[string]string, len(u.imgID))
	for k, v := range u.imgID {
		prevID[k] = v
	}
	u.mu.RUnlock()

	res := map[string]string{}
	ids := map[string]string{}
	for ref := range refs {
		if hexID.MatchString(ref) || strings.Contains(ref, "@sha256:") {
			res[ref] = "local" // pinned or referenced by ID: nothing to update
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		img, err := u.d.ImageInspect(cctx, ref)
		if err != nil || len(img.RepoDigests) == 0 {
			cancel()
			res[ref] = "local"
			continue
		}
		ids[ref] = img.ID
		// The image behind this name changed locally (someone pulled it): the
		// registry digest we remember is from before that pull and may be stale
		// (images get rebuilt between checks), so look this one up again now.
		pulled := prevID[ref] != "" && prevID[ref] != img.ID
		if online || pulled {
			digest, err := u.d.RemoteDigest(cctx, ref)
			if err != nil {
				log.Printf("update check %s: %v", ref, err)
			} else {
				remote[ref] = digest
			}
		}
		cancel()
		digest, ok := remote[ref]
		if !ok {
			continue // never reached the registry for this one: unknown
		}
		st := "available"
		for _, rd := range img.RepoDigests {
			if strings.HasSuffix(rd, "@"+digest) {
				st = "current"
			}
		}
		res[ref] = st
	}
	u.mu.Lock()
	u.byRef, u.imgID, u.remote = res, ids, remote
	if online {
		u.last = time.Now()
	}
	u.mu.Unlock()
	u.inv.Update(func(c *Container) bool {
		s := u.Status(c)
		if s != c.Update {
			c.Update = s
			return true
		}
		return false
	})
}

// ---------------------------------------------------------------------------
// Ports in use on the host (TheBar runs with host networking, so /proc/net
// shows the host's sockets).

type UsedPort struct {
	Port  int    `json:"port"`
	Proto string `json:"proto"`
	Owner string `json:"owner"` // container name, or "" for host processes
	Bind  string `json:"bind"`
}

func listeningPorts() map[string]string { // "port/proto" -> bind address
	out := map[string]string{}
	read := func(file, proto string, state string, v6 bool) {
		b, err := os.ReadFile(file)
		if err != nil {
			return
		}
		for _, l := range strings.Split(string(b), "\n")[1:] {
			fs := strings.Fields(l)
			if len(fs) < 4 || fs[3] != state {
				continue
			}
			addr, portHex, _ := strings.Cut(fs[1], ":")
			port, err := strconv.ParseInt(portHex, 16, 32)
			if err != nil {
				continue
			}
			bind := "all"
			if strings.Trim(addr, "0") != "" {
				if v6 {
					bind = "specific"
				} else if len(addr) == 8 {
					a, _ := strconv.ParseUint(addr, 16, 32)
					bind = net.IPv4(byte(a), byte(a>>8), byte(a>>16), byte(a>>24)).String()
				}
			}
			key := strconv.Itoa(int(port)) + "/" + proto
			if prev, ok := out[key]; !ok || prev != "all" {
				out[key] = bind
			}
		}
	}
	read("/proc/net/tcp", "tcp", "0A", false)
	read("/proc/net/tcp6", "tcp", "0A", true)
	read("/proc/net/udp", "udp", "07", false)
	read("/proc/net/udp6", "udp", "07", true)
	return out
}

func UsedPorts(inv *Inventory) []UsedPort {
	owners := map[string]string{}
	for _, c := range inv.List() {
		for _, p := range c.Ports {
			owners[strconv.Itoa(p.Host)+"/"+p.Proto] = c.Name
		}
		if c.NetworkMode == "host" && c.State == "running" {
			if wp := webPort(c); wp > 0 {
				owners[strconv.Itoa(wp)+"/tcp"] = c.Name
			}
		}
	}
	listen := listeningPorts()
	seen := map[string]bool{}
	var out []UsedPort
	for key, bind := range listen {
		ps, proto, _ := strings.Cut(key, "/")
		port, _ := strconv.Atoi(ps)
		if bind == "127.0.0.1" || (proto == "udp" && port > 32767) {
			continue // local-only listeners and ephemeral UDP aren't interesting
		}
		seen[key] = true
		out = append(out, UsedPort{Port: port, Proto: proto, Owner: owners[key], Bind: bind})
	}
	// published ports of stopped containers are "reserved" too
	for key, owner := range owners {
		if !seen[key] {
			ps, proto, _ := strings.Cut(key, "/")
			port, _ := strconv.Atoi(ps)
			out = append(out, UsedPort{Port: port, Proto: proto, Owner: owner, Bind: "stopped"})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Proto < out[j].Proto
	})
	return out
}
