#!/usr/bin/env bash
set -euo pipefail
# Called with the collector environment and one successful sample.
code="$1"
samples="$2"
psql "$DB_URL" -Xq -w -v ON_ERROR_STOP=1 -v node="$code" -v samples="$samples" \
    -f /opt/26x-node/lib/collect-usage.sql >/dev/null
