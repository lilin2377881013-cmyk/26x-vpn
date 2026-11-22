"""26x Telegram download assistant. Python 3.9+, standard library only."""
import argparse
import contextlib
import fcntl
import hashlib
import hmac
import json
import logging
import os
import re
import secrets
import sqlite3
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

ORIGIN = "https://26xvpn.com"
BOT = "Get26xbot"
SUPPORT = "https://t.me/ios999888000"
STATE = Path(os.environ.get("TELEGRAM_STATE_DIR", "/var/lib/26x-telegram"))
LIMIT = 50_000_000
CODE = re.compile(r"[A-Za-z0-9_-]{16}")
PLATFORMS = {
    "ios-tf": ("iPhone / iPad", "先安装苹果的 TestFlight，再打开邀请链接，点击「安装」。"),
    "android": ("安卓", "领取 APK 后打开文件，按系统提示安装 26x。"),
    "windows": ("Windows", "领取 x64 安装程序，双击运行，按向导完成安装后从开始菜单打开 26x。"),
    "macos": ("Mac", "打开 DMG，把 26x 拖入「应用程序」。首次连接时，按 macOS 提示允许 26x 的网络扩展和 VPN 配置。"),
}
WELCOME = "26x VPN · 官方下载助手\n\n选择你的设备，获取最新版和安装指南。新用户可以在客户端免费试用 15 分钟。"
DESCRIPTION = (
    "26x VPN · 稳定运营 5 年+\n\n"
    "电信、联通、移动三网深度优化，流量不限。\n"
    "新用户注册可免费试用 15 分钟，会员支持 USDT 支付。\n\n"
    "点击下方「开始」，选择你的设备，领取客户端。"
)
TRIAL = "15 分钟免费试用\n\n打开 26x，在「连接」页点击「免费试用」。首次启用后开始计时，到期后自动断开。已经注册的用户也可以在符合试用条件时体验。"
HELP = "安装与连接帮助\n\n选择设备可查看对应安装步骤。如果连接时报错，请把设备系统和完整错误码发给客服。"
PRIVACY = "推广统计按广告来源汇总启动人数、设备选择和领取次数。人数按来源去重，统计库不保存聊天正文和 Telegram 用户名。"
COMMANDS = [{"command": key, "description": label} for key, label in [
    ("start", "打开主菜单"), ("download", "选择设备并下载"), ("trial", "查看试用说明"),
    ("help", "安装与连接帮助"), ("support", "联系人工客服"), ("privacy", "查看统计说明")]]


def connect_db():
    STATE.mkdir(mode=0o700, parents=True, exist_ok=True)
    db = sqlite3.connect(STATE / "assistant.sqlite3", timeout=8)
    db.execute("PRAGMA journal_mode=WAL")
    db.executescript("""
        CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
        CREATE TABLE IF NOT EXISTS audience (
          source TEXT, event TEXT, visitor BLOB, PRIMARY KEY(source,event,visitor));
        CREATE TABLE IF NOT EXISTS totals (
          source TEXT, event TEXT, count INTEGER NOT NULL, PRIMARY KEY(source,event));
        CREATE TABLE IF NOT EXISTS active_referrals (
          chat_key BLOB PRIMARY KEY, source TEXT NOT NULL);
        CREATE TABLE IF NOT EXISTS files (
          release TEXT PRIMARY KEY, file_id TEXT NOT NULL);
        CREATE TABLE IF NOT EXISTS imports (
          ticket TEXT PRIMARY KEY, platform TEXT, release TEXT, name TEXT, size INTEGER, expires INTEGER);
        CREATE TABLE IF NOT EXISTS upload_batches (
          ticket TEXT PRIMARY KEY REFERENCES imports(ticket), code TEXT NOT NULL,
          uploader TEXT);
    """)
    with db:
        db.execute("INSERT OR IGNORE INTO settings VALUES ('salt',?)", (secrets.token_hex(32),))
    return db


def setting(db, key, default=""):
    row = db.execute("SELECT value FROM settings WHERE key=?", (key,)).fetchone()
    return row[0] if row else default


