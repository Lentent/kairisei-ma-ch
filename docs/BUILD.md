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
运行 `kairi-server.exe -version`（Linux 使用对应程序）可查看服务端版本；此命令直接退出，不启动服务或打开存档。

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

**RAR 解压出来的整个文件夹就是运行目录，里面的 `resource-set/` 就是游戏资源。源码目录只用于编译。**

1. 选择与源码版本配套的完整服务端资源包，将所有 RAR 分卷放在一起，从 `part1.rar` 解压。
2. 保留解压出来的整个 `kairisei-ma-cn602-server/` 文件夹，放在哪里都可以，例如 Windows 的 `D:\Games\kairisei-ma-cn602-server\`。
3. 正常停止服务，将源码编译得到的 `_local/bin/` 中对应程序复制到这个文件夹，替换同名程序，再从这个文件夹启动。

解压后的相对位置应保持如下（只列出主要文件）：

```text
kairisei-ma-cn602-server/       ← 运行目录，也是 -PackageRoot 指向的位置
├── kairi-server.exe            ← Windows：替换为自己编译的这个文件
├── kairi-server-linux-amd64    ← Linux x64：替换这个文件
├── kairi-server-linux-arm64    ← Linux ARM64：替换这个文件
├── Kairisei-Launcher.exe       ← Windows 图形启动器
├── Start-Server.ps1
├── Stop-Server.ps1
├── Start-Server-linux-amd64.sh
├── Start-Server-linux-arm64.sh
├── deployment.json            ← 保留包内配套文件
├── resource-set/               ← 保留解压出来的整个资源目录
│   ├── resource-set.json
│   └── ...                    ← 其余资源及配置，保持原来的内部路径
├── cdn.json                    ← 可选 CDN 下载配置
└── _local/data/                ← 首次启动后生成的数据库与存档
```

例如，Windows 将 **源码目录的 `_local/bin/kairi-server.exe`** 复制到 **`D:\Games\kairisei-ma-cn602-server\kairi-server.exe`**，然后双击同目录的 `Kairisei-Launcher.exe` 即可。

**`resource-set/` 与 `deployment.json`、服务端程序放在同一层。** 不需要把资源复制进源码的 `server/` 或 `_local/bin/`，也不需要单独拆开资源目录。发布版本升级时一起更新配套服务端、资源包和客户端，保留 `_local/data/` 中的数据库与自己的部署配置。

Linux 同样替换自己系统对应的程序，再使用包内对应的 Linux 启动脚本；保留配套的 `Start-Server-linux.sh` 与 `server-arguments.sh`。

### 可选：从源码目录调用 Windows 启停脚本

如果使用图形启动器，无需执行这一节。命令在**源码仓库根目录**运行，`-PackageRoot` 填上面整个运行目录，不能填它里面的 `resource-set/`：

```powershell
.\tools\powershell\Start-Server.ps1 -PackageRoot 'D:\Games\kairisei-ma-cn602-server' -AdvertiseHost 192.168.1.100 -ValidationOnly
.\tools\powershell\Stop-Server.ps1 -PackageRoot 'D:\Games\kairisei-ma-cn602-server'
```

上例适用于当前测试包，`-ValidationOnly` 是测试包启动所需参数。省略 `-PackageRoot` 时，脚本默认查找源码仓库下的 `_local/deployment/kairisei-ma-cn602-server/`；这只是默认位置，不要求把已有服务端搬过去。

源码仓库不附带 APK 和游戏资源，构建脚本只生成服务端程序；资源直接使用完整发布包解压得到的 `resource-set/`。

## 配置与持久化

`server/config/` 是默认配置；实际运行以完整资源包的配置入口初始化。运营后台编辑的卡池、Boss 掉落、商店／兑换、公告、奖励、购买开关等保存到 `_local/data/` 中的 SQLite 公共配置，重启保留，不需重新编译；CDN 和监听地址使用部署文件。更新程序保留自己的数据库和部署配置，不提交到 Git。
