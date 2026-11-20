#!/usr/bin/env bash
set -Eeuo pipefail

BASE_URL="http://127.0.0.1:9000"
WEBSOCKET_URL="ws://127.0.0.1:9000/api/v1/ws"
DIAGNOSTICS_URL="http://127.0.0.1:6060"
PUBLISH_BASE="rtmp://127.0.0.1:1935/live"
ORIGIN="http://localhost:5173"
CONNECTION_LEVELS="1000,1500,2000,2500,3000"
REPEATS=1
DURATION="2m"
DRAIN="30s"
RAMP="10ms"
WARMUP="2s"
SEND_INTERVAL="2s"
REPORT_INTERVAL="5s"
SETTLE="10s"
MAX_P95_LATENCY="500ms"
FFMPEG_BIN="ffmpeg"
REPORT_DIRECTORY=""
CONTINUE_ON_FAILURE=0
SKIP_BUILD=0
RUN_IDLE=1
RUN_ACTIVE=1

usage() {
  cat <<'EOF'
Usage: bash scripts/loopback-load-test.sh [options]

Runs a staged WebSocket load test entirely through 127.0.0.1. The script
registers a temporary owner, creates a room, starts a low-bitrate FFmpeg
publisher, then runs listener-only and one-sender stages at every connection
level.

Options:
  --connections LIST          Comma-separated levels (default: 1000,1500,2000,2500,3000)
  --repeats NUMBER            Repetitions per mode and level (default: 1)
  --duration DURATION         Message phase per stage (default: 2m)
  --drain DURATION            Drain time per stage (default: 30s)
  --ramp DURATION             Delay between connection attempts (default: 10ms)
  --warmup DURATION           Settling time after dialing (default: 2s)
  --send-interval DURATION    One-sender message interval (default: 2s)
  --report-interval DURATION  Benchmark progress interval (default: 5s)
  --settle DURATION           Pause between stages (default: 10s)
  --max-p95-latency DURATION  Active-stage limit; empty disables (default: 500ms)
  --modes LIST                idle, active, or idle,active (default: both)
  --continue-on-failure       Continue after a failed stage
  --skip-build                Reuse bin/ws-benchmark
  --ffmpeg PATH               FFmpeg executable (default: ffmpeg)
  --publish-base URL          Local SRS RTMP base (default: rtmp://127.0.0.1:1935/live)
  --base-url URL              Backend HTTP URL (default: http://127.0.0.1:9000)
  --websocket-url URL         Backend WebSocket URL
  --diagnostics-url URL       Diagnostics URL (default: http://127.0.0.1:6060)
  --origin ORIGIN             WebSocket Origin header
  --report-directory PATH     Result directory; default uses test-results/loopback-TIMESTAMP
  -h, --help

For a run that survives SSH or VSCode closing, use:
  bash scripts/start-loopback-load-test.sh [the same options]
EOF
}

need_value() {
  if [[ $# -lt 2 || -z ${2:-} ]]; then
    echo "Missing value for $1" >&2
    exit 2
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --connections) need_value "$@"; CONNECTION_LEVELS=$2; shift 2 ;;
    --repeats) need_value "$@"; REPEATS=$2; shift 2 ;;
    --duration) need_value "$@"; DURATION=$2; shift 2 ;;
    --drain) need_value "$@"; DRAIN=$2; shift 2 ;;
    --ramp) need_value "$@"; RAMP=$2; shift 2 ;;
    --warmup) need_value "$@"; WARMUP=$2; shift 2 ;;
    --send-interval) need_value "$@"; SEND_INTERVAL=$2; shift 2 ;;
    --report-interval) need_value "$@"; REPORT_INTERVAL=$2; shift 2 ;;
    --settle) need_value "$@"; SETTLE=$2; shift 2 ;;
    --max-p95-latency) need_value "$@"; MAX_P95_LATENCY=$2; shift 2 ;;
    --modes) need_value "$@"
      case ",$2," in
        *,idle,*) RUN_IDLE=1 ;; *) RUN_IDLE=0 ;;
      esac
      case ",$2," in
        *,active,*) RUN_ACTIVE=1 ;; *) RUN_ACTIVE=0 ;;
      esac
      if (( RUN_IDLE == 0 && RUN_ACTIVE == 0 )); then
        echo "--modes must contain idle and/or active" >&2
        exit 2
      fi
      shift 2
      ;;
    --continue-on-failure) CONTINUE_ON_FAILURE=1; shift ;;
    --skip-build) SKIP_BUILD=1; shift ;;
    --ffmpeg) need_value "$@"; FFMPEG_BIN=$2; shift 2 ;;
    --publish-base) need_value "$@"; PUBLISH_BASE=$2; shift 2 ;;
    --base-url) need_value "$@"; BASE_URL=${2%/}; shift 2 ;;
    --websocket-url) need_value "$@"; WEBSOCKET_URL=$2; shift 2 ;;
    --diagnostics-url) need_value "$@"; DIAGNOSTICS_URL=${2%/}; shift 2 ;;
    --origin) need_value "$@"; ORIGIN=$2; shift 2 ;;
    --report-directory) need_value "$@"; REPORT_DIRECTORY=$2; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ ! $REPEATS =~ ^[1-9][0-9]*$ ]]; then
  echo "--repeats must be a positive integer" >&2
  exit 2
