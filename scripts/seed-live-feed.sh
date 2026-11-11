#!/usr/bin/env bash

set -Eeuo pipefail

BASE_URL="http://127.0.0.1:9000"
COUNT=25
STATE_DIR=""
STOP_STATE_DIR=""
PASSWORD='FeedTest123!'
API_STATUS=""
API_BODY=""

usage() {
  cat <<'EOF'
Usage:
  ./scripts/seed-live-feed.sh [--base-url URL] [--count NUMBER] [--state-dir DIR]
  ./scripts/seed-live-feed.sh --stop-state DIR [--base-url URL]

Create and start test rooms for live Feed pagination, or stop rooms created by a
previous run.

Options:
  --base-url URL     Backend address. Default: http://127.0.0.1:9000
  --count NUMBER     Number of rooms to create. Default: 25; range: 1-100
  --state-dir DIR    Directory used to save the manifest and session cookies
  --stop-state DIR   Stop every room recorded in DIR/rooms.tsv
  -h, --help         Show this help
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
    --base-url)
      need_value "$@"
      BASE_URL=${2%/}
      shift 2
      ;;
    --count)
      need_value "$@"
      COUNT=$2
      shift 2
      ;;
    --state-dir)
      need_value "$@"
      STATE_DIR=$2
      shift 2
      ;;
    --stop-state)
      need_value "$@"
      STOP_STATE_DIR=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

for command_name in curl python3; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "Required command not found: $command_name" >&2
    exit 2
  fi
done

if [[ ! $COUNT =~ ^[0-9]+$ ]] || (( COUNT < 1 || COUNT > 100 )); then
  echo "--count must be an integer between 1 and 100" >&2
  exit 2
fi

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
PROJECT_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)

api_request() {
  local method=$1 url=$2 body=${3:-} cookie_jar=${4:-}
  local response_file curl_status exit_code
  response_file=$(mktemp "${TMPDIR:-/tmp}/live-feed-api.XXXXXX")

  local -a args=(
    --silent
    --show-error
    --connect-timeout 5
    --max-time 30
    --request "$method"
    --output "$response_file"
    --write-out '%{http_code}'
  )

  if [[ -n $cookie_jar ]]; then
    args+=(--cookie "$cookie_jar" --cookie-jar "$cookie_jar")
  fi

  if [[ -n $body ]]; then
    args+=(--header 'Content-Type: application/json' --data "$body")
  fi

  set +e
  curl_status=$(curl "${args[@]}" "$url")
  exit_code=$?
  set -e

  API_BODY=$(<"$response_file")
  rm -f -- "$response_file"

  if (( exit_code != 0 )); then
    echo "Request failed: $method $url (curl exit $exit_code)" >&2
    exit 1
  fi

  API_STATUS=$curl_status
}

expect_status() {
  local expected=$1 action=$2
  if [[ $API_STATUS != "$expected" ]]; then
    echo "$action failed: status=$API_STATUS body=$API_BODY" >&2
    exit 1
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
print(value)
' "$path" <<<"$json"
}

stop_seeded_rooms() {
  local requested_dir=$1 state_dir manifest failures=0 stopped=0
  state_dir=$(cd -- "$requested_dir" && pwd)
  manifest="$state_dir/rooms.tsv"

  if [[ ! -f $manifest ]]; then
    echo "Manifest not found: $manifest" >&2
    exit 2
  fi

  echo "Stopping rooms recorded in $manifest"

  while IFS=$'\t' read -r index username email room_id cookie_jar; do
    [[ $index == "index" || -z $room_id ]] && continue

    api_request POST "$BASE_URL/api/v1/rooms/$room_id/stop" '{}' "$cookie_jar"
    if [[ $API_STATUS == "200" ]]; then
      printf '[%s] stopped room_id=%s owner=%s\n' "$index" "$room_id" "$username"
      stopped=$((stopped + 1))
    else
      printf '[%s] failed room_id=%s status=%s body=%s\n' \
        "$index" "$room_id" "$API_STATUS" "$API_BODY" >&2
      failures=$((failures + 1))
    fi
  done <"$manifest"

  printf 'Stopped %d room(s); failures=%d. Test users and offline rooms remain in MySQL.\n' \
    "$stopped" "$failures"

  (( failures == 0 ))
}

