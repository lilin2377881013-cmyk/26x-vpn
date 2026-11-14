#!/usr/bin/env python3
"""Manage 26x VLESS users over a local, access-controlled Xray Unix socket."""

import argparse
import json
import re
import stat
import subprocess
import sys
import uuid
from pathlib import Path


class XrayUsers:
    def __init__(self, executable, socket, tag):
        self.executable = executable
        self.socket = socket
        self.tag = tag

    def command(self, operation, *arguments, data=None):
        result = subprocess.run(
            [self.executable, "api", operation, f"--server=unix:{self.socket}",
             "--timeout=5", *arguments],
            input=data, capture_output=True, text=True, timeout=8, check=False,
        )
        if result.returncode != 0:
            raise RuntimeError(f"Xray operation {operation} failed")
        return result.stdout

    def lookup(self, label):
        reply = json.loads(self.command("inbounduser", f"-tag={self.tag}", f"-email={label}"))
        return next((user["account"] for user in reply.get("users", [])
                     if user.get("email") == label), None)

    def ensure(self, label, credential, flow):
        existing = self.lookup(label)
        if existing is not None:
            if existing.get("id") != credential or existing.get("flow", "") != flow:
                raise RuntimeError("Device has a different credential installed")
            return
        user = {"email": label, "id": credential, "flow": flow}
        # adu parses a full inbound definition before extracting its users.
        config = {"inbounds": [{"tag": self.tag, "protocol": "vless", "port": 1,
                                "settings": {"decryption": "none", "clients": [user]}}]}
        self.command("adu", "stdin:", data=json.dumps(config))
        existing = self.lookup(label)
        if existing is None or existing.get("id") != credential or existing.get("flow", "") != flow:
            raise RuntimeError("Xray did not install the requested credential")

    def revoke(self, label):
        if self.lookup(label) is not None:
            self.command("rmu", f"-tag={self.tag}", label)
            if self.lookup(label) is not None:
                raise RuntimeError("Xray still accepts the revoked credential")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--xray", default="/usr/local/bin/xray")
    parser.add_argument("--socket", type=Path, required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("operation", choices=["ensure", "revoke"])
    parser.add_argument("device_id", type=uuid.UUID)
    options = parser.parse_args()

    if not re.fullmatch(r"[A-Za-z0-9_.-]{1,64}", options.tag):
        parser.error("Invalid inbound tag")
    label = f"26x-device-{options.device_id}"
    try:
        socket = options.socket.resolve(strict=True)
        if not stat.S_ISSOCK(socket.stat().st_mode):
            raise ValueError("Xray management endpoint must be a local Unix socket")
        users = XrayUsers(options.xray, socket, options.tag)
        if options.operation == "ensure":
            payload = json.loads(sys.stdin.read(4096))
            credential = str(uuid.UUID(payload["credential"]))
            flow = payload.get("flow", "xtls-rprx-vision")
            if flow not in ("", "xtls-rprx-vision"):
                raise ValueError("Unsupported VLESS flow")
            users.ensure(label, credential, flow)
        else:
            users.revoke(label)
    except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.TimeoutExpired):
        parser.exit(1, f"Unable to {options.operation} Xray access for {label}.\n")
    print(f"{options.operation}: {label}")


if __name__ == "__main__":
    main()