def save_setting(db, key, value):
    db.execute("INSERT OR REPLACE INTO settings VALUES (?,?)", (key, str(value)))


class TelegramError(Exception):
    def __init__(self, method, code, delay=0):
        super().__init__(f"Telegram {method} returned {code}")
        self.code = code
        self.delay = delay


def telegram(method, data, attachment=None):
    token = Path(os.environ["TELEGRAM_TOKEN_FILE"]).read_text().strip()
    address = f"https://api.telegram.org/bot{token}/{method}"
    content_type = "application/json"
    if attachment is None:
        body = json.dumps(data).encode()
    else:
        field, name, content = attachment
        boundary = secrets.token_hex(16)
        parts = []
        for key, value in data.items():
            value = value if isinstance(value, str) else json.dumps(value)
            parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"\r\n\r\n{value}\r\n'.encode())
        parts += [f'--{boundary}\r\nContent-Disposition: form-data; name="{field}"; filename="{name}"\r\nContent-Type: application/octet-stream\r\n\r\n'.encode(),
                  content, f"\r\n--{boundary}--\r\n".encode()]
        body = b"".join(parts)
        content_type = "multipart/form-data; boundary=" + boundary
    request = urllib.request.Request(address, data=body, headers={"Content-Type": content_type})
    try:
        with urllib.request.urlopen(request, timeout=120 if attachment else 45) as response:
            return json.load(response)["result"]
    except urllib.error.HTTPError as error:
        try:
            delay = json.load(error).get("parameters", {}).get("retry_after", 0)
        except (ValueError, OSError):
            delay = 0
        raise TelegramError(method, error.code, delay) from None


def download_info(platform):
    with urllib.request.urlopen(ORIGIN + "/api/v1/downloads", timeout=8) as response:
        url = json.load(response)["links"].get(platform, "")
    parts = urllib.parse.urlsplit(url)
    if platform == "ios-tf":
        if not re.fullmatch(r"https://testflight\.apple\.com/join/[a-zA-Z0-9]+", url):
            raise ValueError("TestFlight link unavailable")
        return url, "", 0, ""
    name = parts.path.rsplit("/", 1)[-1]
    if not re.fullmatch(r"[A-Za-z0-9._+-]+\.(dmg|apk|exe)", name) or url != ORIGIN + "/downloads/" + name:
        raise ValueError("Package URL unavailable")
    with urllib.request.urlopen(urllib.request.Request(url, method="HEAD"), timeout=10) as response:
        size = int(response.headers["Content-Length"])
        signature = response.headers.get("ETag") or response.headers.get("Last-Modified") or ""
    if size <= 0 or not signature:
        raise ValueError("Package metadata unavailable")
    return url, name, size, f"{url}|{size}|{signature}"


def button(label, action, source):
    return {"text": label, "callback_data": action + ":" + source}


def home(source):
    platforms = [button(label, "choose/" + key, source) for key, (label, _) in PLATFORMS.items()]
    return {"inline_keyboard": [platforms[:2], platforms[2:], [
        button("试用怎么用", "trial", source), {"text": "联系客服", "url": SUPPORT}],
        [button("安装与连接帮助", "help", source), button("隐私说明", "privacy", source)]]}


def back(source):
    return {"inline_keyboard": [[button("返回主菜单", "home", source), {"text": "联系客服", "url": SUPPORT}]]}


def welcome(chat, source):
    params = {"chat_id": chat, "caption": WELCOME, "reply_markup": home(source)}
    photo = os.environ.get("TELEGRAM_POSTER_FILE_ID", "")
    if photo:
        telegram("sendPhoto", {**params, "photo": photo})
    else:
        poster = Path(os.environ["TELEGRAM_POSTER_FILE"])
        telegram("sendPhoto", params, ("photo", poster.name, poster.read_bytes()))


def record(db, source, event, user, count=1):
    visitor = hmac.digest(bytes.fromhex(setting(db, "salt")), f"{source}:{user}".encode(), "sha256")
    db.execute("INSERT OR IGNORE INTO audience VALUES (?,?,?)", (source, event, visitor))
    db.execute("""INSERT INTO totals VALUES (?,?,?) ON CONFLICT(source,event)
        DO UPDATE SET count=count+excluded.count""", (source, event, count))


