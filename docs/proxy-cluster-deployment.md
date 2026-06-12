# 代理集群部署与客户端接入手册

本文档记录当前代理集群的实际部署参数、新代理节点加入方法，以及客户端如何把本地端口代理到代理节点。

## 当前 bootstrap 节点

当前 bootstrap 节点：

```text
节点公网 IP: 124.71.154.57
SSH: ldc@124.71.154.57:22
控制面 Web/API: http://124.71.154.57:8088
frps 接入端口: 7000
业务对外代理端口范围: 11000-12000
```

注意：

- `8080` 已被 bootstrap 机器上的其他服务占用，因此 frp-cluster 控制面使用 `8088`。
- bootstrap 机器只有 `11000-12000` 作为业务对外代理端口。客户端配置里的 `remotePort` 必须从这个区间里选择。
- 新代理节点也建议使用 `8088` 作为控制面端口，使用 `7000` 作为 frps 接入端口。
- 云厂商安全组和机器防火墙需要放行控制面端口、frps 端口和业务对外端口。

## 端口规划

bootstrap 节点至少需要放行：

```text
TCP 22       SSH 管理
TCP 8088     frp-cluster Web/API 控制面
TCP 7000     frps 接入端口
TCP 11000-12000 业务对外代理端口
```

如果业务对外端口没有放行，即使客户端成功连接 frps，外部用户也无法访问 `124.71.154.57:11000-12000`。

## 一、新代理节点加入集群

以下操作在“新代理服务器”上执行。

### 1. 准备安装包

把对应架构的安装包复制到新代理服务器，例如 x86_64/amd64 机器使用：

```bash
scp frp-cluster-bundle-linux-amd64.tar.gz 用户名@新节点IP:/tmp/
```

在新节点上解压：

```bash
cd /tmp
tar -xzf frp-cluster-bundle-linux-amd64.tar.gz
cd frp-cluster-bundle-linux-amd64
```

### 2. 在 bootstrap 节点生成加入 token

在任意已安装 `frp-cluster` CLI 的机器上执行：

```bash
JOIN_TOKEN=$(frp-cluster token \
  --control-url http://124.71.154.57:8088 \
  --ttl 2h \
  --uses 1)

echo "$JOIN_TOKEN"
```

如果当前机器没有 `frp-cluster` 命令，也可以登录 bootstrap 节点执行：

```bash
ssh ldc@124.71.154.57

JOIN_TOKEN=$(frp-cluster token \
  --control-url http://127.0.0.1:8088 \
  --ttl 2h \
  --uses 1)

echo "$JOIN_TOKEN"
```

### 3. 创建新代理节点配置

在新代理节点的安装包目录中创建 `proxy-node.env`：

```bash
cat > proxy-node.env <<'EOF'
BOOTSTRAP_CONTROL_URL=http://124.71.154.57:8088
NODE_CONTROL_URL=http://新代理节点公网IP:8088
JOIN_TOKEN=替换为上一步生成的join_token

NODE_ID=edge-new
PUBLIC_ADDR=新代理节点公网IP

BIND_PORT=7000
ALLOW_PORTS=11000-12000
VHOST_HTTP_PORT=0
VHOST_HTTPS_PORT=0

CONTROL_LISTEN=:8088
CONTROL_DATA=/var/lib/frp-cluster/cluster.json
PUBLIC_ENTRY_HOST=ssh.buaadcl.tech
DNS_UPDATE_HOOK=/usr/local/bin/frp-cluster-alidns-update
WEB_DIR=/usr/local/share/frp-cluster/web
ADMIN_PASSWORD_FILE=/etc/frp-cluster/admin-password
AUTH_CONFIG_FILE=/etc/frp-cluster/auth.env
ALIDNS_CONFIG_FILE=/etc/frp-cluster/alidns.env

REGION=
TAGS=
AGENT_INTERVAL=30s
PROBE_SIZE=262144
EOF
```

需要替换：

```text
新代理节点公网IP
edge-new
替换为上一步生成的join_token
```

如果还没有正式域名，可以先把 `PUBLIC_ENTRY_HOST` 留空；确认域名后再写入 `/etc/frp-cluster/node.env` 并重启控制面：

