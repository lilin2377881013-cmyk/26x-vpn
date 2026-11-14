#!/usr/bin/env bash
set -euo pipefail
candidate="$1"
config=/usr/local/etc/xray/config.json
binary=/usr/local/bin/xray
backup=$(mktemp -d /root/26x-xray-backup.XXXXXX)
umask 077
cp -p "$binary" "$backup/xray"
cp -p "$config" "$backup/config.json"
python3 - "$config" "$backup/config.next.json" <<'PY'
import json, subprocess, sys
from pathlib import Path

source, target = map(Path, sys.argv[1:])
config = json.loads(source.read_text())
for inbound in config["inbounds"]:
    if inbound["protocol"] != "vless":
        continue
    raw = subprocess.check_output(["/usr/local/bin/xray", "api", "inbounduser",
                                   "--server=127.0.0.1:10085", "-tag=" + inbound["tag"]])
    users = json.loads(raw)["users"]
    assert users and all(user["email"] and user["account"]["id"] for user in users)
    inbound["settings"]["clients"] = [
        {"id": user["account"]["id"], "flow": user["account"].get("flow", ""),
         "email": user["email"], "level": user.get("level", 0)} for user in users
    ]
Path(target).write_text(json.dumps(config, indent=2))
PY
"$candidate" run -test -config "$backup/config.next.json"
install -o "$(stat -c %u "$config")" -g "$(stat -c %g "$config")" -m "$(stat -c %a "$config")" "$backup/config.next.json" "$config.next.json"
mv "$config.next.json" "$config"
install -m 755 "$candidate" "$binary.next"
mv "$binary.next" "$binary"
if systemctl restart xray && sleep 1 && systemctl is-active --quiet xray &&
   "$binary" api statsquery --server=127.0.0.1:10085 -pattern user >/dev/null; then
    echo "Xray running; backup: $backup"
    exit 0
fi
cp -p "$backup/config.json" "$config"
cp -p "$backup/xray" "$binary"
systemctl restart xray
exit 1
