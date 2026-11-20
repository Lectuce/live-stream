#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
PROJECT_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
MAIN_SCRIPT="$SCRIPT_DIR/loopback-load-test.sh"

if ! command -v tmux >/dev/null 2>&1; then
  echo "tmux is required. Install it with: sudo apt install tmux" >&2
  exit 2
fi

STAMP=$(date +%Y%m%d-%H%M%S)
SESSION_NAME="loopback-load-$STAMP"
REPORT_DIR="$PROJECT_ROOT/test-results/loopback-$STAMP"
mkdir -p "$REPORT_DIR"

printf -v command '%q ' bash "$MAIN_SCRIPT" "$@" --report-directory "$REPORT_DIR"
tmux new-session -d -s "$SESSION_NAME" -c "$PROJECT_ROOT"
tmux set-option -t "$SESSION_NAME" remain-on-exit on
tmux send-keys -t "$SESSION_NAME" "$command" C-m

cat <<EOF
Loopback load test started independently from this SSH/VSCode session.

Session: $SESSION_NAME
Results: $REPORT_DIR

Watch live output:
  tmux attach -t $SESSION_NAME

Detach without stopping it:
  Ctrl+B, then D

Check progress without attaching:
  tmux capture-pane -p -t $SESSION_NAME -S -50
  tail -f '$REPORT_DIR/run.log'
EOF
