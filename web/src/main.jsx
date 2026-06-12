import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Alert,
  App,
  Avatar,
  Button,
  Card,
  Col,
  Descriptions,
  Divider,
  Drawer,
  Form,
  Input,
  InputNumber,
  Layout,
  Menu,
  Modal,
  Popconfirm,
  Row,
  Select,
  Space,
  Statistic,
  Switch,
  Table,
  Tabs,
  Tag,
  Typography,
} from "antd";
import {
  ApiOutlined,
  CloudOutlined,
  CloudServerOutlined,
  ControlOutlined,
  CopyOutlined,
  DashboardOutlined,
  DownloadOutlined,
  KeyOutlined,
  LockOutlined,
  LoginOutlined,
  LogoutOutlined,
  NodeIndexOutlined,
  PlusOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  SwapOutlined,
  ToolOutlined,
} from "@ant-design/icons";
import QRCode from "qrcode";
import "antd/dist/reset.css";
import "./styles.css";

const { Header, Sider, Content } = Layout;
const { Text, Title, Paragraph } = Typography;

async function api(path, options = {}) {
  const res = await fetch(path, {
    credentials: "include",
    ...options,
    headers: {
      ...(options.body ? { "content-type": "application/json" } : {}),
      ...(options.headers || {}),
    },
  });
  const contentType = res.headers.get("content-type") || "";
  const body = contentType.includes("application/json") ? await res.json() : await res.text();
  if (!res.ok) {
    throw new Error(typeof body === "object" ? body.error || res.statusText : body || res.statusText);
  }
  return body;
}

const fmtTime = (value) => (value ? new Date(value).toLocaleString() : "-");
const fmtMbps = (value) => (value ? `${Math.round(value / 1000000)} Mbps` : "-");
const fmtBytes = (value) => {
  const bytes = Number(value || 0);
  if (bytes >= 1024 ** 4) return `${(bytes / 1024 ** 4).toFixed(2)} TB`;
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(2)} GB`;
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(1)} MB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${bytes} B`;
};
const statusColor = (value) => (value === "online" ? "green" : value === "draining" ? "gold" : "red");
const nodePalette = ["#1677ff", "#13c2c2", "#52c41a", "#fa8c16", "#eb2f96", "#722ed1", "#2f54eb", "#a0d911"];

