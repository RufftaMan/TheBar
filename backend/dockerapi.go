package main

// Minimal Docker Engine API client over the unix socket.
// We only need a handful of endpoints, so this avoids pulling in the full SDK.

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Docker struct {
	http   *http.Client // short requests
	stream *http.Client // long-lived streams (events, logs)
}

func NewDocker(sock string) *Docker {
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", sock)
	}
	return &Docker{
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{DialContext: dial, MaxIdleConnsPerHost: 16},
		},
		stream: &http.Client{Transport: &http.Transport{DialContext: dial}},
	}
}

type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return fmt.Sprintf("docker: %d %s", e.Status, e.Message) }

func (d *Docker) do(ctx context.Context, c *http.Client, method, path string, q url.Values) (*http.Response, error) {
	u := "http://docker" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		var m struct{ Message string }
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = json.Unmarshal(b, &m)
		if m.Message == "" {
			m.Message = strings.TrimSpace(string(b))
		}
		return nil, &apiError{resp.StatusCode, m.Message}
	}
	return resp, nil
}

func (d *Docker) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	resp, err := d.do(ctx, d.http, "GET", path, q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

func (d *Docker) post(ctx context.Context, path string, q url.Values) error {
	resp, err := d.do(ctx, d.http, "POST", path, q)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ---- types (only the fields we use) ----

type dPort struct {
	IP          string
	PrivatePort int
	PublicPort  int
	Type        string
}

type dContainerSummary struct {
	ID     string `json:"Id"`
	Names  []string
	Image  string
	State  string
	Status string
}

type dInspect struct {
	ID      string `json:"Id"`
	Name    string
	Created string
	Image   string // image ID
	State   struct {
		Status     string
		Running    bool
		Paused     bool
		Restarting bool
		OOMKilled  bool
		ExitCode   int
		StartedAt  string
		FinishedAt string
		Health     *struct{ Status string }
	}
	RestartCount int
	HostConfig   struct {
		NetworkMode   string
		RestartPolicy struct{ Name string }
	}
	Config struct {
		Image        string
		Labels       map[string]string
		ExposedPorts map[string]struct{}
		Tty          bool
	}
	NetworkSettings struct {
		Ports    map[string][]struct{ HostIp, HostPort string }
		Networks map[string]struct {
			IPAddress string
			Aliases   []string
		}
	}
	Mounts []struct {
		Type        string
		Name        string
		Source      string
		Destination string
	}
}

type dStats struct {
	Read     string `json:"read"`
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs  int    `json:"online_cpus"`
	} `json:"cpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IOServiceBytesRecursive []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
}

type dImage struct {
	ID          string `json:"Id"`
	RepoDigests []string
	RepoTags    []string
}

type dEvent struct {
	Type   string
	Action string
	Actor  struct {
		ID         string
		Attributes map[string]string
	}
	Time int64 `json:"time"`
}

// ---- endpoints ----

func (d *Docker) List(ctx context.Context) ([]dContainerSummary, error) {
	var out []dContainerSummary
	err := d.getJSON(ctx, "/containers/json", url.Values{"all": {"1"}}, &out)
	return out, err
}

func (d *Docker) Inspect(ctx context.Context, id string) (*dInspect, error) {
	var out dInspect
	if err := d.getJSON(ctx, "/containers/"+id+"/json", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (d *Docker) Stats(ctx context.Context, id string) (*dStats, error) {
	var out dStats
	err := d.getJSON(ctx, "/containers/"+id+"/stats", url.Values{"stream": {"false"}, "one-shot": {"true"}}, &out)
	return &out, err
}

func (d *Docker) Action(ctx context.Context, id, action string) error {
	switch action {
	case "start", "stop", "restart", "pause", "unpause":
	default:
		return fmt.Errorf("unknown action %q", action)
	}
	return d.post(ctx, "/containers/"+id+"/"+action, nil)
}

func (d *Docker) ImageInspect(ctx context.Context, ref string) (*dImage, error) {
	var out dImage
	if err := d.getJSON(ctx, "/images/"+ref+"/json", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoteDigest asks the daemon (with its registry credentials) for the
// current manifest digest of an image reference.
func (d *Docker) RemoteDigest(ctx context.Context, ref string) (string, error) {
	var out struct{ Descriptor struct{ Digest string } }
	if err := d.getJSON(ctx, "/distribution/"+ref+"/json", nil, &out); err != nil {
		return "", err
	}
	return out.Descriptor.Digest, nil
}

type dfResult struct {
	Containers []struct {
		ID     string `json:"Id"`
		SizeRw int64
	}
	Volumes []struct {
		Name      string
		UsageData struct{ Size int64 }
	}
}

func (d *Docker) DiskUsage(ctx context.Context) (*dfResult, error) {
	var out dfResult
	err := d.getJSON(ctx, "/system/df", url.Values{"type": {"container", "volume"}}, &out)
	return &out, err
}

// Events streams container and image events until ctx is cancelled or the stream breaks.
func (d *Docker) Events(ctx context.Context, fn func(dEvent)) error {
	resp, err := d.do(ctx, d.stream, "GET", "/events", url.Values{"filters": {`{"type":["container","image"]}`}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	for {
		var ev dEvent
		if err := dec.Decode(&ev); err != nil {
			return err
		}
		fn(ev)
	}
}

// Logs streams log lines (stdout+stderr) to fn. Handles the multiplexed format
// used when the container has no TTY.
func (d *Docker) Logs(ctx context.Context, id string, tail int, follow, tty bool, fn func(stream byte, line string)) error {
	q := url.Values{
		"stdout":     {"1"},
		"stderr":     {"1"},
		"timestamps": {"1"},
		"tail":       {fmt.Sprint(tail)},
	}
	if follow {
		q.Set("follow", "1")
	}
	resp, err := d.do(ctx, d.stream, "GET", "/containers/"+id+"/logs", q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if tty {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			fn(1, sc.Text())
		}
		return sc.Err()
	}
	r := bufio.NewReader(resp.Body)
	hdr := make([]byte, 8)
	var partial [3]string // per-stream carry-over for lines split across frames
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			return err
		}
		n := binary.BigEndian.Uint32(hdr[4:])
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return err
		}
		s := hdr[0]
		if s > 2 {
			s = 1
		}
		text := partial[s] + string(buf)
		lines := strings.Split(text, "\n")
		partial[s] = lines[len(lines)-1]
		for _, l := range lines[:len(lines)-1] {
			fn(s, l)
		}
	}
}