```bash
sudo sed -i 's/^PUBLIC_ENTRY_HOST=.*/PUBLIC_ENTRY_HOST=ssh.buaadcl.tech/' /etc/frp-cluster/node.env
sudo sed -i 's#^DNS_UPDATE_HOOK=.*#DNS_UPDATE_HOOK=/usr/local/bin/frp-cluster-alidns-update#' /etc/frp-cluster/node.env
sudo systemctl restart frp-cluster-control
```

`NODE_ID` 必须全局唯一，例如：

```text
edge-124
edge-hk-1
edge-sg-1
edge-us-1
```

### 4. 执行一键加入

```bash
sudo ./scripts/proxy-node-join.sh ./proxy-node.env
```

脚本会完成：

- 安装 `frp-cluster` 到 `/usr/local/bin/frp-cluster`
- 安装 `frps` 到 `/usr/local/bin/frps`
- 写入 `/etc/frp-cluster/node.env`
- 生成 `/etc/frp/frps.toml`
- 限制 frps 只接受 `ALLOW_PORTS` 指定的业务远程端口，默认 `11000-12000`
- 安装并启用 systemd 服务：
  - `frp-cluster-control.service`
  - `frps.service`
  - `frp-cluster-agent.service`

### 5. 检查新节点状态

```bash
systemctl status frp-cluster-control frps frp-cluster-agent --no-pager
```

确认监听端口：

```bash
ss -ltnp | grep -E ':8088|:7000'
```

确认控制面健康：

```bash
frp-cluster health --control-url http://127.0.0.1:8088 --timeout 2s
```

期望输出：

```text
ok
```

### 6. 在 Web 端查看

打开任意可访问的控制面：

```text
http://124.71.154.57:8088/
http://新代理节点公网IP:8088/
```

如果浏览器打不开，但服务器本机 `frp-cluster health --control-url http://127.0.0.1:8088` 正常，优先检查云安全组和防火墙是否放行 `8088/tcp`。

首次打开控制面时，进入“绑定 Microsoft Authenticator”流程：

```text
1. 输入 /etc/frp-cluster/admin-password 中的初始化口令。
2. 用 Microsoft Authenticator 扫描页面二维码。
3. 输入 App 中显示的 6 位动态验证码完成绑定。
4. 后续登录只使用 Microsoft Authenticator 动态验证码。
```

控制面“阿里云 DNS”页面可以维护 `AccessKey ID`、`AccessKey Secret`、主域名、TTL、解析线路等配置；Secret 留空保存时会保留服务器上的旧值。控制面“运维向导”页面可以调整代理节点网络信息上报间隔 `AGENT_INTERVAL` 和主动测速大小 `PROBE_SIZE`，保存后按提示重启 `frp-cluster-agent` 生效。

## 二、客户端把本地端口代理到集群

客户端节点是运行 `frpc` 的业务机器。它会连接代理集群中的 frps，把本地端口暴露成代理节点公网端口。

示例目标：

```text
客户端本地服务: 127.0.0.1:8080
希望外部访问: 124.71.154.57:11000
```

对应代理规则：

```text
web:tcp:127.0.0.1:8080:11000
```

字段含义：

```text
web        代理名称
tcp        代理类型
127.0.0.1  客户端本地服务地址
8080       客户端本地服务端口
11000      代理节点对外端口
```

### 1. 准备客户端安装包

把安装包复制到客户端机器：

```bash
scp frp-cluster-bundle-linux-amd64.tar.gz 用户名@客户端IP:/tmp/
```

在客户端机器上解压：

```bash
cd /tmp
tar -xzf frp-cluster-bundle-linux-amd64.tar.gz
cd frp-cluster-bundle-linux-amd64
```

### 2. 创建客户端配置

把本地 `127.0.0.1:8080` 映射到代理节点公网 `11000`：

```bash
cat > client.env <<'EOF'
CONTROL_URL=http://124.71.154.57:8088
CLIENT_ID=client-app-1

MODE=failover
LIMIT=1
INTERVAL=30s
DRAIN_TIMEOUT=30s

PROXIES='web:tcp:127.0.0.1:8080:11000'

WORK_DIR=/var/lib/frp-cluster/frpc.d
FRPC_BIN=/usr/local/bin/frpc
EOF
```

注意：

- `PROXIES` 必须加单引号，因为多个代理规则会用分号分隔。
- `remotePort` 必须使用 `11000-12000` 之间的端口。
- `CLIENT_ID` 必须唯一，建议按业务命名，例如 `client-api-1`、`client-web-1`。

