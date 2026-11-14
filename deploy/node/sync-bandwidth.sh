#!/usr/bin/env bash
set -euo pipefail
source /opt/26x-node/lib/common.sh
rows=$(db "SELECT code FROM nodes WHERE status IN ('online','degraded') ORDER BY code")
failed=0
while read -r code; do
    [[ -n "$code" ]] || continue
    if ! read -r host port user < <(python3 -c 'import json,sys;j=json.load(open(sys.argv[1]));print(j["host"],j.get("ssh_port",22),j.get("ssh_user","root"))' "$STATE_DIR/node-$code.json"); then
        failed=1; continue
    fi
    if ! config=$(psql "$DB_URL" -XqAtw -v ON_ERROR_STOP=1 -v node="$code" -f /opt/26x-node/lib/bandwidth-config.sql) || [[ -z "$config" ]]; then
        failed=1; continue
    fi
    if ! printf '%s\n' "$config" | SSHPASS="$(cat "$STATE_DIR/.vault-$code")" sshpass -e ssh \
        -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/opt/26x-node/state/bandwidth-known-hosts \
        -o ConnectTimeout=8 -p "$port" "$user@$host" \
        'set -eu; install -d -m 755 /etc/26x; tmp=$(mktemp /etc/26x/.bandwidth.XXXXXX); cat > "$tmp"; chown root:xray "$tmp"; chmod 640 "$tmp"; mv "$tmp" /etc/26x/bandwidth.json'; then
        failed=1
    fi
done <<< "$rows"
exit "$failed"