if [[ -n $STOP_STATE_DIR ]]; then
  stop_seeded_rooms "$STOP_STATE_DIR"
  exit $?
fi

api_request GET "$BASE_URL/api/v1/rooms"
expect_status 200 "Backend readiness check"

STAMP="$(date +%Y%m%d%H%M%S)-$$"
if [[ -z $STATE_DIR ]]; then
  STATE_DIR="$PROJECT_ROOT/test-results/live-feed-seed-$STAMP"
elif [[ $STATE_DIR != /* ]]; then
  STATE_DIR="$PROJECT_ROOT/$STATE_DIR"
fi

mkdir -p -- "$STATE_DIR/cookies"
chmod 700 "$STATE_DIR" "$STATE_DIR/cookies"
STATE_DIR=$(cd -- "$STATE_DIR" && pwd)
MANIFEST="$STATE_DIR/rooms.tsv"
printf 'index\tusername\temail\troom_id\tcookie_jar\n' >"$MANIFEST"
chmod 600 "$MANIFEST"

SAFE_STAMP=${STAMP//-/}

echo "Creating and starting $COUNT test room(s) against $BASE_URL"
echo "State directory: $STATE_DIR"

for index in $(seq 1 "$COUNT"); do
  suffix=$(printf '%03d' "$index")
  username="feed${SAFE_STAMP}${suffix}"
  email="$username@example.com"
  room_title="Feed Test $suffix"
  cookie_jar="$STATE_DIR/cookies/$username.cookies"

  api_request POST "$BASE_URL/api/v1/auth/register" \
    "{\"username\":\"$username\",\"email\":\"$email\",\"password\":\"$PASSWORD\"}"
  expect_status 200 "Register $username"

  api_request POST "$BASE_URL/api/v1/auth/login" \
    "{\"email\":\"$email\",\"password\":\"$PASSWORD\"}" "$cookie_jar"
  expect_status 200 "Login $username"
  chmod 600 "$cookie_jar"

  api_request POST "$BASE_URL/api/v1/rooms" \
    "{\"title\":\"$room_title\"}" "$cookie_jar"
  expect_status 200 "Create room for $username"
  room_id=$(json_get "$API_BODY" data.room_id)

  if [[ ! $room_id =~ ^[1-9][0-9]*$ ]]; then
    echo "Create room returned invalid room_id: $room_id" >&2
    exit 1
  fi

  api_request POST "$BASE_URL/api/v1/rooms/$room_id/start" \
    "{\"room_title\":\"$room_title\"}" "$cookie_jar"
  expect_status 200 "Start room $room_id"

  printf '%s\t%s\t%s\t%s\t%s\n' \
    "$index" "$username" "$email" "$room_id" "$cookie_jar" >>"$MANIFEST"
  printf '[%02d/%02d] started room_id=%s title=%s\n' \
    "$index" "$COUNT" "$room_id" "$room_title"
done

api_request GET "$BASE_URL/api/v1/feeds/live/hot?limit=20"
expect_status 200 "Live Feed verification"
FEED_ITEMS=$(API_BODY_VALUE=$API_BODY python3 -c '
import json, os
body = json.loads(os.environ["API_BODY_VALUE"])
print(len(body.get("data", {}).get("items", [])))
')

cat <<EOF

Created and started $COUNT test rooms.
The first Feed request currently returns $FEED_ITEMS item(s).

Manifest:
  $MANIFEST

Open the Feed page and verify that scrolling past the first 20 rooms loads the
next page without duplicates.

Stop all rooms from this run with:
  ./scripts/seed-live-feed.sh --base-url "$BASE_URL" --stop-state "$STATE_DIR"
EOF