### 3. 安装客户端服务并开机自启

```bash
sudo ./scripts/client-install.sh ./client.env
```

脚本会完成：

- 安装 `frp-cluster` 到 `/usr/local/bin/frp-cluster`
- 安装 `frpc` 到 `/usr/local/bin/frpc`
- 写入 `/etc/frp-cluster/client.env`
- 安装并启用 `frp-cluster-client.service`
- 周期性从控制面拉取可用代理节点配置
- 启动并托管对应的 `frpc` 进程

### 4. 检查客户端状态

```bash
systemctl status frp-cluster-client --no-pager
```

查看生成的 frpc 配置：

```bash
ls -l /var/lib/frp-cluster/frpc.d/
sed -n '1,120p' /var/lib/frp-cluster/frpc.d/*.toml
```

正常情况下会看到类似：

```toml
serverAddr = "124.71.154.57"
serverPort = 7000

[[proxies]]
name = "web"
type = "tcp"
localIP = "127.0.0.1"
localPort = 8080
remotePort = 11000
```

### 5. 从外部访问业务

客户端服务启动后，外部访问：

```text
http://124.71.154.57:11000
```

就会转发到客户端机器本地：

```text
127.0.0.1:8080
```

如果访问失败，按顺序检查：

```bash
# 客户端本地服务是否正常
curl -i http://127.0.0.1:8080

# 客户端 frp-cluster 服务是否正常
systemctl status frp-cluster-client --no-pager

# 客户端是否生成 frpc 配置
ls -l /var/lib/frp-cluster/frpc.d/

# bootstrap 节点是否放行业务端口
# 需要在云安全组和系统防火墙放行 TCP 11000-12000
```

## 三、本机 SSH 映射到 124 节点 11022

本机当前已配置一个用户级 systemd 服务，把本机 `127.0.0.1:22` 映射到 `124.71.154.57:11022`。

当前配置：

```text
控制面: http://124.71.154.57:8088
客户端 ID: local-ssh
模式: failover
节点数量: 1
代理规则: ssh:tcp:127.0.0.1:22:11022
服务名: frp-cluster-local-ssh.service
配置目录: ~/.local/state/frp-cluster/local-ssh/frpc.d
```

本机查看服务：

```bash
systemctl --user status frp-cluster-local-ssh.service --no-pager
journalctl --user -u frp-cluster-local-ssh.service -n 100 --no-pager
```

本机生成的 frpc 配置位置：

```bash
sed -n '1,120p' ~/.local/state/frp-cluster/local-ssh/frpc.d/*.toml
```

124 节点上确认 frps 监听：

```bash
ssh ldc@124.71.154.57
sudo ss -ltnp | grep -E ':7000|:11022'
```

124 节点本机验证转发链路：

```bash
ssh-keyscan -p 11022 -T 5 127.0.0.1
```

能看到 `SSH-2.0-OpenSSH` 和 host key 输出，就表示链路已打通：

```text
124.71.154.57 frps:11022 -> 本机 frpc -> 本机 127.0.0.1:22
```

从其他外部机器访问：

```bash
ssh -p 11022 本机用户名@124.71.154.57
```

注意：这台本机当前到 `124.71.154.57` 的公网访问走 Mihomo/TUN。用本机自己访问 `124.71.154.57:11022` 会形成特殊回环路径，可能被本地 TUN 或代理策略关闭，不能作为有效外部连通性测试。用 124 节点本机访问 `127.0.0.1:11022`，或者用第三台外部机器访问 `124.71.154.57:11022` 才是有效测试。

## 四、DNS 稳定入口与管理端手动切换

客户端不要直接写死某一个 frps 配置文件，而是运行 `frp-cluster client` 常驻服务，并固定连接控制面：

```text
CONTROL_URL=http://124.71.154.57:8088
MODE=failover
LIMIT=1
INTERVAL=30s
DRAIN_TIMEOUT=30s
PROXIES='ssh:tcp:127.0.0.1:22:11022'
```

外部用户不要直接访问代理节点 IP，应访问稳定 DNS 名称。例如：

```text
ssh.buaadcl.tech:11022
```

DNS 解析由控制面切换 hook 更新。初始记录应指向当前节点：

