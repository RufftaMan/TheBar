package main

// Compose integration: read/edit/validate compose files, apply changes,
// detect drift (file changed but containers not recreated), stack actions.
//
// Compose files are mounted into TheBar at the same paths as on the host, so
// relative paths inside them resolve exactly like they do for you.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Project struct {
	Name     string
	Dir      string
	Files    []string
	Services map[string]string // service -> config hash of its (first) container
}

type Compose struct {
	inv     *Inventory
	jobs    *Jobs
	dataDir string

	mu    sync.Mutex
	drift map[string][]string // project -> services with drift
	mtime map[string]time.Time
}

func NewCompose(inv *Inventory, jobs *Jobs, dataDir string) *Compose {
	return &Compose{inv: inv, jobs: jobs, dataDir: dataDir, drift: map[string][]string{}, mtime: map[string]time.Time{}}
}

func (c *Compose) Projects() map[string]*Project {
	out := map[string]*Project{}
	for _, ct := range c.inv.List() {
		if ct.Project == "" || len(ct.ConfigFiles) == 0 {
			continue
		}
		p, ok := out[ct.Project]
		if !ok {
			p = &Project{Name: ct.Project, Dir: ct.WorkingDir, Files: ct.ConfigFiles, Services: map[string]string{}}
			out[ct.Project] = p
		}
		if _, seen := p.Services[ct.Service]; !seen {
			p.Services[ct.Service] = ct.ConfigHash
		}
	}
	return out
}

func (c *Compose) Project(name string) (*Project, error) {
	p, ok := c.Projects()[name]
	if !ok {
		return nil, fmt.Errorf("unknown compose project %q", name)
	}
	return p, nil
}

func (p *Project) envFile() string {
	f := filepath.Join(p.Dir, ".env")
	if _, err := os.Stat(f); err == nil {
		return f
	}
	return ""
}

// editable lists files the editor may touch: compose files + the .env.
func (p *Project) editable() []string {
	files := append([]string(nil), p.Files...)
	if e := p.envFile(); e != "" {
		files = append(files, e)
	}
	return files
}

func composeEnv(dir string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PWD=") || strings.HasPrefix(kv, "THEBAR_") {
			continue
		}
		env = append(env, kv)
	}
	// compose files may use ${PWD}; give them the project dir like a shell would
	env = append(env, "PWD="+dir)
	// The compose CLI must talk to the same Docker as TheBar's own API client.
	// Without this, TheBar pointed at another socket (a demo, a test) would run
	// its stack actions against whatever Docker the shell environment names.
	if sock := os.Getenv("THEBAR_DOCKER_SOCK"); sock != "" {
		env = append(env, "DOCKER_HOST=unix://"+sock)
	}
	return env
}

// args builds `docker compose ...` for the project. Overrides swap a file for
// a temporary copy (used for validation before saving).
func (p *Project) args(override map[string]string, envFile string, cmd ...string) step {
	a := []string{"docker", "compose", "--ansi", "never", "--progress", "plain", "-p", p.Name, "--project-directory", p.Dir}
	for _, f := range p.Files {
		if o, ok := override[f]; ok {
			f = o
		}
		a = append(a, "-f", f)
	}
	if envFile != "" {
		a = append(a, "--env-file", envFile)
	}
	return step{dir: p.Dir, env: composeEnv(p.Dir), args: append(a, cmd...)}
}

type FileDoc struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"` // compose | env
	Content string `json:"content"`
	Mtime   int64  `json:"mtime"`
}

func (c *Compose) Read(name string) (map[string]any, error) {
	p, err := c.Project(name)
	if err != nil {
		return nil, err
	}
	var files []FileDoc
	for _, f := range p.editable() {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		st, _ := os.Stat(f)
		kind := "compose"
		if strings.HasSuffix(f, ".env") {
			kind = "env"
		}
		files = append(files, FileDoc{Path: f, Kind: kind, Content: string(b), Mtime: st.ModTime().UnixNano()})
	}
	c.mu.Lock()
	drift := c.drift[name]
	c.mu.Unlock()
	return map[string]any{"project": name, "dir": p.Dir, "files": files, "drift": drift}, nil
}

