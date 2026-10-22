// fakedocker pretends to be a Docker host with a made-up set of services, for
// TheBar screenshots, demos and tests. It never touches the real Docker.
//
//	go run ./cmd/fakedocker
//	THEBAR_DOCKER_SOCK=/tmp/thebar-demo/docker.sock THEBAR_SELF=thebar THEBAR_AUTH=off \
//	  THEBAR_DATA=/tmp/thebar-demo/data THEBAR_LISTEN=127.0.0.1:8801 thebar
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/RufftaMan/TheBar/internal/fakedocker"
)

func main() {
	dir := flag.String("dir", filepath.Join(os.TempDir(), "thebar-demo"), "folder for the socket and the demo compose files")
	sock := flag.String("socket", "", "unix socket to listen on (default: <dir>/docker.sock)")
	flag.Parse()
	if *sock == "" {
		*sock = filepath.Join(*dir, "docker.sock")
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		log.Fatal(err)
	}
	srv, err := fakedocker.New(filepath.Join(*dir, "stacks"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Fake Docker host listening on %s\nStart TheBar against it with:\n  THEBAR_DOCKER_SOCK=%s THEBAR_SELF=thebar THEBAR_AUTH=off THEBAR_DATA=%s/data THEBAR_LISTEN=127.0.0.1:8801 thebar\n",
		*sock, *sock, *dir)
	log.Fatal(srv.Serve(*sock))
}
