package main

// Host-wide numbers for the strip at the top. TheBar runs with host
// networking, and /proc/{stat,meminfo,loadavg,uptime,net} are host-wide.

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Disk struct {
	Label   string `json:"label"`
	Path    string `json:"path"`
	Total   uint64 `json:"total"`
	Used    uint64 `json:"used"`
	Problem string `json:"problem,omitempty"` // set when the disk can't actually be read

	Optional bool `json:"-"` // "Label=/path?": only shown while something is mounted there
}

type HostInfo struct {
	Hostname string     `json:"hostname"`
	CPU      float64    `json:"cpu"` // percent of all cores
	Cores    int        `json:"cores"`
	Load     [3]float64 `json:"load"`
	MemTotal uint64     `json:"memTotal"`
	MemUsed  uint64     `json:"memUsed"`
	Uptime   int64      `json:"uptime"` // seconds
	Iface    string     `json:"iface"`
	Rx       float64    `json:"rx"`
	Tx       float64    `json:"tx"`
	Disks    []Disk     `json:"disks"`
}

type Host struct {
	hub   *Hub
	disks []Disk // label+path configured

	mu   sync.RWMutex
	info HostInfo

	probing sync.Map // path -> true while a disk check is still running

	prevIdle, prevTotal uint64
	prevRx, prevTx      uint64
	prevT               time.Time
}

// parseDisks reads "System=/opt,Storage=/mnt/storage,USB=/mnt/usb?".
// A trailing "?" marks a mount point that is often empty.
func parseDisks(spec string) []Disk {
	var out []Disk
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		label, path, ok := strings.Cut(part, "=")
		if !ok {
			path = label
		}
		path = strings.TrimSpace(path)
		optional := strings.HasSuffix(path, "?")
		path = strings.TrimSuffix(path, "?")
		out = append(out, Disk{Label: strings.TrimSpace(label), Path: path, Optional: optional})
	}
	return out
}

func NewHost(hub *Hub, disks string) *Host {
	return &Host{hub: hub, disks: parseDisks(disks)}
}

func (h *Host) Info() HostInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.info
}

