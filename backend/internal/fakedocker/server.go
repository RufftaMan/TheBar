package fakedocker

import (
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/fs"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed demo
var demoFS embed.FS

// Server is the fake Docker Engine.
type Server struct {
	mu      sync.Mutex
	start   time.Time
	workdir string
	cs      []*container
	imgs    map[string]*image
	subs    map[chan []byte]struct{}
}

// New creates the fake host. The demo compose files are written below workdir
// (one folder per project), where TheBar can read and edit them.
func New(workdir string) (*Server, error) {
	err := fs.WalkDir(demoFS, "demo", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := demoFS.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(workdir, strings.TrimPrefix(p, "demo/"))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		return nil, err
	}
	cs, imgs := demo(workdir)
	now := time.Now()
	for _, c := range cs {
		switch c.state {
		case "running":
			c.started = now.Add(-c.age)
		case "restarting":
			c.started = now.Add(-12 * time.Second)
		case "exited":
			c.started = now.Add(-c.age)
			c.finished = now.Add(-c.exitedAgo)
		}
	}
	return &Server{start: now, workdir: workdir, cs: cs, imgs: imgs, subs: map[chan []byte]struct{}{}}, nil
}

// Serve answers on a unix socket until the listener breaks.
func (s *Server) Serve(sock string) error {
	os.Remove(sock)
	l, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	defer os.Remove(sock)
	return (&http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}).Serve(l)
}

func (s *Server) find(id string) *container {
	for _, c := range s.cs {
		if c.id == id || c.name == id || (len(id) >= 12 && strings.HasPrefix(c.id, id)) {
			return c
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter, what string) {
	writeJSON(w, 404, map[string]string{"message": what})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	w.Header().Set("Api-Version", "1.47")
	switch {
	case p == "/_ping":
		w.Write([]byte("OK"))
	case p == "/version":
		writeJSON(w, 200, map[string]string{"Version": "fake", "ApiVersion": "1.47", "MinAPIVersion": "1.24"})
	case p == "/containers/json":
		s.list(w, r)
	case strings.HasPrefix(p, "/containers/"):
		id, action, _ := strings.Cut(strings.TrimPrefix(p, "/containers/"), "/")
		s.mu.Lock()
		c := s.find(id)
		s.mu.Unlock()
		if c == nil {
			notFound(w, "No such container: "+id)
			return
		}
		switch {
		case action == "json" && r.Method == "GET":
			writeJSON(w, 200, s.inspect(c))
		case action == "stats":
			writeJSON(w, 200, s.stats(c))
		case action == "logs":
			s.logs(w, r, c)
		case r.Method == "POST":
			s.act(w, c, action)
		default:
			notFound(w, "fakedocker: not implemented: "+p)
		}
	case p == "/events":
		s.events(w, r)
	case strings.HasPrefix(p, "/images/") && strings.HasSuffix(p, "/json"):
		ref := strings.TrimSuffix(strings.TrimPrefix(p, "/images/"), "/json")
		if im, ok := s.imgs[ref]; ok {
			writeJSON(w, 200, map[string]any{"Id": im.id, "RepoTags": []string{im.ref}, "RepoDigests": []string{repo(im.ref) + "@" + im.digest}})
			return
		}
		notFound(w, "No such image: "+ref)
	case strings.HasPrefix(p, "/distribution/") && strings.HasSuffix(p, "/json"):
		ref := strings.TrimSuffix(strings.TrimPrefix(p, "/distribution/"), "/json")
		if im, ok := s.imgs[ref]; ok {
			writeJSON(w, 200, map[string]any{"Descriptor": map[string]string{"digest": im.remote, "mediaType": "application/vnd.oci.image.index.v1+json"}})
			return
		}
		notFound(w, "No such image: "+ref)
	case p == "/system/df":
		s.df(w)
	default:
		// anything that would change the host (compose up/pull/prune...) lands here and fails harmlessly
		notFound(w, "fakedocker: not implemented: "+r.Method+" "+p)
	}
}

func repo(ref string) string {
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		return ref[:i]
	}
	return ref
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "1" || r.URL.Query().Get("all") == "true"
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []map[string]any{}
	for _, c := range s.cs {
		if !all && c.state != "running" {
			continue
		}
		out = append(out, map[string]any{"Id": c.id, "Names": []string{"/" + c.name}, "Image": c.image, "State": c.state})
	}
	writeJSON(w, 200, out)
}

func (s *Server) inspect(c *container) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	labels := map[string]string{}
	if c.project != "" {
		dir := projectDir(s.workdir, c.project)
		labels["com.docker.compose.project"] = c.project
		labels["com.docker.compose.service"] = c.service
		labels["com.docker.compose.project.working_dir"] = dir
		labels["com.docker.compose.project.config_files"] = filepath.Join(dir, "docker-compose.yml")
		labels["com.docker.compose.container-number"] = "1"
		labels["com.docker.compose.oneoff"] = "False"
		if c.hash != "" {
			labels["com.docker.compose.config-hash"] = c.hash
		}
		var deps []string
		for _, d := range c.dependsOn {
			deps = append(deps, d+":service_started:false")
		}
		labels["com.docker.compose.depends_on"] = strings.Join(deps, ",")
	}
	ports := map[string][]map[string]string{}    // what is published right now
	bindings := map[string][]map[string]string{} // what the container is configured to publish
	exposed := map[string]struct{}{}
	for _, p := range c.ports {
		key := fmt.Sprintf("%d/tcp", p.container)
		b := []map[string]string{{"HostIp": "0.0.0.0", "HostPort": strconv.Itoa(p.host)}, {"HostIp": "::", "HostPort": strconv.Itoa(p.host)}}
		bindings[key] = b
		if c.state != "exited" { // like real Docker: a stopped container publishes nothing
			ports[key] = b
		}
		exposed[key] = struct{}{}
	}
	netMode := "bridge"
	if c.project != "" {
		netMode = c.project + "_default"
	}
	nets := map[string]any{}
	if c.hostNet {
		netMode = "host"
	} else {
		nets[netMode] = map[string]any{"IPAddress": "172.20.0.2", "Aliases": []string{c.name, c.service}}
	}
	state := map[string]any{
		"Status": c.state, "Running": c.state == "running" || c.state == "restarting", "Paused": false,
		"Restarting": c.state == "restarting", "OOMKilled": false, "ExitCode": c.exitCode,
		"StartedAt": c.started.UTC().Format(time.RFC3339Nano), "FinishedAt": "0001-01-01T00:00:00Z",
	}
	if !c.finished.IsZero() {
		state["FinishedAt"] = c.finished.UTC().Format(time.RFC3339Nano)
	}
	if c.health != "" && c.state == "running" {
		state["Health"] = map[string]string{"Status": c.health}
	}
	var mounts []map[string]string
	for _, m := range c.mounts {
		mounts = append(mounts, map[string]string{"Type": m.typ, "Name": m.name, "Destination": m.dest, "Source": "/demo/" + m.dest})
	}
	return map[string]any{
		"Id": c.id, "Name": "/" + c.name, "Created": s.start.Add(-c.age).UTC().Format(time.RFC3339Nano),
		"Image": c.imageID, "State": state, "RestartCount": c.restarts,
		"HostConfig":      map[string]any{"NetworkMode": netMode, "RestartPolicy": map[string]string{"Name": c.policy}, "PortBindings": bindings},
		"Config":          map[string]any{"Image": c.image, "Labels": labels, "ExposedPorts": exposed, "Tty": false},
		"NetworkSettings": map[string]any{"Ports": ports, "Networks": nets},
		"Mounts":          mounts,
	}
}

