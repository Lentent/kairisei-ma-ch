# Linux 兑换域名接入

目标入口：`https://cdk.ccgduckcloud.org/cdk`。

当前仓库已具备网页和兑换接口；这里提供连接配置，还未连接实际 Linux 服务器或修改 Cloudflare。服务器需要运行包含本次 CDK 功能的新版本。当前按游戏 HTTP 默认端口 `26020` 配置；实际端口不同须修改模板。无需重打 APK。

## 在 Linux 游戏服务器准备

先确认本机页面可访问：

```sh
curl -fsS http://127.0.0.1:26020/cdk -o /dev/null
```

安装适合服务器发行版与架构的 `cloudflared`，参考 [Cloudflare 官方安装与配置说明](https://developers.cloudflare.com/tunnel/get-started/)。以下采用本地管理的隧道，参见 [官方本地隧道流程](https://developers.cloudflare.com/tunnel/features/locally-managed-tunnels/create-local-tunnel/)。

以管理员身份在 Linux 运行：

```sh
cloudflared tunnel login
cloudflared tunnel create ma-cdk
```

第一条会提供授权链接，打开链接，选择 `ccgduckcloud.org` 所属区域。第二条输出隧道 UUID 与凭据 JSON 路径。

把仓库 `tools/portable/cloudflared-cdk.example.yml` 放到 `/etc/cloudflared/cdk.yml`，替换其中两处 UUID，并把生成的凭据 JSON 放到模板指定路径。设置凭据权限为仅管理员可读。模板只代理 `/cdk`、`/cdk.js`、`/api/cdk/redeem`，其他请求返回 404。

```sh
cloudflared tunnel --config /etc/cloudflared/cdk.yml ingress validate
cloudflared tunnel route dns ma-cdk cdk.ccgduckcloud.org
cloudflared tunnel --config /etc/cloudflared/cdk.yml run ma-cdk
```

域名必须属于当前授权的 Cloudflare 区域。如果已存在同名 DNS 记录，先核对其用途，避免覆盖现有服务。保留外部 Host 为 `cdk.ccgduckcloud.org`，不要改成 `localhost`，否则兑换接口的同源校验会拒绝请求。

浏览器打开 `https://cdk.ccgduckcloud.org/cdk`，先确认页面加载，再用测试角色兑换测试码；核对后台兑换记录及游戏邮件。不要为这些路径添加缓存全部内容的规则。游戏内兑换入口和网页共用领取记录。

验证后，如这台服务器尚无 cloudflared 系统服务，可安装后台运行服务：

```sh
cloudflared --config /etc/cloudflared/cdk.yml service install
systemctl enable --now cloudflared
systemctl status cloudflared
```

若已有 cloudflared 服务，应合并入口配置并保留原隧道设置，而不是再次安装服务。若游戏服务运行在容器中，隧道进程应能够访问配置中的回源地址，不能直接假定容器内的 `127.0.0.1` 就是游戏宿主机。

网页当前按连接来源 IP 限流；经单个本机隧道代理后，网页用户共用代理 IP 的每分钟 12 次上限。小规模验收可以使用该配置；多人正式开放前应接入可信代理下的真实客户端 IP 配置。原生游戏内兑换仍按角色限流。
