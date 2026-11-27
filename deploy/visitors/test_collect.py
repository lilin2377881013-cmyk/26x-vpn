from datetime import datetime, timezone
import unittest
import collect


class VisitorTests(unittest.TestCase):
    def test_reported_apple_versions(self):
        ua = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_6_1 like Mac OS X) AppleWebKit/605.1 Version/17.6 Mobile/15E Safari/604.1"
        self.assertEqual(collect.describe(ua), ("iPhone", "iOS 17.6.1", "Safari 17.6"))
        frozen = ua.replace("17_6_1", "18_6_2").replace("Version/17.6", "Version/26.1")
        self.assertEqual(collect.describe(frozen), ("iPhone", "iOS（上报 18.6.2，实际版本隐藏）", "Safari 26.1"))
        ipad = ua.replace("iPhone; CPU iPhone OS", "iPad; CPU OS").replace("Version/17.6", "CriOS/130.1.2.3")
        self.assertEqual(collect.describe(ipad), ("iPad", "iPadOS 17.6.1", "Chrome 130.1.2.3"))

    def test_embedded_browser(self):
        ua = "Mozilla/5.0 (Linux; Android 13; Pixel 6 Build/TQ2A; wv) AppleWebKit/537.36 Chrome/118.0 Mobile Safari/537.36"
        self.assertEqual(collect.describe(ua), ("Android 手机 · Pixel 6", "Android 13", "Android WebView 118.0"))
        self.assertEqual(collect.describe(ua+" MicroMessenger/8.0.54")[2], "微信 8.0.54")
        self.assertEqual(collect.describe("Mozilla/5.0 (iPhone; CPU iPhone OS 17_6 like Mac OS X) AppleWebKit/605.1 Mobile/15E")[2], "WebKit（浏览器名称未上报）")

    def test_history_paths_and_retry(self):
        at = b'01/Nov/2026:00:00:00 +0800'
        def visit(path):
            line = b'127.0.0.1 - - ['+at+b'] "GET '+path+b' HTTP/1.1" 200 412 "-" "Mozilla/5.0 (iPhone; CPU iPhone OS 17_6_1 like Mac OS X)"'
            return collect.legacy(line,9,'new',0,2000000000)
        code=b'TestCampaign0123'
        self.assertEqual(visit(b'/?ref='+code),visit(b'/?ref='+code))
        self.assertEqual(visit(b'/?ref='+code)[3],code.decode())
        for path in [b'/?_rsc=a',b'/api/v1/account',b'/admin',b'/images/test.png']:
            self.assertIsNone(visit(path))
        self.assertIsNone(collect.event('x',datetime.now(timezone.utc),'/','','TelegramBot (like TwitterBot)'))


if __name__ == '__main__':
    unittest.main()
