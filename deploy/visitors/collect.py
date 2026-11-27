#!/usr/bin/env python3
"""Import public HTML page loads. Only parsed device labels enter PostgreSQL."""
import argparse
import csv
from datetime import datetime, timezone
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import parse_qs, unquote, urlsplit

PAGES = {"/", "/account", "/account/login", "/privacy", "/support", "/terms"}
CODE = re.compile(r"[A-Za-z0-9_-]{16}")
AUTOMATED = re.compile(r"bot\b|crawler|spider|headless|facebookexternalhit|preview|globalping|Google-InspectionTool|^curl/|^Wget/|^python-|^Go-http-client", re.I)
VERSION = r"(\d+(?:[._]\d+)*)"
BROWSERS = [
    ("微信", r"MicroMessenger/"), ("QQ 浏览器", r"(?:M?QQBrowser)/"),
    ("QQ 内置浏览器", r"\bQQ/"), ("Telegram 内置浏览器", r"Telegram(?:-iOS|-Android)?/"),
    ("抖音内置浏览器", r"aweme/"), ("Samsung Internet", r"SamsungBrowser/"),
    ("UC 浏览器", r"(?:UCBrowser|UCWEB)/"), ("Opera", r"(?:OPR|OPiOS)/"),
    ("Edge", r"(?:EdgiOS|EdgA|Edg|Edge)/"), ("Firefox", r"(?:FxiOS|Firefox)/"),
    ("Chrome", r"(?:CriOS|Chrome|Chromium)/"),
]
LEGACY = re.compile(rb'^\S+ \S+ \S+ \[([^\]]+)\] "GET ([^ ]+) HTTP/[0-9.]+" (200|304) \S+ "[^"]*" "([^"]*)"')
STAMP = "%d/%b/%Y:%H:%M:%S %z"


def version(pattern, ua):
    match = re.search(pattern + VERSION, ua, re.I)
    return match[1].replace("_", ".")[:48] if match else ""


def label(name, number):
    return name + " " + number if number else name


def describe(ua):
    ua = ua[:2000]
    ipad = "iPad" in ua or ("Macintosh" in ua and "Mobile/" in ua)
    if ipad or "iPhone" in ua or "iPod" in ua:
        device = "iPad" if ipad else ("iPod" if "iPod" in ua else "iPhone")
        os_name = "iPadOS" if ipad else "iOS"
        os_version = version(r"(?:CPU (?:iPhone )?|iPhone )OS ", ua)
        system = label(os_name, os_version) if os_version else os_name + "（版本未上报）"
        safari = version(r"Version/", ua)
        if safari and int(safari.split(".")[0]) >= 26 and os_version.startswith("18."):
            system = f"{os_name}（上报 {os_version}，实际版本隐藏）"
    elif "Android" in ua:
        device = "Android 手机" if "Mobile" in ua else "Android 平板"
        model = re.search(r";\s*(?:[a-z]{2}[-_][A-Z]{2};\s*)?([^;()/]{2,60}?)\s+Build/", ua)
        if model and model[1].strip() not in {"K", "wv"}:
            device += " · " + model[1].strip()
        system = label("Android", version(r"Android\s+", ua))
    elif "Windows" in ua:
        device = "Windows 电脑"
        nt = version(r"Windows NT ", ua)
        system = {"10.0": "Windows 10 / 11", "6.3": "Windows 8.1", "6.1": "Windows 7"}.get(nt, label("Windows NT", nt))
    elif "Macintosh" in ua or "Mac OS X" in ua:
        device, system = "Mac", label("macOS", version(r"Mac OS X\s+", ua))
    elif "CrOS" in ua:
        device, system = "Chromebook", label("ChromeOS", version(r"CrOS\s+\S+\s+", ua))
    elif "Linux" in ua:
        device, system = "Linux 设备", "Linux"
    else:
        device, system = "未识别", "未上报"
    for name, token in BROWSERS:
        num = version(token, ua)
        if num:
            if name == "Chrome" and re.search(r";\s*wv\)", ua):
                name = "Android WebView"
            return device, system, label(name, num)
    if "Telegram" in ua:
        browser = "Telegram 内置浏览器"
    elif "Safari/" in ua and "Version/" in ua:
        browser = label("Safari", version(r"Version/", ua))
    elif "AppleWebKit/" in ua:
        browser = "WebKit（浏览器名称未上报）"
    else:
        browser = "未识别"
    return device, system, browser


def event(key, at, page, ref, ua):
    if page not in PAGES or not ua or ua == "-" or AUTOMATED.search(ua):
        return None
    device, system, browser = describe(ua)
    return (key, at.isoformat(), page, ref if CODE.fullmatch(ref or "") else "", device, system, browser)


def live(raw):
    r = json.loads(raw)
    if not re.fullmatch(r"[a-f0-9]{32}", r["id"]):
        raise ValueError("invalid nginx request ID")
    return event(r["id"], datetime.fromisoformat(r["at"]), r["page"], r["ref"], r["ua"])


