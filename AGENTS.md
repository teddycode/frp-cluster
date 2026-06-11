推理过程的说明性文字请用中文

# frp-cluster 关键信息

- 项目目标：基于 frp/frps 构建控制面，实现 frps 代理服务器集群的高可用、负载均衡、多服务器链路聚合配置生成、节点一键加入/退出、状态管理和管理端查看。
- 架构边界：本项目不修改 frp 数据面协议；通过控制面 API、frps 配置生成、frpc 多端点配置和 frps HTTP 插件回调实现集群编排与可观测。
- 主要二进制：`frp-cluster`，包含 `server`、`join`、`agent`、`leave`、`token`、`config` 子命令。
- 默认控制面地址：`:8080`；默认数据文件：`./data/cluster.json`。
- 管理端入口：启动 `frp-cluster server` 后访问 `http://127.0.0.1:8080/`。
- frps 接入方式：先由控制面生成加入 token，再在代理服务器上执行 `frp-cluster join --control-url ... --token ... --node-id ... --public-addr ... --write-frps-config ./frps.toml`，然后托管 `frp-cluster agent --control-url ... --node-id ... --token NODE_TOKEN` 持续心跳。
- 退出方式：在代理服务器上执行 `frp-cluster leave --control-url ... --node-id ... --token ...`。
- frpc 聚合配置：使用 `frp-cluster config frpc --mode aggregate --proxy name:tcp:127.0.0.1:8080:18080 --out-dir ./frpc.d` 输出多份配置，每份配置对应一个 frpc 进程。
- 测试命令：`go test ./...`。
