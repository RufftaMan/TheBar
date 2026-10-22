package main

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Port struct {
	Host      int    `json:"host"`
	Container int    `json:"container"`
	Proto     string `json:"proto"`
}

// Container is what the frontend sees: live state plus enrichments.
type Container struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Project       string            `json:"project,omitempty"`
	Service       string            `json:"service,omitempty"`
	Image         string            `json:"image"`
	ImageID       string            `json:"imageId"`
	State         string            `json:"state"`  // running, exited, restarting, paused, created, dead
	Status        string            `json:"status"` // docker's human string
	Health        string            `json:"health,omitempty"`
	Created       string            `json:"created"`
	StartedAt     string            `json:"startedAt,omitempty"`
	FinishedAt    string            `json:"finishedAt,omitempty"`
	ExitCode      int               `json:"exitCode"`
	OOMKilled     bool              `json:"oomKilled,omitempty"`
	RestartCount  int               `json:"restartCount"`
	RestartPolicy string            `json:"restartPolicy,omitempty"`
	NetworkMode   string            `json:"networkMode"`
	Ports         []Port            `json:"ports"`
	Exposed       []int             `json:"exposed,omitempty"`
	DependsOn     []string          `json:"dependsOn,omitempty"`
	ConfigFiles   []string          `json:"configFiles,omitempty"`
	WorkingDir    string            `json:"workingDir,omitempty"`
	ConfigHash    string            `json:"-"`
	Aliases       []string          `json:"-"`
	Volumes       []string          `json:"-"`
	Tty           bool              `json:"-"`
	Labels        map[string]string `json:"labels,omitempty"` // only thebar.* labels
	Self          bool              `json:"self,omitempty"`

	// enrichments
	WebPort   int       `json:"webPort,omitempty"`   // host port of the web UI
	WebScheme string    `json:"webScheme,omitempty"` // http | https
	AppKey    string    `json:"appKey,omitempty"`
	App       *KnownApp `json:"app,omitempty"`
	Icon      string    `json:"icon,omitempty"`    // URL path of the logo, "" if none
	Domains   []string  `json:"domains,omitempty"` // proxy domains pointing here
	Update    string    `json:"update,omitempty"`  // available | recreate | current | local | error
	Drift     bool      `json:"drift,omitempty"`   // compose file changed since creation
	SizeRw    int64     `json:"sizeRw,omitempty"`  // writable layer bytes
	VolSize   int64     `json:"volSize,omitempty"` // named volume bytes
	Alert     string    `json:"alert,omitempty"`   // human reason when something is wrong
}

type Inventory struct {
	d    *Docker
	hub  *Hub
	self string // our own container name, if detectable

	mu         sync.RWMutex
	containers map[string]*Container // by ID
	enrich     func(*Container)      // icons, domains, updates, drift...
	onCreate   func()                // called when a container is (re)created
	onPull     func()                // called when an image was pulled
	dies       map[string][]time.Time

	kick chan struct{}
}

func NewInventory(d *Docker, hub *Hub, self string) *Inventory {
	return &Inventory{
		d: d, hub: hub, self: self,
		containers: map[string]*Container{},
		dies:       map[string][]time.Time{},
		kick:       make(chan struct{}, 1),
	}
}

func (inv *Inventory) List() []*Container {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	out := make([]*Container, 0, len(inv.containers))
	for _, c := range inv.containers {
		cp := *c
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (inv *Inventory) Get(id string) *Container {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	if c, ok := inv.containers[id]; ok {
		cp := *c
		return &cp
	}
	for _, c := range inv.containers { // allow lookup by name too
		if c.Name == id {
			cp := *c
			return &cp
		}
	}
	return nil
}

// Running returns IDs of running containers.
func (inv *Inventory) Running() []string {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	var ids []string
	for id, c := range inv.containers {
		if c.State == "running" {
			ids = append(ids, id)
		}
	}
	return ids
}

// Refresh asks for a (debounced) re-read of all containers.
func (inv *Inventory) Refresh() {
	select {
	case inv.kick <- struct{}{}:
	default:
	}
}

func (inv *Inventory) Run(ctx context.Context) {
	go inv.watchEvents(ctx)
	inv.load(ctx)
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-inv.kick:
			time.Sleep(250 * time.Millisecond) // let bursts of events settle
			select {
			case <-inv.kick:
			default:
			}
		}
		inv.load(ctx)
	}
}