func (s *Server) stats(c *container) map[string]any {
	t := time.Since(s.start).Seconds()
	sysRate := float64(cores) * 1e9 // nanoseconds of CPU time per second
	off := float64(len(c.name)) * 37 * mb
	out := map[string]any{
		"read": time.Now().UTC().Format(time.RFC3339Nano),
		"cpu_stats": map[string]any{
			"cpu_usage":        map[string]any{"total_usage": uint64(c.cpu.integral(t)*sysRate) + 5_000_000_000},
			"system_cpu_usage": uint64(t*sysRate) + 8_600_000_000_000_000,
			"online_cpus":      cores,
		},
		"memory_stats": map[string]any{"usage": uint64(c.mem.at(t)), "limit": uint64(125 * gb), "stats": map[string]uint64{"inactive_file": 0}},
		"blkio_stats": map[string]any{"io_service_bytes_recursive": []map[string]any{
			{"op": "read", "value": uint64(c.blkR.integral(t) + off)}, {"op": "write", "value": uint64(c.blkW.integral(t) + off)},
		}},
	}
	if !c.hostNet {
		out["networks"] = map[string]any{"eth0": map[string]any{"rx_bytes": uint64(c.rx.integral(t) + off), "tx_bytes": uint64(c.tx.integral(t) + off/2)}}
	}
	return out
}

func (s *Server) df(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cs []map[string]any
	vols := map[string]int64{}
	for _, c := range s.cs {
		cs = append(cs, map[string]any{"Id": c.id, "SizeRw": c.sizeRw})
		for _, m := range c.mounts {
			if m.typ == "volume" {
				h := fnv.New32a()
				h.Write([]byte(m.name))
				vols[m.name] = int64(h.Sum32()%900+100) * int64(mb)
			}
		}
	}
	var vs []map[string]any
	for n, sz := range vols {
		vs = append(vs, map[string]any{"Name": n, "UsageData": map[string]int64{"Size": sz}})
	}
	writeJSON(w, 200, map[string]any{"Containers": cs, "Volumes": vs})
}

// ---- start / stop / restart, and the event stream that announces them ----

