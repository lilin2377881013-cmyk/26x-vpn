import hashlib
import tempfile
import time
import unittest
from pathlib import Path
from unittest.mock import patch

import bot


class AssistantTest(unittest.TestCase):
    def setUp(self):
        folder = tempfile.TemporaryDirectory()
        self.addCleanup(folder.cleanup)
        state = patch.object(bot, "STATE", Path(folder.name))
        state.start()
        self.addCleanup(state.stop)
        self.db = bot.connect_db()
        self.addCleanup(self.db.close)
        api = patch.object(bot, "telegram")
        self.api = api.start()
        self.addCleanup(api.stop)

    def message(self, chat, **values):
        bot.handle(self.db, {"message": {"chat": {"id": chat, "type": "private"}, **values}})

    def click(self, chat, source, action):
        bot.handle(self.db, {"callback_query": {"id": "test", "data": action + ":" + source,
            "message": {"chat": {"id": chat, "type": "private"}}}})

    @patch.dict("os.environ", {"TELEGRAM_POSTER_FILE_ID": "official-poster"})
    def test_independent_campaigns_and_delivery(self):
        first, second = "a" * 16, "b" * 16
        self.message(100, text="/start " + first)
        self.message(100, text="/start " + first)
        self.message(100, text="/start " + second)
        self.click(100, first, "choose/android")
        self.db.execute("INSERT INTO files VALUES ('current','official-document')")
        with patch.object(bot, "download_info", return_value=("https://example.com/app.apk", "app.apk", bot.LIMIT + 1, "current")):
            self.click(100, first, "get/android")
        self.assertEqual(self.api.call_args.args[0], "sendDocument")
        self.assertEqual(self.api.call_args.args[1]["document"], "official-document")
        self.click(100, second, "choose/ios-tf")
        link = "https://testflight.apple.com/join/abcdef"
        with patch.object(bot, "download_info", return_value=(link, "", 0, "")):
            self.click(100, second, "get/ios-tf")
        keyboard = self.api.call_args.args[1]["reply_markup"]["inline_keyboard"]
        self.assertEqual(keyboard[0][0]["url"], link)
        self.assertEqual(keyboard[-1][-1]["url"], bot.SUPPORT)
        metrics = {r["event"]: (r["people"], r["count"]) for r in bot.report(self.db, first)}
        self.assertEqual(metrics, {"start": (1, 2), "choose/android": (1, 1),
                                   "request/android": (1, 1), "sent/android": (1, 1)})
        self.assertEqual({r["event"] for r in bot.report(self.db, second)},
                         {"start", "choose/ios-tf", "request/ios-tf", "sent/ios-tf"})

    def test_batch_bound_to_first_private_sender_and_current_release(self):
        ticket = hashlib.sha256(b"ticket").hexdigest()
        self.db.execute("INSERT INTO imports VALUES (?,?,?,?,?,?)",
            (ticket, "macos", "v1", "client.dmg", 123, int(time.time()) + 120))
        code = bot.batch_code(self.db).split()[1]
        invitation = "/start upload_" + code
        doc = {"file_id": "file", "file_name": "client.dmg", "file_size": 123}
        self.message(101, text=invitation)
        self.message(202, text=invitation)
        with patch.object(bot, "download_info", return_value=("", "client.dmg", 123, "v2")) as catalog:
            self.message(202, document=doc)
            catalog.assert_not_called()
            self.message(101, document={**doc, "file_size": 222})
            catalog.assert_not_called()
            self.message(101, document=doc)
            self.assertEqual(self.db.execute("SELECT count(*) FROM files").fetchone()[0], 0)
            catalog.return_value = ("", "client.dmg", 123, "v1")
            self.message(101, document=doc)
        self.assertEqual(self.db.execute("SELECT * FROM files").fetchall(), [("v1", "file")])
        self.assertEqual(self.db.execute("SELECT count(*) FROM imports").fetchone()[0], 0)
        self.assertEqual(self.db.execute("SELECT count(*) FROM upload_batches").fetchone()[0], 0)

    def test_summary_counts_people_across_packages_and_channels(self):
        ad, group = "a" * 16, "g" * 16
        for source, chat, events in [
            (ad, 100, ["start", "start", "sent/android", "sent/android", "sent/windows", "sent/ios-tf"]),
            (ad, 101, ["start", "request/windows"]),
            (ad, 102, ["start", "sent/macos"]),
            (group, 100, ["start", "sent/ios-tf"]),
            ("", 103, ["start", "sent/macos"]),
        ]:
            for event in events:
                bot.record(self.db, source, event, chat)
        self.assertEqual(bot.channel_summary(self.db), [
            {"code": ad, "started": 3, "packages": 2, "testflight": 1},
            {"code": group, "started": 1, "packages": 0, "testflight": 1},
        ])

    @patch.dict("os.environ", {"TELEGRAM_POSTER_FILE_ID": "official-poster"})
    def test_download_command_uses_current_channel(self):
        for chat, source in [(10, "a" * 16), (20, "g" * 16), (10, "g" * 16), (10, "")]:
            self.message(chat, text="/start " + source)
            self.message(chat, text="/download")
            keyboard = self.api.call_args.args[1]["reply_markup"]["inline_keyboard"]
            self.assertEqual(keyboard[0][0]["callback_data"].partition(":")[2], source)

    def test_old_document_not_sent_after_new_release(self):
        self.db.execute("INSERT INTO files VALUES ('v1','outdated')")
        with patch.object(bot, "download_info", return_value=("https://26xvpn.com/downloads/new.apk",
                                 "new.apk", bot.LIMIT + 1, "v2")):
            self.click(100, "a" * 16, "get/android")
        self.assertEqual(self.api.call_args.args[0], "sendMessage")
        self.assertEqual([r["event"] for r in bot.report(self.db, "a" * 16)], ["request/android"])