def deliver(db, chat, platform, source):
    url, name, size, release = download_info(platform)
    label, guide = PLATFORMS[platform]
    keyboard = back(source)
    if platform == "ios-tf":
        keyboard["inline_keyboard"].insert(0, [{"text": "打开 TestFlight", "url": url}])
        telegram("sendMessage", {"chat_id": chat, "text": guide, "reply_markup": keyboard})
        return True
    row = db.execute("SELECT file_id FROM files WHERE release=?", (release,)).fetchone()
    params = {"chat_id": chat, "caption": label + " 官方安装包\n\n" + guide, "reply_markup": keyboard}
    if row:
        try:
            telegram("sendDocument", {**params, "document": row[0]})
            return True
        except TelegramError as err:
            if err.code != 400:
                raise
            db.execute("DELETE FROM files WHERE release=?", (release,))
    if size > LIMIT:
        keyboard["inline_keyboard"].insert(0, [{"text": "从官网获取最新版", "url": url}])
        telegram("sendMessage", {"chat_id": chat, "text": "最新安装包正在同步到 Telegram，可先从官网下载。", "reply_markup": keyboard})
        return False
    with urllib.request.urlopen(url, timeout=60) as response:
        content = response.read(size + 1)
    if len(content) != size or download_info(platform)[3] != release:
        raise ValueError("Package changed during download")
    sent = telegram("sendDocument", params, ("document", name, content))
    db.execute("INSERT OR REPLACE INTO files VALUES (?,?)", (release, sent["document"]["file_id"]))
    return True


def seed_ticket(db, platform, file):
    url, name, size, release = download_info(platform)
    digest = hashlib.sha256()
    with urllib.request.urlopen(url, timeout=70) as response:
        for block in iter(lambda: response.read(1024 * 1024), b""):
            digest.update(block)
    if file.name != name or file.stat().st_size != size or digest.digest() != hashlib.sha256(file.read_bytes()).digest():
        raise ValueError("Local filename or bytes differ from the official release")
    if download_info(platform)[3] != release:
        raise ValueError("Release changed while preparing import")
    ticket = secrets.token_urlsafe(24)
    with db:
        db.execute("DELETE FROM imports WHERE platform=? OR expires<?", (platform, int(time.time())))
        db.execute("INSERT INTO imports VALUES (?,?,?,?,?,?)",
                   (hashlib.sha256(ticket.encode()).hexdigest(), platform, release, name, size, int(time.time())+86400))
    return "/release " + ticket


def batch_code(db):
    code = secrets.token_urlsafe(24)
    code_hash = hashlib.sha256(code.encode()).hexdigest()
    with db:
        db.execute("DELETE FROM upload_batches")
        cursor = db.execute("""INSERT INTO upload_batches(ticket,code)
            SELECT ticket,? FROM imports WHERE expires>=?""", (code_hash, int(time.time())))
        if not cursor.rowcount:
            raise ValueError("First prepare at least one official release")
    return "/upload " + code


def uploader_id(db, chat):
    return hmac.new(bytes.fromhex(setting(db, "salt")), f"uploader:{chat}".encode(), "sha256").hexdigest()


def pair_upload(db, chat, code):
    code_hash = hashlib.sha256(code.encode()).hexdigest()
    cursor = db.execute("""UPDATE upload_batches SET uploader=? WHERE code=? AND uploader IS NULL
        AND ticket IN (SELECT ticket FROM imports WHERE expires>=?)""",
        (uploader_id(db, chat), code_hash, int(time.time())))
    if cursor.rowcount:
        telegram("sendMessage", {"chat_id": chat, "text": "请将已核对的官方安装包作为文件发到本对话。每份文件收到后会单独确认。"})


