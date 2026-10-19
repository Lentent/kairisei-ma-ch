# 金闪闪龙资源接入

`tools/resources/restore-golden-dragon.py` 将现有资源中的金闪闪龙接入活动 Boss 目录和自定义 Boss 的复制模板。无需重新编译服务端或更换 APK。

| 副本 ID | 难度 | 原始编队 ID |
| --- | --- | --- |
| 30110202 | 中级 | 30110201 |
| 30110203 | 上级 | 30110202 |
| 30110205 | 超级 | 30110203 |
| 30110206 | 超弩级 | 30110204 |

原始编队以连续序号命名，副本入口采用难度序号，不能直接按同 ID 关联。补丁在服务端副本记录中明确指定编队，不修改原编队、部位数量、技能、行动条件、异常抗性或攻击数值。超弩级本体／腕／背部 HP 分别为 2000000／600000／400000。

当前资源缺少原模型 `emt_00134`。按选定的兼容方案，11 条金闪闪龙敌人记录的模型字段改用现有 **日服·セイントドラゴン（31960111）** 使用的 `emt_00104`，副本头像也沿用该来源。保留原来的金闪闪龙名称、数值和 AI；这不代表恢复了原版金闪闪龙美术。客户端 `container.dat` 的 `enemy.csv` 和服务端 CSV 同步更新，其他客户端 master 对象逐字节保持不变。

默认奖励采用当前资源集的本地奖励规则及同属性活动卡兜底，未宣称恢复官方掉落。可在后台修改掉落。原攻略中背部清减益、中毒削弱强化等内容不在此次接入范围；行动仍以原始金闪闪龙 AI 为准。

## 应用到部署资源

使用 Python 3.10 或更新版本，安装 `tools/resources/requirements.txt` 中固定的 UnityPy 依赖。脚本与 `hide-settings-github.py` 放在同一目录。

```sh
python -m pip install -r tools/resources/requirements.txt
python tools/resources/restore-golden-dragon.py /path/to/resource-set --check
# 正常停止服务后应用
python tools/resources/restore-golden-dragon.py /path/to/resource-set --apply
```

工具读取目标服务器自己的当前资源集，核对相关文件与清单哈希，生成客户端和服务端修改，同步更新资源 CRC、版本索引及清单。自动备份到资源集上一级的 `golden-dragon-backups/<时间>/`；写入失败时恢复原文件。不要将另一台服务器生成的旧清单直接覆盖到当前部署。

也可使用 `--output /path/to/new-export` 导出六份增量文件及验证记录，供同一源快照部署。导出目录必须尚不存在。没有专用模型时不会把其他模型宣称为原版美术。

应用后按原方式同步 CDN（如有），启动服务端，客户端完全退出后重新登录以更新资源。在后台「战斗与活动 → 自定义 Boss → 新建 Boss」搜索“金闪闪龙”，选择难度复制。活动入口仍遵循后台现有发布名单和排期。

## 验证

- `CN602_GOLDEN_DRAGON_SET=/path/to/resource-set` 下运行 `go test ./internal/admin -run TestGoldenDragonRestoredTemplates`，核验四个难度的可见、复制、超弩级数值、原封印招式与重启恢复。
- 现有 `TestImportedBossExecution` 可用隔离编队清单核验四套原编队在多回合、低 HP 和全破情形的执行与协议编码。
- `CN602_RUNTIME_SET=/path/to/resource-set` 下运行 `TestCompleteRuntimeSetConstruction`，检查完整资源构造。

上述检查不替代手机上对模型、技能演出和部位点击位置的实战验收。
