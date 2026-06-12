推理过程的说明性文字请用中文

# frp-cluster 项目速记

- 项目目标：基于 frp/frps 构建轻量控制面，管理 frps 代理节点集群，提供高可用、负载均衡、链路聚合配置生成、节点加入/退出、状态观测和 Web 管理端。
- 架构边界：不修改 frp 数据面协议；通过控制面 API、frps 配置生成、frpc 多端点配置、frps HTTP plugin 回调和 peer 状态同步完成编排。
- 后端：Go 项目，入口 `cmd/frp-cluster`，核心包 `internal/control`；二进制为 `frp-cluster`，包含 `server`、`token`、`join`、`leave`、`agent`、`client`、`health`、`dns`、`config` 子命令。
- 后端默认：控制面监听 `:8080`，状态文件 `./data/cluster.json`；`server` 同时提供 `/api/v1/*` API 和静态 Web 托管。
- 前端：前后端分离的 React + Vite + Ant Design 应用，源码在 `web/src`，入口 `web/src/main.jsx`，样式 `web/src/styles.css`，构建产物 `web/dist`。
- 前端开发：在 `web/` 下运行 `npm run dev`；Vite 将 `/api` 代理到 `http://127.0.0.1:8088`。
- 生产集成：先运行 `cd web && npm run build` 生成 `web/dist`，再用 `frp-cluster server --web-dir ./web/dist` 托管管理端；默认访问 `http://127.0.0.1:8080/`。
- 核心流程：创建 join token -> 节点执行 `join` 写入 frps 配置 -> 托管 `agent` 持续心跳 -> 客户端用 `client` 或 `config frpc` 获取 `single`、`failover`、`aggregate` 配置 -> 节点用 `leave` 退出。
- 关键能力：节点网络评分、客户端迁移建议/切换、阿里云 DNS Hook、TOTP 管理端认证、frps plugin 事件采集、peer 控制面状态同步。
- 常用文档：`README.md` 是快速开始；`docs/proxy-cluster-deployment.md` 是部署手册；`examples/*.env.example` 是安装脚本配置样例。
- 常用命令：后端测试 `go test ./...`；后端构建 `go build -o ./bin/frp-cluster ./cmd/frp-cluster`；前端构建 `cd web && npm run build`；发布打包 `./scripts/package-release.sh`。

## 代码注释

1. 每个 Go 文件开头应有包注释，说明该包的功能和设计思路。
2. 主要函数和方法应有注释，说明功能、输入输出参数和返回值。
3. 复杂逻辑块应有简短内联注释，解释工作原理和关键步骤。
