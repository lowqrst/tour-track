#!/usr/bin/env bash
set -euo pipefail

usage() {
    printf 'Usage: bash scripts/start-public.sh [port]\n'
}

if [[ ${1:-} == --help || ${1:-} == -h ]]; then
    usage
    exit 0
fi

port=${1:-8080}
if [[ $# -gt 1 || ! $port =~ ^[0-9]{1,5}$ ]] || (( 10#$port < 1 || 10#$port > 65535 )); then
    usage >&2
    printf 'Port must be an integer between 1 and 65535.\n' >&2
    exit 1
fi
port=$((10#$port))

for dependency in go curl cloudflared; do
    if ! command -v "$dependency" >/dev/null 2>&1; then
        printf 'Required command is missing: %s\n' "$dependency" >&2
        exit 1
    fi
done

project_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
run_dir="$project_root/.cache/public"
origin="http://127.0.0.1:$port"
server_pid=''
tunnel_pid=''

cleanup() {
    trap - EXIT INT TERM
    if [[ -n $tunnel_pid ]]; then
        kill "$tunnel_pid" 2>/dev/null || true
        wait "$tunnel_pid" 2>/dev/null || true
    fi
    if [[ -n $server_pid ]]; then
        kill "$server_pid" 2>/dev/null || true
        wait "$server_pid" 2>/dev/null || true
    fi
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if curl --noproxy '*' --silent --output /dev/null --max-time 1 "$origin/health"; then
    printf 'Port %s is already in use. Stop that server or choose another port.\n' "$port" >&2
    exit 1
fi

mkdir -p "$run_dir"
cd "$project_root"
go build -trimpath -o "$run_dir/tour-track" .

TOUR_TRACK_ADDR="127.0.0.1:$port" TOUR_TRACK_DEMO=0 \
    "$run_dir/tour-track" >"$run_dir/server.log" 2>&1 &
server_pid=$!

ready=false
for attempt in {1..30}; do
    if ! kill -0 "$server_pid" 2>/dev/null; then
        printf 'The server exited. Check %s/server.log.\n' "$run_dir" >&2
        exit 1
    fi
    if curl --noproxy '*' --fail --silent --output /dev/null --max-time 1 "$origin/health"; then
        ready=true
        break
    fi
    sleep 0.2
done
if [[ $ready != true ]]; then
    printf 'The server did not become ready. Check %s/server.log.\n' "$run_dir" >&2
    exit 1
fi

printf 'Local server: %s\n' "$origin"
printf 'Keep this terminal open. Ctrl+C stops the tunnel and server.\n'
# Use a project config rather than any personal named-tunnel configuration.
printf 'no-autoupdate: true\n' >"$run_dir/cloudflared.yml"
cloudflared tunnel --config "$run_dir/cloudflared.yml" --no-autoupdate \
    --protocol http2 --url "$origin" &
tunnel_pid=$!
wait "$tunnel_pid"
