package control

const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>frp-cluster 管理端</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f7f8f5;
      --ink: #17211b;
      --muted: #657069;
      --line: #d9dfd8;
      --panel: #ffffff;
      --accent: #1f7a5a;
      --warn: #b45309;
      --bad: #b42318;
      --ok: #16784f;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      background: var(--bg);
      color: var(--ink);
      font: 14px/1.45 Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      letter-spacing: 0;
    }
    header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 24px;
      padding: 24px 32px 16px;
      border-bottom: 1px solid var(--line);
      background: rgba(247, 248, 245, .92);
      position: sticky;
      top: 0;
      z-index: 3;
      backdrop-filter: blur(14px);
    }
    h1 {
      margin: 0;
      font-size: 24px;
      line-height: 1.1;
      font-weight: 720;
    }
    .sub {
      margin-top: 6px;
      color: var(--muted);
      font-size: 13px;
    }
    button, select, input {
      font: inherit;
      letter-spacing: 0;
    }
    button {
      min-height: 36px;
      border: 1px solid var(--ink);
      background: var(--ink);
      color: #fff;
      padding: 0 14px;
      border-radius: 6px;
      cursor: pointer;
      transition: transform .16s ease, background .16s ease;
      white-space: nowrap;
    }
    button:hover { transform: translateY(-1px); background: #24342b; }
    button.secondary {
      color: var(--ink);
      background: transparent;
      border-color: var(--line);
    }
    button.secondary:hover { background: #eef2ed; }
    button.danger {
      border-color: var(--bad);
      background: var(--bad);
    }
    button.danger:hover { background: #8f1d14; }
    button.small {
      min-height: 30px;
      padding: 0 10px;
      font-size: 12px;
    }
    main {
      display: grid;
      grid-template-columns: 280px minmax(0, 1fr);
      min-height: calc(100vh - 82px);
    }
    aside {
      padding: 24px 20px 32px 32px;
      border-right: 1px solid var(--line);
    }
    .metrics {
      display: grid;
      gap: 14px;
    }
    .metric {
      padding-bottom: 14px;
      border-bottom: 1px solid var(--line);
    }
    .metric strong {
      display: block;
      font-size: 28px;
      line-height: 1;
      margin-bottom: 6px;
    }
    .metric span {
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
    }
    .workspace {
      padding: 24px 32px 36px;
      display: grid;
      gap: 28px;
    }
    section {
      min-width: 0;
    }
    .section-head {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 16px;
      margin-bottom: 12px;
    }
    h2 {
      margin: 0;
      font-size: 16px;
      line-height: 1.2;
    }
    .toolbar {
      display: flex;
      gap: 8px;
      align-items: center;
      flex-wrap: wrap;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      overflow: hidden;
    }
    th, td {
      padding: 12px 14px;
      text-align: left;
      border-bottom: 1px solid var(--line);
      vertical-align: middle;
      word-break: break-word;
    }
    th {
      color: var(--muted);
      font-size: 12px;
      font-weight: 650;
      background: #fbfcfa;
    }
    tr:last-child td { border-bottom: 0; }
    .status {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      min-width: 76px;
      font-size: 12px;
      color: var(--muted);
    }
    .dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;
      background: var(--muted);
      flex: 0 0 auto;
    }
    .online .dot { background: var(--ok); }
    .offline .dot { background: var(--bad); }
    .draining .dot { background: var(--warn); }
    .code {
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-size: 12px;
      color: #26352d;
    }
    .split {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(320px, .7fr);
      gap: 24px;
    }
    .form {
      display: grid;
      grid-template-columns: repeat(4, minmax(120px, 1fr));
      gap: 10px;
      align-items: end;
      margin-bottom: 12px;
    }
    label {
      display: grid;
      gap: 5px;
      color: var(--muted);
      font-size: 12px;
    }
    input, select {
      height: 36px;
      border: 1px solid var(--line);
      border-radius: 6px;
      padding: 0 10px;
      background: #fff;
      color: var(--ink);
      min-width: 0;
    }
    pre {
      margin: 0;
      min-height: 130px;
      max-height: 340px;
      overflow: auto;
      white-space: pre-wrap;
      word-break: break-word;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: #101713;
      color: #d8efe3;
      padding: 14px;
      font-size: 12px;
    }
    .events {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      overflow: hidden;
    }
    .event {
      display: grid;
      grid-template-columns: 160px 140px minmax(0, 1fr);
      gap: 12px;
      padding: 11px 14px;
      border-bottom: 1px solid var(--line);
    }
    .event:last-child { border-bottom: 0; }
    .muted { color: var(--muted); }
    @media (max-width: 900px) {
      header { align-items: flex-start; padding: 20px; }
      main { grid-template-columns: 1fr; }
      aside {
        border-right: 0;
        border-bottom: 1px solid var(--line);
        padding: 18px 20px;
      }
      .metrics { grid-template-columns: repeat(4, minmax(0, 1fr)); }
      .workspace { padding: 20px; }
      .split { grid-template-columns: 1fr; }
      .form { grid-template-columns: 1fr 1fr; }
      .event { grid-template-columns: 1fr; }
    }
    @media (max-width: 620px) {
      header { flex-direction: column; }
      .metrics { grid-template-columns: 1fr 1fr; }
      .form { grid-template-columns: 1fr; }
      th:nth-child(4), td:nth-child(4), th:nth-child(6), td:nth-child(6) { display: none; }
    }
  </style>
</head>
<body>
  <header>
    <div>
      <h1>frp-cluster 管理端</h1>
      <div class="sub" id="clusterName">控制面状态加载中</div>
    </div>
    <div class="toolbar">
      <button class="secondary" id="refreshBtn">刷新</button>
      <button id="tokenBtn">生成 Token</button>
    </div>
  </header>
  <main>
    <aside>
      <div class="metrics">
        <div class="metric"><strong id="onlineNodes">0</strong><span>在线节点</span></div>
        <div class="metric"><strong id="offlineNodes">0</strong><span>离线节点</span></div>
        <div class="metric"><strong id="onlineClients">0</strong><span>在线客户端</span></div>
        <div class="metric"><strong id="onlineProxies">0</strong><span>在线代理</span></div>
      </div>
    </aside>
    <div class="workspace">
      <section>
        <div class="section-head">
          <h2>代理服务器集群</h2>
          <span class="muted" id="nodeHint"></span>
        </div>
        <table>
          <thead><tr><th>节点</th><th>状态</th><th>地址</th><th>控制面</th><th>网络</th><th>负载</th><th>最后心跳</th><th>操作</th></tr></thead>
          <tbody id="nodesBody"></tbody>
        </table>
      </section>
      <section class="split">
        <div>
          <div class="section-head">
            <h2>客户端与代理</h2>
            <span class="muted" id="clientHint"></span>
          </div>
          <table>
            <thead><tr><th>客户端</th><th>状态</th><th>节点</th><th>目标</th><th>代理数</th><th>操作</th></tr></thead>
            <tbody id="clientsBody"></tbody>
          </table>
        </div>
        <div>
          <div class="section-head">
            <h2>配置生成</h2>
          </div>
          <div class="form">
            <label>客户端 ID<input id="clientId" value="app-1"></label>
            <label>模式<select id="mode"><option value="aggregate">aggregate</option><option value="failover">failover</option><option value="single">single</option></select></label>
            <button id="configBtn">生成 frpc</button>
          </div>
          <pre id="configOut">选择模式后生成客户端配置。</pre>
        </div>
      </section>
      <section>
        <div class="section-head">
          <h2>Token 与一键加入</h2>
        </div>
        <div class="form">
          <label>节点 ID<input id="joinNodeId" value="edge-a"></label>
          <label>公网地址<input id="joinPublicAddr" value="203.0.113.10"></label>
          <label>节点 Web 地址<input id="joinControlURL" value="http://203.0.113.10:8080"></label>
          <label>端口<input id="joinBindPort" value="7000"></label>
          <label>frps 配置路径<input id="joinFrpsConfig" value="/etc/frp/frps.toml"></label>
          <button id="joinCmdBtn">生成命令</button>
        </div>
        <pre id="tokenOut">生成 token 后可以创建 join 命令。</pre>
      </section>
      <section>
        <div class="section-head">
          <h2>事件</h2>
          <span class="muted" id="eventHint"></span>
        </div>
        <div class="events" id="eventsBody"></div>
      </section>
    </div>
  </main>
  <script>
    const state = { snapshot: null, token: "" };
    const $ = (id) => document.getElementById(id);
    const fmtTime = (value) => value ? new Date(value).toLocaleString() : "-";
    const status = (value) => '<span class="status ' + value + '"><span class="dot"></span>' + value + '</span>';
    const fmtMbps = (bps) => bps ? Math.round(bps / 1000000) + " Mbps" : "-";
    const esc = (value) => String(value ?? "").replace(/[&<>"']/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "\"": "&quot;", "'": "&#39;" }[ch]));
    const network = (n) => {
      const net = n.network || {};
      const stale = net.stale ? " / stale" : "";
      return '<span class="code">' + esc(net.latency_ms || "-") + ' ms / ' + esc(fmtMbps(net.bandwidth_bps)) + ' / ' + esc(net.score || 0) + esc(stale) + '</span>';
    };
    const migration = (c) => c.preferred_node_id ? '<span class="code">' + esc(c.migration_state) + ' -> ' + esc(c.preferred_node_id) + '</span>' : '<span class="muted">粘住当前节点</span>';
    const nodeOptions = (selected) => {
      const nodes = (state.snapshot && state.snapshot.nodes || []).filter((n) => n.status === "online");
      return nodes.map((n) => '<option value="' + esc(n.id) + '"' + (n.id === selected ? " selected" : "") + '>' + esc(n.id) + ' / ' + esc(n.public_addr) + '</option>').join("");
    };
    async function api(path, options) {
      const res = await fetch(path, options);
      if (!res.ok) {
        let message = res.statusText;
        try { message = (await res.json()).error || message; } catch (_) {}
        throw new Error(message);
      }
      const contentType = res.headers.get("content-type") || "";
      return contentType.includes("application/json") ? res.json() : res.text();
    }
    function render(snapshot) {
      state.snapshot = snapshot;
      const peerCount = (snapshot.config.peer_urls || []).length;
      $("clusterName").textContent = snapshot.config.name + " / peers " + peerCount + " / " + new Date().toLocaleTimeString();
      $("onlineNodes").textContent = snapshot.summary.online_nodes;
      $("offlineNodes").textContent = snapshot.summary.offline_nodes;
      $("onlineClients").textContent = snapshot.summary.online_clients;
      $("onlineProxies").textContent = snapshot.summary.online_proxies;
      $("nodeHint").textContent = snapshot.nodes.length + " 个节点";
      $("clientHint").textContent = snapshot.clients.length + " 个客户端";
      $("eventHint").textContent = snapshot.events.length + " 条最近事件";
      $("nodesBody").innerHTML = snapshot.nodes.map((n) => '<tr><td><strong>' + esc(n.id) + '</strong><div class="muted">' + esc(n.region || "-") + '</div></td><td>' + status(esc(n.status)) + '</td><td class="code">' + esc(n.public_addr) + ':' + esc(n.bind_port) + '</td><td class="code">' + (n.control_url ? '<a href="' + esc(n.control_url) + '" target="_blank" rel="noreferrer">' + esc(n.control_url) + '</a>' : '-') + '</td><td>' + network(n) + '</td><td>' + esc(n.client_count) + ' 客户端 / ' + esc(n.proxy_count) + ' 代理</td><td>' + fmtTime(n.last_seen_at) + '</td><td><button class="danger small" data-leave-node="' + esc(n.id) + '">退出</button></td></tr>').join("") || '<tr><td colspan="8" class="muted">暂无代理服务器节点</td></tr>';
      $("clientsBody").innerHTML = snapshot.clients.map((c) => '<tr><td><strong>' + esc(c.id) + '</strong><div class="muted">' + esc(c.user || "-") + '</div></td><td>' + status(esc(c.status)) + '</td><td class="code">' + esc(c.node_id || "-") + '</td><td>' + migration(c) + '</td><td>' + esc(c.proxy_count) + '</td><td><div class="toolbar"><select data-client-target="' + esc(c.id) + '">' + nodeOptions(c.preferred_node_id || c.node_id) + '</select><button class="small" data-switch-client="' + esc(c.id) + '">切换</button><button class="secondary small" data-clear-client="' + esc(c.id) + '">清除</button></div></td></tr>').join("") || '<tr><td colspan="6" class="muted">暂无客户端状态</td></tr>';
      $("eventsBody").innerHTML = snapshot.events.map((e) => '<div class="event"><div class="muted">' + fmtTime(e.created_at) + '</div><div class="code">' + esc(e.type) + '</div><div>' + esc(e.message) + '</div></div>').join("") || '<div class="event"><div class="muted">暂无事件</div></div>';
    }
    async function refresh() {
      render(await api("/api/v1/cluster"));
    }
    $("refreshBtn").onclick = refresh;
    $("tokenBtn").onclick = async () => {
      const token = await api("/api/v1/tokens", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ ttl: "2h", uses: 3 }) });
      state.token = token.token;
      $("tokenOut").textContent = token.token;
      await refresh();
    };
    $("configBtn").onclick = async () => {
      const clientId = encodeURIComponent($("clientId").value || "app-1");
      const mode = encodeURIComponent($("mode").value || "aggregate");
      $("configOut").textContent = await api("/api/v1/config/frpc?client_id=" + clientId + "&mode=" + mode);
    };
    $("joinCmdBtn").onclick = async () => {
      if (!state.token) {
        const token = await api("/api/v1/tokens", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ ttl: "2h", uses: 1 }) });
        state.token = token.token;
      }
      const params = new URLSearchParams({
        token: state.token,
        node_id: $("joinNodeId").value,
        public_addr: $("joinPublicAddr").value,
        bind_port: $("joinBindPort").value,
        node_control_url: $("joinControlURL").value,
        write_frps_config: $("joinFrpsConfig").value
      });
      const result = await api("/api/v1/commands/join?" + params);
      $("tokenOut").textContent = result.command;
    };
    $("nodesBody").onclick = async (event) => {
      const btn = event.target.closest("[data-leave-node]");
      if (!btn) return;
      const nodeID = btn.getAttribute("data-leave-node");
      if (!confirm("确认让节点 " + nodeID + " 退出集群？")) return;
      await api("/api/v1/nodes/" + encodeURIComponent(nodeID) + "/admin-leave", { method: "POST" });
      await refresh();
    };
    $("clientsBody").onclick = async (event) => {
      const switchBtn = event.target.closest("[data-switch-client]");
      const clearBtn = event.target.closest("[data-clear-client]");
      if (!switchBtn && !clearBtn) return;
      const clientID = (switchBtn || clearBtn).getAttribute(switchBtn ? "data-switch-client" : "data-clear-client");
      let nodeID = "";
      if (switchBtn) {
        const selector = document.querySelector('[data-client-target="' + CSS.escape(clientID) + '"]');
        nodeID = selector ? selector.value : "";
        if (!nodeID) return;
        if (!confirm("确认把客户端 " + clientID + " 手动切换到 " + nodeID + "？")) return;
      } else if (!confirm("确认清除客户端 " + clientID + " 的手动目标？")) {
        return;
      }
      await api("/api/v1/clients/" + encodeURIComponent(clientID) + "/target", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ node_id: nodeID })
      });
      await refresh();
    };
    refresh();
    setInterval(refresh, 10000);
  </script>
</body>
</html>`