var errStale = errors.New("the file was changed on disk since you opened it; reload and try again")

// Save validates new content with `docker compose config`, backs up the old
// file and writes the new one in place (keeping owner and permissions).
func (c *Compose) Save(ctx context.Context, name, path, content string, baseMtime int64) (map[string]any, error) {
	p, err := c.Project(name)
	if err != nil {
		return nil, err
	}
	allowed := false
	for _, f := range p.editable() {
		if f == path {
			allowed = true
		}
	}
	if !allowed {
		return nil, fmt.Errorf("%s is not a config file of %s", path, name)
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if baseMtime != 0 && st.ModTime().UnixNano() != baseMtime {
		return nil, errStale
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	// validate using a temp copy in place of the real file
	tmpDir, err := os.MkdirTemp(c.dataDir, "validate-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	tmp := filepath.Join(tmpDir, filepath.Base(path))
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return nil, err
	}
	var s step
	if strings.HasSuffix(path, ".env") {
		s = p.args(nil, tmp, "config", "-q")
	} else {
		s = p.args(map[string]string{path: tmp}, "", "config", "-q")
	}
	if out, err := RunQuick(ctx, s); err != nil {
		out = strings.ReplaceAll(out, tmp, path)
		return map[string]any{"ok": false, "error": strings.TrimSpace(out)}, nil
	}

	backup, err := c.backup(name, path)
	if err != nil {
		return nil, fmt.Errorf("backup failed, not saving: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return nil, err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	st, _ = os.Stat(path)
	drift := c.computeDrift(ctx, p)
	return map[string]any{"ok": true, "mtime": st.ModTime().UnixNano(), "backup": filepath.Base(backup), "drift": drift}, nil
}

// projectName matches what compose accepts as a project name. Names that reach
// the file system come from URLs, so anything else (".." and friends) is refused.
var projectName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func (c *Compose) backupDir(project string) (string, error) {
	if !projectName.MatchString(project) {
		return "", fmt.Errorf("invalid project name %q", project)
	}
	return filepath.Join(c.dataDir, "backups", project), nil
}

func (c *Compose) backup(project, path string) (string, error) {
	dir, err := c.backupDir(project)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	base := backupBase(path)
	dst := filepath.Join(dir, base+"."+time.Now().Format("20060102-150405"))
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		return "", err
	}
	// keep the 25 newest per file
	matches, _ := filepath.Glob(filepath.Join(dir, base+".*"))
	sort.Strings(matches)
	for len(matches) > 25 {
		os.Remove(matches[0])
		matches = matches[1:]
	}
	return dst, nil
}

// backupBase makes a flat, unique-ish name: /opt/myapp/docker-compose.yml -> opt_myapp_docker-compose.yml
func backupBase(path string) string {
	return strings.ReplaceAll(strings.TrimPrefix(path, "/"), "/", "_")
}

type Backup struct {
	Name string `json:"name"`
	Time int64  `json:"time"`
	Size int64  `json:"size"`
}

func (c *Compose) Backups(project, path string) ([]Backup, error) {
	dir, err := c.backupDir(project)
	if err != nil {
		return nil, err
	}
	matches, _ := filepath.Glob(filepath.Join(dir, backupBase(path)+".*"))
	var out []Backup
	for i := len(matches) - 1; i >= 0; i-- {
		st, err := os.Stat(matches[i])
		if err != nil {
			continue
		}
		out = append(out, Backup{Name: filepath.Base(matches[i]), Time: st.ModTime().UnixMilli(), Size: st.Size()})
	}
	return out, nil
}

func (c *Compose) ReadBackup(project, name string) (string, error) {
	if strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
		return "", errors.New("bad backup name")
	}
	dir, err := c.backupDir(project)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, name))
	return string(b), err
}

