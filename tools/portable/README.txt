乖离性百万亚瑟王 · 服务端使用说明

本项目永久免费：https://github.com/kuuhaku1314/kairisei-ma-ch

启动服务
将同一版本的全部 RAR 分卷放在一起，从 part1.rar 完整解压。
一套包包含 Windows x64、Linux x64、Linux ARM64 程序和完整游戏资源。
Windows：打开 Kairisei-Launcher.exe，选择模拟器或局域网，点击“启动服务”。
Windows 命令行也可运行 Start-Server.cmd，结束时运行 Stop-Server.cmd。
Linux：按 CPU 选择脚本，将地址换成手机能访问到的服务器 IPv4：
  sh Start-Server-linux-amd64.sh 192.168.2.149 26020
  sh Start-Server-linux-arm64.sh 192.168.2.149 26020
Linux 末尾加 --dry-run 可检查参数；Ctrl+C 或 SIGTERM 正常停止。
无需 Go、Python、Unity 或 Android SDK；Windows 使用系统 PowerShell，Linux 需要 sh 和 coreutils。
默认游戏端口 26020、战斗端口 26021。手机与电脑处于同一局域网，并放行这两个 TCP 端口。
Admin 默认 http://127.0.0.1:26022/，仅允许在服务器本机访问。

安装与连接
安装同次发布的 APK；APK 在服务端分卷之外单独提供，可覆盖安装。
QQ 可能打开旧的同名 APK，请下载后用文件管理器打开本次文件安装。
首次打开授予资源存储权限，先进入标题页创建 Android/data 下的目录，再退出。
手机用 ZArchiver 等文件管理器将 local_server.txt 复制到：
  Android/data/com.netease.ma.netease/files/local_server.txt
文件只填一行服务器 IPv4:端口，例如 192.168.2.149:26020；同机 Android Emulator 使用 10.0.2.2:26020。
不读取 Download 下的地址文件。修改后重新打开游戏。
首次下载末尾若停在进入游戏页面，关闭游戏再打开即可，无需清除资源。

账号与存档
标题右下角“用户中心”可打开账号窗口；已绑定角色正常点击标题时直接进入游戏。
新玩家选择游客进入；已有角色可在窗口绑定，或在游戏内“菜单 → 设置 → 用户中心”绑定。
重装或换设备后先配置原服务器地址，再回标题账号窗口输入账号和密码登录。
游戏内用户中心用于绑定，切换或找回请回标题。忘记密码可请管理员在“账号管理”重置。
服务端进度保存在 _local/data/；绑定账号不能找回已删除的服务器存档。
更新前正常停服并备份，再更新程序和配套 resource-set，保留自己的存档与配置。
当前数据库格式为 schema3，同格式更新无需清档；不加载更早格式的数据库。
首次启动自动创建数据库；包内不含已有账号。不要让多个服务端进程同时打开同一份存档。
resource-set 是完整运行资源，请保留整个目录。日志位于 _local/runtime/。

后台配置
除新手单抽和新手连抽外，全部内置卡池均可在后台调整并开关。
「Boss 掉落」按难度、怪物或部位配置奖励和概率，可复制到同组难度。
「兑换所配置」可复制店铺、批量添加商品、改价、限购和上下架，也可按现有配方补齐强化／进化素材草稿。
这些配置保存到 SQLite 公共运营配置，重启保留；已有兑换次数保留，正在进行的战斗沿用原掉落计划。
「公告与奖励」可修改公告、签到奖励、新账号初始资源、剧情首通水晶、看板价格和购买开关。
普通签到每7次循环，新手签到只发一轮，累计登录独立发放；修改配置不清除已领取记录。
初始资源只影响新账号，关闭看板购买不回收已有角色；无需重新同步 CDN。
Boss 发布中的起止时间目前仅控制活动目录展示，不是逐 Boss 独立排期或严格入场截止。

可选 CDN
默认关闭，资源直接从自己的服务端下载。
复制 cdn-sync.example.json 为 cdn-sync.json，填写其中五项配置。
Windows 双击 Sync-CDN.cmd，Linux 运行 sh Sync-CDN.sh，无需安装其他工具。
增量同步并检查成功后会启用 CDN；重启服务、重新登录生效。详见 CDN.md。
同步继承 HTTP_PROXY、HTTPS_PROXY、NO_PROXY 环境变量；配置中的密钥请勿转发。
