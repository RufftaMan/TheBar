package main

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RufftaMan/TheBar/internal/fakedocker"
)

// startFake runs the fake Docker on a temporary socket.
func startFake(t *testing.T) (*Docker, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "fd") // short path: unix sockets have a length limit
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	srv, err := fakedocker.New(filepath.Join(dir, "stacks"))
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "d.sock")
	go srv.Serve(sock)
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return NewDocker(sock), filepath.Join(dir, "stacks")
}

func TestTheBarSeesTheFakeHost(t *testing.T) {
	d, stacks := startFake(t)
	inv := NewInventory(d, NewHub(), "thebar")
	inv.load(context.Background())
	byName := map[string]*Container{}
	for _, c := range inv.List() {
		byName[c.Name] = c
	}
	if len(byName) < 20 {
		t.Fatalf("only %d containers", len(byName))
	}

	// states TheBar must be able to show
	if c := byName["radarr"]; c == nil || c.State != "running" || c.Project != "media" || len(c.Ports) == 0 || c.Ports[0].Host != 7878 {
		t.Fatalf("radarr: %+v", c)
	}
	if c := byName["lidarr"]; c == nil || c.State != "exited" || c.FinishedAt == "" {
		t.Fatalf("lidarr should be stopped: %+v", c)
	}

	// A stopped container publishes no ports (real Docker reports none), but it
	// must keep its web UI: otherwise the card changes shape during every update
	// and the helpers that were folded into it pop out as "new" cards.
	if c := byName["lidarr"]; webPort(c) != 8686 {
		t.Fatalf("stopped lidarr lost its web port: ports=%v webPort=%d", c.Ports, webPort(c))
	}
	if c := byName["paperless-ngx"]; c == nil || c.Alert != "Restart loop" {
		t.Fatalf("paperless should be a restart loop, alert=%q", byName["paperless-ngx"].Alert)
	}
	if c := byName["immich_server"]; c == nil || c.Health != "healthy" || len(c.DependsOn) != 2 {
		t.Fatalf("immich_server: %+v", c)
	}
	if c := byName["jellyfin"]; c == nil || c.NetworkMode != "host" {
		t.Fatalf("jellyfin should use host networking: %+v", c)
	}
	if c := byName["thebar"]; c == nil || !c.Self {
		t.Fatal("the thebar container should be recognised as TheBar itself")
	}

	// the compose files the Config tab edits exist where the labels say
	for _, c := range byName {
		for _, f := range c.ConfigFiles {
			if !strings.HasPrefix(f, stacks) {
				t.Fatalf("%s: compose file %s is outside the demo folder", c.Name, f)
			}
			if _, err := os.Stat(f); err != nil {
				t.Fatalf("%s: %v", c.Name, err)
			}
		}
	}
}

func TestFakeStatsAreCountersThatGrow(t *testing.T) {
	d, _ := startFake(t)
	ctx := context.Background()
	list, _ := d.List(ctx)
	var id string
	for _, c := range list {
		if c.Names[0] == "/sabnzbd" {
			id = c.ID
		}
	}
	a, err := d.Stats(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	b, _ := d.Stats(ctx, id)
	if b.CPUStats.CPUUsage.TotalUsage <= a.CPUStats.CPUUsage.TotalUsage || b.CPUStats.SystemUsage <= a.CPUStats.SystemUsage {
		t.Fatal("CPU counters must increase")
	}
	if b.Networks["eth0"].RxBytes <= a.Networks["eth0"].RxBytes {
		t.Fatal("network counters must increase")
	}
	if b.MemoryStats.Usage == 0 || b.MemoryStats.Limit == 0 {
		t.Fatal("memory missing")
	}
}

func TestFakeLogsUseDockersStreamFormat(t *testing.T) {
	d, _ := startFake(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	list, _ := d.List(ctx)
	var id string
	for _, c := range list {
		if c.Names[0] == "/sonarr" {
			id = c.ID
		}
	}
	n := 0
	err := d.Logs(ctx, id, 30, false, false, func(stream byte, line string) {
		n++
		if i := strings.IndexByte(line, ' '); i < 20 || line[4] != '-' {
			t.Errorf("line without a docker timestamp: %q", line)
		}
	})
	if err != nil && err.Error() != "EOF" {
		t.Fatal(err)
	}
	if n != 30 {
		t.Fatalf("got %d log lines, want 30", n)
	}
	_ = bufio.NewReader
}

func TestFakeUpdateStatesAndActions(t *testing.T) {
	d, _ := startFake(t)
	ctx := context.Background()
	inv := NewInventory(d, NewHub(), "")
	inv.load(ctx)
	u := NewUpdates(d, inv)
	u.check(ctx, true)
	status := map[string]string{}
	for _, c := range inv.List() {
		status[c.Name] = c.Update
	}
	if status["radarr"] != "available" || status["sonarr"] != "available" {
		t.Fatalf("radarr/sonarr should have an update available: %v", status)
	}
	if status["nextcloud-app-1"] != "recreate" {
		t.Fatalf("nextcloud should need a recreate, has %q", status["nextcloud-app-1"])
	}
	if status["grafana"] != "current" || status["jellyfin"] != "current" {
		t.Fatalf("grafana/jellyfin should be current: %v", status)
	}

	// stop and start go through and show up in the next listing
	id := inv.Get("sabnzbd").ID
	if err := d.Action(ctx, id, "stop"); err != nil {
		t.Fatal(err)
	}
	inv.load(ctx)
	if got := inv.Get("sabnzbd").State; got != "exited" {
		t.Fatalf("after stop: %s", got)
	}
	if err := d.Action(ctx, id, "start"); err != nil {
		t.Fatal(err)
	}
	inv.load(ctx)
	if got := inv.Get("sabnzbd").State; got != "running" {
		t.Fatalf("after start: %s", got)
	}
	// anything that would change the host is refused
	if err := d.post(ctx, "/containers/create", nil); err == nil {
		t.Fatal("the fake host must refuse to create things")
	}
}