function LoginView({ auth, onLogin }) {
  const { message } = App.useApp();
  const [mode, setMode] = useState(auth.bootstrap_required ? "setup" : "login");
  const [setup, setSetup] = useState(null);
  const [qr, setQr] = useState("");
  const [loading, setLoading] = useState(false);
  const [setupForm] = Form.useForm();
  const [confirmForm] = Form.useForm();

  const createSetup = async (values) => {
    setLoading(true);
    try {
      const result = await api("/api/v1/auth/totp/setup", {
        method: "POST",
        body: JSON.stringify(values),
      });
      setSetup(result);
      confirmForm.setFieldsValue({ secret: result.secret, account: result.account, bootstrap_password: values.bootstrap_password });
      setQr(await QRCode.toDataURL(result.otpauth_uri, { margin: 1, width: 220 }));
    } catch (err) {
      message.error(err.message);
    } finally {
      setLoading(false);
    }
  };

  const confirmSetup = async (values) => {
    setLoading(true);
    try {
      await api("/api/v1/auth/totp/confirm", { method: "POST", body: JSON.stringify(values) });
      message.success("Microsoft Authenticator 已绑定");
      onLogin();
    } catch (err) {
      message.error(err.message);
    } finally {
      setLoading(false);
    }
  };

  const login = async ({ code }) => {
    setLoading(true);
    try {
      await api("/api/v1/auth/login", { method: "POST", body: JSON.stringify({ code }) });
      onLogin();
    } catch (err) {
      message.error(err.message);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="login-shell">
      <div className="login-panel">
        <Space direction="vertical" size={4} className="full">
          <Avatar size={44} icon={<SafetyCertificateOutlined />} className="login-avatar" />
          <Title level={2}>frp-cluster 控制台</Title>
          <Text type="secondary">{mode === "setup" ? "绑定 Microsoft Authenticator" : "使用 Microsoft Authenticator 验证码登录"}</Text>
        </Space>
        {mode === "login" && (
          <Form layout="vertical" onFinish={login} className="login-form">
            <Form.Item name="code" label="6 位动态验证码" rules={[{ required: true, len: 6, message: "请输入 6 位验证码" }]}>
              <Input prefix={<LockOutlined />} inputMode="numeric" maxLength={6} autoFocus />
            </Form.Item>
            <Button type="primary" htmlType="submit" icon={<LoginOutlined />} loading={loading} block>登录</Button>
            <Button type="link" block onClick={() => setMode("setup")}>重新绑定 Authenticator</Button>
          </Form>
        )}
        {mode === "setup" && (
          <Space direction="vertical" className="full" size="middle">
            <Alert type="info" showIcon message="首次绑定需要输入服务器上的初始化口令。绑定完成后日常登录只使用 Microsoft Authenticator 动态码。" />
            {!setup && (
              <Form layout="vertical" form={setupForm} onFinish={createSetup} className="login-form" initialValues={{ account: "admin" }}>
                <Form.Item name="bootstrap_password" label="初始化口令" rules={[{ required: true, message: "请输入初始化口令" }]}>
                  <Input.Password prefix={<KeyOutlined />} />
                </Form.Item>
                <Form.Item name="account" label="账号标签">
                  <Input />
                </Form.Item>
                <Button type="primary" htmlType="submit" loading={loading} block>生成绑定二维码</Button>
                {!auth.bootstrap_required && <Button type="link" block onClick={() => setMode("login")}>返回登录</Button>}
              </Form>
            )}
            {setup && (
              <Form layout="vertical" form={confirmForm} onFinish={confirmSetup}>
                <div className="qr-box">{qr && <img src={qr} alt="Authenticator QR" />}</div>
                <Text code copyable>{setup.secret}</Text>
                <Form.Item hidden name="bootstrap_password"><Input /></Form.Item>
                <Form.Item hidden name="secret"><Input /></Form.Item>
                <Form.Item hidden name="account"><Input /></Form.Item>
                <Form.Item name="code" label="Authenticator 中显示的 6 位验证码" rules={[{ required: true, len: 6 }]}>
                  <Input inputMode="numeric" maxLength={6} />
                </Form.Item>
                <Button type="primary" htmlType="submit" loading={loading} block>确认绑定并登录</Button>
              </Form>
            )}
          </Space>
        )}
      </div>
    </div>
  );
}

function Dashboard() {
  const { message, modal } = App.useApp();
  const [snapshot, setSnapshot] = useState(null);
  const [adminConfig, setAdminConfig] = useState(null);
  const [traffic, setTraffic] = useState(null);
  const [activeKey, setActiveKey] = useState("overview");
  const [loading, setLoading] = useState(false);
  const [guide, setGuide] = useState(null);
  const [joinCommand, setJoinCommand] = useState("");
  const [token, setToken] = useState("");
  const [settingsForm] = Form.useForm();
  const [aliForm] = Form.useForm();
  const [agentForm] = Form.useForm();
  const [joinForm] = Form.useForm();
  const [clientForm] = Form.useForm();
  const clientValues = Form.useWatch([], clientForm) || {};

  const refresh = async () => {
    setLoading(true);
    try {
      const [cluster, config, trafficSeries] = await Promise.all([api("/api/v1/cluster"), api("/api/v1/admin/config"), api("/api/v1/traffic?range=24h")]);
      setSnapshot(cluster);
      setAdminConfig(config);
      setTraffic(trafficSeries);
      settingsForm.setFieldsValue({
        auto_migration: cluster.config?.auto_migration,
        migration_score_gap: cluster.config?.migration_score_gap,
        public_entry_host: cluster.config?.public_entry_host,
        dns_update_hook: cluster.config?.dns_update_hook,
      });
      aliForm.setFieldsValue(config.alidns || {});
      agentForm.setFieldsValue(config.agent || {});
    } catch (err) {
      message.error(err.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, 10000);
    return () => clearInterval(id);
  }, []);

  const onlineNodes = useMemo(() => (snapshot?.nodes || []).filter((node) => node.status === "online"), [snapshot]);
  const nodeOptions = onlineNodes.map((node) => ({ label: `${node.id} / ${node.public_addr}`, value: node.id }));

  const updateSettings = async (values) => {
    await api("/api/v1/settings", { method: "PATCH", body: JSON.stringify(values) });
    message.success("集群策略已保存");
    refresh();
  };

  const saveAliDNS = async (values) => {
    await api("/api/v1/admin/config", { method: "PATCH", body: JSON.stringify({ alidns: values }) });
    message.success("阿里云 DNS 配置已保存");
    refresh();
  };

  const saveAgent = async (values) => {
    await api("/api/v1/admin/config", { method: "PATCH", body: JSON.stringify({ agent: values }) });
    message.success("网络采集配置已保存");
    modal.confirm({
      title: "重启网络采集 Agent",
      content: "新的更新速率需要重启 frp-cluster-agent 后生效。",
      onOk: async () => {
        await api("/api/v1/admin/agent/restart", { method: "POST" });
        message.success("Agent 已重启");
      },
    });
    refresh();
  };

  const switchClient = async (clientID, nodeID) => {
    await api(`/api/v1/clients/${encodeURIComponent(clientID)}/target`, {
      method: "POST",
      body: JSON.stringify({ node_id: nodeID }),
    });
    message.success("切换已提交");
    refresh();
  };

  const clearClientTarget = async (clientID) => {
    await api(`/api/v1/clients/${encodeURIComponent(clientID)}/target`, {
      method: "POST",
      body: JSON.stringify({ node_id: "" }),
    });
    message.success("手动目标已清除");
    refresh();
  };

  const testDNS = async (nodeID) => {
    const result = await api("/api/v1/dns/test", {
      method: "POST",
      body: JSON.stringify({ node_id: nodeID }),
    });
    message.success(`DNS 已更新到 ${result.dns?.target_ip || nodeID}`);
    refresh();
  };

  const createToken = async () => {
    const created = await api("/api/v1/tokens", { method: "POST", body: JSON.stringify({ ttl: "2h", uses: 1 }) });
    setToken(created.token);
    return created.token;
  };

  const generateJoinCommand = async (values) => {
    const joinToken = token || (await createToken());
    const params = new URLSearchParams({
      control_url: values.bootstrap_control_url || window.location.origin,
      token: joinToken,
      node_id: values.node_id,
      public_addr: values.public_addr,
      bind_port: String(values.bind_port || 7000),
      node_control_url: values.node_control_url,
      write_frps_config: values.write_frps_config || "/etc/frp/frps.toml",
    });
    if (values.vhost_http_port) params.set("vhost_http_port", String(values.vhost_http_port));
    if (values.vhost_https_port) params.set("vhost_https_port", String(values.vhost_https_port));
    if (values.region) params.set("region", values.region);
    if (values.tags) params.set("tags", values.tags);
    const result = await api(`/api/v1/commands/join?${params}`);
    setJoinCommand(result.command);
    return joinToken;
  };

  const clientCommand = (values) => {
    const proxies = values.proxies || "ssh:tcp:127.0.0.1:22:11022";
    const mode = values.mode || "failover";
    const limit = values.limit || 1;
    const interval = values.interval || "30s";
    const failoverInterval = values.failover_interval || "10s";
    return [
      "frp-cluster client",
      `--control-url ${shellArg(values.control_url || window.location.origin)}`,
      `--client-id ${shellArg(values.client_id || "local-ssh")}`,
      `--mode ${shellArg(mode)}`,
      `--limit ${shellArg(limit)}`,
      `--interval ${shellArg(interval)}`,
      `--failover-interval ${shellArg(failoverInterval)}`,
      `--drain-timeout ${shellArg(values.drain_timeout || "30s")}`,
      `--work-dir ${shellArg(values.work_dir || "/var/lib/frp-cluster/frpc.d")}`,
      `--frpc-bin ${shellArg(values.frpc_bin || "/usr/local/bin/frpc")}`,
      ...proxies.split(";").filter(Boolean).map((proxy) => `--proxy ${shellArg(proxy.trim())}`),
    ].join(" ");
  };

  const nodeMaterial = (values = joinForm.getFieldsValue(), tokenOverride = "") => buildNodeMaterial(values, tokenOverride || token);
  const clientMaterial = (values = clientForm.getFieldsValue()) => buildClientMaterial(values);

  if (!snapshot || !adminConfig) {
    return <div className="loading-state">加载控制台...</div>;
  }

  const nodeColumns = [
    { title: "节点", dataIndex: "id", render: (_, node) => <NodeCell node={node} /> },
    { title: "状态", dataIndex: "status", render: (value) => <Tag color={statusColor(value)}>{value}</Tag> },
    { title: "接入地址", render: (_, node) => <Text code>{node.public_addr}:{node.bind_port}</Text> },
    { title: "网络质量", render: (_, node) => <NetworkCell network={node.network} /> },
    { title: "负载", render: (_, node) => `${node.client_count} 客户端 / ${node.proxy_count} 代理` },
    { title: "最后心跳", dataIndex: "last_seen_at", render: fmtTime },
    {
      title: "操作",
      render: (_, node) => (
        <Popconfirm title="DNS 自检会真实更新解析" description={`确认把稳定入口指向 ${node.id}?`} onConfirm={() => testDNS(node.id)}>
          <Button size="small" icon={<ApiOutlined />}>DNS 自检</Button>
        </Popconfirm>
      ),
    },
  ];

  const clientColumns = [
    { title: "客户端", dataIndex: "id", render: (_, client) => <ClientCell client={client} /> },
    { title: "状态", dataIndex: "status", render: (value) => <Tag color={statusColor(value)}>{value}</Tag> },
    { title: "当前节点", dataIndex: "node_id", render: (value) => <Text code>{value || "-"}</Text> },
    { title: "端口代理", render: (_, client) => <ProxyTags proxies={client.proxies || []} /> },
    { title: "目标策略", render: (_, client) => <TargetCell client={client} /> },
    {
      title: "切换",
      width: 320,
      render: (_, client) => (
        <Space.Compact block>
          <Select className="client-target-select" options={nodeOptions} defaultValue={client.preferred_node_id || client.node_id} onChange={(value) => (client._target = value)} />
          <Button icon={<SwapOutlined />} onClick={() => {
            const nodeID = client._target || client.preferred_node_id || client.node_id;
            modal.confirm({ title: "切换并更新 DNS", content: `确认把 ${client.id} 切换到 ${nodeID}?`, onOk: () => switchClient(client.id, nodeID) });
          }}>切换</Button>
          <Button onClick={() => clearClientTarget(client.id)}>清除</Button>
        </Space.Compact>
      ),
    },
  ];

  const menuItems = [
    { key: "overview", icon: <DashboardOutlined />, label: "总览" },
    { key: "nodes", icon: <CloudServerOutlined />, label: "代理节点" },
    { key: "clients", icon: <NodeIndexOutlined />, label: "客户端代理" },
    { key: "dns", icon: <CloudOutlined />, label: "阿里云 DNS" },
    { key: "security", icon: <SafetyCertificateOutlined />, label: "认证安全" },
    { key: "ops", icon: <ToolOutlined />, label: "运维向导" },
  ];

  return (
    <Layout className="admin-shell">
      <Sider width={248} className="admin-sider">
        <div className="brand-block">
          <Avatar shape="square" icon={<CloudServerOutlined />} />
          <div>
            <div className="brand-title">frp-cluster</div>
            <div className="brand-subtitle">Proxy Control Plane</div>
          </div>
        </div>
        <Menu theme="dark" mode="inline" selectedKeys={[activeKey]} items={menuItems} onClick={({ key }) => setActiveKey(key)} />
      </Sider>
      <Layout>
        <Header className="admin-header">
          <div>
            <Title level={3}>{pageTitle(activeKey)}</Title>
            <Text type="secondary">稳定入口 {snapshot.config.public_entry_host || "未配置"} · 本月切换 {snapshot.summary.switches_this_month}</Text>
          </div>
          <Space>
            <Button icon={<ReloadOutlined />} loading={loading} onClick={refresh}>刷新</Button>
            <Button icon={<LogoutOutlined />} onClick={async () => { await api("/api/v1/auth/logout", { method: "POST" }); window.location.reload(); }}>退出</Button>
          </Space>
        </Header>
        <Content className="admin-content">
          {activeKey === "overview" && (
            <Space direction="vertical" size="large" className="full">
              <Row gutter={[16, 16]}>
                <Metric title="在线节点" value={snapshot.summary.online_nodes} icon={<CloudServerOutlined />} />
                <Metric title="在线客户端" value={snapshot.summary.online_clients} icon={<NodeIndexOutlined />} />
                <Metric title="在线代理" value={snapshot.summary.online_proxies} icon={<ControlOutlined />} />
                <Metric title="本月切换" value={snapshot.summary.switches_this_month} icon={<SwapOutlined />} />
              </Row>
              <TrafficPanel traffic={traffic} nodes={snapshot.nodes} />
              <Alert type={snapshot.config.public_entry_host && snapshot.config.dns_update_hook ? "success" : "warning"} showIcon message="DNS 稳定入口" description={snapshot.config.public_entry_host && snapshot.config.dns_update_hook ? `${snapshot.config.public_entry_host} 由 ${snapshot.config.dns_update_hook} 更新` : "请配置稳定入口和 DNS Hook 后再执行切换。"} />
              <Row gutter={[16, 16]}>
                <Col xs={24} xl={14}><Card title="节点网络概览"><Table rowKey="id" columns={nodeColumns.slice(0, 6)} dataSource={snapshot.nodes} pagination={false} size="middle" scroll={{ x: 900 }} /></Card></Col>
                <Col xs={24} xl={10}><Card title="最近事件"><EventList events={snapshot.events} /></Card></Col>
              </Row>
            </Space>
          )}
          {activeKey === "nodes" && <Card title="代理服务器节点" extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => setGuide("node")}>新建代理节点</Button>}><Table rowKey="id" columns={nodeColumns} dataSource={snapshot.nodes} pagination={false} scroll={{ x: 1100 }} /></Card>}
          {activeKey === "clients" && <Card title="客户端与端口代理" extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => setGuide("client")}>新建客户端代理</Button>}><Table rowKey="id" columns={clientColumns} dataSource={snapshot.clients} pagination={false} scroll={{ x: 1250 }} /></Card>}
          {activeKey === "dns" && (
            <Row gutter={[16, 16]}>
              <Col xs={24} xl={12}>
                <Card title="阿里云 DNS API">
                  <Form form={aliForm} layout="vertical" onFinish={saveAliDNS}>
                    <Form.Item name="access_key_id" label="AccessKey ID" rules={[{ required: true }]}><Input /></Form.Item>
                    <Form.Item name="access_key_secret" label={`AccessKey Secret${adminConfig.alidns?.access_key_secret_set ? "（已配置，留空则不修改）" : ""}`}>
                      <Input.Password placeholder={adminConfig.alidns?.access_key_secret_set ? "留空保持现有 Secret" : "请输入 Secret"} />
                    </Form.Item>
                    <Row gutter={12}>
                      <Col span={12}><Form.Item name="domain_name" label="主域名"><Input /></Form.Item></Col>
                      <Col span={12}><Form.Item name="rr" label="固定 RR"><Input placeholder="留空则由稳定入口自动推导" /></Form.Item></Col>
                      <Col span={12}><Form.Item name="ttl" label="TTL"><Input /></Form.Item></Col>
                      <Col span={12}><Form.Item name="line" label="解析线路"><Input /></Form.Item></Col>
                      <Col span={24}><Form.Item name="endpoint" label="API Endpoint"><Input /></Form.Item></Col>
                    </Row>
                    <Button type="primary" htmlType="submit">保存阿里云配置</Button>
                  </Form>
                </Card>
              </Col>
              <Col xs={24} xl={12}>
                <Card title="集群切换策略">
                  <Form form={settingsForm} layout="vertical" onFinish={updateSettings}>
                    <Form.Item name="auto_migration" label="自动最优代理节点选择和切换" valuePropName="checked"><Switch checkedChildren="开启" unCheckedChildren="关闭" /></Form.Item>
                    <Form.Item name="migration_score_gap" label="自动切换分数阈值"><InputNumber min={0} className="full" /></Form.Item>
                    <Form.Item name="public_entry_host" label="稳定入口域名"><Input /></Form.Item>
                    <Form.Item name="dns_update_hook" label="DNS 更新 Hook"><Input /></Form.Item>
                    <Button type="primary" htmlType="submit">保存切换策略</Button>
                  </Form>
                </Card>
              </Col>
            </Row>
          )}
          {activeKey === "security" && <SecurityPanel onRefresh={refresh} />}
          {activeKey === "ops" && (
            <OpsPanel
              setGuide={setGuide}
              joinCommand={joinCommand}
              joinForm={joinForm}
              generateJoinCommand={generateJoinCommand}
              clientForm={clientForm}
              clientValues={clientValues}
              clientCommand={clientCommand}
              nodeMaterial={nodeMaterial}
              clientMaterial={clientMaterial}
              agentForm={agentForm}
              saveAgent={saveAgent}
            />
          )}
        </Content>
      </Layout>
      <Drawer title={guide === "node" ? "一键新增代理节点" : "一键配置客户端端口转发"} width={720} open={!!guide} onClose={() => setGuide(null)}>
        {guide === "node" ? <NodeGuide joinForm={joinForm} generateJoinCommand={generateJoinCommand} joinCommand={joinCommand} nodeMaterial={nodeMaterial} /> : <ClientGuide clientForm={clientForm} clientValues={clientValues} clientCommand={clientCommand} clientMaterial={clientMaterial} />}
      </Drawer>
    </Layout>
  );
}

