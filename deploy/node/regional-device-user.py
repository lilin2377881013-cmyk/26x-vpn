#!/usr/bin/env python3
"""Reconcile one device on the regional node's loopback Xray API."""
import argparse
import json
import subprocess
import sys
import uuid
from pathlib import Path


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("port", type=int)
    parser.add_argument("device", type=uuid.UUID)
    args = parser.parse_args()
    payload = json.load(sys.stdin)
    label = f"26x-device-{args.device}"
    config = json.loads(Path("/usr/local/etc/xray/config.json").read_text())
    tags = [entry["tag"] for entry in config["inbounds"]
            if entry["protocol"] == "vless" and int(entry["port"]) == args.port]
    if len(tags) != 1:
        raise RuntimeError("Cannot identify regional VLESS inbound")
    tag = tags[0]
    command = ["/usr/local/bin/xray", "api"]

    def call(action, *options, data=None):
        return subprocess.run(
            [*command, action, "--server=127.0.0.1:10085", "--timeout=4", *options],
            input=data, capture_output=True, text=True, timeout=5, check=True,
        ).stdout

    def lookup():
        users = json.loads(call("inbounduser", f"-tag={tag}")).get("users", [])
        return next((item["account"]["id"] for item in users if item["email"] == label), None)

    wanted = payload.get("credential")
    current = lookup()
    if wanted:
        wanted = str(uuid.UUID(wanted))
    if current != wanted:
        if current:
            call("rmu", f"-tag={tag}", label)
        if wanted:
            user = {"email": label, "id": wanted, "flow": "xtls-rprx-vision"}
            inbound = {"tag": tag, "protocol": "vless", "port": 1,
                       "settings": {"decryption": "none", "clients": [user]}}
            call("adu", "stdin:", data=json.dumps({"inbounds": [inbound]}))
    if lookup() != wanted:
        raise RuntimeError("Regional credential operation did not take effect")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, RuntimeError, subprocess.SubprocessError):
        sys.exit("Unable to reconcile regional device credential")
