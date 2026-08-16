#!/usr/bin/env bash
# Sources .env so DATABASE_URL is set even outside an interactive shell.
set -euo pipefail
cd "$(dirname "$0")/.."

set -a
source .env
set +a

go test ./internal/repository/... -v -count=1 "$@"