function Metric({ title, value, icon }) {
  return <Col xs={12} lg={6}><Card><Statistic title={title} value={value} prefix={icon} /></Card></Col>;
}

function TrafficPanel({ traffic, nodes }) {
  const nodeNames = useMemo(() => Object.fromEntries((nodes || []).map((node) => [node.id, node.name || node.id])), [nodes]);
  const totals = traffic?.totals || {};
  return (
    <Card title="代理节点流量趋势" extra={<Text type="secondary">近 24 小时 · 采样约 1 分钟</Text>}>
      <Row gutter={[16, 16]}>
        <Col xs={24} md={8}><Statistic title="累计总入站" value={fmtBytes(totals.total_in_bytes)} /></Col>
        <Col xs={24} md={8}><Statistic title="累计总出站" value={fmtBytes(totals.total_out_bytes)} /></Col>
        <Col xs={24} md={8}><Statistic title="近 24 小时增量" value={fmtBytes((totals.delta_in_bytes || 0) + (totals.delta_out_bytes || 0))} /></Col>
      </Row>
      <TrafficChart samples={traffic?.samples || []} nodes={traffic?.nodes || []} nodeNames={nodeNames} />
    </Card>
  );
}

function TrafficChart({ samples, nodes, nodeNames }) {
  const chart = useMemo(() => buildTrafficChart(samples), [samples]);
  if (!chart.points.length) {
    return <div className="empty-chart">等待 agent 上报 frps dashboard 流量样本</div>;
  }
  return (
    <div className="traffic-chart-wrap">
      <svg className="traffic-chart" viewBox="0 0 900 260" role="img" aria-label="代理节点流量趋势">
        <line className="chart-axis" x1="56" y1="212" x2="872" y2="212" />
        <line className="chart-axis" x1="56" y1="28" x2="56" y2="212" />
        {[0, 0.5, 1].map((ratio) => (
          <g key={ratio}>
            <line className="chart-grid" x1="56" y1={212 - ratio * 184} x2="872" y2={212 - ratio * 184} />
            <text className="chart-label" x="18" y={216 - ratio * 184}>{fmtBytes(chart.maxValue * ratio)}</text>
          </g>
        ))}
        <polyline className="chart-line chart-line-total" points={chart.totalLine} fill="none" />
        {chart.nodeLines.map((line, index) => (
          <polyline key={line.nodeID} className="chart-line" stroke={nodePalette[index % nodePalette.length]} points={line.points} fill="none" />
        ))}
        {chart.ticks.map((tick) => (
          <text key={tick.x} className="chart-label" x={tick.x} y="238" textAnchor="middle">{tick.label}</text>
        ))}
      </svg>
      <div className="traffic-legend">
        <span><i className="legend-dot total" />总流量</span>
        {nodes.map((node, index) => (
          <span key={node.node_id}><i className="legend-dot" style={{ background: nodePalette[index % nodePalette.length] }} />{nodeNames[node.node_id] || node.node_id} {fmtBytes((node.totals?.delta_in_bytes || 0) + (node.totals?.delta_out_bytes || 0))}</span>
        ))}
      </div>
    </div>
  );
}