def import_document(db, message):
    chat = message["chat"]["id"]
    doc = message["document"]
    ticket = hashlib.sha256(message.get("caption", "").removeprefix("/release ").strip().encode()).hexdigest()
    row = db.execute("""SELECT i.platform,i.release,i.ticket FROM imports i
        LEFT JOIN upload_batches b ON b.ticket=i.ticket
        WHERE (i.ticket=? OR b.uploader=?) AND i.expires>=? AND i.name=? AND i.size=?""",
        (ticket, uploader_id(db, chat), int(time.time()), doc.get("file_name"), doc.get("file_size"))).fetchone()
    if not row:
        return
    if download_info(row[0])[3] != row[1]:
        return
    db.execute("INSERT OR REPLACE INTO files VALUES (?,?)", (row[1], doc["file_id"]))
    db.execute("DELETE FROM upload_batches WHERE ticket=?", (row[2],))
    db.execute("DELETE FROM imports WHERE ticket=?", (row[2],))
    telegram("sendMessage", {"chat_id":chat, "text":"已接收官网版本 " + doc["file_name"]})


def handle(db, update):
    cb = update.get("callback_query")
    message = cb.get("message", {}) if cb else update.get("message", {})
    if message.get("chat", {}).get("type") != "private":
        return
    chat = message["chat"]["id"]
    source = ""
    if cb:
        try:
            telegram("answerCallbackQuery", {"callback_query_id": cb["id"]})
        except TelegramError as err:
            if err.code != 400:
                raise
        action, _, source = cb.get("data", "").partition(":")
    else:
        if "document" in message:
            import_document(db, message)
            return
        words = message.get("text","").strip().split()
        command = words[0].split("@")[0].lower() if words else ""
        action = command.removeprefix("/") if command.startswith("/") else "help"
        if action == "upload":
            if len(words) == 2:
                pair_upload(db, chat, words[1])
            return
        if action == "start" and len(words) == 2:
            source = words[1]
            if source.startswith("upload_"):
                pair_upload(db, chat, source.removeprefix("upload_"))
                return
        chat_key = hmac.digest(bytes.fromhex(setting(db, "salt")), f"active-referral:{chat}".encode(), "sha256")
        if action == "start":
            if CODE.fullmatch(source):
                db.execute("INSERT OR REPLACE INTO active_referrals VALUES (?,?)", (chat_key, source))
            else:
                db.execute("DELETE FROM active_referrals WHERE chat_key=?", (chat_key,))
        else:
            row = db.execute("SELECT source FROM active_referrals WHERE chat_key=?", (chat_key,)).fetchone()
            source = row[0] if row else ""
    if not CODE.fullmatch(source):
        source = ""
    if action in ("start", "download", "home"):
        welcome(chat, source)
        if action == "start":
            record(db, source, "start", chat)
        return
    verb, _, platform = action.partition("/")
    if verb in ("choose","get") and platform in PLATFORMS:
        label, guide = PLATFORMS[platform]
        if verb == "choose":
            keyboard = back(source)
            keyboard["inline_keyboard"].insert(0, [button("获取 TestFlight 链接" if platform == "ios-tf" else "领取安装包", "get/"+platform, source)])
            telegram("sendMessage", {"chat_id":chat, "text":label+" 安装指南\n\n"+guide,"reply_markup":keyboard})
            record(db, source, "choose/"+platform, chat)
        else:
            try:
                sent = deliver(db, chat, platform, source)
            except (OSError, ValueError, KeyError) as exc:
                logging.warning("Release unavailable for %s (%s)", platform,type(exc).__name__)
                telegram("sendMessage", {"chat_id":chat, "text":"暂时无法读取最新版，请稍后重试。", "reply_markup":back(source)})
                sent = False
            record(db, source, "request/"+platform, chat)
            if sent:
                record(db, source, "sent/"+platform, chat)
        return
    text = {"trial":TRIAL,"privacy":PRIVACY,"support":"26x 人工客服 · @ios999888000"}.get(action, HELP)
    telegram("sendMessage", {"chat_id":chat,"text":text,"reply_markup":home(source) if action=="help" else back(source)})


def report(db, source):
    return [{"event":event,"people":people,"count":count} for event,people,count in db.execute("""
      SELECT t.event,(SELECT count(*) FROM audience a WHERE a.source=t.source AND a.event=t.event),t.count
      FROM totals t WHERE t.source=? ORDER BY t.event""",(source,))]