func (h *Host) Run(ctx context.Context) {
	t := time.NewTicker(statsInterval)
	defer t.Stop()
	for {
		h.sample()
		h.hub.Broadcast("host", h.Info())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (h *Host) sample() {
	var in HostInfo
	in.Hostname = readHostname()
	now := time.Now()

	// CPU
	if f, err := os.ReadFile("/proc/stat"); err == nil {
		line, _, _ := strings.Cut(string(f), "\n")
		fields := strings.Fields(line)
		var total, idle uint64
		for i, v := range fields[1:] {
			n, _ := strconv.ParseUint(v, 10, 64)
			if i < 8 { // skip guest columns, they're included in user/nice
				total += n
			}
			if i == 3 || i == 4 { // idle + iowait
				idle += n
			}
		}
		if h.prevTotal > 0 && total > h.prevTotal {
			in.CPU = round2(100 * (1 - float64(idle-h.prevIdle)/float64(total-h.prevTotal)))
		}
		h.prevIdle, h.prevTotal = idle, total
		in.Cores = strings.Count(string(f), "\ncpu")
	}

	// memory
	if f, err := os.Open("/proc/meminfo"); err == nil {
		sc := bufio.NewScanner(f)
		var avail uint64
		for sc.Scan() {
			k, v, _ := strings.Cut(sc.Text(), ":")
			n, _ := strconv.ParseUint(strings.Fields(v)[0], 10, 64)
			switch k {
			case "MemTotal":
				in.MemTotal = n * 1024
			case "MemAvailable":
				avail = n * 1024
			}
		}
		f.Close()
		in.MemUsed = in.MemTotal - avail
	}

	if f, err := os.ReadFile("/proc/loadavg"); err == nil {
		fs := strings.Fields(string(f))
		for i := 0; i < 3 && i < len(fs); i++ {
			in.Load[i], _ = strconv.ParseFloat(fs[i], 64)
		}
	}
	if f, err := os.ReadFile("/proc/uptime"); err == nil {
		up, _ := strconv.ParseFloat(strings.Fields(string(f))[0], 64)
		in.Uptime = int64(up)
	}

	// network on the default-route interface
	in.Iface = defaultIface()
	if in.Iface != "" {
		rx, tx := ifaceBytes(in.Iface)
		if !h.prevT.IsZero() && rx >= h.prevRx && tx >= h.prevTx {
			dt := now.Sub(h.prevT).Seconds()
			in.Rx, in.Tx = float64(rx-h.prevRx)/dt, float64(tx-h.prevTx)/dt
		}
		h.prevRx, h.prevTx = rx, tx
	}
	h.prevT = now

	for _, d := range h.disks {
		if d.Optional && !h.isMountPoint(d.Path) {
			continue // nothing plugged in right now
		}
		d.Total, d.Used, d.Problem = h.probeDisk(d.Path)
		in.Disks = append(in.Disks, d)
	}

	h.mu.Lock()
	h.info = in
	h.mu.Unlock()
}

func readHostname() string {
	if b, err := os.ReadFile("/etc/hostname"); err == nil {
		return strings.TrimSpace(string(b))
	}
	n, _ := os.Hostname()
	return n
}

// probeDisk reports size and usage of the filesystem at path, and checks that
// it can really be read. statfs alone isn't enough: a USB drive that vanished
// while mounted keeps answering statfs with its last known numbers, but
// reading the directory fails with an I/O error.
// Runs with a timeout so a hung mount can't stall the loop; while a previous
// check of the same path is still stuck, no new one is started.
func (h *Host) probeDisk(path string) (total, used uint64, problem string) {
	if _, busy := h.probing.LoadOrStore(path, true); busy {
		return 0, 0, "Not responding"
	}
	type result struct {
		total, used uint64
		problem     string
	}
	done := make(chan result, 1)
	go func() {
		defer h.probing.Delete(path)
		var r result
		var st syscall.Statfs_t
		if err := syscall.Statfs(path, &st); err != nil {
			r.problem = "Not available (" + errText(err) + ")"
			done <- r
			return
		}
		bs := uint64(st.Bsize)
		r.total = st.Blocks * bs
		r.used = r.total - st.Bfree*bs
		f, err := os.Open(path)
		if err == nil {
			_, err = f.Readdirnames(1)
			f.Close()
			if errors.Is(err, io.EOF) {
				err = nil // empty directory is fine
			}
		}
		if err != nil {
			if errors.Is(err, syscall.EIO) {
				r.problem = "I/O error: drive disconnected or failing"
			} else {
				r.problem = "Can't read (" + errText(err) + ")"
			}
		}
		done <- r
	}()
	select {
	case r := <-done:
		return r.total, r.used, r.problem
	case <-time.After(2 * time.Second):
		return 0, 0, "Not responding"
	}
}

// isMountPoint tells whether another filesystem is mounted at path (its
// device differs from its parent's). When in doubt (errors, timeouts) it says
// yes, so probeDisk gets to report the actual problem.
func (h *Host) isMountPoint(path string) bool {
	key := "mnt:" + path
	if _, busy := h.probing.LoadOrStore(key, true); busy {
		return true
	}
	done := make(chan bool, 1)
	go func() {
		defer h.probing.Delete(key)
		var a, b syscall.Stat_t
		if syscall.Stat(path, &a) != nil || syscall.Stat(filepath.Dir(path), &b) != nil {
			done <- true
			return
		}
		done <- a.Dev != b.Dev
	}()
	select {
	case m := <-done:
		return m
	case <-time.After(time.Second):
		return true
	}
}

func errText(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno.Error()
	}
	return err.Error()
}

func defaultIface() string {
	f, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(f), "\n")[1:] {
		fs := strings.Fields(l)
		if len(fs) > 2 && fs[1] == "00000000" {
			return fs[0]
		}
	}
	return ""
}

func ifaceBytes(iface string) (rx, tx uint64) {
	f, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return
	}
	for _, l := range strings.Split(string(f), "\n") {
		name, rest, ok := strings.Cut(l, ":")
		if !ok || strings.TrimSpace(name) != iface {
			continue
		}
		fs := strings.Fields(rest)
		if len(fs) >= 9 {
			rx, _ = strconv.ParseUint(fs[0], 10, 64)
			tx, _ = strconv.ParseUint(fs[8], 10, 64)
		}
	}
	return
}
