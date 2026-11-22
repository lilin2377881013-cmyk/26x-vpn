# 26x Telegram 官方助手

`@Get26xbot` 从官网 `/api/v1/downloads` 读取当前版本。iOS 显示 TestFlight 邀请，安卓、Windows 和 macOS 发送 Telegram 文件。客服入口指向 `@ios999888000`。

## 服务部署

1. 将 `bot.py` 和仓库的 `client/assets/branding/26x-icon-original.jpg` 放入服务器的 `/opt/26x/telegram/`。需要 Python 3.9 或更高版本。
2. 将 BotFather 提供的 token 保存为 `/etc/26x/telegram.token`，所有者为 root，权限为 0600。安装 `deploy/systemd/26x-telegram.service` 并运行 `systemctl daemon-reload`。
3. 每个机器人只运行一个接收进程。从本机迁移时先停止原进程，使用 SQLite 的 `.backup` 导出数据库并恢复到目标服务的可写状态目录。库中包含已验证安装包对应的文件、推广统计和接收进度。使用 systemd 的服务身份写入，避免用 root 建立服务无法更新的数据库。
4. 启动 `systemctl enable --now 26x-telegram`，检查 `systemctl status 26x-telegram`。同时部署本次官网构建，即可在「代理推广」查看 Telegram 链接和统计明细。机器人统计接口监听本机 `127.0.0.1:8796`，由官网服务核验后台账号的角色后读取。

本地运行时设置 `TELEGRAM_TOKEN_FILE`、`TELEGRAM_POSTER_FILE` 和 `TELEGRAM_STATE_DIR`，执行 `python3 bot.py run`。首次配置机器人名称、头像和命令使用 `python3 bot.py profile`。

### 官网的普通 SSH 账号部署

官网使用 `tgdeploy` 的 systemd 用户服务。程序和头像位于 `~/.local/share/26x-telegram/app/`，SQLite 库位于同级的 `state/`。令牌文件为 `~/.config/26x-telegram/token`，权限为 0600。将本目录的 `26x-telegram.user.service` 安装为 `~/.config/systemd/user/26x-telegram.service`。迁移时先停止旧主机上的接收进程，再导出 SQLite 最终快照。

以部署账号运行 `loginctl enable-linger`，然后运行 `systemctl --user daemon-reload` 和 `systemctl --user enable --now 26x-telegram.service`。用 `loginctl show-user tgdeploy -p Linger` 核对返回值为 `yes`，再用 `systemctl --user status 26x-telegram` 查看运行状态。这样即使所有 SSH 会话都已断开，服务也能在服务器上持续运行并随服务器启动。

## 更新安装包

官网发布新版后，机器人按最新下载地址、尺寸和文件版本标识查找已经存入 Telegram 的安装包。小于上传上限的包会在用户首次领取时上传。较大的包需要先用 Telegram 客户端发送一次。新包正在同步时，机器人提供官网中的同一版本下载地址。

使用机器人运行账户，在同一个状态目录下执行以下命令，文件名须与官网下载名一致。

```sh
python3 bot.py prepare-release android /path/to/current-release.apk
python3 bot.py prepare-release macos /path/to/current-release.dmg
python3 bot.py prepare-release windows /path/to/current-setup.exe
python3 bot.py pair-imports
```

每条 `prepare-release` 命令将本地文件的 SHA-256 与官网文件核对。将最后输出的 `/upload …` 命令私发给机器人，看到提示后将核对过的原文件拖入同一私聊。机器人会逐份回复文件名。批次绑定到首个使用上传凭据的私聊，每份仅接收一次，准备后 24 小时失效。请使用受信任的发布账号，私下保存上传凭据。

## 推广统计

后台生成的 Telegram 链接使用代理的 16 字符推广码。按链接分别统计启动、选择平台、点击领取，以及 Telegram 成功发出文件或 TestFlight 链接的次数。人数使用按推广码区分的 HMAC 标识去重。终端安装、文件下载完成与会员购买需由各自的业务系统核验。

`python3 -m unittest discover -s deploy/telegram` 可在仓库根目录运行离线测试。
