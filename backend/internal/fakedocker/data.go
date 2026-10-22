// Package fakedocker pretends to be a Docker host full of made-up services.
//
// It speaks just enough of the Docker Engine API for TheBar (list/inspect,
// stats, logs, events, start/stop/restart, image digests, disk usage), so
// TheBar can run against it for screenshots, demos and tests without touching
// a real Docker daemon.
package fakedocker

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"path/filepath"
	"time"
)

const cores = 12 // the pretend host has 12 cores

// wave is a value that drifts smoothly: base + amp*(1+sin)/2.
type wave struct{ base, amp, period, phase float64 }

func (w wave) at(t float64) float64 {
	if w.amp == 0 {
		return w.base
	}
	return w.base + w.amp*(1+math.Sin(2*math.Pi*t/w.period+w.phase))/2
}

// integral is the area under the wave from 0 to t, so counters (bytes, CPU
// time) grow smoothly and TheBar's rate calculation shows the wave.
func (w wave) integral(t float64) float64 {
	if w.amp == 0 {
		return w.base * t
	}
	om := 2 * math.Pi / w.period
	return w.base*t + w.amp/2*(t-(math.Cos(om*t+w.phase)-math.Cos(w.phase))/om)
}

type port struct{ host, container int }

type mount struct{ typ, name, dest string }

// image is what the "registry" and the local image store know about a name.
type image struct {
	ref    string
	id     string // local image ID
	digest string // local repo digest
	remote string // digest the registry serves now (differs: update available)
}

type container struct {
	name, service, project string
	image                  string // reference, e.g. lscr.io/linuxserver/radarr:latest
	imageID                string // the image the container was created from
	ports                  []port
	hostNet                bool
	health                 string
	state                  string // running | exited | restarting
	age                    time.Duration
	exitedAgo              time.Duration
	exitCode               int
	restarts               int
	policy                 string
	dependsOn              []string
	mounts                 []mount
	hash                   string // compose config-hash label ("" = unknown, never flagged)
	logKind                string
	sizeRw                 int64

	cpu, mem, rx, tx, blkR, blkW wave

	// runtime
	id       string
	started  time.Time
	finished time.Time
}

func idOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func digestOf(s string) string { return "sha256:" + idOf("digest|"+s) }

const (
	kb = 1024.0
	mb = 1024 * kb
	gb = 1024 * mb
)

