# 服务端业务配置与目录

| 文件 | 实际用途 |
| --- | --- |
| `cn602-save-template.json` | 新账号初始数据和版本化业务目录 |
| `cn602-player-progression-runtime.json` | 经验／等级、BP／好友上限、职业基础属性和助战友情点 |
| `cn602-login-bonus-runtime.json` | 登录日界及每日、新手、累计奖励的默认表 |
| `cn602-pvp-runtime.json` | PVP 场地、段位、挑战和结算规则；`server_replay` 专用字段不影响 `client_native_local` 模式 |

升级补满 AP／BP 并重置恢复计时。`config_version` 控制业务数据更新。
签到／抽卡奖励和经验曲线采用本地运营策略。

## 修改后何时生效

- 四份 `cn602-*.json` 从启动参数指定的完整资源集中加载；修改后重新构建资源包并重启服务。
- 后台已保存的运营设置优先于对应默认值；账号数据和后台配置保存在 SQLite。
- 发布时配套更新服务端和资源包，保留原数据库。
