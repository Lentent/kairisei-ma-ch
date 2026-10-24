# 名声培养锁定提示

点击名声培养的“开始培养”按钮时，`Dev.OnDev` 从 `CardMgr` 读取所选卡牌当前的 `is_lock`。非零时使用原有弹窗显示“卡牌已锁定，请先解锁后再进行名声培养。”，确认后关闭弹窗并停留当前页面，不发送培养请求、不扣除培养点数。未锁定时继续原有的名声数量检查和培养请求。未选择卡牌的原有提示保留；正在培养中的完成和取消入口保持原逻辑。

服务端 `StartCardFameTraining` 同时将锁定卡牌的失败改为 `ErrCardLocked`，通过现有业务错误协议提示解锁，避免返回 HTTP 400。卡牌不存在时返回 `ErrCardUnavailable`。解锁后可正常开始培养。

## 应用客户端补丁

这是托管客户端程序集补丁，需要从待发布 APK 提取 `assets/bin/Data/Managed/Assembly-CSharp.dll`，修改后放回同一位置，重新对齐并使用原证书签名。只更新服务端不会增加按钮点击时的本地检测。补丁脚本使用 Mono.Cecil 0.11.6，拒绝覆盖原文件、重复补丁以及不匹配的按钮方法布局。

```powershell
./tools/client/Patch-FameTrainingLock.ps1 -AssemblyPath ./original/Assembly-CSharp.dll -CecilPath ./Mono.Cecil.dll -OutputPath ./patched/Assembly-CSharp.dll
./tools/client/Verify-FameTrainingLock.ps1 -OriginalPath ./original/Assembly-CSharp.dll -PatchedPath ./patched/Assembly-CSharp.dll -CecilPath ./Mono.Cecil.dll
```

## 验证记录

- `go test ./internal/game ./internal/httpapi -count=1 -timeout=120s` 通过，覆盖锁定拒绝时不改卡牌／余额／培养状态、解锁后正常培养，以及业务错误响应协议。
- 本地 216 任务红点包的程序集经过补丁后，逐一比较 31889 个方法，仅 `Dev.OnDev` 改变。
- 对实际输出的 `OnDev` 指令执行 9 组模拟，覆盖未选卡牌、锁定／未锁定、零／负数培养名声、非零锁定值和缺失卡牌时原有逻辑。锁定提示使用 `ERROR_DIALOG_CB.CLOSE`，锁定分支不发送请求，未锁定有效选择仅发送一次请求。
- 本地验证输出在 `_local/fame-training-lock/`。已生成用于校验的未签名 APK，原包 1844 个非签名条目中仅 `Assembly-CSharp.dll` 改变，并通过 zipalign 校验。
- 本地底包版本为 6.0.8／60216，当前服务端最低要求为 6.0.9；此校验包不是当前版本的正式安装包。签名配置文件 `密钥/keystore-password.txt` 缺失，未生成签名安装包。正式发布时应将补丁应用到当前版本 APK，沿用其版本、资源和签名。
- 尚未安装到 Android 设备验证。实机验收需确认锁定卡牌弹出提示、确认后停留当前页面，解锁同卡后可以正常培养，已在培养的卡牌仍可完成和取消。
