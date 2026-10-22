package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeEngine serves just enough of the Docker API for the update checker:
// one local image and the registry's current digest for it.
type fakeEngine struct {
	mu     sync.Mutex
	id     string // local image ID
	digest string // local image's repo digest
	remote string // what the registry currently serves
	asked  int    // registry lookups so far
}

func (f *fakeEngine) set(id, digest, remote string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.id, f.digest, f.remote = id, digest, remote
}

func (f *fakeEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case strings.HasPrefix(r.URL.Path, "/images/"):
		json.NewEncoder(w).Encode(map[string]any{"Id": f.id, "RepoDigests": []string{"example/img@" + f.digest}})
	case strings.HasPrefix(r.URL.Path, "/distribution/"):
		f.asked++
		json.NewEncoder(w).Encode(map[string]any{"Descriptor": map[string]string{"digest": f.remote}})
	default:
		http.NotFound(w, r)
	}
}

func newTestUpdates(t *testing.T, f *fakeEngine) (*Updates, *Inventory) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", srv.Listener.Addr().String())
	}
	d := &Docker{http: &http.Client{Transport: &http.Transport{DialContext: dial}}}
	inv := NewInventory(d, NewHub(), "")
	inv.containers["c1"] = &Container{ID: "c1", Name: "app", Image: "example/img:latest", ImageID: "id1"}
	return NewUpdates(d, inv), inv
}

func badge(inv *Inventory) string { return inv.Get("c1").Update }

// The registry moves on between the last online check and the moment you
// update (linuxserver images are rebuilt several times a day). The badge must
// go away after the update, not linger until the next 12-hourly check.
func TestBadgeClearsAfterUpdateEvenIfRegistryMovedOn(t *testing.T) {
	f := &fakeEngine{}
	u, inv := newTestUpdates(t, f)
	ctx := context.Background()

	// 1. regular check: local image is older than the registry's
	f.set("id1", "sha256:d1", "sha256:d2")
	u.check(ctx, true)
	if got := badge(inv); got != "available" {
		t.Fatalf("step 1: badge = %q, want available", got)
	}

	// 2. meanwhile the registry got an even newer build (d3); the user pulls it.
	//    Nothing asks the registry again on its own: only the local state changed.
	f.set("id2", "sha256:d3", "sha256:d3")
	u.check(ctx, false)
	if got := badge(inv); got != "recreate" {
		t.Fatalf("step 2 (pulled, container still old): badge = %q, want recreate", got)
	}

	// 3. container recreated on the new image
	inv.containers["c1"].ImageID = "id2"
	u.check(ctx, false)
	if got := badge(inv); got != "current" {
		t.Fatalf("step 3 (recreated): badge = %q, want current", got)
	}
}

// Local re-checks must not hammer the registry when nothing changed locally.
func TestLocalRecheckDoesNotAskRegistry(t *testing.T) {
	f := &fakeEngine{}
	u, _ := newTestUpdates(t, f)
	f.set("id1", "sha256:d1", "sha256:d1")
	u.check(context.Background(), true)
	asked := f.asked
	for i := 0; i < 3; i++ {
		u.check(context.Background(), false)
	}
	if f.asked != asked {
		t.Fatalf("registry asked %d more times during local re-checks", f.asked-asked)
	}
}