```text
ssh.buaadcl.tech A 124.71.154.57
```

如果要把外部入口切到新代理节点，管理端切换会调用 hook 把 DNS 改成：

```text
ssh.buaadcl.tech A 新代理节点公网IP
```

客户端配置里的 `CONTROL_URL`、`CLIENT_ID`、`PROXIES` 都不需要变化。

工作方式：

1. 客户端每 `INTERVAL` 向控制面拉取配置。
2. 如果客户端已经在线，控制面会优先返回它当前所在节点的 frpc 配置。
3. 新代理节点加入后，即使网络评分更好，客户端也不会自动切过去。
4. 只有管理端显式选择目标节点后，控制面才会返回新节点配置。
5. `frp-cluster client` 收到新配置后启动新的 frpc，并在 `DRAIN_TIMEOUT` 后停止旧 frpc。

因此新增代理节点后，本地客户端配置不需要修改，也不会自动漂移。需要满足以下条件：

- 新代理节点已经按“一、新代理节点加入集群”完成加入。
- 新代理节点的 frps 接入端口 `7000` 对客户端出口 IP 放行。
- 新代理节点的业务对外端口 `11000-12000` 已在云安全组和防火墙放行。
- 所有节点都允许相同的业务远程端口，例如 `11022`。

### 1. 在管理端手动切换客户端节点

打开控制面：

```text
http://124.71.154.57:8088/
```

在“客户端与代理”表里：

```text
1. 找到客户端，例如 local-ssh。
2. 在目标节点下拉框里选择新代理节点，例如 edge-new。
3. 点击“切换并更新 DNS”。
4. 等待客户端下一次同步，默认最多 30 秒。
5. 确认 frpc 配置里的 serverAddr 已变成新节点公网 IP。
```

如果 `PUBLIC_ENTRY_HOST` 或 `DNS_UPDATE_HOOK` 未配置，管理端会拒绝切换，避免客户端已经切到新节点但外部稳定域名还指向旧节点。

切换前建议先在“代理服务器集群”表里对当前节点点击“DNS 自检”。这个动作会真实执行 `DNS_UPDATE_HOOK`，把稳定入口 DNS 指向所选节点。生产环境第一次配置 Hook 时，先选当前正在承载流量的节点自检，避免自检本身改变入口。

也可以用 API 自检 DNS Hook：

```bash
curl -X POST http://124.71.154.57:8088/api/v1/dns/test \
  -H 'content-type: application/json' \
  -d '{"node_id":"edge-124","client_id":"local-ssh"}'
```

成功后再确认 DNS 已解析到该节点公网 IP：

```bash
dig +short ssh.buaadcl.tech
```

也可以用 API 手动切换并更新 DNS：

```bash
curl -X POST http://124.71.154.57:8088/api/v1/clients/local-ssh/target \
  -H 'content-type: application/json' \
  -d '{"node_id":"edge-new"}'
```

非空 `node_id` 会强制先执行 DNS 更新 hook；hook 失败则不会写入客户端目标，客户端也不会切换。目标节点必须在线，否则控制面也会拒绝切换。

清除手动目标：

```bash
curl -X POST http://124.71.154.57:8088/api/v1/clients/local-ssh/target \
  -H 'content-type: application/json' \
  -d '{"node_id":""}'
```

### 2. 配置 DNS 更新 Hook

控制面切换前会执行 `DNS_UPDATE_HOOK` 指向的脚本。脚本成功退出后，控制面才会写入客户端手动目标。

Hook 会收到这些环境变量：

```text
FRP_CLUSTER_DNS_HOST       稳定入口域名，例如 ssh.buaadcl.tech
FRP_CLUSTER_DNS_TARGET_IP  目标代理节点公网 IP
FRP_CLUSTER_NODE_ID        目标代理节点 ID
FRP_CLUSTER_CLIENT_ID      被切换的客户端 ID
```

阿里云 DNS 配置文件放在 `/etc/frp-cluster/alidns.env`，权限建议 `0600`：

