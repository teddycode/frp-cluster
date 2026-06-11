# frp-cluster

frp-cluster 是一个面向 frp/frps 的轻量控制面，用于管理多个 frps 代理服务器节点，生成节点加入命令、frps 配置和 frpc 多节点配置，并在管理端查看节点、客户端、代理和事件状态。

## 能力范围

- frps 节点通过一次性 join token 加入集群，加入后获得 node token。
- 每个代理服务器节点都可以运行本地控制面 Web/API，并通过 peer 同步节点、token、客户端、代理和事件状态。
- 访问任意代理节点的 Web 端都可以生成加入命令、生成 token、查看集群状态，并把节点标记退出。
- 节点可以通过 CLI/API 心跳和退出，退出后相关客户端与代理状态会被标记离线。
- frpc 配置支持 `single`、`failover`、`aggregate` 三种模式。
- `failover` 模式生成受 `--limit` 控制的多节点候选配置，适合主备/多备。
- `aggregate` 模式生成多份 frpc 配置文件，适合每个 frps 节点启动一个 frpc 进程，实现多服务器链路并行。
- agent 心跳会主动探测控制面延迟、上下行带宽，并采集网卡实际收发速率；控制面按网络评分优先选择节点。
- frps 配置内置 HTTP plugin 回调，控制面可以接收 Login/NewProxy/CloseProxy/Ping 等事件并刷新管理端状态。
- 管理端内置在控制面服务里，无需单独部署前端。

## 一键安装包

生成包含 `frp-cluster`、`frps`、`frpc`、systemd 服务和安装脚本的 Linux 安装包：

```bash
./scripts/package-release.sh
```

产物位于 `dist/`：

```text
dist/frp-cluster-bundle-linux-amd64.tar.gz
dist/frp-cluster-bundle-linux-arm64.tar.gz
```

当前 124 bootstrap 节点、新代理节点加入、客户端端口代理的完整操作手册见：

```text
docs/proxy-cluster-deployment.md
```

## P2P 代理节点部署

安装包内的代理节点相关文件：

- `scripts/proxy-node-join.sh`：安装二进制、启动本地 Web 控制面、加入集群、生成 frps 配置并启动 frps/agent，全部配置为开机自启。
- `scripts/proxy-node-leave.sh`：调用节点退出接口并停止本机 frps/agent。
- `systemd/frp-cluster-control.service`：本机控制面 Web/API。
- `systemd/frps.service`：frps 服务。
- `systemd/frp-cluster-agent.service`：节点心跳服务。

### 首个代理节点

在第一台代理服务器上解压安装包：

```bash
tar -xzf frp-cluster-bundle-linux-amd64.tar.gz
cd frp-cluster-bundle-linux-amd64
cp examples/proxy-node.env.example proxy-node.env
```

首个节点可以让 `BOOTSTRAP_CONTROL_URL` 等于自己的 `NODE_CONTROL_URL`，并把 `JOIN_TOKEN` 设为 `auto`：

```bash
BOOTSTRAP_CONTROL_URL=http://203.0.113.10:8080
NODE_CONTROL_URL=http://203.0.113.10:8080
JOIN_TOKEN=auto
NODE_ID=edge-a
PUBLIC_ADDR=203.0.113.10
BIND_PORT=7000
```

执行：

```bash
sudo ./scripts/proxy-node-join.sh ./proxy-node.env
```

管理端：

```text
http://203.0.113.10:8080/
```

### 新增代理节点

可以在任意已有代理节点 Web 端点击“生成 Token”或“生成命令”，也可以用 CLI 创建 token：

```bash
JOIN_TOKEN=$(frp-cluster token --control-url http://203.0.113.10:8080 --ttl 2h --uses 1)
```

在新代理服务器上填写：

```bash
BOOTSTRAP_CONTROL_URL=http://203.0.113.10:8080
NODE_CONTROL_URL=http://203.0.113.11:8080
JOIN_TOKEN=join_xxx
NODE_ID=edge-b
PUBLIC_ADDR=203.0.113.11
BIND_PORT=7000
```

然后执行：

```bash
sudo ./scripts/proxy-node-join.sh ./proxy-node.env
```

加入后，每个代理服务器都会运行 Web 控制面并互相同步状态。访问任意节点的 `NODE_CONTROL_URL` 都可以查看集群、生成加入命令、让节点退出集群视图。

### 退出代理节点

在要退出的代理节点上执行：

