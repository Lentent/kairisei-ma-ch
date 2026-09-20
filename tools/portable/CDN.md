# 可选 CDN 使用说明

默认从自己的服务端下载资源，无需配置 CDN。启用后仍需保留完整的 `resource-set` 目录。

## 配置与同步

在服务端解压目录，把 `cdn-sync.example.json` 复制为 `cdn-sync.json`，填写：

```json
{
  "endpoint_url": "https://你的账户ID.r2.cloudflarestorage.com",
  "bucket": "kairisei",
  "access_key_id": "你的访问密钥ID",
  "secret_access_key": "你的机密访问密钥",
  "public_url": "https://pub-你的编号.r2.dev"
}
```

以上为 R2 示例，也可使用兼容 S3 的存储。`endpoint_url` 填上传地址，`public_url` 填桶的公开下载根地址，不加 `/cn602`。
先开启桶的公开读取，并给密钥授予该桶的对象读写权限。不要分享填写了密钥的配置文件。

- Windows：双击 `Sync-CDN.cmd`。
- Linux：运行 `sh Sync-CDN.sh`。
- 只检查本地配置：在以上命令末尾添加 `--dry-run`，不上传文件。

无需安装其他工具。再次运行会跳过已完成的文件；失败或中断后可直接重跑。
同步成功后会自动写入 `cdn.json`，重启服务并让客户端重新登录即可生效。
同步失败时保留原配置，操作记录位于 `_local/cdn-sync/`。

## 更换地址或关闭

资源已上传时，可直接修改 `cdn.json` 的 `base_url`，填写完整资源地址，例如：

```json
{"base_url":"https://你的公开下载域名/cn602"}
```

把 `base_url` 改为空字符串即可关闭 CDN。修改后重启服务、重新登录。
CDN 故障时不会自动切回服务端下载，需要手动关闭配置。

## 常见问题

- 下载域名必须允许匿名下载，支持分段下载，禁止对文件转码或改写。手机也需要能访问该域名。
- 上传提示同名文件冲突时，不要覆盖正在使用的资源；换一个 `prefix` 后重新同步。默认 `prefix` 为 `cn602`。
- 需要代理时，运行前设置 `HTTPS_PROXY`；直连例外使用 `NO_PROXY`。脚本不读取 Windows 系统代理设置。
  PowerShell 示例：`$env:HTTPS_PROXY='http://127.0.0.1:7890'`，然后运行 `.\Sync-CDN.cmd`。
  Linux 示例：`HTTPS_PROXY=http://127.0.0.1:7890 sh Sync-CDN.sh`。
- 可在 JSON 中设置 `workers` 调整上传并发，范围 1～32，默认 8。其他存储可按厂商要求设置 `region` 和 `addressing_style`。