func (s *Server) act(w http.ResponseWriter, c *container, action string) {
	s.mu.Lock()
	now := time.Now()
	var events []string
	switch action {
	case "start":
		if c.state == "exited" {
			c.state, c.started, c.finished = "running", now, time.Time{}
			events = []string{"start"}
		}
	case "stop":
		if c.state == "running" {
			c.state, c.finished, c.exitCode = "exited", now, 0
			events = []string{"kill", "die", "stop"}
		}
	case "restart":
		if c.state == "running" {
			c.started = now
			events = []string{"die", "start", "restart"}
		}
	case "pause", "unpause":
	default:
		s.mu.Unlock()
		notFound(w, "fakedocker: not implemented: "+action)
		return
	}
	for _, e := range events {
		ev, _ := json.Marshal(map[string]any{"Type": "container", "Action": e, "time": now.Unix(),
			"Actor": map[string]any{"ID": c.id, "Attributes": map[string]string{"name": c.name}}})
		for ch := range s.subs {
			select {
			case ch <- append(ev, '\n'):
			default:
			}
		}
	}
	s.mu.Unlock()
	w.WriteHeader(204)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	ch := make(chan []byte, 32)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()
	if fl != nil {
		fl.Flush()
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-ch:
			w.Write(b)
			if fl != nil {
				fl.Flush()
			}
		}
	}
}

// ---- logs ----

var logKinds = map[string][]string{
	"arr": {
		"[Info] RssSyncService: Starting RSS Sync",
		"[Info] DownloadDecisionMaker: Processing 100 releases",
		"[Info] RssSyncService: RSS Sync Completed. Reports found: 100, Reports grabbed: 0",
		"[Info] CheckHealthService: Health check passed",
		"[Info] RefreshMonitoredDownloads: Checking for completed downloads",
	},
	"grafana": {
		`logger=cleanup level=info msg="Completed cleanup jobs" duration=11.2ms`,
		`logger=plugins.update.checker level=info msg="Update check succeeded" duration=87.4ms`,
		`logger=infra.usagestats level=info msg="Usage stats are ready to report"`,
		`logger=context level=info msg="Request Completed" method=GET path=/api/health status=200 remote_addr=172.20.0.1 time_ms=1 size=67`,
	},
	"prom": {
		`ts=%s level=info component=tsdb msg="Head GC completed" caller=head.go:1260 duration=4.1ms`,
		`ts=%s level=info component=tsdb msg="WAL checkpoint complete" first=612 last=613 duration=25.3ms`,
		`ts=%s level=info component=tsdb msg="Compactions: head compaction completed"`,
	},
	"postgres": {
		"LOG:  checkpoint starting: time",
		"LOG:  checkpoint complete: wrote 24 buffers (0.1%); 0 WAL file(s) added, 0 removed, 0 recycled",
		"LOG:  automatic vacuum of table \"app.public.sessions\": index scans: 1",
	},
	"generic": {
		`172.20.0.1 - - "GET /api/v1/status HTTP/1.1" 200 54 "-" "TheBar"`,
		`INFO  request handled method=GET path=/health status=200 duration=2ms`,
		`INFO  background job finished name=cleanup took=140ms`,
		`172.20.0.1 - - "GET / HTTP/1.1" 200 1840 "-" "Mozilla/5.0"`,
	},
}

var crashLines = []string{
	"Waiting for PostgreSQL to start...",
	"Error: connect ECONNREFUSED 172.20.0.9:5432",
	"django.db.utils.OperationalError: connection to server at \"db\" (172.20.0.9), port 5432 failed",
	"Container exited with code 1, restarting",
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request, c *container) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tail <= 0 || tail > 1000 {
		tail = 200
	}
	follow := r.URL.Query().Get("follow") == "1" || r.URL.Query().Get("follow") == "true"
	fl, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
	w.WriteHeader(200)

	h := fnv.New64a()
	h.Write([]byte(c.name))
	rnd := rand.New(rand.NewSource(int64(h.Sum64())))
	pick := func() (byte, string) {
		if c.logKind == "crash" {
			l := crashLines[rnd.Intn(len(crashLines))]
			if strings.HasPrefix(l, "Error") || strings.HasPrefix(l, "django") {
				return 2, l
			}
			return 1, l
		}
		set := logKinds[c.logKind]
		if set == nil {
			set = logKinds["generic"]
		}
		l := set[rnd.Intn(len(set))]
		if strings.Contains(l, "%s") {
			l = fmt.Sprintf(l, time.Now().UTC().Format(time.RFC3339))
		}
		return 1, l
	}
	frame := func(stream byte, ts time.Time, line string) error {
		payload := ts.UTC().Format(time.RFC3339Nano) + " " + line + "\n"
		hdr := make([]byte, 8)
		hdr[0] = stream
		binary.BigEndian.PutUint32(hdr[4:], uint32(len(payload)))
		if _, err := w.Write(append(hdr, payload...)); err != nil {
			return err
		}
		return nil
	}
	end := time.Now()
	if c.state == "exited" {
		end = c.finished
	}
	for i := tail; i > 0; i-- { // history: one line every ~15 s, oldest first
		st, l := pick()
		if err := frame(st, end.Add(-time.Duration(i)*15*time.Second), l); err != nil {
			return
		}
	}
	if fl != nil {
		fl.Flush()
	}
	if !follow || c.state == "exited" {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
			st, l := pick()
			if err := frame(st, time.Now(), l); err != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
	}
}