```bash
sudo ./scripts/proxy-node-leave.sh /etc/frp-cluster/node.env
```

也可以在任意代理节点 Web 端的节点表点击“退出”，该操作会把目标节点在 P2P 控制面状态中标记为离线；如果目标机器上的 agent 仍在运行，它后续心跳可能再次上线，因此生产退出建议优先使用本机 `proxy-node-leave.sh`。

## 客户端一键安装

客户端安装包内包含 `frpc` 和 `frp-cluster client` 托管服务。解压后：

```bash
cp examples/client.env.example client.env
```

示例：

```bash
CONTROL_URL=http://203.0.113.10:8080
CLIENT_ID=app-1
MODE=failover
LIMIT=1
PROXIES='web:tcp:127.0.0.1:8080:18080;ssh:tcp:127.0.0.1:22:22022'
```

安装并配置开机自启：

```bash
sudo ./scripts/client-install.sh ./client.env
```

查看状态：

```bash
systemctl status frp-cluster-client --no-pager
```

## CLI 快速开始

```bash
go build -o ./bin/frp-cluster ./cmd/frp-cluster
./bin/frp-cluster server --listen :8080 --data ./data/cluster.json --public-url http://203.0.113.10:8080
```

打开管理端：

```text
http://127.0.0.1:8080/
```

创建加入 token：

```bash
JOIN_TOKEN=$(./bin/frp-cluster token --control-url http://127.0.0.1:8080 --ttl 2h --uses 2)
```

加入两台代理服务器：

```bash
./bin/frp-cluster join --control-url http://127.0.0.1:8080 --token "$JOIN_TOKEN" --node-id edge-a --public-addr 203.0.113.10 --node-control-url http://203.0.113.10:8080 --bind-port 7000 --write-frps-config ./frps-edge-a.toml
./bin/frp-cluster join --control-url http://127.0.0.1:8080 --token "$JOIN_TOKEN" --node-id edge-b --public-addr 203.0.113.11 --node-control-url http://203.0.113.11:8080 --bind-port 7000
```

在每台代理服务器上托管节点心跳：

```bash
./bin/frp-cluster agent --control-url http://127.0.0.1:8080 --node-id edge-a --token NODE_TOKEN
```

生成 frpc 聚合配置包：

```bash
./bin/frp-cluster config frpc \
  --control-url http://127.0.0.1:8080 \
  --client-id app-1 \
  --mode aggregate \
  --proxy web:tcp:127.0.0.1:8080:18080 \
  --proxy ssh:tcp:127.0.0.1:22:22022 \
  --out-dir ./frpc.d
```

也可以让 `frp-cluster client` 常驻运行，周期性拉取控制面按网络评分排序后的 frpc 配置，并托管 frpc 进程：

```bash
./bin/frp-cluster client \
  --control-url http://127.0.0.1:8080 \
  --client-id app-1 \
  --proxy web:tcp:127.0.0.1:8080:18080 \
  --frpc-bin /usr/local/bin/frpc \
  --work-dir ./frpc.d
```

退出节点：

```bash
./bin/frp-cluster leave --control-url http://127.0.0.1:8080 --node-id edge-a --token NODE_TOKEN
```

## API

- `GET /api/v1/cluster`
- `POST /api/v1/tokens`
- `GET /api/v1/tokens`
- `POST /api/v1/nodes/join`
- `POST /api/v1/nodes/{id}/heartbeat`
- `POST /api/v1/nodes/{id}/leave`
- `POST /api/v1/nodes/{id}/admin-leave`
- `GET|POST /api/v1/peer/state`
- `GET|POST /api/v1/network/probe`
- `GET /api/v1/config/frps?node_id=edge-a`
- `GET /api/v1/config/frpc?client_id=app-1&mode=aggregate`
- `GET /api/v1/config/frpc?client_id=app-1&mode=aggregate&format=json`
- `GET /api/v1/commands/join?...`
- `POST /api/v1/frp/plugin/{node_id}`

## 测试

```bash
go test ./...
```

## 重要边界

frp-cluster 不修改 frp 数据面协议。高可用、负载均衡和网络质量迁移通过控制面维护多个 frps 节点、按健康与网络评分生成 frpc 多端点/多进程配置、并结合上层 DNS/LB 或客户端多进程运行实现。控制面会自动把新选择和迁移目标指向更优节点，但单个已经建立的 TCP 连接不会被拆分或热搬迁到另一个 frps 节点。
# frp-cluster
