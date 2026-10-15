#!/usr/bin/env bash
# Takes screenshots of the built web UI against the stub daemon (made-up data), by
# driving headless Chrome through shoot.mjs (Node 22 or later).
# See README.md. Settings come from the environment:
#   CHROME        path to Chrome or Chromium (default: found on this machine)
#   SHOTS_PORT    port for the stub daemon (default 5199)
#   SHOTS_OUT     output directory (default web/shots/out)
#   SHOTS_SIZE    CSS pixels, WxH (default 1440x900)
#   SHOTS_SCALE   device scale factor (default 2)
#   SHOTS_THEMES  "light dark", or one of them (default both)
#   SHOTS_PAGES   page names to take, "timeline board" (default all)
#   BUSY=1        the stub serves a heavy day, for the timeline
#   SHOTS_NO_BUILD=1  use web/dist as it is
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
web=$(dirname "$here")
port=${SHOTS_PORT:-5199}
out=${SHOTS_OUT:-$here/out}
size=${SHOTS_SIZE:-1440x900}
scale=${SHOTS_SCALE:-2}
themes=${SHOTS_THEMES:-light dark}
w=${size%x*}
h=${size#*x}

chrome=${CHROME:-}
if [ -z "$chrome" ]; then
  for c in "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
    "/Applications/Chromium.app/Contents/MacOS/Chromium" \
    google-chrome google-chrome-stable chromium chromium-browser chrome; do
    if [ -x "$c" ] || command -v "$c" >/dev/null 2>&1; then chrome=$c; break; fi
  done
fi
[ -n "$chrome" ] || { echo "shots: no Chrome found; set CHROME to its path" >&2; exit 1; }

if [ -z "${SHOTS_NO_BUILD:-}" ] || [ ! -f "$web/dist/index.html" ]; then
  (cd "$web" && { [ -d node_modules ] || npm ci --no-audit --no-fund; } && npm run build)
fi

mkdir -p "$out"
STUB_STRICT=1 node "$here/stub-daemon.mjs" "$web/dist" "$port" >/dev/null &
stub=$!
trap 'kill $stub 2>/dev/null || true' EXIT
for _ in $(seq 1 50); do
  curl -sf "http://127.0.0.1:$port/v1/status" >/dev/null && break
  sleep 0.1
done

CHROME="$chrome" SHOTS_BASE="http://127.0.0.1:$port" SHOTS_OUT="$out" SHOTS_W="$w" SHOTS_H="$h" \
  SHOTS_SCALE="$scale" SHOTS_THEMES="$themes" SHOTS_PAGES="${SHOTS_PAGES:-}" node "$here/shoot.mjs"