function buildTrafficChart(samples) {
  const sorted = [...samples].sort((a, b) => new Date(a.at) - new Date(b.at));
  const byTime = new Map();
  const nodeIDs = [...new Set(sorted.map((sample) => sample.node_id).filter(Boolean))].sort();
  for (const sample of sorted) {
    const key = new Date(sample.at).getTime();
    if (!Number.isFinite(key)) continue;
    const bucket = byTime.get(key) || { at: key, total: 0, nodes: {} };
    const value = (sample.delta_in_bytes || 0) + (sample.delta_out_bytes || 0);
    bucket.total += value;
    bucket.nodes[sample.node_id] = value;
    byTime.set(key, bucket);
  }
  const points = [...byTime.values()].sort((a, b) => a.at - b.at);
  const minAt = points[0]?.at || Date.now();
  const maxAt = points[points.length - 1]?.at || minAt + 1;
  const maxValue = Math.max(1, ...points.map((point) => point.total), ...points.flatMap((point) => nodeIDs.map((nodeID) => point.nodes[nodeID] || 0)));
  const x = (at) => 56 + ((at - minAt) / Math.max(1, maxAt - minAt)) * 816;
  const y = (value) => 212 - (value / maxValue) * 184;
  const lineFor = (fn) => points.map((point) => `${x(point.at).toFixed(1)},${y(fn(point)).toFixed(1)}`).join(" ");
  const ticks = [points[0], points[Math.floor(points.length / 2)], points[points.length - 1]].filter(Boolean).map((point) => ({
    x: x(point.at),
    label: new Date(point.at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
  }));
  return {
    points,
    maxValue,
    totalLine: lineFor((point) => point.total),
    nodeLines: nodeIDs.map((nodeID) => ({ nodeID, points: lineFor((point) => point.nodes[nodeID] || 0) })),
    ticks,
  };
}

function NodeCell({ node }) {
  return <Space direction="vertical" size={0}><Text strong>{node.id}</Text><Text type="secondary">{node.region || "-"}</Text></Space>;
}

function ClientCell({ client }) {
  return <Space direction="vertical" size={0}><Text strong>{client.id}</Text><Text type="secondary">{client.remote_addr || client.user || "-"}</Text></Space>;
}

function NetworkCell({ network = {} }) {
  return <Space direction="vertical" size={0}><Text code>{network.latency_ms || "-"} ms / {fmtMbps(network.bandwidth_bps)}</Text><Text type="secondary">score {network.score || 0}{network.stale ? " / stale" : ""}</Text></Space>;
}

function ProxyTags({ proxies }) {
  if (!proxies.length) return <Text type="secondary">无代理</Text>;
  return <Space wrap>{proxies.map((proxy) => <Tag key={proxy.id} color={proxy.status === "online" ? "blue" : "default"}>{proxy.name} / {proxy.type} / {proxy.remote_port}</Tag>)}</Space>;
}

function TargetCell({ client }) {
  return <Space direction="vertical" size={2}><Text>{client.preferred_node_id ? `${client.migration_state} -> ${client.preferred_node_id}` : "粘住当前节点"}</Text>{client.migration_reason && <Text type="secondary">{client.migration_reason}</Text>}</Space>;
}

function EventList({ events }) {
  return <Space direction="vertical" className="full">{events.slice(0, 8).map((event) => <div className="event-row" key={event.id}><Text code>{event.type}</Text><Text>{event.message}</Text><Text type="secondary">{fmtTime(event.created_at)}</Text></div>)}</Space>;
}

function SecurityPanel() {
  const [setup, setSetup] = useState(null);
  const [qr, setQr] = useState("");
  const [form] = Form.useForm();
  const { message } = App.useApp();
  const create = async (values) => {
    const result = await api("/api/v1/auth/totp/setup", { method: "POST", body: JSON.stringify(values) });
    setSetup(result);
    form.setFieldsValue({ ...values, secret: result.secret });
    setQr(await QRCode.toDataURL(result.otpauth_uri, { margin: 1, width: 220 }));
  };
  const confirm = async (values) => {
    await api("/api/v1/auth/totp/confirm", { method: "POST", body: JSON.stringify(values) });
    message.success("Authenticator 已更新");
  };
  return <Card title="Microsoft Authenticator">
    <Alert type="info" showIcon message="使用标准 TOTP 动态验证码。" description="扫描二维码绑定 Microsoft Authenticator；轮换绑定时需输入服务器初始化口令或在已登录会话中操作。" />
    <Divider />
    <Form form={form} layout="vertical" onFinish={setup ? confirm : create} initialValues={{ account: "admin" }}>
      <Row gutter={12}>
        <Col xs={24} md={12}><Form.Item name="bootstrap_password" label="初始化口令"><Input.Password /></Form.Item></Col>
        <Col xs={24} md={12}><Form.Item name="account" label="账号标签"><Input /></Form.Item></Col>
      </Row>
      {setup && <><div className="qr-box">{qr && <img src={qr} alt="Authenticator QR" />}</div><Text code copyable>{setup.secret}</Text><Form.Item hidden name="secret"><Input /></Form.Item><Form.Item name="code" label="6 位验证码" rules={[{ required: true, len: 6 }]}><Input maxLength={6} /></Form.Item></>}
      <Button type="primary" htmlType="submit">{setup ? "确认绑定" : "生成二维码"}</Button>
    </Form>
  </Card>;
}

function OpsPanel({ setGuide, joinCommand, joinForm, generateJoinCommand, clientForm, clientValues, clientCommand, nodeMaterial, clientMaterial, agentForm, saveAgent }) {
  const clientEnv = clientMaterial().env;
  return <Space direction="vertical" size="large" className="full">
    <Row gutter={[16, 16]}>
      <Col xs={24} xl={12}>
        <Card title="一键新增代理节点" extra={<Button icon={<PlusOutlined />} onClick={() => setGuide("node")}>打开向导</Button>}>
          <Paragraph>生成 join token、节点加入命令和 proxy-node.env，安装脚本会写入 systemd、frps 配置和 agent 心跳服务。</Paragraph>
          {joinCommand && <CommandBlock value={joinCommand} />}
        </Card>
      </Col>
      <Col xs={24} xl={12}>
        <Card title="一键配置客户端端口转发" extra={<Button icon={<ControlOutlined />} onClick={() => setGuide("client")}>打开向导</Button>}>
          <Paragraph>生成 client.env 和 failover 客户端命令，当前节点不可连时会按间隔尝试其他代理节点。</Paragraph>
          <Space wrap>
            <Button icon={<DownloadOutlined />} onClick={() => downloadText("client.env", clientEnv)}>下载 client.env</Button>
          </Space>
          <CommandBlock value={clientCommand(clientValues)} />
        </Card>
      </Col>
    </Row>
    <Card title="代理节点网络信息更新速率">
      <Form form={agentForm} layout="vertical" onFinish={saveAgent}>
        <Row gutter={12}>
          <Col xs={24} md={8}><Form.Item name="interval" label="网络信息上报间隔"><Input placeholder="30s" /></Form.Item></Col>
          <Col xs={24} md={8}><Form.Item name="probe_size" label="主动测速大小 bytes"><Input placeholder="262144" /></Form.Item></Col>
          <Col xs={24} md={8}><Form.Item name="frps_dashboard_url" label="frps dashboard URL"><Input placeholder="http://127.0.0.1:7500" /></Form.Item></Col>
        </Row>
        <Button type="primary" htmlType="submit">保存并重启 Agent</Button>
      </Form>
    </Card>
  </Space>;
}

function NodeGuide({ joinForm, generateJoinCommand, joinCommand, nodeMaterial }) {
  const values = Form.useWatch([], joinForm) || {};
  const material = nodeMaterial(values);
  const downloadNodeEnv = async () => {
    const formValues = await joinForm.validateFields();
    const joinToken = await generateJoinCommand(formValues);
    downloadText("proxy-node.env", nodeMaterial(formValues, joinToken).env);
  };
  return (
    <Space direction="vertical" className="full" size="large">
      <Alert type="info" showIcon message="在新代理节点安装包目录执行生成的脚本命令。" />
      <Form layout="vertical" form={joinForm} onFinish={generateJoinCommand} initialValues={{
        bootstrap_control_url: window.location.origin,
        node_control_url: window.location.origin,
        bind_port: 7000,
        allow_ports: "11000-12000",
        vhost_http_port: 0,
        vhost_https_port: 0,
        control_listen: ":8080",
        control_data: "/var/lib/frp-cluster/cluster.json",
        web_dir: "/usr/local/share/frp-cluster/web",
        admin_password_file: "/etc/frp-cluster/admin-password",
        auth_config_file: "/etc/frp-cluster/auth.env",
        alidns_config_file: "/etc/frp-cluster/alidns.env",
        agent_interval: "30s",
        probe_size: "262144",
        frps_dashboard_url: "http://127.0.0.1:7500",
        write_frps_config: "/etc/frp/frps.toml",
      }}>
        <Row gutter={12}>
          <Col xs={24} md={12}><Form.Item name="node_id" label="节点 ID" rules={[{ required: true }]}><Input placeholder="edge-hk-1" /></Form.Item></Col>
          <Col xs={24} md={12}><Form.Item name="public_addr" label="节点公网 IP" rules={[{ required: true }]}><Input /></Form.Item></Col>
          <Col xs={24} md={12}><Form.Item name="bootstrap_control_url" label="已有控制面 URL"><Input /></Form.Item></Col>
          <Col xs={24} md={12}><Form.Item name="node_control_url" label="本节点控制面 URL" rules={[{ required: true }]}><Input placeholder="http://1.2.3.4:8080" /></Form.Item></Col>
          <Col xs={24} md={8}><Form.Item name="bind_port" label="frps 接入端口"><InputNumber className="full" /></Form.Item></Col>
          <Col xs={24} md={8}><Form.Item name="vhost_http_port" label="HTTP vhost"><InputNumber className="full" /></Form.Item></Col>
          <Col xs={24} md={8}><Form.Item name="vhost_https_port" label="HTTPS vhost"><InputNumber className="full" /></Form.Item></Col>
          <Col xs={24} md={12}><Form.Item name="allow_ports" label="允许远端端口"><Input /></Form.Item></Col>
          <Col xs={24} md={12}><Form.Item name="region" label="区域"><Input /></Form.Item></Col>
          <Col xs={24}><Form.Item name="tags" label="标签"><Input placeholder="prod,cn-north" /></Form.Item></Col>
          <Col xs={24} md={8}><Form.Item name="agent_interval" label="agent 上报间隔"><Input /></Form.Item></Col>
          <Col xs={24} md={8}><Form.Item name="probe_size" label="主动测速大小 bytes"><Input /></Form.Item></Col>
          <Col xs={24} md={8}><Form.Item name="frps_dashboard_url" label="frps dashboard URL"><Input /></Form.Item></Col>
          <Col xs={24}><Form.Item name="write_frps_config" label="frps 配置路径"><Input /></Form.Item></Col>
        </Row>
        <Space wrap>
          <Button type="primary" htmlType="submit" icon={<PlusOutlined />}>生成加入命令</Button>
          <Button icon={<DownloadOutlined />} onClick={downloadNodeEnv}>下载 proxy-node.env</Button>
        </Space>
      </Form>
      {joinCommand && <CommandBlock value={joinCommand} />}
      <CommandBlock title="目标机器一键执行" value={material.installCommand} />
    </Space>
  );
}

function ClientGuide({ clientForm, clientValues, clientCommand, clientMaterial }) {
  const material = clientMaterial(clientValues);
  return (
    <Space direction="vertical" className="full" size="large">
      <Alert type="info" showIcon message="客户端会优先粘住当前可用节点；当前节点不可连时按 failover-interval 尝试其他节点。" />
      <Form layout="vertical" form={clientForm} initialValues={{
        control_url: window.location.origin,
        client_id: "local-ssh",
        mode: "failover",
        limit: 1,
        interval: "30s",
        failover_interval: "10s",
        drain_timeout: "30s",
        proxies: "ssh:tcp:127.0.0.1:22:11022",
        frpc_bin: "/usr/local/bin/frpc",
        work_dir: "/var/lib/frp-cluster/frpc.d",
      }}>
        <Row gutter={12}>
          <Col xs={24} md={12}><Form.Item name="control_url" label="控制面 URL"><Input /></Form.Item></Col>
          <Col xs={24} md={12}><Form.Item name="client_id" label="客户端 ID"><Input /></Form.Item></Col>
          <Col xs={24} md={8}><Form.Item name="mode" label="模式"><Select options={[{ value: "failover", label: "failover" }, { value: "single", label: "single" }, { value: "aggregate", label: "aggregate" }]} /></Form.Item></Col>
          <Col xs={24} md={8}><Form.Item name="limit" label="节点数量"><InputNumber min={1} className="full" /></Form.Item></Col>
          <Col xs={24} md={8}><Form.Item name="interval" label="配置刷新间隔"><Input /></Form.Item></Col>
          <Col xs={24} md={12}><Form.Item name="failover_interval" label="故障重试间隔"><Input /></Form.Item></Col>
          <Col xs={24} md={12}><Form.Item name="drain_timeout" label="旧进程保留时间"><Input /></Form.Item></Col>
          <Col xs={24}><Form.Item name="proxies" label="端口转发列表"><Input.TextArea rows={3} placeholder="ssh:tcp:127.0.0.1:22:11022;web:tcp:127.0.0.1:8080:18080" /></Form.Item></Col>
          <Col xs={24} md={12}><Form.Item name="frpc_bin" label="frpc 路径"><Input /></Form.Item></Col>
          <Col xs={24} md={12}><Form.Item name="work_dir" label="配置目录"><Input /></Form.Item></Col>
        </Row>
      </Form>
      <Space wrap>
        <Button icon={<DownloadOutlined />} onClick={() => downloadText("client.env", material.env)}>下载 client.env</Button>
      </Space>
      <CommandBlock value={clientCommand(clientValues)} />
      <CommandBlock title="目标机器一键执行" value={material.installCommand} />
    </Space>
  );
}

function CommandBlock({ title = "命令", value }) {
  const { message } = App.useApp();
  return <Card className="command-card"><Space align="start" className="command-head"><Text code>{title}</Text><Button size="small" icon={<CopyOutlined />} onClick={() => navigator.clipboard.writeText(value).then(() => message.success("已复制"))}>复制</Button></Space><pre>{value}</pre></Card>;
}

function pageTitle(key) {
  return { overview: "集群总览", nodes: "代理节点", clients: "客户端代理", dns: "阿里云 DNS 与切换策略", security: "认证安全", ops: "运维向导" }[key] || "控制台";
}

function buildNodeMaterial(values = {}, joinToken = "") {
  const env = envText({
    BOOTSTRAP_CONTROL_URL: values.bootstrap_control_url || window.location.origin,
    NODE_CONTROL_URL: values.node_control_url || window.location.origin,
    JOIN_TOKEN: joinToken || "auto",
    NODE_ID: values.node_id || "edge-a",
    PUBLIC_ADDR: values.public_addr || "THIS_NODE_PUBLIC_IP",
    BIND_PORT: values.bind_port || 7000,
    ALLOW_PORTS: values.allow_ports || "11000-12000",
    VHOST_HTTP_PORT: values.vhost_http_port || 0,
    VHOST_HTTPS_PORT: values.vhost_https_port || 0,
    CONTROL_LISTEN: values.control_listen || ":8080",
    CONTROL_DATA: values.control_data || "/var/lib/frp-cluster/cluster.json",
    PUBLIC_ENTRY_HOST: values.public_entry_host || "",
    DNS_UPDATE_HOOK: values.dns_update_hook || "",
    WEB_DIR: values.web_dir || "/usr/local/share/frp-cluster/web",
    ADMIN_PASSWORD_FILE: values.admin_password_file || "/etc/frp-cluster/admin-password",
    AUTH_CONFIG_FILE: values.auth_config_file || "/etc/frp-cluster/auth.env",
    ALIDNS_CONFIG_FILE: values.alidns_config_file || "/etc/frp-cluster/alidns.env",
    REGION: values.region || "",
    TAGS: values.tags || "",
    AGENT_INTERVAL: values.agent_interval || "30s",
    PROBE_SIZE: values.probe_size || "262144",
    FRPS_DASHBOARD_URL: values.frps_dashboard_url || "http://127.0.0.1:7500",
  });
  return { env, installCommand: "sudo ./scripts/proxy-node-join.sh ./proxy-node.env" };
}

function buildClientMaterial(values = {}) {
  const env = envText({
    CONTROL_URL: values.control_url || window.location.origin,
    CLIENT_ID: values.client_id || "local-ssh",
    MODE: values.mode || "failover",
    LIMIT: values.limit || 1,
    INTERVAL: values.interval || "30s",
    FAILOVER_INTERVAL: values.failover_interval || "10s",
    DRAIN_TIMEOUT: values.drain_timeout || "30s",
    PROXIES: values.proxies || "ssh:tcp:127.0.0.1:22:11022",
    WORK_DIR: values.work_dir || "/var/lib/frp-cluster/frpc.d",
    FRPC_BIN: values.frpc_bin || "/usr/local/bin/frpc",
  });
  return { env, installCommand: "sudo ./scripts/client-install.sh ./client.env" };
}

function envText(values) {
  return Object.entries(values).map(([key, value]) => `${key}=${envValue(value)}`).join("\n") + "\n";
}

function envValue(value) {
  const raw = String(value ?? "");
  if (/^[A-Za-z0-9_./:@,%+-]*$/.test(raw)) return raw;
  return `'${raw.replaceAll("'", "'\\''")}'`;
}

function downloadText(filename, content) {
  const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

function shellArg(value) {
  const raw = String(value || "");
  if (/^[A-Za-z0-9_./:=@-]+$/.test(raw)) return raw;
  return `'${raw.replaceAll("'", "'\\''")}'`;
}

function Root() {
  const [auth, setAuth] = useState(null);
  const checkAuth = async () => {
    try {
      setAuth(await api("/api/v1/auth/me"));
    } catch {
      setAuth({ auth_enabled: true, authenticated: false, bootstrap_required: true });
    }
  };
  useEffect(() => { checkAuth(); }, []);
  if (!auth) return <div className="loading-state">加载认证状态...</div>;
  if (auth.auth_enabled && !auth.authenticated) return <LoginView auth={auth} onLogin={checkAuth} />;
  return <Dashboard />;
}

createRoot(document.getElementById("root")).render(<App><Root /></App>);