// demo returns the cast of containers. Compose projects get a real compose
// file under workdir, so TheBar's editor has something to show.
func demo(workdir string) ([]*container, map[string]*image) {
	var cs []*container
	imgs := map[string]*image{}

	use := func(ref string, newer bool) *image {
		if im, ok := imgs[ref]; ok {
			return im
		}
		im := &image{ref: ref, id: "sha256:" + idOf("image|"+ref), digest: digestOf(ref), remote: digestOf(ref)}
		if newer {
			im.remote = digestOf(ref + "|newer")
		}
		imgs[ref] = im
		return im
	}
	add := func(c *container, newer bool) *container {
		im := use(c.image, newer)
		if c.imageID == "" {
			c.imageID = im.id
		}
		if c.state == "" {
			c.state = "running"
		}
		if c.policy == "" {
			c.policy = "unless-stopped"
		}
		if c.age == 0 {
			c.age = 3 * 24 * time.Hour
		}
		cs = append(cs, c)
		return c
	}
	idle := func(cpu float64, memMB float64) container {
		return container{
			cpu: wave{cpu, cpu, 37, 0.7}, mem: wave{memMB * mb, memMB * mb * 0.04, 90, 1.1},
			rx: wave{1 * kb, 2 * kb, 29, 0.3}, tx: wave{0.5 * kb, 1.5 * kb, 41, 2.0},
			blkR: wave{0, 4 * kb, 55, 0.2}, blkW: wave{2 * kb, 6 * kb, 23, 1.4},
		}
	}
	with := func(base container, f func(*container)) *container { f(&base); return &base }

	// ── media ──
	arr := func(name, img string, p port, cpuLoad, memMB float64, age time.Duration, kind string) *container {
		return with(idle(cpuLoad, memMB), func(c *container) {
			c.name, c.service, c.project, c.image, c.ports = name, name, "media", img, []port{p}
			c.age, c.logKind, c.sizeRw = age, kind, 24*int64(kb)*int64(1+len(name))
			c.mounts = []mount{{"bind", "", "/config"}}
		})
	}
	add(with(*arr("radarr", "lscr.io/linuxserver/radarr:latest", port{7878, 7878}, 0.0009, 270, 13*time.Hour, "arr"), func(c *container) {
		c.rx, c.tx = wave{3 * kb, 60 * kb, 61, 0.4}, wave{2 * kb, 30 * kb, 61, 0.4}
	}), true) // update available
	add(with(*arr("sonarr", "lscr.io/linuxserver/sonarr:latest", port{8989, 8989}, 0.0012, 480, 4*24*time.Hour, "arr"), func(c *container) {
		c.rx, c.tx = wave{3 * kb, 40 * kb, 53, 1.4}, wave{2 * kb, 20 * kb, 53, 1.4}
	}), true) // update available
	add(with(*arr("lidarr", "lscr.io/linuxserver/lidarr:latest", port{8686, 8686}, 0.0006, 180, 6*time.Hour, "arr"), func(c *container) {
		c.state, c.exitedAgo, c.exitCode = "exited", 3*time.Hour, 0
	}), false)
	add(with(*arr("sabnzbd", "lscr.io/linuxserver/sabnzbd:latest", port{8585, 8080}, 0.011, 160, 4*24*time.Hour, "arr"), func(c *container) {
		c.cpu = wave{0.008, 0.06, 47, 0.2}
		c.rx, c.tx = wave{40 * kb, 2.4 * mb, 47, 0.2}, wave{2 * kb, 80 * kb, 47, 0.2}
		c.blkW = wave{200 * kb, 3 * mb, 47, 0.2}
	}), false)
	add(with(idle(0.003, 690), func(c *container) {
		c.name, c.service, c.project, c.image = "jellyfin", "jellyfin", "media", "lscr.io/linuxserver/jellyfin:latest"
		c.hostNet, c.age, c.logKind = true, 13*time.Hour, "arr"
		c.cpu = wave{0.01, 0.1, 71, 2.2}
		c.sizeRw = 28 * int64(kb)
	}), false)

	// ── monitoring ──
	add(with(idle(0.004, 415), func(c *container) {
		c.name, c.service, c.project, c.image, c.ports = "grafana", "grafana", "monitoring", "grafana/grafana:latest", []port{{3000, 3000}}
		c.age, c.logKind, c.health = 5*24*time.Hour, "grafana", ""
		c.hash = "0badc0ffee" // the compose file has changed since: "Config changed"
		c.mounts = []mount{{"bind", "", "/var/lib/grafana"}}
	}), false)
	add(with(idle(0.003, 520), func(c *container) {
		c.name, c.service, c.project, c.image, c.ports = "prometheus", "prometheus", "monitoring", "prom/prometheus:latest", []port{{9090, 9090}}
		c.age, c.logKind = 5*24*time.Hour, "prom"
		c.rx, c.tx = wave{10 * kb, 24 * kb, 60, 0.1}, wave{4 * kb, 10 * kb, 60, 0.1}
		c.mounts = []mount{{"volume", "monitoring_prometheus_data", "/prometheus"}}
	}), false)
	add(with(idle(0.012, 265), func(c *container) {
		c.name, c.service, c.project, c.image, c.ports = "cadvisor", "cadvisor", "monitoring", "gcr.io/cadvisor/cadvisor:latest", []port{{8081, 8080}}
		c.age, c.logKind, c.health = 5*24*time.Hour, "generic", "healthy"
		c.cpu = wave{0.01, 0.014, 33, 0.4}
	}), false)
	add(with(idle(0.0008, 22), func(c *container) {
		c.name, c.service, c.project, c.image, c.ports = "node-exporter", "node-exporter", "monitoring", "prom/node-exporter:latest", []port{{9100, 9100}}
		c.age, c.logKind = 5*24*time.Hour, "generic"
	}), false)

	// ── photos (one app with three helpers) ──
	add(with(idle(0.006, 1300), func(c *container) {
		c.name, c.service, c.project, c.image, c.ports = "immich_server", "immich-server", "photos", "ghcr.io/immich-app/immich-server:v2", []port{{2283, 2283}}
		c.age, c.logKind, c.health, c.policy = 5*24*time.Hour, "generic", "healthy", "always"
		c.dependsOn = []string{"database", "redis"}
		c.rx, c.tx = wave{2 * kb, 30 * kb, 67, 0.9}, wave{2 * kb, 40 * kb, 67, 0.9}
		c.sizeRw = 800 * int64(mb)
	}), false)
	add(with(idle(0.002, 980), func(c *container) {
		c.name, c.service, c.project, c.image = "immich_machine_learning", "immich-machine-learning", "photos", "ghcr.io/immich-app/immich-machine-learning:v2"
		c.age, c.logKind, c.health, c.policy = 5*24*time.Hour, "generic", "healthy", "always"
	}), false)
	add(with(idle(0.0009, 355), func(c *container) {
		c.name, c.service, c.project, c.image = "immich_postgres", "database", "photos", "ghcr.io/immich-app/postgres:14-vectorchord0.4.3-pgvectors0.2.0"
		c.age, c.logKind, c.health, c.policy = 5*24*time.Hour, "postgres", "healthy", "always"
	}), false)
	add(with(idle(0.0004, 12), func(c *container) {
		c.name, c.service, c.project, c.image = "immich_redis", "redis", "photos", "valkey/valkey:8"
		c.age, c.logKind, c.health, c.policy = 5*24*time.Hour, "generic", "healthy", "always"
	}), false)

	// ── cloud (nextcloud + helpers); the app image was pulled but the container is old ──
	ncOld := "sha256:" + idOf("image|nextcloud:latest|old")
	add(with(idle(0.0015, 2500), func(c *container) {
		c.name, c.service, c.project, c.image, c.ports = "nextcloud-app-1", "app", "cloud", "nextcloud:latest", []port{{8080, 80}}
		c.age, c.logKind, c.imageID = 4*24*time.Hour, "generic", ncOld
		c.dependsOn = []string{"db", "redis"}
		c.mounts = []mount{{"volume", "cloud_nextcloud", "/var/www/html"}}
		c.sizeRw = 5 * int64(gb)
	}), false)
	add(with(idle(0.0004, 40), func(c *container) {
		c.name, c.service, c.project, c.image, c.imageID = "nextcloud-cron-1", "cron", "cloud", "nextcloud:latest", ncOld
		c.age, c.logKind = 4*24*time.Hour, "generic"
		c.dependsOn = []string{"db", "redis"}
	}), false)
	add(with(idle(0.0012, 190), func(c *container) {
		c.name, c.service, c.project, c.image = "nextcloud-db-1", "db", "cloud", "postgres:16"
		c.age, c.logKind = 4*24*time.Hour, "postgres"
	}), false)
	add(with(idle(0.0003, 9), func(c *container) {
		c.name, c.service, c.project, c.image = "nextcloud-redis-1", "redis", "cloud", "redis:alpine"
		c.age, c.logKind = 4*24*time.Hour, "generic"
	}), false)

	// ── tools (a stack that's shown collapsed) ──
	add(with(idle(0.0004, 38), func(c *container) {
		c.name, c.service, c.project, c.image, c.ports = "vaultwarden", "vaultwarden", "tools", "vaultwarden/server:latest", []port{{8222, 80}}
		c.age, c.logKind, c.health = 9*24*time.Hour, "generic", "healthy"
	}), false)
	add(with(idle(0.004, 150), func(c *container) {
		c.name, c.service, c.project, c.image, c.ports = "uptime-kuma", "uptime-kuma", "tools", "louislam/uptime-kuma:1", []port{{3001, 3001}}
		c.age, c.logKind, c.health = 9*24*time.Hour, "generic", "healthy"
		c.cpu = wave{0.003, 0.01, 29, 1.2}
	}), false)
	add(with(idle(0.0008, 95), func(c *container) {
		c.name, c.service, c.project, c.image, c.ports = "syncthing", "syncthing", "tools", "lscr.io/linuxserver/syncthing:latest", []port{{8384, 8384}}
		c.age, c.logKind = 2*24*time.Hour, "generic"
	}), false)

	// ── standalone containers (no compose project) ──
	add(with(idle(0.0014, 60), func(c *container) {
		c.name, c.image, c.ports = "pihole", "pihole/pihole:latest", []port{{8053, 80}}
		c.age, c.logKind, c.health = 12*24*time.Hour, "generic", "healthy"
	}), false)
	add(with(idle(0.003, 710), func(c *container) {
		c.name, c.image, c.hostNet = "home-assistant", "ghcr.io/home-assistant/home-assistant:stable", true
		c.age, c.logKind = 3*24*time.Hour, "generic"
	}), false)
	add(with(idle(0, 0), func(c *container) {
		c.name, c.image, c.ports = "paperless-ngx", "ghcr.io/paperless-ngx/paperless-ngx:latest", []port{{8000, 8000}}
		c.state, c.restarts, c.age, c.logKind = "restarting", 14, 40*time.Minute, "crash"
		c.exitCode = 1
	}), false)
	add(with(idle(0.0004, 190), func(c *container) {
		c.name, c.image, c.ports = "proxy-app-1", "jc21/nginx-proxy-manager:latest", []port{{81, 81}, {80, 80}, {443, 443}}
		c.age, c.logKind = 5*24*time.Hour, "generic"
	}), false)
	add(with(idle(0.0006, 20), func(c *container) {
		c.name, c.image, c.hostNet = "thebar", "thebar:latest", true
		c.age, c.logKind, c.health = 50*time.Minute, "generic", "healthy"
	}), false)

	for _, c := range cs {
		c.id = idOf("container|" + c.name)
	}
	_ = workdir
	return cs, imgs
}

func projectDir(workdir, project string) string { return filepath.Join(workdir, project) }