fi

IFS=',' read -r -a LEVELS <<<"$CONNECTION_LEVELS"
if (( ${#LEVELS[@]} == 0 )); then
  echo "--connections must contain at least one level" >&2
  exit 2
fi
previous_level=0
for level in "${LEVELS[@]}"; do
  if [[ ! $level =~ ^[1-9][0-9]*$ ]]; then
    echo "Invalid connection level: $level" >&2
    exit 2
  fi
  if (( level <= previous_level )); then
    echo "Connection levels must be strictly increasing" >&2
    exit 2
  fi
  previous_level=$level
done

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
PROJECT_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
cd "$PROJECT_ROOT"

STAMP=$(date +%Y%m%d-%H%M%S)
if [[ -z $REPORT_DIRECTORY ]]; then
  REPORT_DIR="$PROJECT_ROOT/test-results/loopback-$STAMP"
elif [[ $REPORT_DIRECTORY = /* ]]; then
  REPORT_DIR=$REPORT_DIRECTORY
else
  REPORT_DIR="$PROJECT_ROOT/$REPORT_DIRECTORY"
fi
mkdir -p "$REPORT_DIR"
exec > >(tee -a "$REPORT_DIR/run.log") 2>&1

TMP_DIR=$(mktemp -d)
COOKIE_JAR="$TMP_DIR/owner.cookies"
BENCHMARK_BIN="$PROJECT_ROOT/bin/ws-benchmark"
SUMMARY_FILE="$REPORT_DIR/summary.tsv"
ROOM_ID=0
ROOM_STARTED=0
PUBLISHER_PID=0
BENCHMARK_PID=0
MONITOR_PID=0
OUTPUT_PID=0
SUITE_FAILED=0
API_STATUS=0
API_BODY=""

step() { printf '\n\033[36m==> %s\033[0m\n' "$1"; }

cleanup() {
  local exit_code=$?
  trap - EXIT
  set +e
  if (( BENCHMARK_PID > 0 )); then
    kill "$BENCHMARK_PID" 2>/dev/null
    wait "$BENCHMARK_PID" 2>/dev/null
  fi
  if (( MONITOR_PID > 0 )); then
    kill "$MONITOR_PID" 2>/dev/null
    wait "$MONITOR_PID" 2>/dev/null
  fi
  if (( OUTPUT_PID > 0 )); then
    kill "$OUTPUT_PID" 2>/dev/null
    wait "$OUTPUT_PID" 2>/dev/null
  fi
  if (( ROOM_STARTED == 1 && ROOM_ID > 0 )); then
    curl --silent --show-error --max-time 10 --request POST \
      --cookie "$COOKIE_JAR" --cookie-jar "$COOKIE_JAR" \
      "$BASE_URL/api/v1/rooms/$ROOM_ID/stop" >/dev/null
  fi
  if (( PUBLISHER_PID > 0 )); then
    kill "$PUBLISHER_PID" 2>/dev/null
    wait "$PUBLISHER_PID" 2>/dev/null
  fi
  rm -rf -- "$TMP_DIR"
  printf '\nResults: %s\n' "$REPORT_DIR"
  exit "$exit_code"
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

die() {
  echo "ERROR: $1" >&2
  exit 1
}

for command_name in curl python3 go "$FFMPEG_BIN"; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command not found: $command_name"
done

api_request() {
  local method=$1 path=$2 body=${3:-} cookie=${4:-}
  local response_file http_code curl_exit
  response_file=$(mktemp "$TMP_DIR/api.XXXXXX")
  local -a args=(--silent --show-error --connect-timeout 5 --max-time 30
    --request "$method" --output "$response_file" --write-out '%{http_code}')
  if [[ -n $cookie ]]; then
    args+=(--cookie "$cookie" --cookie-jar "$cookie")
  fi
  if [[ -n $body ]]; then
    args+=(--header 'Content-Type: application/json' --data "$body")
  fi
  set +e
  http_code=$(curl "${args[@]}" "$BASE_URL$path")
  curl_exit=$?
  set -e
  API_BODY=$(<"$response_file")
  rm -f -- "$response_file"
  (( curl_exit == 0 )) || die "curl failed for $method $path with exit code $curl_exit"
  API_STATUS=$http_code
}

expect_status() {
  local expected=$1 action=$2
  if [[ $API_STATUS != "$expected" ]]; then
    die "$action failed: status=$API_STATUS expected=$expected body=$API_BODY"
  fi
}

json_get() {
  local json=$1 path=$2
  python3 -c '
import json, sys
value = json.load(sys.stdin)
for part in sys.argv[1].split("."):
    if part:
        value = value[int(part)] if isinstance(value, list) else value[part]
print("" if value is None else value)
' "$path" <<<"$json"
}

duration_seconds() {
  python3 - "$1" <<'PY'
import re, sys
text = sys.argv[1]
units = {"ns": 1e-9, "us": 1e-6, "µs": 1e-6, "ms": 1e-3, "s": 1, "m": 60, "h": 3600}
parts = re.findall(r"([0-9]+(?:\.[0-9]+)?)(ns|us|µs|ms|s|m|h)", text)
if not parts or "".join(value + unit for value, unit in parts) != text:
    raise SystemExit(f"invalid duration: {text}")
print(sum(float(value) * units[unit] for value, unit in parts))
PY
}

validate_final() {
  local log_file=$1 mode=$2 connections=$3 repeat=$4 result_file=$5
  python3 - "$log_file" "$mode" "$connections" "$repeat" "$MAX_P95_LATENCY" "$result_file" <<'PY'
import json, math, re, sys

log_path, mode, connections, repeat, max_p95, result_path = sys.argv[1:]
lines = open(log_path, encoding="utf-8", errors="replace").read().splitlines()
finals = [line for line in lines if "[FINAL]" in line]
fields = {}
reasons = []
if not finals:
    reasons.append("missing [FINAL] line")
else:
    fields = dict(re.findall(r"([a-zA-Z0-9_]+)=([^\s]+)", finals[-1]))

def number(name):
    try:
        return int(fields[name])
    except (KeyError, ValueError):
        reasons.append(f"missing or invalid {name}")
        return 0

def duration_seconds(text):
    units = {"ns": 1e-9, "us": 1e-6, "µs": 1e-6, "ms": 1e-3, "s": 1, "m": 60, "h": 3600}
    parts = re.findall(r"([0-9]+(?:\.[0-9]+)?)(ns|us|µs|ms|s|m|h)", text)
    if not parts or "".join(value + unit for value, unit in parts) != text:
        raise ValueError(text)
    return sum(float(value) * units[unit] for value, unit in parts)

if fields:
    attempted = number("dial_attempted")
    succeeded = number("dial_succeeded")
    failed = number("dial_failed")
    errors = number("errors")
    unexpected = number("unexpected_disconnects")
    read_failed = number("read_failed")
    if attempted != int(connections) or succeeded != attempted or failed != 0:
        reasons.append(f"dial {succeeded}/{attempted}, failed={failed}")
    if errors != 0:
        reasons.append(f"errors={errors}")
    if unexpected != 0 or read_failed != 0:
        reasons.append(f"unexpected_disconnects={unexpected}, read_failed={read_failed}")
    if mode == "active":
        sent = number("sent")
        accepted = number("accepted")
        rejected = number("rejected")
        realtime = number("realtime_received")
        received = number("received")
        expected = number("expected")
        if sent <= 0 or accepted != sent or rejected != 0:
            reasons.append(f"sent/accepted/rejected={sent}/{accepted}/{rejected}")
        if realtime != expected or received != expected:
            reasons.append(f"deliveries realtime/final/expected={realtime}/{received}/{expected}")
        if max_p95:
            try:
                measured = duration_seconds(fields["realtime_latency_p95"])
                limit = duration_seconds(max_p95)
                if measured > limit:
                    reasons.append(f"realtime P95 {fields['realtime_latency_p95']} > {max_p95}")
            except (KeyError, ValueError):
                reasons.append("missing or invalid realtime_latency_p95")

result = {
    "passed": not reasons,
    "mode": mode,
    "connections": int(connections),
    "repeat": int(repeat),
    "reasons": reasons,
    "final": finals[-1] if finals else "",
    "fields": fields,
}
with open(result_path, "w", encoding="utf-8") as output:
    json.dump(result, output, ensure_ascii=False, indent=2)

status = "PASS" if not reasons else "FAIL"
reason = "; ".join(reasons)
print("\t".join([
    str(connections), str(repeat), mode, status,
    fields.get("dial_succeeded", "0"), fields.get("dial_attempted", "0"),
    fields.get("sent", "0"), fields.get("received", "0"), fields.get("expected", "0"),
    fields.get("realtime_latency_p95", "0s"), fields.get("realtime_latency_p99", "0s"),
    reason,
]))
raise SystemExit(0 if not reasons else 1)
PY
}

run_stage() {
  local connections=$1 mode=$2 repeat=$3 active_clients=0
  [[ $mode == active ]] && active_clients=1
  local stage="${connections}-${mode}-r${repeat}"
  local log_file="$REPORT_DIR/$stage.log"
  local resource_file="$REPORT_DIR/$stage-resources.log"
  local result_file="$REPORT_DIR/$stage-result.json"
  local metrics_before="$REPORT_DIR/$stage-ws-before.json"
  local metrics_after="$REPORT_DIR/$stage-ws-after.json"
  local output_fifo="$TMP_DIR/$stage.output"

  step "Stage $stage"
  curl --silent --show-error --max-time 10 "$DIAGNOSTICS_URL/debug/ws/metrics" >"$metrics_before"

  local -a args=(
    -urls "$WEBSOCKET_URL"
    -replay-base-url "$BASE_URL"
    -origin "$ORIGIN"
    -room "$ROOM_ID"
    -c "$connections"
    -active-clients "$active_clients"
    -r "$SEND_INTERVAL"
    -ramp "$RAMP"
    -warmup "$WARMUP"
    -d "$DURATION"
    -drain "$DRAIN"
    -report "$REPORT_INTERVAL"
  )
  if (( active_clients > 0 )); then
    args+=(-sid "$OWNER_SID")
  fi

  mkfifo "$output_fifo"
  tee "$log_file" <"$output_fifo" &
  OUTPUT_PID=$!
  "$BENCHMARK_BIN" "${args[@]}" >"$output_fifo" 2>&1 &
  BENCHMARK_PID=$!
  if command -v pidstat >/dev/null 2>&1; then
    pidstat -p "$BACKEND_PID,$BENCHMARK_PID" -u -r -w 1 >"$resource_file" 2>&1 &
    MONITOR_PID=$!
  fi

  set +e
  wait "$BENCHMARK_PID"
  local benchmark_exit=$?
  set -e
  BENCHMARK_PID=0
  wait "$OUTPUT_PID"
  OUTPUT_PID=0
  rm -f -- "$output_fifo"
  if (( MONITOR_PID > 0 )); then
    kill "$MONITOR_PID" 2>/dev/null || true
    wait "$MONITOR_PID" 2>/dev/null || true
    MONITOR_PID=0
  fi
  curl --silent --show-error --max-time 10 "$DIAGNOSTICS_URL/debug/ws/metrics" >"$metrics_after"

  local validation_line validation_exit
  set +e
  validation_line=$(validate_final "$log_file" "$mode" "$connections" "$repeat" "$result_file")
  validation_exit=$?
  set -e
  printf '%s\n' "$validation_line" | tee -a "$SUMMARY_FILE"
  if (( benchmark_exit != 0 )); then
    echo "Benchmark process exited with code $benchmark_exit" >&2
    validation_exit=1
  fi
  return "$validation_exit"
}

step "Checking local prerequisites"
curl --silent --show-error --fail --max-time 5 "$BASE_URL/api/v1/rooms" >/dev/null \
  || die "backend is not reachable at $BASE_URL"
curl --silent --show-error --fail --max-time 5 "$DIAGNOSTICS_URL/debug/ws/metrics" >/dev/null \
  || die "diagnostics server is not reachable at $DIAGNOSTICS_URL"

BACKEND_PID=$(pgrep -f '/bin/live-stream-server' | head -n 1 || true)
[[ $BACKEND_PID =~ ^[1-9][0-9]*$ ]] || die "unable to find bin/live-stream-server process"

required_fds=$(( LEVELS[${#LEVELS[@]}-1] + 4096 ))
soft_fds=$(ulimit -Sn)
if [[ $soft_fds != unlimited && $soft_fds =~ ^[0-9]+$ && $soft_fds -lt $required_fds ]]; then
  hard_fds=$(ulimit -Hn)
  if [[ $hard_fds == unlimited || $hard_fds -ge $required_fds ]]; then
    ulimit -Sn "$required_fds"
  else
    die "load generator open-file limit is $soft_fds/$hard_fds; need at least $required_fds"
  fi
fi
backend_fds=$(awk '/Max open files/ {print $4}' "/proc/$BACKEND_PID/limits")
if [[ $backend_fds =~ ^[0-9]+$ && $backend_fds -lt $required_fds ]]; then
  die "backend open-file limit is $backend_fds; restart it with at least $required_fds"
fi

if (( SKIP_BUILD == 0 )); then
  step "Building benchmark binary"
  mkdir -p "$(dirname "$BENCHMARK_BIN")"
  go test ./cmd/benchmark ./internal/ws
  go build -o "$BENCHMARK_BIN" ./cmd/benchmark
elif [[ ! -x $BENCHMARK_BIN ]]; then
  die "--skip-build was supplied but $BENCHMARK_BIN does not exist"
fi

step "Creating temporary owner and room"
USER_STAMP="$(date +%Y%m%d%H%M%S)-$$"
OWNER_NAME="loopback${USER_STAMP//-/}"
OWNER_EMAIL="$OWNER_NAME@example.com"
PASSWORD='LoopbackBenchmark123!'

api_request POST /api/v1/auth/register \
  "{\"username\":\"$OWNER_NAME\",\"email\":\"$OWNER_EMAIL\",\"password\":\"$PASSWORD\"}"
expect_status 200 "registration"

api_request POST /api/v1/auth/login \
  "{\"email\":\"$OWNER_EMAIL\",\"password\":\"$PASSWORD\"}" "$COOKIE_JAR"
expect_status 200 "login"
OWNER_SID=$(awk '$6 == "sid" {value=$7} END {print value}' "$COOKIE_JAR")
[[ $OWNER_SID =~ ^[0-9a-fA-F]{64}$ ]] || die "login did not return a valid sid"

api_request POST /api/v1/rooms \
  "{\"title\":\"Loopback Benchmark $USER_STAMP\"}" "$COOKIE_JAR"
expect_status 200 "room creation"
ROOM_ID=$(json_get "$API_BODY" data.room_id)
[[ $ROOM_ID =~ ^[1-9][0-9]*$ ]] || die "invalid room_id: $ROOM_ID"

api_request POST "/api/v1/rooms/$ROOM_ID/start" \
  "{\"room_title\":\"Loopback Benchmark $USER_STAMP\"}" "$COOKIE_JAR"
expect_status 200 "room start"
ROOM_STARTED=1
STREAM_KEY=$(json_get "$API_BODY" data.stream_key)
[[ -n $STREAM_KEY ]] || die "room start did not return stream_key"

step "Starting low-bitrate local publisher"
"$FFMPEG_BIN" -nostdin -hide_banner -loglevel warning -re \
  -f lavfi -i color=c=black:size=160x90:rate=5 \
  -re -f lavfi -i anullsrc=r=32000:cl=mono \
  -c:v libx264 -preset ultrafast -tune zerolatency -pix_fmt yuv420p \
  -g 10 -b:v 50k -maxrate 60k -bufsize 120k \
  -c:a aac -b:a 16k -f flv "${PUBLISH_BASE%/}/$STREAM_KEY" \
  >"$REPORT_DIR/publisher.log" 2>&1 &
PUBLISHER_PID=$!

live_deadline=$(( $(date +%s) + 60 ))
while (( $(date +%s) < live_deadline )); do
  if ! kill -0 "$PUBLISHER_PID" 2>/dev/null; then
    die "FFmpeg publisher exited early; see $REPORT_DIR/publisher.log"
  fi
  api_request GET "/api/v1/rooms/$ROOM_ID"
  if [[ $API_STATUS == 200 && $(json_get "$API_BODY" data.status) == 1 ]]; then
    break
  fi
  sleep 1
done
api_request GET "/api/v1/rooms/$ROOM_ID"
[[ $API_STATUS == 200 && $(json_get "$API_BODY" data.status) == 1 ]] \
  || die "room did not become live within 60 seconds"

printf 'connections\trepeat\tmode\tstatus\tdial_succeeded\tdial_attempted\tsent\treceived\texpected\tp95\tp99\treason\n' \
  >"$SUMMARY_FILE"

step "Starting staged loopback test"
printf 'Room: %s\nLevels: %s\nRepeats: %s\nResults: %s\n' \
  "$ROOM_ID" "$CONNECTION_LEVELS" "$REPEATS" "$REPORT_DIR"

for connections in "${LEVELS[@]}"; do
  for (( repeat=1; repeat<=REPEATS; repeat++ )); do
    if (( RUN_IDLE == 1 )); then
      if ! run_stage "$connections" idle "$repeat"; then
        SUITE_FAILED=1
        if (( CONTINUE_ON_FAILURE == 0 )); then
          echo "Stopping at first failed stage: ${connections}-idle-r${repeat}" >&2
          exit 1
        fi
      fi
      sleep "$(duration_seconds "$SETTLE")"
    fi
    if (( RUN_ACTIVE == 1 )); then
      if ! run_stage "$connections" active "$repeat"; then
        SUITE_FAILED=1
        if (( CONTINUE_ON_FAILURE == 0 )); then
          echo "Stopping at first failed stage: ${connections}-active-r${repeat}" >&2
          exit 1
        fi
      fi
      sleep "$(duration_seconds "$SETTLE")"
    fi
  done
done

if (( SUITE_FAILED == 1 )); then
  echo "STAGED LOOPBACK LOAD TEST COMPLETED WITH FAILURES" >&2
  exit 1
fi

printf '\n\033[32mSTAGED LOOPBACK LOAD TEST PASSED\033[0m\n'
column -t -s $'\t' "$SUMMARY_FILE" 2>/dev/null || cat "$SUMMARY_FILE"
