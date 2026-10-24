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
