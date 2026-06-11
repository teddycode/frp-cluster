# frp-cluster 需求与实现记录

## 目标

基于 frp 服务实现 frps 服务端高可用、负载均衡和多服务器链路聚合。一个或多个 frpc 客户端可以同时面向多个 frps 代理服务器生成配置，代理服务器可以通过 token 加入或退出集群，管理端可以查看客户端和代理服务器状态。

## 功能清单

| 编号 | 功能 | 当前实现 |
| --- | --- | --- |
| R1 | frps 集群高可用 | 控制面维护多个 server node，按在线状态和网络评分生成 frpc 多节点配置；`failover` 可限制候选节点数量，`aggregate` 输出全部在线节点。 |
| R2 | 负载均衡 | 控制面按节点网络评分、最少客户端数和代理数选择推荐节点；管理 API 返回节点负载和网络质量指标。 |
| R3 | 多服务器链路聚合 | 配置生成支持 `single`、`failover`、`aggregate` 三种模式；`aggregate` 为每个在线节点生成一份可独立运行的 frpc 配置。 |
| R4 | token 加入集群 | 管理端/CLI 可创建带 TTL 的 join token；代理服务器调用 join API 后成为集群节点。 |
| R5 | 一键退出集群 | 代理服务器使用节点 token 调用 leave API，控制面将节点标记为离线。 |
| R6 | 配置生成 | 生成 frps 配置、frpc 聚合/故障切换配置、业务代理配置包和节点 join 命令；`client` 子命令可周期性同步并启用最新配置。 |
| R7 | 管理端系统 | 内置 Web 管理端显示节点、客户端、代理、事件和 token。 |
| R8 | 客户端状态 | frps HTTP 插件回调记录 login/new_proxy/close_proxy 等事件并刷新客户端、代理状态。 |
| R9 | 节点心跳 | `agent` 子命令以 node token 定期上报心跳、控制面延迟、主动探测带宽和网卡实际收发速率，支持退出时自动 leave。 |
| R10 | 网络感知迁移 | 控制面为网络评分明显更优的节点生成优先配置，并对仍在线但处在较差节点上的客户端记录迁移目标和事件；`client` 子命令先启动新 frpc 后 drain 旧进程，实现现有 frp 边界内的自动切换。 |
| R11 | 业务测试 | Go 单元测试覆盖 token、join/leave、网络指标、调度、迁移建议、配置生成和插件事件。 |

## 设计原则

- 控制面只做编排和状态管理，不侵入 frp 核心转发链路。
- 数据面高可用依赖 frpc 多配置/多进程或上层 DNS/LB，控制面提供确定性配置、网络评分排序与状态发现。
- 节点加入使用一次性 join token，加入后下发长期 node token 用于心跳和退出。
- 状态持久化采用 JSON 文件，方便单机部署；后续可以替换为 etcd/PostgreSQL。

## 核心对象

- Cluster：全局配置，包括共享认证 token、dashboard、插件路径等。
- ServerNode：frps 代理服务器节点，包含公网地址、绑定端口、健康状态、负载统计、网络指标和 node token。
- Client：frpc 客户端状态，包含用户、来源节点、迁移目标、最后上线时间、代理数量。
- Proxy：frp 代理条目，包含名称、类型、客户端、节点和状态。
- JoinToken：带 TTL 和使用次数的代理服务器入群凭证。
- Event：集群事件流，用于管理端审计和排障。

## API 概览

- `GET /api/v1/cluster`：查看集群状态。
- `POST /api/v1/tokens`：创建 join token。
- `GET /api/v1/tokens`：查看未过期 token。
- `POST /api/v1/nodes/join`：代理服务器加入集群。
- `POST /api/v1/nodes/{id}/heartbeat`：节点心跳，可携带延迟、主动探测带宽和实际收发速率。
- `POST /api/v1/nodes/{id}/leave`：节点退出。
- `GET|POST /api/v1/network/probe`：agent 主动下载/上传探测数据以估算带宽。
- `GET /api/v1/config/frps?node_id=...`：生成 frps 配置。
- `GET /api/v1/config/frpc?client_id=...&mode=aggregate|failover|single`：生成 frpc 配置预览。
- `GET /api/v1/config/frpc?client_id=...&mode=aggregate&format=json`：生成 frpc 多文件配置包。
- `GET /api/v1/commands/join?...`：生成一键 join 命令。
- `POST /api/v1/frp/plugin`：接收 frps HTTP 插件事件。

## 部署流程

1. 启动控制面：

   ```bash
   frp-cluster server --listen :8080 --data ./data/cluster.json
   ```

2. 创建代理服务器加入 token：

   ```bash
   frp-cluster token --control-url http://127.0.0.1:8080 --ttl 2h --uses 3
   ```

3. 在每台 frps 代理服务器上加入集群：

   ```bash
   frp-cluster join --control-url http://CONTROL:8080 --token JOIN_TOKEN --node-id edge-a --public-addr 203.0.113.10 --bind-port 7000 --write-frps-config ./frps-edge-a.toml
   ```

   托管节点心跳：

   ```bash
   frp-cluster agent --control-url http://CONTROL:8080 --node-id edge-a --token NODE_TOKEN
   ```

   默认主动探测 256 KiB 上下行带宽；可通过 `--probe-size 0` 关闭主动探测，仅保留控制面延迟和网卡实际收发速率。

4. 生成并部署该节点 frps 配置：

   ```bash
   frp-cluster config frps --control-url http://CONTROL:8080 --node-id edge-a > frps.toml
   frps -c frps.toml
   ```

5. 为客户端生成 frpc 配置：

   ```bash
   frp-cluster config frpc --control-url http://CONTROL:8080 --client-id app-1 --mode aggregate --out-dir ./frpc.d
   ```

   真实业务代理示例：

   ```bash
   frp-cluster config frpc --control-url http://CONTROL:8080 --client-id app-1 --mode aggregate --proxy web:tcp:127.0.0.1:8080:18080 --out-dir ./frpc.d
   ```

   或直接托管自动切换客户端：

   ```bash
   frp-cluster client --control-url http://CONTROL:8080 --client-id app-1 --proxy web:tcp:127.0.0.1:8080:18080 --frpc-bin /usr/local/bin/frpc --work-dir ./frpc.d
   ```

## 当前限制与后续计划

- 当前版本使用文件持久化，适合单控制面；控制面自身 HA 可通过外部主备或后续接入 etcd 实现。
- frpc `failover`/`aggregate` 通过多份代理配置实现；网络质量迁移通过自动重排候选节点、迁移建议和 `client` 子命令的先启后停进程切换生效，不热搬迁已经建立的单条 TCP 流。单业务端口的真实 L4 负载均衡仍建议放在 DNS/LB 层。
- 后续可增加 mTLS、RBAC、节点自动安装 frp、Prometheus 指标和更细粒度的代理拓扑编辑。
