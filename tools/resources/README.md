# 清空设置页 GitHub 地址文字

`hide-settings-github.py` 修改服务端下发的 `main_c/ui/ui_option.dat`，只清空设置页地址标签的 `mText`。无需修改 APK，也无需编译 Go。

工具读取部署中的资源和索引，保留其他 Unity 对象、标签的其他字段及其他资源的索引。同步更新该 bundle 的 CRC、存储标志、`version.dat`、`asset-map.json` 与 `resource-set.json`。不会用制作时的旧资源索引覆盖当前资源集。

## 本地资源与服务器不同，能否使用

可以。输入必须是要修改的服务器当前资源集。脚本不读取你电脑的资源，不要求两套文件的大小、哈希或自定义内容相同。路径从该服务器的 `resource-set.json` 解析，修改的是服务器自己的 UI 和索引。

不要将本地旧 `version.dat`、`asset-map.json` 或 `resource-set.json` 覆盖到服务器。推荐直接在服务器运行 `--check`、停服后运行 `--apply`，这样无需下载完整资源，也不会混入电脑上的资源。

如果需要在电脑上处理，可从服务器下载下面四个文件，按服务器原有相对路径放在一个新的资源目录中：

- `resource-set.json`
- 清单中 `cn-asset-map` 指向的文件，通常是 `asset-map.json`
- 清单中 `cn-patch-root` 指向的目录下的 `version.dat`
- 同一目录下的 `main_c/ui/ui_option.dat`

工具只需要这四份文件，就可以用 `--output` 生成增量修改；不需要读取卡牌、皮肤、语音等其余资源文件。导出后，目标服务器的这四份源文件必须仍与制作时一致，才能覆盖，否则应重新下载并制作。验证报告中的 `source_files` 记录了制作时的源文件 SHA-256。

支持的资源格式是 `cn602-bootstrap`：资源清单 schema 1、资产索引 schema 2，以及含原设置页标签的 UnityFS bundle。`--check` 会验证服务器这四份文件相互一致、标签可识别，且只改动目标 `mText`。文件格式或目标标签不兼容时，工具拒绝修改，不能凭“和本地不同”直接认定兼容。

## Ubuntu 服务端安装

把本目录中的文件放到 `/opt/kairisei/settings-github-patch/`，不要放进 `resource-set/`。创建工具自己的 Python 环境并安装固定版本依赖：

```sh
cd /opt/kairisei/settings-github-patch
python3 -m venv .venv
.venv/bin/python -m pip install -r requirements.txt
.venv/bin/python hide-settings-github.py /opt/kairisei/resource-set --check
```

如 Ubuntu 提示缺少 venv，先安装 `python3-venv`。检查成功后正常停止游戏服务，再应用：

```sh
.venv/bin/python hide-settings-github.py /opt/kairisei/resource-set --apply
```

脚本自动备份四个原文件到 `resource-set/` 同级的 `settings-github-backups/<时间>/`。任何替换失败都会尝试恢复已经替换的文件。依赖只供工具使用，不影响服务端运行环境。

完成后启动服务，客户端退出并重新登录以检查新资源。启用了 CDN 的部署还需在启动服务前执行原有的 `Sync-CDN.sh` 同步新资源；新 bundle 的下载 URL 使用新的 CRC，旧资源对象无需删除。手机验收：打开设置页，确认地址文字消失，其他设置控件正常。

## 导出增量文件

如需先制作文件供审核或由运维覆盖，使用服务器当前的资源集，也可使用上面四份服务器文件的快照：

```sh
.venv/bin/python hide-settings-github.py /opt/kairisei/resource-set --output /opt/kairisei/settings-github-export
```

输出包含四个资源文件和 `validation.json`。该导出只能覆盖制作时相同的资源集；若目标资源后续有变化，应重新运行工具。`--check` 不写文件。不要在游戏服务运行或资源同步进行中执行 `--apply`。

遇到不支持的标签、哈希不一致或 CAB 不匹配，工具会拒绝修改。针对当前资源的文本检查及其他对象逐字节对比通过，才会输出或应用补丁。重复执行仍会校验索引；已经清空的文字不会再次改动 bundle。
