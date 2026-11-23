package main

import "testing"

func withListening(t *testing.T, ports ...int) {
	t.Helper()
	old := listeningTCP
	t.Cleanup(func() { listeningTCP = old })
	listeningTCP = func() map[int]bool {
		m := map[int]bool{}
		for _, p := range ports {
			m[p] = true
		}
		return m
	}
}

func host(state string, exposed ...int) *Container {
	return &Container{Name: "app", NetworkMode: "host", State: state, Exposed: exposed}
}

// A container on host networking publishes nothing, so there are no port
// bindings to read. Its image's declared ports (EXPOSE) tell where to look.
func TestHostModeUsesTheImagesDeclaredPortThatIsListening(t *testing.T) {
	withListening(t, 22, 8430, 9000)
	if got := webPort(host("running", 8430)); got != 8430 {
		t.Fatalf("one declared port that listens: got %d, want 8430", got)
	}
}

func TestHostModePrefersTheUsualWebPorts(t *testing.T) {
	withListening(t, 80, 8080, 9100)
	if got := webPort(host("running", 8080, 9100, 80)); got != 80 {
		t.Fatalf("got %d, want 80 (usual web port beats the rest)", got)
	}
	withListening(t, 9100, 9200)
	if got := webPort(host("running", 9200, 9100)); got != 9100 {
		t.Fatalf("no usual port: got %d, want the lowest listening one (9100)", got)
	}
}

func TestHostModeDoesNotLinkToAPortNobodyListensOn(t *testing.T) {
	withListening(t, 22)
	if got := webPort(host("running", 8430)); got != 0 {
		t.Fatalf("declared but not listening: got %d, want 0", got)
	}
}

func TestHostModeStoppedAppKeepsItsLink(t *testing.T) {
	withListening(t) // nothing listens while it is stopped
	if got := webPort(host("exited", 8430)); got != 8430 {
		t.Fatalf("stopped app lost its web UI: got %d", got)
	}
}

func TestHostModeWithoutAnyHintHasNoLink(t *testing.T) {
	withListening(t, 8430)
	if got := webPort(host("running")); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

func TestKnownAppsKeepTheirPortInHostMode(t *testing.T) {
	withListening(t)
	c := host("running", 8096, 8920)
	c.App = &KnownApp{Name: "Jellyfin", Port: 8096}
	if got := webPort(c); got != 8096 {
		t.Fatalf("got %d, want 8096", got)
	}
}

// For anything else there is a label: thebar.port=<host port> wins in every mode.
func TestPortLabelOverridesEverything(t *testing.T) {
	withListening(t, 8430, 9999)
	c := host("running", 8430)
	c.Labels = map[string]string{"thebar.port": "9999"}
	if got := webPort(c); got != 9999 {
		t.Fatalf("host mode: got %d, want 9999", got)
	}
	b := &Container{Name: "x", NetworkMode: "bridge", Ports: []Port{{Host: 8000, Container: 80, Proto: "tcp"}}, Labels: map[string]string{"thebar.port": "9000"}}
	if got := webPort(b); got != 9000 {
		t.Fatalf("bridge mode: got %d, want 9000", got)
	}
	for _, bad := range []string{"abc", "0", "70000", "-1", ""} {
		c.Labels = map[string]string{"thebar.port": bad}
		if got := webPort(c); got != 8430 {
			t.Fatalf("label %q should be ignored, got %d", bad, got)
		}
	}
}

// Sidecars whose API listens on loopback only are not web UIs.
func TestLoopbackOnlyListenersAreNotWebUIs(t *testing.T) {
	merged := map[string]string{}
	mergeBind(merged, "5580/tcp", bindOf("0100007F", false))                        // 127.0.0.1
	mergeBind(merged, "8430/tcp", bindOf("00000000", false))                        // 0.0.0.0
	mergeBind(merged, "8441/tcp", bindOf("0100007F", false))                        // loopback first,
	mergeBind(merged, "8441/tcp", bindOf("00000000", false))                        // then all
	mergeBind(merged, "9000/tcp", bindOf("00000000", false))                        // all first,
	mergeBind(merged, "9000/tcp", bindOf("0100007F", false))                        // then loopback
	mergeBind(merged, "9100/tcp", bindOf("0501A8C0", false))                        // 192.168.1.5 first,
	mergeBind(merged, "9100/tcp", bindOf("0100007F", false))                        // then loopback
	mergeBind(merged, "5581/tcp", bindOf("00000000000000000000000001000000", true)) // ::1
	mergeBind(merged, "5582/udp", bindOf("0100007F", false))
	up := tcpWebPorts(merged)
	for port, want := range map[int]bool{
		5580: false, // loopback only: ignored
		8430: true,  // 0.0.0.0: used
		8441: true,  // loopback and all addresses: used
		9000: true,
		9100: true,  // loopback and a LAN address: used
		5581: false, // ::1 only: ignored
		5582: false, // not tcp
	} {
		if up[port] != want {
			t.Errorf("port %d: listed=%v, want %v", port, up[port], want)
		}
	}
}

func TestBindOf(t *testing.T) {
	for _, c := range []struct {
		addr string
		v6   bool
		want string
	}{
		{"00000000", false, "all"},
		{"0100007F", false, "127.0.0.1"},
		{"0501A8C0", false, "192.168.1.5"},
		{"00000000000000000000000000000000", true, "all"},
		{"00000000000000000000000001000000", true, "127.0.0.1"},
		{"B80D0120000000000000000001000000", true, "specific"},
	} {
		if got := bindOf(c.addr, c.v6); got != c.want {
			t.Errorf("bindOf(%s, %v) = %q, want %q", c.addr, c.v6, got, c.want)
		}
	}
}