```bash
sudo tee /etc/frp-cluster/alidns.env >/dev/null <<'EOF'
ALIDNS_ACCESS_KEY_ID=替换为AccessKeyID
ALIDNS_ACCESS_KEY_SECRET=替换为AccessKeySecret
ALIDNS_DOMAIN_NAME=buaadcl.tech
ALIDNS_RR=
ALIDNS_RECORD_TYPE=A
ALIDNS_TTL=600
ALIDNS_ENDPOINT=alidns.cn-hangzhou.aliyuncs.com
ALIDNS_LINE=default
EOF
sudo chmod 0600 /etc/frp-cluster/alidns.env
```

DNS Hook 脚本调用内置阿里云更新器：

```bash
sudo tee /usr/local/bin/frp-cluster-alidns-update >/dev/null <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

: "${FRP_CLUSTER_DNS_HOST:?}"
: "${FRP_CLUSTER_DNS_TARGET_IP:?}"

/usr/local/bin/frp-cluster dns alidns-update --config /etc/frp-cluster/alidns.env
EOF
sudo chmod 0755 /usr/local/bin/frp-cluster-alidns-update
```

如果阿里云中还没有对应 A 记录，更新器会自动创建；已有记录则更新到目标代理节点公网 IP。

建议 DNS TTL 设置为较短值，例如：

```text
TTL 600
```

切换前先确认新节点已经监听业务端口：

```bash
ssh 新节点用户@新节点公网IP
sudo ss -ltnp | grep ':11022'
```

从任意外部机器验证稳定入口：

```bash
ssh-keyscan -p 11022 -T 5 ssh.buaadcl.tech
```

### 3. 查看当前客户端实际目标

本机用户级服务：

```bash
sed -n '1,80p' ~/.local/state/frp-cluster/local-ssh/frpc.d/*.toml
```

系统级客户端服务：

```bash
sed -n '1,80p' /var/lib/frp-cluster/frpc.d/*.toml
```

看 `serverAddr` 字段即可。它表示客户端当前连接的代理节点。

## 五、多个本地端口代理

如果客户端要同时暴露多个本地服务，例如：

```text
127.0.0.1:8080 -> 124.71.154.57:11000
127.0.0.1:9000 -> 124.71.154.57:11001
127.0.0.1:22   -> 124.71.154.57:11022
```

配置：

```bash
PROXIES='web:tcp:127.0.0.1:8080:11000;api:tcp:127.0.0.1:9000:11001;ssh:tcp:127.0.0.1:22:11022'
```

完整 `client.env` 示例：

```bash
cat > client.env <<'EOF'
CONTROL_URL=http://124.71.154.57:8088
CLIENT_ID=client-multi-1

MODE=failover
LIMIT=1
INTERVAL=30s
DRAIN_TIMEOUT=30s

PROXIES='web:tcp:127.0.0.1:8080:11000;api:tcp:127.0.0.1:9000:11001;ssh:tcp:127.0.0.1:22:11022'

WORK_DIR=/var/lib/frp-cluster/frpc.d
FRPC_BIN=/usr/local/bin/frpc
EOF

sudo ./scripts/client-install.sh ./client.env
```

## 六、端口冲突处理

同一个代理集群里，每个 `remotePort` 同一时间只能被一个代理占用。

例如已经使用：

```text
web:tcp:127.0.0.1:8080:11000
```

其他客户端就不能再使用 `11000`，需要换成：

```text
11001
11002
...
12000
```

建议维护一张业务端口表：

```text
11000  client-app-1 web
11001  client-app-1 api
11002  client-app-2 web
11022  client-admin ssh
```

## 七、常用运维命令

bootstrap 节点查看状态：

```bash
ssh ldc@124.71.154.57

systemctl status frp-cluster-control frps frp-cluster-agent --no-pager
frp-cluster health --control-url http://127.0.0.1:8088 --timeout 2s
```

生成新节点 token：

```bash
frp-cluster token --control-url http://127.0.0.1:8088 --ttl 2h --uses 1
```

查看当前集群配置是否能生成客户端配置：

```bash
frp-cluster config frpc \
  --control-url http://127.0.0.1:8088 \
  --client-id verify \
  --mode aggregate \
  --proxy web:tcp:127.0.0.1:8080:11000 \
  --out-dir /tmp/frpc-verify
```

退出某个代理节点，在目标代理节点执行：

```bash
sudo ./scripts/proxy-node-leave.sh /etc/frp-cluster/node.env
```

重启客户端服务：

```bash
sudo systemctl restart frp-cluster-client
sudo systemctl status frp-cluster-client --no-pager
```