func (inv *Inventory) watchEvents(ctx context.Context) {
	for ctx.Err() == nil {
		err := inv.d.Events(ctx, func(ev dEvent) {
			if ev.Type == "image" {
				if ev.Action == "pull" && inv.onPull != nil {
					inv.onPull()
				}
				return
			}
			switch ev.Action {
			case "create":
				if inv.onCreate != nil {
					inv.onCreate()
				}
			case "die", "oom":
				inv.mu.Lock()
				inv.dies[ev.Actor.ID] = append(inv.dies[ev.Actor.ID], time.Now())
				inv.mu.Unlock()
			}
			if strings.HasPrefix(ev.Action, "exec_") { // healthchecks create these constantly
				return
			}
			inv.Refresh()
		})
		if ctx.Err() == nil {
			log.Printf("docker events stream ended: %v (reconnecting)", err)
			time.Sleep(3 * time.Second)
		}
	}
}

func (inv *Inventory) load(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	list, err := inv.d.List(ctx)
	if err != nil {
		log.Printf("list containers: %v", err)
		return
	}
	next := map[string]*Container{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, s := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			in, err := inv.d.Inspect(ctx, id)
			if err != nil {
				return // removed in between
			}
			c := fromInspect(in)
			mu.Lock()
			next[c.ID] = c
			mu.Unlock()
		}(s.ID)
	}
	wg.Wait()

	inv.mu.Lock()
	for id, c := range next {
		c.Self = inv.self != "" && c.Name == inv.self
		// carry over slow-changing enrichments so they don't flicker
		if old, ok := inv.containers[id]; ok {
			c.SizeRw, c.VolSize, c.Update, c.Drift = old.SizeRw, old.VolSize, old.Update, old.Drift
		}
		c.Alert = inv.alertFor(c)
	}
	// forget restart history of removed containers
	for id := range inv.dies {
		if _, ok := next[id]; !ok {
			delete(inv.dies, id)
		}
	}
	enrich := inv.enrich
	inv.mu.Unlock()

	if enrich != nil {
		for _, c := range next {
			enrich(c)
		}
	}

	inv.mu.Lock()
	changed := !sameContainers(inv.containers, next)
	inv.containers = next
	inv.mu.Unlock()
	if changed {
		inv.hub.Broadcast("containers", inv.List())
	}
}

// Update mutates containers in place (used by background enrichers) and
// broadcasts if anything changed.
func (inv *Inventory) Update(fn func(c *Container) bool) {
	inv.mu.Lock()
	changed := false
	for _, c := range inv.containers {
		if fn(c) {
			changed = true
		}
	}
	inv.mu.Unlock()
	if changed {
		inv.hub.Broadcast("containers", inv.List())
	}
}

func sameContainers(a, b map[string]*Container) bool {
	if len(a) != len(b) {
		return false
	}
	for id, x := range a {
		y, ok := b[id]
		if !ok {
			return false
		}
		bx, _ := json.Marshal(x)
		by, _ := json.Marshal(y)
		if string(bx) != string(by) {
			return false
		}
	}
	return true
}

// alertFor explains what's wrong with a container, or "" if nothing is.
// Must be called with inv.mu held.
func (inv *Inventory) alertFor(c *Container) string {
	// restart loop: 3+ deaths within 10 minutes while a restart policy is set
	var recent []time.Time
	for _, t := range inv.dies[c.ID] {
		if time.Since(t) < 10*time.Minute {
			recent = append(recent, t)
		}
	}
	inv.dies[c.ID] = recent
	keepsRunning := c.RestartPolicy == "always" || c.RestartPolicy == "unless-stopped" || c.RestartPolicy == "on-failure"
	switch {
	case c.State == "restarting" || (keepsRunning && len(recent) >= 3):
		return "Restart loop"
	case c.OOMKilled:
		return "Killed: out of memory"
	case c.Health == "unhealthy":
		return "Unhealthy"
	case c.State == "dead":
		return "Dead"
	case c.State == "exited" && c.ExitCode != 0 && c.ExitCode != 137 && c.ExitCode != 143:
		return "Crashed (exit " + strconv.Itoa(c.ExitCode) + ")"
	}
	return ""
}

