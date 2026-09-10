# 从源码构建服务端

需要 Go 1.25 或更高版本；首次构建需要下载 go.mod/go.sum 锁定的依赖。服务端使用纯 Go SQLite，设置 CGO_ENABLED=0 即可交叉编译，不需要 C 编译器、Unity、Android SDK 或原游戏 APK。

在仓库根运行，默认先在本机执行测试和 vet，再生成 Windows x64、Linux x64、Linux ARM64 三个程序：

```powershell
# Windows PowerShell 5.1 或 PowerShell 7
.\tools\powershell\Build-Server.ps1
# 仅构建一个目标；已检查过的源码可加 -SkipTests
.\tools\powershell\Build-Server.ps1 -Targets windows-amd64 -SkipTests
```

```sh
# Linux / POSIX shell
sh tools/sh/Build-Server.sh
# 仅构建一个目标
sh tools/sh/Build-Server.sh linux-arm64 --skip-tests
```

输出均在 `_local/bin/`：`kairi-server.exe`、`kairi-server-linux-amd64`、`kairi-server-linux-arm64`。
构建会覆盖该目录中上次的同名编译结果；PowerShell 加 `-DryRun`、shell 加 `--dry-run` 可预览。
二进制内已包含运营后台网页、SQLite、可选 S3/R2 同步功能，不需要单独构建前端或安装 Python。

也可直接执行：

```sh
cd server
go test ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -trimpath '-ldflags=-s -w' -o ../_local/bin/kairi-server-linux-arm64 ./cmd/kairi-server
```

源码内的合成协议与领域测试不需要游戏资源。需要原始 master / 完整运行资源的检查会明确跳过；有完整资源时可设置 `CN602_RUNTIME_SET` 为含 `resource-set.json` 的目录再运行测试。这不会启动常驻服务。通过交叉编译不代表目标设备的动态验收已经完成。

## Windows 图形启动器

```powershell
.\tools\powershell\Build-Launcher.ps1
# 可选：使用另行取得的发布包内图标
.\tools\powershell\Build-Launcher.ps1 -IconPath .\_local\deployment\kairisei-ma-cn602-server\resource-set\launcher.ico
```

使用 Windows 自带 .NET Framework 4 的 C# 编译器，输出 `_local/bin/Kairisei-Launcher.exe`。不传图标也能独立构建；图标和游戏资源不存入源码仓库。

## 使用构建结果

运行仍需要另行取得的、同一版本的完整服务端资源包。源码仓库不包含游戏 APK、原版资源、账号存档、密钥，也不承诺从源码生成这些游戏资源或 APK。

将所有 RAR 分卷放在一起，从 part1 完整解压，例如放到 `_local/deployment/kairisei-ma-cn602-server/`。正常停止该包的服务端后，将 `_local/bin/` 中对应系统的程序覆盖到解压目录。资源仍使用整套 `resource-set/` 与配套 `deployment.json`；不要再使用旧版 `_local/resources` 的复制方式，也不要更改资源清单来适配源码。

Windows 可继续使用解压目录里的启动器，或从源码仓库调用发布包当前的启停脚本：

```powershell
.\tools\powershell\Start-Server.ps1 -AdvertiseHost 192.168.1.100
.\tools\powershell\Stop-Server.ps1
# 其他解压位置可加 -PackageRoot '完整解压目录'
```

测试资源包仍需按包内说明传 `-ValidationOnly`。停止使用发布包的正常关闭通道，等待 SQLite 写入完成。

Linux 更换自己编译的程序后，应只更新 `linux-startup.sha256` 中对应程序的 SHA-256 行（用 `sha256sum kairi-server-linux-arm64` 或 amd64 文件生成），保留其余启动文件与资源的校验行，再使用该包的 Linux 启动脚本。修改服务端代码不会改变 CDN 资源内容，不需要重复上传资源。

## 配置与持久化

`server/config/` 是默认配置；实际运行以完整资源包的配置入口初始化。运营后台编辑的卡池、Boss 掉落、商店／兑换、公告、奖励、购买开关等保存到 `_local/data/` 中的 SQLite 公共配置，重启保留，不需重新编译；CDN 和监听地址使用部署文件。更新程序保留自己的数据库和部署配置，不提交到 Git。
