#!/usr/bin/env bash

set -Eeuo pipefail

BASE_URL="http://127.0.0.1:9000"
WEBSOCKET_URL="ws://127.0.0.1:9000/api/v1/ws"
ORIGIN="http://localhost:5173"
PROFILE="Smoke"
MAX_LOSS_PERCENT="0"
MAX_P95_LATENCY=""
LIKE_RATE_LIMIT=10
SKIP_CODE_CHECKS=0
SKIP_FRONTEND_CHECKS=0
SKIP_HISTORY_CHECKS=0
FAIL_ON_KNOWN_ISSUES=0
KAFKA_BROKERS=""
KAFKA_TOPIC="danmu_topic"
KAFKA_MESSAGES=100000
KAFKA_WORKERS=8
DIAGNOSTICS_BASE_URL=""
REPORT_DIRECTORY="test-results"

usage() {
  cat <<'EOF'
Usage: ./scripts/full-test.sh [options]

Linux-native full functional and load test for live-stream.

Options:
  --base-url URL                    Default: http://127.0.0.1:9000
  --websocket-url URL               Default: ws://127.0.0.1:9000/api/v1/ws
  --origin ORIGIN                   Default: http://localhost:5173
  --profile NAME                    Functional|Smoke|Standard|Stress|Extreme
  --max-loss-percent NUMBER         Default: 0
  --max-p95-latency DURATION        Example: 500ms; empty disables threshold
  --like-rate-limit NUMBER          Must match server config; 0 skips check
  --skip-code-checks
  --skip-frontend-checks
  --skip-history-checks
  --fail-on-known-issues
  --kafka-brokers LIST              Example: 127.0.0.1:9092; empty skips
  --kafka-topic TOPIC               Default: danmu_topic
  --kafka-messages NUMBER           Default: 100000
  --kafka-workers NUMBER            Default: 8
  --diagnostics-base-url URL         Example: http://127.0.0.1:6060
  --report-directory PATH           Default: test-results
  -h, --help
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
    --base-url) need_value "$@"; BASE_URL=$2; shift 2 ;;
    --websocket-url) need_value "$@"; WEBSOCKET_URL=$2; shift 2 ;;
    --origin) need_value "$@"; ORIGIN=$2; shift 2 ;;
    --profile) need_value "$@"; PROFILE=$2; shift 2 ;;
    --max-loss-percent) need_value "$@"; MAX_LOSS_PERCENT=$2; shift 2 ;;
    --max-p95-latency) need_value "$@"; MAX_P95_LATENCY=$2; shift 2 ;;
    --like-rate-limit) need_value "$@"; LIKE_RATE_LIMIT=$2; shift 2 ;;
    --skip-code-checks) SKIP_CODE_CHECKS=1; shift ;;
    --skip-frontend-checks) SKIP_FRONTEND_CHECKS=1; shift ;;
    --skip-history-checks) SKIP_HISTORY_CHECKS=1; shift ;;
    --fail-on-known-issues) FAIL_ON_KNOWN_ISSUES=1; shift ;;
    --kafka-brokers) need_value "$@"; KAFKA_BROKERS=$2; shift 2 ;;
    --kafka-topic) need_value "$@"; KAFKA_TOPIC=$2; shift 2 ;;
    --kafka-messages) need_value "$@"; KAFKA_MESSAGES=$2; shift 2 ;;
    --kafka-workers) need_value "$@"; KAFKA_WORKERS=$2; shift 2 ;;
    --diagnostics-base-url) need_value "$@"; DIAGNOSTICS_BASE_URL=$2; shift 2 ;;
    --report-directory) need_value "$@"; REPORT_DIRECTORY=$2; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

case "$PROFILE" in
  Functional|Smoke|Standard|Stress|Extreme) ;;
  *) echo "Invalid --profile: $PROFILE" >&2; exit 2 ;;
esac

for value in "$BASE_URL" "$WEBSOCKET_URL" "$ORIGIN" "$DIAGNOSTICS_BASE_URL"; do
  if [[ $value == *'['* || $value == *']'* ]]; then
    echo "Invalid URL value: $value" >&2
    echo "Use a plain URL, not Markdown such as [http://...](http://...)." >&2
    exit 2
  fi
done

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
PROJECT_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
cd "$PROJECT_ROOT"

for command_name in bash curl python3 go; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "Required command not found: $command_name" >&2
    exit 2
  fi
done