func fromInspect(in *dInspect) *Container {
	l := in.Config.Labels
	c := &Container{
		ID:            in.ID,
		Name:          strings.TrimPrefix(in.Name, "/"),
		Project:       l["com.docker.compose.project"],
		Service:       l["com.docker.compose.service"],
		Image:         in.Config.Image,
		ImageID:       in.Image,
		State:         in.State.Status,
		Created:       in.Created,
		ExitCode:      in.State.ExitCode,
		OOMKilled:     in.State.OOMKilled,
		RestartCount:  in.RestartCount,
		RestartPolicy: in.HostConfig.RestartPolicy.Name,
		NetworkMode:   in.HostConfig.NetworkMode,
		WorkingDir:    l["com.docker.compose.project.working_dir"],
		ConfigHash:    l["com.docker.compose.config-hash"],
		Tty:           in.Config.Tty,
		Ports:         []Port{},
	}
	if in.State.Health != nil {
		c.Health = in.State.Health.Status
	}
	if in.State.Running || in.State.Restarting {
		c.StartedAt = in.State.StartedAt
	} else if !strings.HasPrefix(in.State.FinishedAt, "0001") {
		c.FinishedAt = in.State.FinishedAt
	}
	c.Status = humanStatus(c)
	if f := l["com.docker.compose.project.config_files"]; f != "" {
		c.ConfigFiles = strings.Split(f, ",")
	}
	if dep := l["com.docker.compose.depends_on"]; dep != "" {
		for _, p := range strings.Split(dep, ",") {
			if name, _, _ := strings.Cut(p, ":"); name != "" {
				c.DependsOn = append(c.DependsOn, name)
			}
		}
	}
	for k, v := range l {
		if strings.HasPrefix(k, "thebar.") {
			if c.Labels == nil {
				c.Labels = map[string]string{}
			}
			c.Labels[k] = v
		}
	}
	seen := map[string]bool{}
	for spec, binds := range in.NetworkSettings.Ports {
		cp, proto, _ := strings.Cut(spec, "/")
		cport, _ := strconv.Atoi(cp)
		for _, b := range binds {
			hp, _ := strconv.Atoi(b.HostPort)
			key := b.HostPort + "/" + proto
			if hp == 0 || seen[key] {
				continue // skip duplicate v4/v6 bindings
			}
			seen[key] = true
			c.Ports = append(c.Ports, Port{Host: hp, Container: cport, Proto: proto})
		}
	}
	sort.Slice(c.Ports, func(i, j int) bool { return c.Ports[i].Host < c.Ports[j].Host })
	for spec := range in.Config.ExposedPorts {
		cp, _, _ := strings.Cut(spec, "/")
		if p, _ := strconv.Atoi(cp); p > 0 {
			c.Exposed = append(c.Exposed, p)
		}
	}
	sort.Ints(c.Exposed)
	for _, n := range in.NetworkSettings.Networks {
		c.Aliases = append(c.Aliases, n.Aliases...)
	}
	for _, m := range in.Mounts {
		if m.Type == "volume" {
			c.Volumes = append(c.Volumes, m.Name)
		}
	}
	key, app, ok := matchKnown(c.Image, c.Service)
	if ok {
		c.AppKey = key
		a := app
		c.App = &a
	}
	return c
}

func humanStatus(c *Container) string {
	switch c.State {
	case "running":
		if c.Health != "" {
			return "Running (" + c.Health + ")"
		}
		return "Running"
	case "exited":
		return "Exited (" + strconv.Itoa(c.ExitCode) + ")"
	}
	if c.State == "" {
		return "Unknown"
	}
	return strings.ToUpper(c.State[:1]) + c.State[1:]
}
