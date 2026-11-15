#!/usr/bin/env python3
"""Install and revoke runtime VLESS credentials for regional paid accounts."""
import base64
import json
import os
import re
import subprocess
import sys
import uuid
from pathlib import Path

from cryptography.exceptions import InvalidTag
from cryptography.hazmat.primitives.ciphers.aead import AESGCM


def sql(query):
    return subprocess.run(
        ["runuser", "-u", "postgres", "--", "psql", "-XqAt", "-d", "26x_control", "-v", "ON_ERROR_STOP=1"],
        input=query, capture_output=True, text=True, check=True,
    ).stdout.strip()


def main():
    txt = Path("/etc/26x/api.env").read_text()
    key = AESGCM(base64.b64decode(re.search(r"^CREDENTIAL_ENCRYPTION_KEY=(\S+)$", txt, re.M).group(1)))
    state = Path("/opt/26x-node/state")
    eligible = """d.revoked_at IS NULL AND u.status='active'
        AND e.enabled AND n.status IN ('online','degraded') AND EXISTS (
            SELECT 1 FROM subscriptions s WHERE s.user_id=u.id AND s.status='active'
            AND s.starts_at<=now() AND (s.ends_at IS NULL OR s.ends_at>now())
        ) AND (NOT EXISTS (SELECT 1 FROM trial_accounts t WHERE t.user_id=u.id)
               OR (UPPER(n.region_code)='HK' AND lease.expires_at>now()))"""
    rows = json.loads(sql(f"""
        SELECT COALESCE(json_agg(selection),'[]') FROM (
            SELECT n.code, e.port, e.id AS endpoint, d.id AS device, c.id AS id, c.status,
                   encode(c.credential_ciphertext, 'hex') AS cipher, COALESCE(({eligible}),FALSE) AS eligible,
                   EXISTS (SELECT 1 FROM trial_accounts t WHERE t.user_id=u.id) AS trial
            FROM devices d JOIN users u ON u.id=d.user_id
            CROSS JOIN node_endpoints e JOIN nodes n ON n.id=e.node_id
            LEFT JOIN device_node_credentials c ON c.device_id=d.id AND c.endpoint_id=e.id
            LEFT JOIN connection_leases lease ON lease.device_id=d.id AND lease.endpoint_id=e.id
            WHERE e.protocol='vless_reality' AND n.code<>'hk-01'
            AND (
                ((c.id IS NULL OR c.managed AND c.status='revoked') AND ({eligible}))
                OR (c.managed AND c.status IN ('pending','active','revoking') AND c.reconcile_after<=now())
            )
            ORDER BY CASE WHEN lease.expires_at>now() AND (c.id IS NULL OR c.status IN ('pending','revoked')) THEN 0 ELSE 1 END,
                COALESCE(c.reconcile_after, d.created_at), n.code LIMIT 32
        ) selection;
    """))
    failures = 0
    for row in rows:
        code = row["code"]
        if not re.fullmatch(r"[a-z0-9][a-z0-9-]{1,47}", code):
            continue
        device, endpoint = str(uuid.UUID(row["device"])), str(uuid.UUID(row["endpoint"]))
        aad = f"26x/node-credential/v1/{device}/{endpoint}".encode()
        wanted = row["eligible"] and row["status"] != "revoking"
        try:
            node = json.loads((state / f"node-{code}.json").read_text())
            auth = (state / f".vault-{code}").read_text().strip()
            if wanted and row["status"] in (None, "revoked"):
                credential = {"credential": str(uuid.uuid4()), "fingerprint": "chrome", "flow": "xtls-rprx-vision"}
                nonce = os.urandom(12)
                encrypted = (nonce + key.encrypt(nonce, json.dumps(credential).encode(), aad)).hex()
                result = sql(f"""
                  INSERT INTO device_node_credentials
                    (device_id, endpoint_id, credential_ciphertext, key_version, status, managed)
                  VALUES ('{device}','{endpoint}',decode('{encrypted}','hex'),'v1','pending',TRUE)
                  ON CONFLICT (device_id,endpoint_id) DO UPDATE
                    SET credential_ciphertext=EXCLUDED.credential_ciphertext, status='pending',
                        revoked_at=NULL,reconcile_after=now()
                    WHERE device_node_credentials.managed AND device_node_credentials.status='revoked'
                  RETURNING id;
                """)
                if not result:
                    continue
                row["id"], row["cipher"] = result, encrypted
            payload = {}
            if wanted:
                encrypted = bytes.fromhex(row["cipher"])
                payload = json.loads(key.decrypt(encrypted[:12], encrypted[12:], aad))
            identifier = str(uuid.UUID(row["id"]))
            operation = f"python3 /usr/local/lib/26x/regional-device-user.py {int(row['port'])} {device}"
            subprocess.run(
                ["sshpass", "-e", "ssh", "-T", "-o", "ConnectTimeout=6", "-o", "StrictHostKeyChecking=yes",
                 "-o", f"UserKnownHostsFile={state / 'bandwidth-known-hosts'}", "-p", str(int(node.get("ssh_port", 22))),
                 "-l", node.get("ssh_user", "root"), "--", node["host"], operation],
                input=json.dumps(payload), capture_output=True, text=True, timeout=16, check=True,
                env={**os.environ, "SSHPASS": auth},
            )
            status = "active" if wanted else "revoked"
            reconcile_at = "now()+interval '2 minutes'"
            if wanted and row["trial"]:
                reconcile_at = f"LEAST({reconcile_at}, COALESCE((SELECT expires_at FROM connection_leases WHERE device_id='{device}' AND endpoint_id='{endpoint}'),now()))"
            sql(f"""UPDATE device_node_credentials SET status='{status}',
                    revoked_at={'NULL' if wanted else 'now()'},
                    reconcile_after={reconcile_at}
                    WHERE id='{identifier}' AND managed
                      AND (status<>'revoking' OR '{status}'='revoked');""")
        except (OSError, KeyError, ValueError, InvalidTag, subprocess.SubprocessError):
            failures += 1
            if row["id"]:
                identifier = str(uuid.UUID(row["id"]))
                sql(f"UPDATE device_node_credentials SET reconcile_after=now()+interval '30 seconds' WHERE id='{identifier}' AND managed;")
    if failures:
        sys.exit(f"Unable to reconcile {failures} regional device credentials")


if __name__ == "__main__":
    main()
