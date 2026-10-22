#!/usr/bin/env bash
# Rebuilds docs/screenshots from TheBar running against a fake Docker host.
# Nothing here touches your real Docker. Needs: go, node/npm and docker (the
# headless browser runs in Playwright's official image).
#
#   tools/screenshots/run.sh            build everything, wait for the charts to fill, take the pictures
#   SKIP_BUILD=1 tools/screenshots/run.sh   reuse the frontend that's already built
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
WORK="$(mktemp -d)"
DEMO=/tmp/thebar-demo              # fixed, so the compose paths in the pictures look tidy
PORT="${PORT:-18899}"
WARMUP="${WARMUP:-100}"            # seconds of stats history before the shots
PLAYWRIGHT_IMAGE="mcr.microsoft.com/playwright:v1.64.1-noble"
pids=()
cleanup() { for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done; rm -rf "$WORK" "$DEMO"; }
trap cleanup EXIT

if [ -z "${SKIP_BUILD:-}" ]; then
  (cd "$ROOT/frontend" && npm ci --no-audit --no-fund && npm run build)
fi
(cd "$ROOT/backend" && go build -o "$WORK/thebar" . && go build -o "$WORK/fakedocker" ./cmd/fakedocker)

rm -rf "$DEMO"
"$WORK/fakedocker" --dir "$DEMO" >"$WORK/fakedocker.log" 2>&1 &
pids+=($!)
for _ in $(seq 50); do [ -S "$DEMO/docker.sock" ] && break; sleep 0.2; done

# THEBAR_DOCKER_SOCK also steers TheBar's docker compose commands to the fake host
THEBAR_DOCKER_SOCK="$DEMO/docker.sock" THEBAR_SELF=thebar THEBAR_AUTH=off \
  THEBAR_DATA="$DEMO/data" THEBAR_LISTEN="127.0.0.1:$PORT" "$WORK/thebar" >"$WORK/thebar.log" 2>&1 &
pids+=($!)
for _ in $(seq 50); do curl -fs "http://127.0.0.1:$PORT/" >/dev/null && break; sleep 0.2; done

echo "Waiting ${WARMUP}s for the charts to fill..."
sleep "$WARMUP"

mkdir -p "$ROOT/docs/screenshots"
docker run --rm --network host --user "$(id -u):$(id -g)" -e HOME=/tmp \
  -e BASE="http://127.0.0.1:$PORT" -e OUT=/out \
  -v "$ROOT/tools/screenshots:/w" -v "$ROOT/docs/screenshots:/out" -w /w \
  "$PLAYWRIGHT_IMAGE" sh -c "npm ci --no-audit --no-fund >/dev/null && node capture.mjs"