TMP_DIR=$(mktemp -d)
OWNER_COOKIE_JAR="$TMP_DIR/owner.cookies"
OTHER_COOKIE_JAR="$TMP_DIR/other.cookies"
CHECK_FILE="$TMP_DIR/checks.tsv"
: >"$CHECK_FILE"
STARTED_AT=$(date --iso-8601=seconds)
STARTED_EPOCH=$(date +%s)
STAMP=$(date +%Y%m%d-%H%M%S)
ROOM_ID=0
ROOM_STARTED=0
SUITE_PASSED=0
KNOWN_ISSUES=0
API_STATUS=0
API_BODY=""

if [[ $REPORT_DIRECTORY = /* ]]; then
  REPORT_DIR=$REPORT_DIRECTORY
else
  REPORT_DIR="$PROJECT_ROOT/$REPORT_DIRECTORY"
fi
mkdir -p "$REPORT_DIR"
MARKDOWN_REPORT="$REPORT_DIR/full-test-linux-$STAMP.md"
JSON_REPORT="$REPORT_DIR/full-test-linux-$STAMP.json"

sanitize_detail() {
  local value=${1:-}
  value=${value//$'\t'/ }
  value=${value//$'\r'/ }
  value=${value//$'\n'/ }
  printf '%s' "$value"
}

record_check() {
  local status=$1 name=$2 detail=${3:-}
  detail=$(sanitize_detail "$detail")
  printf '%s\t%s\t%s\n' "$status" "$name" "$detail" >>"$CHECK_FILE"
  case "$status" in
    PASS) printf '\033[32m[%s]\033[0m %s%s\n' "$status" "$name" "${detail:+: $detail}" ;;
    WARN|SKIP) printf '\033[33m[%s]\033[0m %s%s\n' "$status" "$name" "${detail:+: $detail}" ;;
    *) printf '\033[31m[%s]\033[0m %s%s\n' "$status" "$name" "${detail:+: $detail}" ;;
  esac
}

step() {
  printf '\n\033[36m==> %s\033[0m\n' "$1"
}

pass() { record_check PASS "$1" "${2:-}"; }
skip() { record_check SKIP "$1" "${2:-}"; }

known_issue() {
  KNOWN_ISSUES=$((KNOWN_ISSUES + 1))
  record_check WARN "$1" "$2"
  if (( FAIL_ON_KNOWN_ISSUES )); then
    echo "Known issue is fatal because --fail-on-known-issues was supplied." >&2
    exit 1
  fi
}

die() {
  record_check FAIL "Suite execution" "$1"
  echo "$1" >&2
  exit 1
}

assert_true() {
  local name=$1 detail=${2:-}
  shift 2 || true
  if "$@"; then
    pass "$name" "$detail"
  else
    die "$name failed${detail:+: $detail}"
  fi
}

api_request() {
  local method=$1 url=$2 body=${3:-} cookie_jar=${4:-}
  local response_file curl_status
  response_file=$(mktemp "$TMP_DIR/api.XXXXXX")
  local -a args=(--silent --show-error --connect-timeout 5 --max-time 30
    --request "$method" --output "$response_file" --write-out '%{http_code}')
  if [[ -n $cookie_jar ]]; then
    args+=(--cookie "$cookie_jar" --cookie-jar "$cookie_jar")
  fi
  if [[ -n $body ]]; then
    args+=(--header 'Content-Type: application/json' --data "$body")
  fi
  set +e
  curl_status=$(curl "${args[@]}" "$url")
  local exit_code=$?
  set -e
  API_BODY=$(<"$response_file")
  rm -f -- "$response_file"
  if (( exit_code != 0 )); then
    die "curl failed for $method $url with exit code $exit_code"
  fi
  API_STATUS=$curl_status
}

assert_status() {
  local name=$1 expected=$2
  if [[ $API_STATUS == "$expected" ]]; then
    pass "$name" "status=$API_STATUS"
  else
    die "$name: status=$API_STATUS expected=$expected body=$API_BODY"
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
if isinstance(value, bool):
    print("true" if value else "false")
elif value is None:
    print("")
else:
    print(value)
' "$path" <<<"$json"
}

json_test() {
  local json=$1 expression=$2
  JSON_INPUT=$json python3 -c '
import json, os, sys
data = json.loads(os.environ["JSON_INPUT"])
sys.exit(0 if eval(sys.argv[1], {"__builtins__": {}}, {"data": data, "len": len, "int": int, "str": str}) else 1)
' "$expression"
}

extract_sid() {
  awk -F '\t' '$6 == "sid" { value=$7 } END { print value }' "$1"
}

metric_value() {
  local line=$1 key=$2 token
  token=$(grep -oE "(^|[[:space:]])${key}=[^[:space:]]+" <<<"$line" | tail -n 1 || true)
  printf '%s' "${token#*=}"
}

duration_ms() {
  python3 - "$1" <<'PY'
import re, sys
text = sys.argv[1]
parts = list(re.finditer(r'(\d+(?:\.\d+)?)(ms|us|µs|ns|h|m|s)', text))
if not parts or ''.join(m.group(0) for m in parts) != text:
    raise SystemExit(2)
scale = {'h': 3600000, 'm': 60000, 's': 1000, 'ms': 1, 'us': .001, 'µs': .001, 'ns': .000001}
print(sum(float(m.group(1)) * scale[m.group(2)] for m in parts))
PY
}

write_report() {
  local exit_code=$1 finished_at finished_epoch duration result
  finished_at=$(date --iso-8601=seconds)
  finished_epoch=$(date +%s)
  duration=$((finished_epoch - STARTED_EPOCH))
  result=FAIL
  if (( SUITE_PASSED == 1 && exit_code == 0 )); then result=PASS; fi

  {
    echo '# live-stream Linux automated test report'
    echo
    echo "- Result: **$result**"
    echo "- Started: $STARTED_AT"
    echo "- Finished: $finished_at"
    echo "- Duration: ${duration}s"
    echo "- Target: $BASE_URL"
    echo "- Profile: $PROFILE"
    echo
    echo '## Checks'
    echo
    echo '| Status | Check | Detail |'
    echo '|---|---|---|'
    while IFS=$'\t' read -r status name detail; do
      detail=${detail//|/\\|}
      echo "| $status | $name | $detail |"
    done <"$CHECK_FILE"
  } >"$MARKDOWN_REPORT"

  python3 - "$CHECK_FILE" "$JSON_REPORT" "$STARTED_AT" "$finished_at" "$duration" \
    "$BASE_URL" "$WEBSOCKET_URL" "$PROFILE" "$result" "$KNOWN_ISSUES" <<'PY'
import json, sys
check_path, output_path = sys.argv[1], sys.argv[2]
checks = []
with open(check_path, encoding='utf-8') as source:
    for line in source:
        parts = line.rstrip('\n').split('\t', 2)
        parts += [''] * (3 - len(parts))
        checks.append({'status': parts[0], 'name': parts[1], 'detail': parts[2]})
report = {
    'started_at': sys.argv[3], 'finished_at': sys.argv[4],
    'duration_sec': int(sys.argv[5]), 'base_url': sys.argv[6],
    'websocket_url': sys.argv[7], 'profile': sys.argv[8],
    'passed': sys.argv[9] == 'PASS', 'known_issue_count': int(sys.argv[10]),
    'checks': checks,
}
with open(output_path, 'w', encoding='utf-8') as target:
    json.dump(report, target, ensure_ascii=False, indent=2)
PY
  printf '\nReports:\n  %s\n  %s\n' "$MARKDOWN_REPORT" "$JSON_REPORT"
}

cleanup() {
  local exit_code=$?
  set +e
  if (( ROOM_STARTED == 1 && ROOM_ID > 0 )) && [[ -s $OWNER_COOKIE_JAR ]]; then
    curl --silent --show-error --max-time 10 --request POST \
      --cookie "$OWNER_COOKIE_JAR" --cookie-jar "$OWNER_COOKIE_JAR" \
      "$BASE_URL/api/v1/rooms/$ROOM_ID/stop" >/dev/null
  fi
  write_report "$exit_code"
  rm -rf -- "$TMP_DIR"
  exit "$exit_code"
}
trap cleanup EXIT

run_go_helper() {
  local output_file=$1
  shift
  set +e
  go run ./cmd/e2e "$@" 2>&1 | tee "$output_file"
  local exit_code=${PIPESTATUS[0]}
  set -e
  if (( exit_code != 0 )); then
    die "cmd/e2e exited with code $exit_code"
  fi
}

test_handshake_status() {
  local expected=$1 name=$2 output_file="$TMP_DIR/handshake-$expected.log"
  run_go_helper "$output_file" \
    -ws-url "$WEBSOCKET_URL" -origin "$ORIGIN" -room "$ROOM_ID" \
    -expect-handshake-status "$expected" -timeout 10s
  if grep -q "\[E2E_HANDSHAKE\] status=$expected" "$output_file"; then
    pass "$name" "handshake HTTP $expected"
  else
    die "$name did not observe HTTP $expected"
  fi
}

diagnostics_snapshot() {
  local phase=$1
  [[ -z $DIAGNOSTICS_BASE_URL ]] && return 0
  local path
  for path in /debug/ws/metrics /debug/kafka/realtime; do
    api_request GET "${DIAGNOSTICS_BASE_URL%/}$path"
    if [[ $API_STATUS == 200 ]]; then
      pass "Diagnostics $phase $path" "$API_BODY"
    else
      record_check WARN "Diagnostics $phase $path" "status=$API_STATUS"
    fi
  done
}

wait_for_protocol_history() {
  local ids_json=$1 attempt
  for attempt in $(seq 1 40); do
    api_request GET "$BASE_URL/api/v1/rooms/$ROOM_ID/messages?limit=100"
    if [[ $API_STATUS == 200 ]] && IDS_JSON=$ids_json JSON_INPUT=$API_BODY python3 -c '
import json, os, sys
ids = set(json.loads(os.environ["IDS_JSON"]))
body = json.loads(os.environ["JSON_INPUT"])
found = {str(item.get("message_id")) for item in body["data"]["items"]}
sys.exit(0 if ids <= found else 1)
'; then
      HISTORY_BODY=$API_BODY
      return 0
    fi
    sleep 0.5
  done
  die "Timed out waiting for protocol messages in MySQL history"
}

check_benchmark_history() {
  local attempt
  (( SKIP_HISTORY_CHECKS == 1 )) && return 0
  for attempt in $(seq 1 20); do
    api_request GET "$BASE_URL/api/v1/rooms/$ROOM_ID/messages?limit=100"
    if [[ $API_STATUS == 200 ]] && JSON_INPUT=$API_BODY python3 -c '
import json, os, sys
body = json.loads(os.environ["JSON_INPUT"])
sys.exit(0 if any(str(item.get("content", "")).startswith("bench-") for item in body["data"]["items"]) else 1)
'; then
      return 0
    fi
    sleep 1
  done
  die "No bench-* message appeared in history"
}

run_load_stage() {
  local name=$1 connections=$2 active=$3 duration=$4 ramp=$5 drain=$6
  local output_file="$TMP_DIR/load-$name.log"
  step "Load stage $name: $connections connections, $active senders"
  set +e
  go run ./cmd/benchmark \
    -urls "$WEBSOCKET_URL" -origin "$ORIGIN" -room "$ROOM_ID" -sid "$OWNER_SID" \
    -c "$connections" -active-clients "$active" -r 1s -ramp "$ramp" \
    -warmup 3s -d "$duration" -drain "$drain" -ack-every 0 \
    -replay-base-url "$BASE_URL" 2>&1 | tee "$output_file"
  local benchmark_exit=${PIPESTATUS[0]}
  set -e
  (( benchmark_exit == 0 )) || die "Load stage $name exited with code $benchmark_exit"

  local final_line sent received expected errors peak attempted succeeded failed p95 missing loss
  final_line=$(grep '\[FINAL\]' "$output_file" | tail -n 1 || true)
  [[ -n $final_line ]] || die "Load stage $name did not print [FINAL]"
  sent=$(metric_value "$final_line" sent)
  received=$(metric_value "$final_line" received)
  expected=$(metric_value "$final_line" expected)
  errors=$(metric_value "$final_line" errors)
  peak=$(metric_value "$final_line" peak_conns)
  attempted=$(metric_value "$final_line" dial_attempted)
  succeeded=$(metric_value "$final_line" dial_succeeded)
  failed=$(metric_value "$final_line" dial_failed)
  p95=$(metric_value "$final_line" latency_p95)
  for value in "$sent" "$received" "$expected" "$errors" "$peak" "$attempted" "$succeeded" "$failed" "$p95"; do
    [[ -n $value ]] || die "Unable to parse load metrics: $final_line"
  done
  (( errors == 0 )) || die "Load stage $name reported $errors errors"
  (( attempted == connections && succeeded == connections && failed == 0 && peak == connections )) || \
    die "Connection target missed: attempted=$attempted succeeded=$succeeded failed=$failed peak=$peak target=$connections"
  (( active == 0 || sent > 0 )) || die "Load stage $name sent no messages"
  (( received <= expected )) || die "Duplicate deliveries: received=$received expected=$expected"
  missing=$((expected - received))
  loss=$(python3 - "$missing" "$expected" <<'PY'
import sys
missing, expected = int(sys.argv[1]), int(sys.argv[2])
print(0 if expected == 0 else missing * 100.0 / expected)
PY
)
  python3 - "$loss" "$MAX_LOSS_PERCENT" <<'PY' || die "Loss $loss% exceeded limit $MAX_LOSS_PERCENT%"
import sys
raise SystemExit(0 if float(sys.argv[1]) <= float(sys.argv[2]) else 1)
PY
  if [[ -n $MAX_P95_LATENCY && $active -gt 0 ]]; then
    local measured_ms limit_ms
    measured_ms=$(duration_ms "$p95")
    limit_ms=$(duration_ms "$MAX_P95_LATENCY")
    python3 - "$measured_ms" "$limit_ms" <<'PY' || die "P95 $p95 exceeded limit $MAX_P95_LATENCY"
import sys
raise SystemExit(0 if float(sys.argv[1]) <= float(sys.argv[2]) else 1)
PY
  fi
  check_benchmark_history
  pass "Load stage $name" "connections=$connections sent=$sent received=$received loss=$loss% p95=$p95"
}

step "Checking prerequisites and source"
if (( SKIP_CODE_CHECKS == 0 )); then
  go test ./...
  pass "go test ./..."
  go vet ./...
  pass "go vet ./..."
else
  skip "Go tests and vet" "--skip-code-checks"
fi

if (( SKIP_FRONTEND_CHECKS == 1 )); then
  skip "Frontend lint and build" "--skip-frontend-checks"
elif [[ -f frontend/package.json ]]; then
  if command -v npm >/dev/null 2>&1; then
    (
      cd frontend
      if [[ ! -d node_modules ]]; then npm ci; fi
      npm run lint
      npm run build
    )
    pass "Frontend lint and build"
  else
    skip "Frontend lint and build" "npm is not installed"
  fi
else
  skip "Frontend lint and build" "frontend/package.json not found"
fi

step "Auditing like consistency implementation"
if grep -q 'repo.ApplyLikeEvent(ctx, event)' internal/kafka/realtime_consumer.go \
  && grep -q 'OnConflict{DoNothing: true}' internal/repo/like.go \
  && grep -q 'primaryKey' internal/model/like_event.go; then
  pass "Like increment crash idempotency" "durable like_events.message_id transaction"
else
  known_issue "Like increment can repeat after restart" "Durable message_id idempotency path was not detected."
fi
if grep -q 'UpdateColumn("like_count"' internal/repo/like.go \
  && grep -q 'ORDER BY room.like_count DESC' internal/repo/room.go; then
  pass "Like count write-back for ranking" "rooms.like_count is updated transactionally"
else
  known_issue "Like ranking count is stale" "MySQL room counter update was not detected."
fi
if grep -q 'func ReconcileRoomLikeCounts' internal/dao/redis.go \
  && grep -q 'restoreRoomLikeCountScript.Run' internal/dao/redis.go; then
  pass "Redis like recovery" "periodic MySQL-to-Redis reconciliation detected"
else
  known_issue "Redis like loss has no recovery" "Reconciliation path was not detected."
fi

step "Checking HTTP API and authentication boundaries"
api_request GET "$BASE_URL/api/v1/rooms"
assert_status "Public room list" 200
api_request GET "$BASE_URL/api/v1/auth/me"
assert_status "Unauthenticated /auth/me is rejected" 401
api_request GET "$BASE_URL/api/v1/rooms/mine"
assert_status "Unauthenticated /rooms/mine is rejected" 401
api_request POST "$BASE_URL/api/v1/rooms" '{"title":"unauthorized"}'
assert_status "Unauthenticated room creation is rejected" 401
api_request POST "$BASE_URL/api/v1/auth/register" '{"username":"x","email":"invalid","password":"short"}'
assert_status "Registration DTO validation" 400

USER_STAMP="$(date +%Y%m%d%H%M%S)-$$"
OWNER_NAME="e2eowner${USER_STAMP//-/}"
OTHER_NAME="e2eother${USER_STAMP//-/}"
OWNER_EMAIL="$OWNER_NAME@example.com"
OTHER_EMAIL="$OTHER_NAME@example.com"
PASSWORD='E2ePassword123!'

step "Testing registration, login, and Session"
api_request POST "$BASE_URL/api/v1/auth/register" \
  "{\"username\":\"$OWNER_NAME\",\"email\":\"$OWNER_EMAIL\",\"password\":\"$PASSWORD\"}"
assert_status "Owner registration" 200
OWNER_ID=$(json_get "$API_BODY" data.user_id)
[[ $OWNER_ID =~ ^[1-9][0-9]*$ ]] || die "Owner registration returned invalid user_id=$OWNER_ID"
pass "Owner receives auto-increment user_id" "user_id=$OWNER_ID"

api_request POST "$BASE_URL/api/v1/auth/register" \
  "{\"username\":\"$OTHER_NAME\",\"email\":\"$OTHER_EMAIL\",\"password\":\"$PASSWORD\"}"
assert_status "Second-user registration" 200
OTHER_ID=$(json_get "$API_BODY" data.user_id)
[[ $OTHER_ID =~ ^[1-9][0-9]*$ && $OTHER_ID != "$OWNER_ID" ]] || die "Second user_id is invalid"
pass "Second user has a distinct user_id" "user_id=$OTHER_ID"

api_request POST "$BASE_URL/api/v1/auth/register" \
  "{\"username\":\"duplicate${USER_STAMP//-/}\",\"email\":\"$OWNER_EMAIL\",\"password\":\"$PASSWORD\"}"
assert_status "Duplicate email is rejected" 409
api_request POST "$BASE_URL/api/v1/auth/login" \
  "{\"email\":\"$OWNER_EMAIL\",\"password\":\"WrongPassword123!\"}"
assert_status "Wrong password is rejected" 401

api_request POST "$BASE_URL/api/v1/auth/login" \
  "{\"email\":\"$OWNER_EMAIL\",\"password\":\"$PASSWORD\"}" "$OWNER_COOKIE_JAR"
assert_status "Owner login" 200
OWNER_SID=$(extract_sid "$OWNER_COOKIE_JAR")
[[ $OWNER_SID =~ ^[0-9a-fA-F]{64}$ ]] || die "Owner sid is missing or malformed"
pass "Owner received random sid" "64-hex session cookie"

api_request POST "$BASE_URL/api/v1/auth/login" \
  "{\"email\":\"$OTHER_EMAIL\",\"password\":\"$PASSWORD\"}" "$OTHER_COOKIE_JAR"
assert_status "Second-user login" 200
OTHER_SID=$(extract_sid "$OTHER_COOKIE_JAR")
[[ $OTHER_SID =~ ^[0-9a-fA-F]{64}$ ]] || die "Second-user sid is missing or malformed"
pass "Second user received random sid" "64-hex session cookie"

api_request GET "$BASE_URL/api/v1/auth/me" "" "$OWNER_COOKIE_JAR"
assert_status "Owner session lookup" 200
[[ $(json_get "$API_BODY" data.user_id) == "$OWNER_ID" ]] || die "Session maps to the wrong owner"
pass "Session maps to the correct owner"

step "Testing room lifecycle and owner permissions"
api_request POST "$BASE_URL/api/v1/rooms" "{\"title\":\"E2E Linux Room $USER_STAMP\"}" "$OWNER_COOKIE_JAR"
assert_status "Room creation" 200
ROOM_ID=$(json_get "$API_BODY" data.room_id)
[[ $ROOM_ID =~ ^[1-9][0-9]*$ ]] || die "Room creation returned invalid room_id=$ROOM_ID"
pass "Room receives auto-increment room_id" "room_id=$ROOM_ID"
api_request POST "$BASE_URL/api/v1/rooms" '{"title":"duplicate room"}' "$OWNER_COOKIE_JAR"
assert_status "One-room-per-owner rule" 409
api_request GET "$BASE_URL/api/v1/rooms/mine" "" "$OWNER_COOKIE_JAR"
assert_status "Get my room" 200
[[ $(json_get "$API_BODY" data.room_id) == "$ROOM_ID" ]] || die "My-room ownership is incorrect"
pass "My-room ownership is correct"
api_request GET "$BASE_URL/api/v1/rooms/$ROOM_ID"
assert_status "Room detail" 200
[[ $(json_get "$API_BODY" data.status) == 0 ]] || die "New room is not offline"
pass "New room starts offline"
test_handshake_status 403 "Offline room rejects WebSocket upgrade"

UPDATED_TITLE="E2E Linux Updated $USER_STAMP"
api_request PATCH "$BASE_URL/api/v1/rooms/$ROOM_ID" "{\"room_title\":\"$UPDATED_TITLE\"}" "$OWNER_COOKIE_JAR"
assert_status "Owner can update room" 200
[[ $(json_get "$API_BODY" data.room_title) == "$UPDATED_TITLE" ]] || die "Updated title mismatch"
pass "Updated title is returned"
api_request PATCH "$BASE_URL/api/v1/rooms/$ROOM_ID" '{"room_title":"forged"}' "$OTHER_COOKIE_JAR"
if [[ $API_STATUS == 403 ]]; then
  pass "Non-owner cannot update room" "status=403"
elif [[ $API_STATUS == 500 ]]; then
  known_issue "Non-owner room update uses HTTP 500 instead of 403" "Permission is enforced but status mapping is incorrect."
else
  die "Non-owner update returned status=$API_STATUS, expected 403"
fi
api_request POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" "{\"room_title\":\"$UPDATED_TITLE\"}" "$OTHER_COOKIE_JAR"
assert_status "Non-owner cannot start room" 403
api_request POST "$BASE_URL/api/v1/rooms/$ROOM_ID/start" "{\"room_title\":\"$UPDATED_TITLE\"}" "$OWNER_COOKIE_JAR"
assert_status "Owner starts room" 200
ROOM_STARTED=1
api_request GET "$BASE_URL/api/v1/rooms"
assert_status "Live-room list after start" 200
if ROOM_ID_VALUE=$ROOM_ID JSON_INPUT=$API_BODY python3 -c '
import json, os, sys
body=json.loads(os.environ["JSON_INPUT"])
room=int(os.environ["ROOM_ID_VALUE"])
sys.exit(0 if any(int(item["room_id"]) == room for item in body["data"]) else 1)
'; then
  pass "Started room appears in live-room ranking"
else
  die "Started room is missing from live-room ranking"
fi

diagnostics_snapshot before-protocol
step "Testing WebSocket protocol, ordering, ACK, likes, and rate limiting"
E2E_LOG="$TMP_DIR/e2e.log"
run_go_helper "$E2E_LOG" \
  -ws-url "$WEBSOCKET_URL" -origin "$ORIGIN" -room "$ROOM_ID" \
  -sid "$OWNER_SID" -user "$OWNER_ID" -username "$OWNER_NAME" \
  -like-rate-limit "$LIKE_RATE_LIMIT" -timeout 15s
E2E_LINE=$(grep '\[E2E_JSON\]' "$E2E_LOG" | tail -n 1 || true)
[[ -n $E2E_LINE ]] || die "WebSocket protocol helper did not emit E2E JSON"
E2E_JSON=${E2E_LINE#*\[E2E_JSON\] }
for field in origin_blocked guest_read_ok guest_send_blocked generated_id_valid spoofing_blocked ordering_ok ack_sent; do
  [[ $(json_get "$E2E_JSON" "$field") == true ]] || die "WebSocket protocol assertion failed: $field"
  pass "WebSocket $field"
done
if [[ $(json_get "$E2E_JSON" duplicate_like_applied) == true ]]; then
  known_issue "Like message_id is not idempotent" "Duplicate logical like increased the observed count."
else
  pass "Like message_id is idempotent" "duplicate count unchanged"
fi
if (( LIKE_RATE_LIMIT > 0 )); then
  [[ $(json_get "$E2E_JSON" like_rate_limited) == true ]] || die "Like rate limit was not enforced"
  pass "Per-user/per-room like rate limit" "limit=$LIKE_RATE_LIMIT/s"
fi
diagnostics_snapshot after-protocol

if (( SKIP_HISTORY_CHECKS == 0 )); then
  step "Testing MySQL history pagination and replay"
  MESSAGE_IDS_JSON=$(python3 -c 'import json,sys; print(json.dumps(json.load(sys.stdin)["message_ids"]))' <<<"$E2E_JSON")
  wait_for_protocol_history "$MESSAGE_IDS_JSON"
  IDS_JSON=$MESSAGE_IDS_JSON JSON_INPUT=$HISTORY_BODY python3 -c '
import collections, json, os, sys
ids=json.loads(os.environ["IDS_JSON"])
items=json.loads(os.environ["JSON_INPUT"])["data"]["items"]
counts=collections.Counter(str(item.get("message_id")) for item in items)
sys.exit(0 if all(counts[mid] == 1 for mid in ids) else 1)
' || die "MySQL message_id idempotency failed"
  pass "Kafka persistence and MySQL message_id idempotency"
  api_request GET "$BASE_URL/api/v1/rooms/$ROOM_ID/messages?limit=not-a-number"
  assert_status "History rejects invalid limit" 400
  api_request GET "$BASE_URL/api/v1/rooms/$ROOM_ID/messages?limit=2"
  assert_status "History cursor page" 200
  json_test "$API_BODY" 'len(data["data"]["items"]) == 2 and "has_more" in data["data"]' || die "History limit/has_more assertion failed"
  pass "History limit and has_more"
  FIRST_SEQ=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["room_sequences"][0])' <<<"$E2E_JSON")
  api_request GET "$BASE_URL/api/v1/rooms/$ROOM_ID/messages/replay?after_seq=$FIRST_SEQ&limit=100"
  assert_status "Replay after room_seq" 200
  IDS_JSON=$MESSAGE_IDS_JSON AFTER_SEQ=$FIRST_SEQ JSON_INPUT=$API_BODY python3 -c '
import json, os, sys
ids=set(json.loads(os.environ["IDS_JSON"]))
after=int(os.environ["AFTER_SEQ"])
items=[item for item in json.loads(os.environ["JSON_INPUT"])["data"]["items"] if item.get("message_id") in ids]
seq=[int(item["room_seq"]) for item in items]
sys.exit(0 if len(seq) >= 2 and all(value > after for value in seq) and all(a < b for a,b in zip(seq,seq[1:])) else 1)
' || die "Replay did not return increasing room_seq values"
  pass "Replay returns increasing messages after acknowledged sequence"
else
  skip "History and replay checks" "--skip-history-checks"
fi

api_request GET "$BASE_URL/api/v1/rooms/$ROOM_ID"
assert_status "Room detail after likes" 200
DETAIL_LIKES=$(json_get "$API_BODY" data.like_count)
OBSERVED_LIKES=$(json_get "$E2E_JSON" like_count)
python3 - "$DETAIL_LIKES" "$OBSERVED_LIKES" <<'PY' || die "MySQL room like_count is behind realtime count"
import sys
raise SystemExit(0 if int(sys.argv[1]) >= int(sys.argv[2]) else 1)
PY
pass "Room detail/ranking exposes durable likes" "mysql=$DETAIL_LIKES observed=$OBSERVED_LIKES"

case "$PROFILE" in
  Functional) skip "WebSocket load test" "profile=Functional" ;;
  Smoke) run_load_stage smoke-100x1 100 1 15s 5ms 10s ;;
  Standard)
    run_load_stage baseline-100x1 100 1 20s 5ms 10s
    run_load_stage standard-1000x10 1000 10 30s 2ms 15s
    ;;
  Stress)
    run_load_stage standard-1000x10 1000 10 45s 2ms 20s
    run_load_stage stress-5000x10 5000 10 1m 1ms 30s
    ;;
  Extreme)
    run_load_stage standard-1000x10 1000 10 45s 2ms 20s
    run_load_stage stress-5000x10 5000 10 1m 1ms 30s
    run_load_stage extreme-10000x10 10000 10 2m 1ms 1m
    ;;
esac

if [[ -n $KAFKA_BROKERS ]]; then
  step "Running Kafka pure-write benchmark"
  go run ./cmd/benchmark/kafka -brokers "$KAFKA_BROKERS" -topic "$KAFKA_TOPIC" \
    -n "$KAFKA_MESSAGES" -w "$KAFKA_WORKERS" -async=false -batch 100
  pass "Kafka pure-write benchmark" "messages=$KAFKA_MESSAGES workers=$KAFKA_WORKERS"
else
  skip "Kafka pure-write benchmark" "pass --kafka-brokers to enable"
fi

step "Stopping room and testing logout"
api_request POST "$BASE_URL/api/v1/rooms/$ROOM_ID/stop" "" "$OTHER_COOKIE_JAR"
assert_status "Non-owner cannot stop room" 403
api_request POST "$BASE_URL/api/v1/rooms/$ROOM_ID/stop" "" "$OWNER_COOKIE_JAR"
assert_status "Owner stops room" 200
ROOM_STARTED=0
test_handshake_status 403 "Stopped room rejects WebSocket upgrade"
api_request POST "$BASE_URL/api/v1/auth/logout" "" "$OWNER_COOKIE_JAR"
assert_status "Owner logout" 200
api_request GET "$BASE_URL/api/v1/auth/me" "" "$OWNER_COOKIE_JAR"
assert_status "Logged-out sid cannot access /auth/me" 401
api_request POST "$BASE_URL/api/v1/auth/logout" "" "$OTHER_COOKIE_JAR"
assert_status "Second-user logout" 200

SUITE_PASSED=1
printf '\n\033[32mFULL LINUX AUTOMATED TEST PASSED\033[0m\n'
printf 'Profile: %s\nKnown issues: %d\n' "$PROFILE" "$KNOWN_ISSUES"
