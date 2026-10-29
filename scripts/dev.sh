#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
config=${1:-configs/config.yaml}
backend_pid=''
frontend_pid=''

cleanup() {
  trap - EXIT INT TERM
  if [ -n "$frontend_pid" ]; then kill "$frontend_pid" 2>/dev/null || true; fi
  if [ -n "$backend_pid" ]; then kill "$backend_pid" 2>/dev/null || true; fi
  wait 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [ ! -x web/node_modules/.bin/vite ]; then
  printf '%s\n' '缺少前端依赖，请先运行 make install。' >&2
  exit 1
fi

./bin/intent-admin serve --config "$config" &
backend_pid=$!
(cd web && exec ./node_modules/.bin/vite --host 127.0.0.1 --strictPort) &
frontend_pid=$!

# Exit and clean up both children when either server stops.
while kill -0 "$backend_pid" 2>/dev/null && kill -0 "$frontend_pid" 2>/dev/null; do
  sleep 1
done
if kill -0 "$backend_pid" 2>/dev/null; then
  wait "$frontend_pid"
else
  wait "$backend_pid"
fi
