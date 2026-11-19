#!/usr/bin/env python3
"""Mirror published packages; enable temporary redirects only while HTTPS is healthy."""
import argparse
import fcntl
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import shlex
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.parse
import urllib.request

STATE = Path('/var/lib/26x-download-edge')
ROUTES = Path('/etc/nginx/26x-download-edge-routes.conf')
MANIFEST = STATE / 'manifest.json'
HOST = '94.103.6.235'
PACKAGES = Path('/var/www/26x-downloads')
KNOWN_HOSTS = '/opt/26x-node/state/bandwidth-known-hosts'
VAULT = Path('/opt/26x-node/state/.vault-hk-regional-01')
CHANNELS = {'macos', 'windows', 'android'}


def atomic_write(path, value, mode=0o644):
    with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as f:
        name = f.name
        f.write(value.encode())
        f.flush()
        os.fsync(f.fileno())
    try:
        os.chmod(name, mode)
        os.replace(name, path)
    finally:
        Path(name).unlink(missing_ok=True)


def apply_routes(files):
    text = '# Managed by 26x-download-edge; empty means origin fallback.\n'
    for f in files:
        name = safe_name(f['name'])
        text += (f'location = /downloads/{name} {{\n'
                 '    add_header Cache-Control "no-store" always;\n'
                 f'    return 302 https://{HOST}/downloads/{name};\n'
                 '}\n')
    old = ROUTES.read_text() if ROUTES.exists() else ''
    if old == text:
        return
    atomic_write(ROUTES, text)
    try:
        subprocess.run(['nginx', '-t'], check=True, capture_output=True, timeout=15)
        subprocess.run(['systemctl', 'reload', 'nginx'], check=True, timeout=20)
    except Exception:
        atomic_write(ROUTES, old)
        raise


def safe_name(name):
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._+-]*\.(apk|exe|dmg|zip)', name):
        raise ValueError('Invalid package filename')
    return name


def ssh(command, **kwargs):
    env = dict(os.environ, SSHPASS=VAULT.read_text().strip())
    args = ['sshpass', '-e', 'ssh', '-T', '-o', 'ConnectTimeout=8',
            '-o', 'StrictHostKeyChecking=yes', '-o',
            'UserKnownHostsFile=' + KNOWN_HOSTS, 'root@' + HOST, command]
    return subprocess.run(args, env=env, check=True, timeout=180, **kwargs)


def published_files():
    sql = "SELECT channel,url FROM download_links WHERE channel IN ('macos','windows','android')"
    result = subprocess.run(['runuser', '-u', 'postgres', '--', 'psql', '-XqAt',
                             '-v', 'ON_ERROR_STOP=1', '-d', '26x_control', '-c', sql],
                            check=True, capture_output=True, text=True, timeout=15)
    files = []
    for row in result.stdout.splitlines():
        channel, url = row.split('|', 1)
        p = urllib.parse.urlsplit(url)
        if channel not in CHANNELS or p.scheme != 'https' or p.netloc != '26xvpn.com':
            continue
        if p.query or p.fragment or not p.path.startswith('/downloads/'):
            continue
        name = safe_name(p.path.removeprefix('/downloads/'))
        source = PACKAGES / name
        if source.is_symlink() or not source.is_file():
            raise ValueError('Published package missing: ' + name)
        size = source.stat().st_size
        if not 0 < size < 2_000_000_000:
            raise ValueError('Invalid package size')
        digest = hashlib.sha256()
        with source.open('rb') as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b''):
                digest.update(chunk)
        files.append(dict(channel=channel, name=name, size=size, sha256=digest.hexdigest()))
    if not files:
        raise ValueError('No published packages')
    return sorted(files, key=lambda f: f['name'])


def sync_files(files):
    probe = '''import hashlib,json,pathlib,sys
result={}
for f in json.load(sys.stdin):
 p=pathlib.Path('/var/www/26x-downloads')/f['name']
 if p.is_file() and p.stat().st_size==f['size']:
  h=hashlib.sha256()
  with p.open('rb') as stream:
   for chunk in iter(lambda:stream.read(1048576),b''):h.update(chunk)
  result[f['name']]=h.hexdigest()
print(json.dumps(result))
'''
    result = ssh('python3 -c ' + shlex.quote(probe), input=json.dumps(files),
                 text=True, capture_output=True)
    existing = json.loads(result.stdout)
    uploader = '''import hashlib,os,pathlib,sys,tempfile
name,expected,size=sys.argv[1:];size=int(size)
root=pathlib.Path('/var/www/26x-downloads');root.mkdir(parents=True,exist_ok=True)
h=hashlib.sha256();count=0
with tempfile.NamedTemporaryFile(dir=root,delete=False) as stream:
 tmp=pathlib.Path(stream.name)
 try:
  for chunk in iter(lambda:sys.stdin.buffer.read(1048576),b''):
   stream.write(chunk);h.update(chunk);count+=len(chunk)
  stream.flush();os.fsync(stream.fileno())
  if count!=size or h.hexdigest()!=expected:raise ValueError('Package checksum mismatch')
  os.chmod(tmp,0o644);os.replace(tmp,root/name)
 finally:tmp.unlink(missing_ok=True)
'''
    for f in files:
        if existing.get(f['name']) == f['sha256']:
            continue
        command = 'python3 -c ' + shlex.quote(uploader) + ' ' + ' '.join(
            shlex.quote(str(f[k])) for k in ('name', 'sha256', 'size'))
        with (PACKAGES / f['name']).open('rb') as stream:
            ssh(command, stdin=stream, stdout=subprocess.DEVNULL)
        print('Mirrored', f['name'], flush=True)
    # AlmaLinux/SELinux content labels must match the existing web root policy.
    ssh('if command -v restorecon >/dev/null; then restorecon -R /var/www/26x-downloads; fi',
        stdout=subprocess.DEVNULL)


def healthy(files):
    context = ssl.create_default_context()
    # Fail back to origin at least one day before a stale certificate could expire.
    import socket
    with socket.create_connection((HOST, 443), timeout=6) as sock:
        with context.wrap_socket(sock, server_hostname=HOST) as tls:
            if ssl.cert_time_to_seconds(tls.getpeercert()['notAfter']) - time.time() < 86400:
                raise ValueError('Mirror certificate needs renewal')
    for f in files:
        name = safe_name(f['name'])
        req = urllib.request.Request(f'https://{HOST}/downloads/{name}', method='HEAD')
        with urllib.request.urlopen(req, context=context, timeout=6) as response:
            if (response.status != 200 or int(response.headers['Content-Length']) != f['size']
                    or response.headers.get('Content-Disposition') != 'attachment'
                    or response.headers.get('Accept-Ranges') != 'bytes'
                    or response.headers.get('X-Content-Type-Options') != 'nosniff'):
                raise ValueError('Mirror file check failed: ' + name)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--health-only', action='store_true')
    args = parser.parse_args()
    ipaddress.IPv4Address(HOST)
    STATE.mkdir(parents=True, exist_ok=True)
    with (STATE / 'lock').open('w') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return
        try:
            if args.health_only:
                files = json.loads(MANIFEST.read_text())['files']
            else:
                files = published_files()
                sync_files(files)
                atomic_write(MANIFEST, json.dumps({'files': files}, indent=2) + '\n')
            healthy(files)
            apply_routes(files)
            print('Download mirror healthy; temporary redirects active.', flush=True)
        except Exception as error:
            apply_routes([])
            print('Origin fallback enabled:', type(error).__name__, str(error), file=sys.stderr)
            raise SystemExit(1)


if __name__ == '__main__':
    main()