def channel_summary(db):
    rows = db.execute("""
        SELECT source,
               COUNT(DISTINCT CASE WHEN event='start' THEN visitor END),
               COUNT(DISTINCT CASE WHEN event IN ('sent/android','sent/macos','sent/windows') THEN visitor END),
               COUNT(DISTINCT CASE WHEN event='sent/ios-tf' THEN visitor END)
        FROM audience
        WHERE source != '' AND event IN ('start','sent/android','sent/macos','sent/windows','sent/ios-tf')
        GROUP BY source
    """)
    return [{"code": code, "started": started, "packages": packages, "testflight": testflight}
            for code, started, packages, testflight in rows]


class Stats(BaseHTTPRequestHandler):
    def do_GET(self):
        code = self.path.removeprefix("/referrals/")
        overview = self.path == "/referrals"
        if not overview and (self.path != "/referrals/"+code or not CODE.fullmatch(code)):
            self.send_error(404)
            return
        with contextlib.closing(sqlite3.connect(STATE/"assistant.sqlite3")) as db:
            data = json.dumps({"items": channel_summary(db) if overview else report(db, code)}).encode()
        self.send_response(200)
        self.send_header("Content-Type","application/json")
        self.send_header("Cache-Control","no-store")
        self.send_header("Content-Length",str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, fmt, *args):
        pass


def run(db):
    with (STATE/"worker.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if telegram("getMe", {})["username"].lower()!=BOT.lower():
            raise ValueError("Configured token belongs to a different bot")
        httpd = ThreadingHTTPServer(("127.0.0.1",int(os.environ.get("TELEGRAM_STATS_PORT","8796"))),Stats)
        threading.Thread(target=httpd.serve_forever,daemon=True).start()
        while True:
            try:
                offset = int(setting(db,"offset","0"))
                if time.time()-float(setting(db,"last","0")) > 6*86400:
                    offset = 0
                updates = telegram("getUpdates", {"offset":offset,"timeout":25,"limit":20,"allowed_updates":["message","callback_query"]})
                for upd in updates:
                    with db:
                        try:
                            handle(db,upd)
                        except TelegramError as err:
                            if err.code not in (400,403):
                                raise
                            logging.info("Skipped undeliverable update (%s)",err.code)
                        save_setting(db,"offset",upd["update_id"]+1)
                        save_setting(db,"last",int(time.time()))
            except (OSError, TelegramError, sqlite3.OperationalError) as err:
                logging.warning("Polling retry (%s)", err if isinstance(err,TelegramError) else type(err).__name__)
                time.sleep(max(3,min(3600,getattr(err,"delay",0))))


if __name__ == "__main__":
    parser=argparse.ArgumentParser()
    sub=parser.add_subparsers(dest="command",required=True)
    sub.add_parser("run")
    sub.add_parser("profile")
    sub.add_parser("pair-imports")
    seed=sub.add_parser("prepare-release")
    seed.add_argument("platform",choices=["android","macos","windows"])
    seed.add_argument("file",type=Path)
    opts=parser.parse_args()
    os.umask(0o077)
    with contextlib.closing(connect_db()) as connection:
        if opts.command=="run":
            run(connection)
        elif opts.command=="pair-imports":
            print(batch_code(connection))
        elif opts.command=="profile":
            if telegram("getMe", {})["username"].lower() != BOT.lower():
                raise ValueError("Token belongs to a different bot")
            telegram("setMyName", {"name":"26x 官方助手"})
            for language in ("", "zh"):
                telegram("setMyDescription", {"description": DESCRIPTION, "language_code": language})
            telegram("setMyShortDescription", {"short_description":"26x VPN 官方下载、安装指南与人工客服。"})
            telegram("setMyCommands", {"commands":COMMANDS})
            poster = Path(os.environ["TELEGRAM_POSTER_FILE"])
            telegram("setMyProfilePhoto", {"photo":{"type":"static","photo":"attach://picture"}},
                     ("picture",poster.name,poster.read_bytes()))
            print("Bot profile, avatar and commands updated")
        else:
            print(seed_ticket(connection,opts.platform,opts.file))
