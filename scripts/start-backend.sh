#!/usr/bin/env bash
set -Eeuo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
binary_path="${BACKEND_BINARY_PATH:-${project_root}/bin/live-stream-server}"
start_dependencies=0
skip_build=0

for argument in "$@"; do
  case "${argument}" in
    --start-dependencies)
      start_dependencies=1
      ;;
    --skip-build)
      skip_build=1
      ;;
    *)
      echo "unknown argument: ${argument}" >&2
      echo "usage: bash scripts/start-backend.sh [--start-dependencies] [--skip-build]" >&2
      exit 2
      ;;
  esac
done

cd "${project_root}"

if [[ "${start_dependencies}" == "1" ]]; then
  echo "==> Starting MySQL, Redis, Kafka and ZooKeeper"
  docker compose up -d --wait
fi

if [[ "${skip_build}" != "1" || ! -x "${binary_path}" ]]; then
  echo "==> Building Linux backend"
  mkdir -p "$(dirname "${binary_path}")"
  go build -o "${binary_path}" .
fi

echo "==> Backend is running in this terminal"
echo "    API: http://127.0.0.1:9000"
echo "    Press Ctrl+C to stop it."
exec "${binary_path}"