def legacy(raw, offset, origin, start, end):
    match = LEGACY.match(raw)
    if not match:
        return None
    try:
        at = datetime.strptime(match[1].decode("ascii"), STAMP)
        u = urlsplit(match[2].decode("ascii"))
        if u.netloc or u.scheme or u.path not in PAGES or not start <= at.timestamp() < end:
            return None
        query = parse_qs(u.query, keep_blank_values=True, max_num_fields=160)
    except (ValueError, UnicodeError):
        return None
    if "_rsc" in query:
        return None
    # Same input file + byte position is stable across retries and gzip rotation.
    key = "log:" + hashlib.sha256(origin.encode() + str(offset).encode() + raw).hexdigest()
    return event(key, at, u.path, query.get("ref", [""])[0], match[4].decode("utf8", "replace"))


def save(rows, count_referrals):
    if not rows:
        return
    buf = io.StringIO()
    csv.writer(buf, lineterminator="\n").writerows(rows)
    sql = """BEGIN;
CREATE TEMP TABLE incoming(k text, at timestamptz, page text, ref text, device text, os text, browser text) ON COMMIT DROP;
\\copy incoming FROM stdin WITH (FORMAT csv)
""" + buf.getvalue() + """\\.
WITH inserted AS (
 INSERT INTO website_visits(event_key, visited_at, page, referral_code, device, os, browser)
 SELECT k,at,page,ref,device,os,browser FROM incoming WHERE at >= now()-interval '30 days'
 ON CONFLICT(event_key) DO NOTHING RETURNING visited_at,page,referral_code,device
)
"""
    if count_referrals:
        sql += """INSERT INTO referral_daily(code,day,device,channel,count)
 SELECT l.code,(i.visited_at AT TIME ZONE 'Asia/Shanghai')::date,split_part(i.device,' · ',1),'',count(*)
 FROM inserted i JOIN referral_links l ON l.code=i.referral_code WHERE i.page='/'
 GROUP BY 1,2,3 ON CONFLICT(code,day,device,channel)
 DO UPDATE SET count=referral_daily.count+EXCLUDED.count;"""
    else:
        sql += "SELECT count(*) FROM inserted;"
    run_sql(sql + "\nCOMMIT;")


def run_sql(sql):
    # Keep DB credentials out of argv and do not pass other API secrets to psql.
    address = urlsplit(os.environ["DATABASE_URL"])
    if address.scheme not in {"postgres", "postgresql"} or not address.path.strip("/"):
        raise ValueError("invalid PostgreSQL connection URL")
    env = {"PATH": os.environ["PATH"], "PGDATABASE": unquote(address.path[1:]), "PGCONNECT_TIMEOUT": "5"}
    for key, value in (("PGHOST", address.hostname), ("PGPORT", address.port),
                       ("PGUSER", address.username), ("PGPASSWORD", address.password)):
        if value is not None:
            env[key] = unquote(str(value))
    sslmode = parse_qs(address.query).get("sslmode")
    if sslmode:
        env["PGSSLMODE"] = sslmode[0]
    subprocess.run(["psql", "-Xq", "-v", "ON_ERROR_STOP=1"], input=sql, text=True, env=env,
                   stdout=subprocess.DEVNULL, check=True)


def collect():
    state = Path(os.environ.get("STATE_DIRECTORY", "/var/lib/26x-website-visits")) / "cursors.json"
    cursors = json.loads(state.read_text()) if state.exists() else {}
    current = {}
    files = sorted(Path("/var/log/nginx").glob("26x-visits.log*"), key=lambda f: f.stat().st_mtime)
    for path in files:
        opener = gzip.open if path.suffix == ".gz" else open
        with opener(path, "rb") as f:
            first = f.readline()
            if not first.endswith(b"\n"):
                continue
            key = hashlib.sha256(first).hexdigest()
            offset, old_size, old_name = cursors.get(key, [0, -1, ""])
            size = path.stat().st_size
            if offset and old_size == size and old_name == path.name:
                current[key] = [offset, size, path.name]
                continue
            f.seek(offset)
            rows = []
            for raw in f:
                if not raw.endswith(b"\n"):
                    break
                row = live(raw)
                if row:
                    rows.append(row)
                offset += len(raw)
                if len(rows) >= 500:
                    save(rows, True)
                    rows.clear()
            save(rows, True)
            # Track bytes actually consumed. A concurrent append is picked up next run.
            seen_size = size if path.suffix == ".gz" else offset
            current[key] = [offset, seen_size, path.name]
    run_sql("DELETE FROM website_visits WHERE visited_at < now()-interval '30 days';")
    tmp = state.with_suffix(".tmp")
    tmp.write_text(json.dumps(current))
    tmp.replace(state)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--legacy-source", help="import one combined-format access log from stdin")
    parser.add_argument("--from-epoch", type=int, default=0)
    parser.add_argument("--until-epoch", type=int)
    args = parser.parse_args()
    if args.legacy_source:
        if args.until_epoch is None:
            parser.error("historical import needs an exclusive cutoff")
        batch = []
        offset = 0
        for raw in sys.stdin.buffer:
            row = legacy(raw, offset, args.legacy_source, args.from_epoch, args.until_epoch)
            offset += len(raw)
            if row:
                batch.append(row)
            if len(batch) >= 500:
                save(batch, False)
                batch.clear()
        save(batch, False)
    else:
        collect()