// computeDrift compares the hash compose would use now with the hash stored on
// the running containers.
func (c *Compose) computeDrift(ctx context.Context, p *Project) []string {
	out, err := RunQuick(ctx, p.args(nil, "", "config", "--hash", "*"))
	var drift []string
	if err == nil {
		for _, line := range strings.Split(out, "\n") {
			fs := strings.Fields(line)
			if len(fs) != 2 {
				continue
			}
			if have, ok := p.Services[fs[0]]; ok && have != "" && have != fs[1] {
				drift = append(drift, fs[0])
			}
		}
	} else {
		log.Printf("drift %s: %v: %s", p.Name, err, strings.TrimSpace(out))
	}
	sort.Strings(drift)
	c.mu.Lock()
	c.drift[p.Name] = drift
	c.mu.Unlock()
	set := map[string]bool{}
	for _, s := range drift {
		set[s] = true
	}
	c.inv.Update(func(ct *Container) bool {
		if ct.Project != p.Name {
			return false
		}
		d := set[ct.Service]
		if ct.Drift != d {
			ct.Drift = d
			return true
		}
		return false
	})
	return drift
}

// RunDrift periodically re-checks drift, and right away when a file's mtime changes.
func (c *Compose) RunDrift(ctx context.Context) {
	time.Sleep(5 * time.Second)
	last := time.Time{}
	for {
		full := time.Since(last) > 10*time.Minute
		for _, p := range c.Projects() {
			changed := false
			for _, f := range p.Files {
				if st, err := os.Stat(f); err == nil {
					c.mu.Lock()
					if !c.mtime[f].Equal(st.ModTime()) {
						c.mtime[f] = st.ModTime()
						changed = true
					}
					c.mu.Unlock()
				}
			}
			if full || changed {
				c.computeDrift(ctx, p)
			}
		}
		if full {
			last = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

// Drift returns the cached drift for a project.
func (c *Compose) Drift(project string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.drift[project]
}

// Action runs a stack-level operation as a background job.
//
//	apply   - recreate services whose config changed (or the given ones)
//	up      - docker compose up -d [services]
//	restart / stop / start
//	update  - pull new images, then up -d
func (c *Compose) Action(ctx context.Context, name, action string, services []string) (*Job, error) {
	p, err := c.Project(name)
	if err != nil {
		return nil, err
	}
	for _, s := range services {
		if _, ok := p.Services[s]; !ok {
			return nil, fmt.Errorf("unknown service %q", s)
		}
	}
	what := name
	if len(services) > 0 {
		what = name + " / " + strings.Join(services, ", ")
	}
	var steps []step
	switch action {
	case "apply":
		if len(services) == 0 {
			services = c.computeDrift(ctx, p)
		}
		if len(services) == 0 {
			return nil, errors.New("nothing to apply: all containers already match the compose file")
		}
		what = name + " / " + strings.Join(services, ", ")
		steps = []step{p.args(nil, "", append([]string{"up", "-d"}, services...)...)}
	case "up":
		steps = []step{p.args(nil, "", append([]string{"up", "-d"}, services...)...)}
	case "restart", "stop", "start":
		steps = []step{p.args(nil, "", append([]string{action}, services...)...)}
	case "update":
		// same routine as the dockerUpdate.sh scripts: pull, recreate, drop old images
		steps = []step{
			p.args(nil, "", append([]string{"pull"}, services...)...),
			p.args(nil, "", append([]string{"up", "-d"}, services...)...),
			{dir: p.Dir, env: composeEnv(p.Dir), args: []string{"docker", "image", "prune", "-f"}},
		}
	default:
		return nil, fmt.Errorf("unknown action %q", action)
	}
	title := map[string]string{"apply": "Apply config", "up": "Up", "restart": "Restart", "stop": "Stop", "start": "Start", "update": "Pull & recreate"}[action]
	job := c.jobs.Start("compose:"+name, title+": "+what, steps)
	// refresh drift once the job is done
	go func() {
		for {
			time.Sleep(2 * time.Second)
			done := false
			for _, j := range c.jobs.List() {
				if j.ID == job.ID && j.State != "running" {
					done = true
				}
			}
			if done {
				time.Sleep(time.Second)
				c.inv.Refresh()
				time.Sleep(time.Second)
				if p2, err := c.Project(name); err == nil {
					c.computeDrift(context.Background(), p2)
				}
				return
			}
		}
	}()
	return job, nil
}
