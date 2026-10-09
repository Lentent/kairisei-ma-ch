# 1.3.1 本地修改

上游基线：33a5733（Fix multiplayer lifecycle and entry fees; release server 1.3.1）。
将 banner-source-1.3.0 中的本地修改按补丁迁入，保留 1.3.1 的多人生命周期、入场扣费等改动。

## 横幅自动更新

- 主页右上横幅、主页活动横幅、各卡池横幅在启动时分别计算 SHA256，图片下载地址带内容版本。
- 读取启动时图片副本，运行期间同一版本地址的内容不变。替换图片完成后重启，再让玩家重新登录。
- 磁盘路径与文件名保持原样；使用 PNG，单张不超过 4 MiB。
- Windows 的 tools/portable/Read-ResourceSet.ps1 已允许指定的25个横幅路径改变内容和大小；其他资源继续校验。无需修改资源清单中的哈希或大小。
- 备份和上传临时文件放在 resource-set 目录外。不要把备份额外登记到资源清单。
- Linux 原启动脚本没有上述资源清单校验，无需添加 Windows 脚本。

代码：server/internal/cnbootstrap/banner_assets.go、resource_runtime.go、bootstrap.go、router.go；server/internal/httpapi/banner_urls.go、router.go、home_handlers.go、gacha_handlers.go。

## 公告

保留多公告编辑、上架/下架、置顶、排序、正文直接展开与去目录样式。
同时迁入旧工作目录中后续增加的新公告主页自动提示、按角色记录已读、签名链接及日志隐藏 token 的处理。具体行为见 NOTICES.md。

## 部署

本次修改的是源码，没有替换正在运行的服务端，也没有重新生成发布程序包。
应从本仓库重新编译 1.3.1，并按上游 1.3.1 的发布要求准备资源与 APK，不要安装旧的 1.3.0 修改程序包。
Windows 还需将 tools/portable/Read-ResourceSet.ps1 复制到服务端根目录覆盖对应脚本。

## 检查

本次迁移已通过 `go test ./...`、`go vet ./...` 和 `git diff --check`。Windows 资源校验脚本与此前通过 Windows PowerShell 5.1 验证的修正版一致。未进行 Android 实机或线上服务器验收。
