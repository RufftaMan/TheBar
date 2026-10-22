package main

import (
	"strings"
	"testing"
)

func hasEnv(env []string, prefix string) string {
	v := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			v = kv // the last one wins, like in a process environment
		}
	}
	return v
}

// A TheBar that talks to another Docker (demo, tests) must not let its
// compose commands reach the Docker named by the surrounding environment.
func TestComposeCommandsFollowTheConfiguredDockerSocket(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///var/run/real-docker.sock")
	t.Setenv("THEBAR_DOCKER_SOCK", "/tmp/demo/docker.sock")
	if got := hasEnv(composeEnv("/stacks/media"), "DOCKER_HOST="); got != "DOCKER_HOST=unix:///tmp/demo/docker.sock" {
		t.Fatalf("compose would talk to %q", got)
	}
	if got := hasEnv(composeEnv("/stacks/media"), "THEBAR_"); got != "" {
		t.Fatalf("TheBar's own settings leaked into the compose environment: %q", got)
	}

	// default setup: nothing is overridden
	t.Setenv("THEBAR_DOCKER_SOCK", "")
	if got := hasEnv(composeEnv("/stacks/media"), "DOCKER_HOST="); got != "DOCKER_HOST=unix:///var/run/real-docker.sock" {
		t.Fatalf("default setup changed DOCKER_HOST to %q", got)
	}
}
