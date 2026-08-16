#!/usr/bin/env bash
# Sources .env so DATABASE_URL is set even outside an interactive shell.
set -euo pipefail
cd "$(dirname "$0")/.."

if [[ -f .env ]]; then
  set -a
  source .env
  set +a
fi

go test ./internal/repository/... -v -count=1 "$@"
