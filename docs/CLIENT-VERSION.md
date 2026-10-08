# 1.3.7 服务端与 6.0.8 客户端

服务端程序版本从 `1.3.6` 升为 `1.3.7`，最低连接版本从 `6.0.7` 升为 `6.0.8`。服务器列表和旧客户端更新提示沿用同一个最低版本常量，登录校验会在创建账号或会话前拒绝 `6.0.7`。

配套 APK 以最近的 `143本地包.apk` 为底包，AndroidManifest.xml 的 `versionName` 从 `6.0.7` 升为 `6.0.8`，`versionCode` 从 `60215` 升为 `60216`。登录的 `clver` 经 `DeviceInfo.GetAppVersion` 读取 Android `PackageInfo.versionName`，因此也同步为 `6.0.8`。

`tools/client/Patch-ApkVersion.cjs` 按二进制 XML 的 manifest 属性定位版本，检查原值和递增的版本码，仅支持编码长度相同的新旧版本名称，保留字符串池大小和资源偏移。先从 APK 提取清单，再执行：

```sh
node tools/client/Patch-ApkVersion.cjs AndroidManifest.original.xml AndroidManifest.xml 6.0.7 6.0.8 60215 60216
```

将输出清单替换回 APK 后，重新对齐并使用原密钥签名。保留包名 `com.netease.ma.netease` 和原签名，递增版本码后可覆盖同签名旧包，无需卸载；底包原有本地地址、账号、Boss 返回页及服务器设置按钮移除补丁保留。

本地输出：

- `_local/version-1.3.7/kairi-server.exe`
- `_local/version-1.3.7/client-6.0.8-60216.apk`
- `_local/version-1.3.7/validation.json`

这些构建产物由仓库的忽略规则排除，APK、服务端二进制和签名密钥不存入源码 Git。正式发行包通过发布流程分发。

已通过 `go test ./cmd/kairi-server ./internal/cnbootstrap -count=1 -timeout=120s`，编译后的 `-version` 输出为 `kairi-server 1.3.7`。APK 的包名、版本、v1/v2/v3 签名及对齐校验通过，原包和新包的 1844 个非签名条目逐项比较，仅 AndroidManifest.xml 改变。尚未实机安装验证。
