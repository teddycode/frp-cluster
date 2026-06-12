import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Alert,
  App,
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
  CloudServerOutlined,
  ControlOutlined,
  CopyOutlined,
  KeyOutlined,
  LoginOutlined,
  LogoutOutlined,
  NodeIndexOutlined,
  PlusOutlined,
  ReloadOutlined,
  SettingOutlined,
  SwapOutlined,
} from "@ant-design/icons";
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
const statusColor = (value) => (value === "online" ? "green" : value === "draining" ? "gold" : "red");

function LoginView({ onLogin }) {
  const { message } = App.useApp();
  const [loading, setLoading] = useState(false);
  const submit = async ({ password }) => {
    setLoading(true);
    try {
      await api("/api/v1/auth/login", { method: "POST", body: JSON.stringify({ password }) });
      onLogin();
    } catch (err) {
      message.error(err.message);
    } finally {
      setLoading(false);
    }
  };
  return (
    <div className="login-shell">
      <Form className="login-panel" layout="vertical" onFinish={submit}>
        <Space direction="vertical" size={4}>
          <Title level={2}>frp-cluster</Title>
          <Text type="secondary">控制面登录</Text>
        </Space>
        <Form.Item name="password" label="口令密码" rules={[{ required: true, message: "请输入口令密码" }]}>
          <Input.Password prefix={<KeyOutlined />} autoFocus />
        </Form.Item>
        <Button type="primary" htmlType="submit" icon={<LoginOutlined />} loading={loading} block>
          登录
        </Button>
      </Form>
    </div>
  );
}

function Dashboard() {
  const { message, modal } = App.useApp();
  const [snapshot, setSnapshot] = useState(null);
  const [loading, setLoading] = useState(false);
  const [guide, setGuide] = useState(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [joinCommand, setJoinCommand] = useState("");
  const [token, setToken] = useState("");
  const [settingsForm] = Form.useForm();
  const [joinForm] = Form.useForm();
  const [clientForm] = Form.useForm();
  const clientValues = Form.useWatch([], clientForm) || {};

  const refresh = async () => {
    setLoading(true);
    try {
      const data = await api("/api/v1/cluster");
      setSnapshot(data);
      settingsForm.setFieldsValue({
        auto_migration: data.config?.auto_migration,
        migration_score_gap: data.config?.migration_score_gap,
        public_entry_host: data.config?.public_entry_host,
        dns_update_hook: data.config?.dns_update_hook,
      });
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
    try {
      await api("/api/v1/settings", { method: "PATCH", body: JSON.stringify(values) });
      message.success("设置已保存");
      setSettingsOpen(false);
      refresh();
    } catch (err) {
      message.error(err.message);
    }
  };

  const switchClient = async (clientID, nodeID) => {
    try {
      await api(`/api/v1/clients/${encodeURIComponent(clientID)}/target`, {
        method: "POST",
        body: JSON.stringify({ node_id: nodeID }),
      });
      message.success("切换已提交");
      refresh();
    } catch (err) {
      message.error(err.message);
    }
  };

  const clearClientTarget = async (clientID) => {
    try {
      await api(`/api/v1/clients/${encodeURIComponent(clientID)}/target`, {
        method: "POST",
        body: JSON.stringify({ node_id: "" }),
      });
      message.success("手动目标已清除");
      refresh();
    } catch (err) {
      message.error(err.message);
    }
  };

  const testDNS = async (nodeID) => {
    try {
      const result = await api("/api/v1/dns/test", {
        method: "POST",
        body: JSON.stringify({ node_id: nodeID }),
      });
      message.success(`DNS 已更新到 ${result.dns?.target_ip || nodeID}`);
      refresh();
    } catch (err) {
      message.error(err.message);
    }
  };

  const createToken = async () => {
    const created = await api("/api/v1/tokens", { method: "POST", body: JSON.stringify({ ttl: "2h", uses: 1 }) });
    setToken(created.token);
    return created.token;
  };

  const generateJoinCommand = async (values) => {
    try {
      const joinToken = token || (await createToken());
      const params = new URLSearchParams({
        token: joinToken,
        node_id: values.node_id,
        public_addr: values.public_addr,
        bind_port: String(values.bind_port || 7000),
        node_control_url: values.node_control_url,
        write_frps_config: values.write_frps_config || "/etc/frp/frps.toml",
      });
      const result = await api(`/api/v1/commands/join?${params}`);
      setJoinCommand(result.command);
    } catch (err) {
      message.error(err.message);
    }
  };

  const clientCommand = (values) => {
    const proxies = values.proxies || "ssh:tcp:127.0.0.1:22:11022";
    return [
      "frp-cluster client",
      `--control-url ${shellArg(values.control_url || window.location.origin)}`,
      `--client-id ${shellArg(values.client_id || "local-ssh")}`,
      "--mode failover",
      "--limit 1",
      "--interval 30s",
      `--work-dir ${shellArg(values.work_dir || "/var/lib/frp-cluster/frpc.d")}`,
      `--frpc-bin ${shellArg(values.frpc_bin || "/usr/local/bin/frpc")}`,
      ...proxies.split(";").filter(Boolean).map((proxy) => `--proxy ${shellArg(proxy.trim())}`),
    ].join(" ");
  };

  if (!snapshot) {
    return <div className="loading-state">加载控制面状态...</div>;
  }

  const nodeColumns = [
    {
      title: "节点",
      dataIndex: "id",
      render: (_, node) => (
        <Space direction="vertical" size={0}>
          <Text strong>{node.id}</Text>
          <Text type="secondary">{node.region || "-"}</Text>
        </Space>
      ),
    },
    { title: "状态", dataIndex: "status", render: (value) => <Tag color={statusColor(value)}>{value}</Tag> },
    { title: "frps", render: (_, node) => <Text code>{node.public_addr}:{node.bind_port}</Text> },
    {
      title: "网络",
      render: (_, node) => (
        <Text code>{node.network?.latency_ms || "-"} ms / {fmtMbps(node.network?.bandwidth_bps)} / score {node.network?.score || 0}</Text>
      ),
    },
    { title: "负载", render: (_, node) => `${node.client_count} 客户端 / ${node.proxy_count} 代理` },
    { title: "最后心跳", dataIndex: "last_seen_at", render: fmtTime },
    {
      title: "操作",
      render: (_, node) => (
        <Space>
          <Popconfirm title="DNS 自检会真实更新解析" description={`确认指向 ${node.id}?`} onConfirm={() => testDNS(node.id)}>
            <Button size="small" icon={<ApiOutlined />}>DNS 自检</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const clientColumns = [
    {
      title: "客户端",
      dataIndex: "id",
      render: (_, client) => (
        <Space direction="vertical" size={0}>
          <Text strong>{client.id}</Text>
          <Text type="secondary">{client.remote_addr || client.user || "-"}</Text>
        </Space>
      ),
    },
    { title: "在线", dataIndex: "status", render: (value) => <Tag color={statusColor(value)}>{value}</Tag> },
    { title: "当前节点", dataIndex: "node_id", render: (value) => <Text code>{value || "-"}</Text> },
    {
      title: "代理与协议",
      render: (_, client) => (
        <Space wrap>
          {(client.proxies || []).map((proxy) => (
            <Tag key={proxy.id} color={proxy.status === "online" ? "blue" : "default"}>
              {proxy.name} / {proxy.type} / {proxy.remote_port}
            </Tag>
          ))}
          {(client.proxies || []).length === 0 && <Text type="secondary">无代理</Text>}
        </Space>
      ),
    },
    {
      title: "目标",
      render: (_, client) => (
        <Space direction="vertical" size={4}>
          <Text>{client.preferred_node_id ? `${client.migration_state} -> ${client.preferred_node_id}` : "粘住当前节点"}</Text>
          {client.migration_reason && <Text type="secondary">{client.migration_reason}</Text>}
        </Space>
      ),
    },
    {
      title: "切换",
      width: 300,
      render: (_, client) => (
        <Space.Compact block>
          <Select
            className="client-target-select"
            options={nodeOptions}
            defaultValue={client.preferred_node_id || client.node_id}
            onChange={(value) => (client._target = value)}
          />
          <Button
            icon={<SwapOutlined />}
            onClick={() => {
              const nodeID = client._target || client.preferred_node_id || client.node_id;
              modal.confirm({
                title: "切换并更新 DNS",
                content: `确认把 ${client.id} 切换到 ${nodeID}?`,
                onOk: () => switchClient(client.id, nodeID),
              });
            }}
          />
          <Button onClick={() => clearClientTarget(client.id)}>清除</Button>
        </Space.Compact>
      ),
    },
  ];

  return (
    <Layout className="app-shell">
      <Sider width={232} className="side-nav">
        <div className="brand">frp-cluster</div>
        <Space direction="vertical" className="nav-actions">
          <Button icon={<ReloadOutlined />} onClick={refresh} loading={loading}>刷新</Button>
          <Button icon={<SettingOutlined />} onClick={() => setSettingsOpen(true)}>控制面设置</Button>
          <Button icon={<PlusOutlined />} onClick={() => setGuide("node")}>新增代理节点</Button>
          <Button icon={<ControlOutlined />} onClick={() => setGuide("client")}>配置客户端转发</Button>
        </Space>
      </Sider>
      <Layout>
        <Header className="topbar">
          <Space direction="vertical" size={0}>
            <Title level={3}>集群控制面</Title>
            <Text type="secondary">入口 {snapshot.config.public_entry_host || "未配置"} / 本月切换 {snapshot.summary.switches_this_month}</Text>
          </Space>
          <Button
            icon={<LogoutOutlined />}
            onClick={async () => {
              await api("/api/v1/auth/logout", { method: "POST" });
              window.location.reload();
            }}
          >
            退出
          </Button>
        </Header>
        <Content className="content">
          <Row gutter={[16, 16]}>
            <Col xs={12} lg={6}><Card><Statistic title="在线节点" value={snapshot.summary.online_nodes} prefix={<CloudServerOutlined />} /></Card></Col>
            <Col xs={12} lg={6}><Card><Statistic title="在线客户端" value={snapshot.summary.online_clients} prefix={<NodeIndexOutlined />} /></Card></Col>
            <Col xs={12} lg={6}><Card><Statistic title="在线代理" value={snapshot.summary.online_proxies} /></Card></Col>
            <Col xs={12} lg={6}><Card><Statistic title="本月切换" value={snapshot.summary.switches_this_month} prefix={<SwapOutlined />} /></Card></Col>
          </Row>
          <Alert
            className="entry-alert"
            type={snapshot.config.public_entry_host && snapshot.config.dns_update_hook ? "success" : "warning"}
            showIcon
            message="DNS 稳定入口"
            description={snapshot.config.public_entry_host && snapshot.config.dns_update_hook ? `${snapshot.config.public_entry_host} 由 ${snapshot.config.dns_update_hook} 更新` : "请配置 PUBLIC_ENTRY_HOST 和 DNS_UPDATE_HOOK 后再执行节点切换。"}
          />
          <Tabs
            items={[
              { key: "nodes", label: "代理节点", children: <Table rowKey="id" columns={nodeColumns} dataSource={snapshot.nodes} pagination={false} scroll={{ x: 1100 }} /> },
              { key: "clients", label: "客户端与端口", children: <Table rowKey="id" columns={clientColumns} dataSource={snapshot.clients} pagination={false} scroll={{ x: 1200 }} /> },
              {
                key: "events",
                label: "事件",
                children: <Table rowKey="id" size="small" dataSource={snapshot.events} columns={[
                  { title: "时间", dataIndex: "created_at", render: fmtTime },
                  { title: "类型", dataIndex: "type", render: (value) => <Text code>{value}</Text> },
                  { title: "节点", dataIndex: "node_id" },
                  { title: "客户端", dataIndex: "client_id" },
                  { title: "消息", dataIndex: "message" },
                ]} pagination={{ pageSize: 8 }} />,
              },
              {
                key: "stats",
                label: "切换统计",
                children: <Table rowKey="month" dataSource={snapshot.switch_metrics} columns={[
                  { title: "月份", dataIndex: "month" },
                  { title: "总次数", dataIndex: "count" },
                  { title: "手动", dataIndex: "manual" },
                  { title: "自动", dataIndex: "automatic" },
                  { title: "更新时间", dataIndex: "updated_at", render: fmtTime },
                ]} pagination={false} />,
              },
            ]}
          />
        </Content>
      </Layout>

      <Drawer title="控制面设置" width={520} open={settingsOpen} onClose={() => setSettingsOpen(false)}>
        <Form layout="vertical" form={settingsForm} onFinish={updateSettings}>
          <Form.Item name="auto_migration" label="自动最优代理节点选择和切换" valuePropName="checked">
            <Switch checkedChildren="开启" unCheckedChildren="关闭" />
          </Form.Item>
          <Form.Item name="migration_score_gap" label="自动推荐分数阈值">
            <InputNumber min={0} className="full" />
          </Form.Item>
          <Form.Item name="public_entry_host" label="稳定入口域名">
            <Input placeholder="ssh.buaadcl.tech" />
          </Form.Item>
          <Form.Item name="dns_update_hook" label="DNS 更新 Hook">
            <Input placeholder="/usr/local/bin/frp-cluster-alidns-update" />
          </Form.Item>
          <Button type="primary" htmlType="submit">保存设置</Button>
        </Form>
      </Drawer>

      <Drawer title={guide === "node" ? "一键新增代理节点" : "一键配置客户端端口转发"} width={720} open={!!guide} onClose={() => setGuide(null)}>
        {guide === "node" ? (
          <Space direction="vertical" className="full" size="large">
            <Alert type="info" showIcon message="在新代理节点安装包目录执行生成的命令。" />
            <Form layout="vertical" form={joinForm} onFinish={generateJoinCommand} initialValues={{ bind_port: 7000, write_frps_config: "/etc/frp/frps.toml" }}>
              <Row gutter={12}>
                <Col span={12}><Form.Item name="node_id" label="节点 ID" rules={[{ required: true }]}><Input placeholder="edge-hk-1" /></Form.Item></Col>
                <Col span={12}><Form.Item name="public_addr" label="节点公网 IP" rules={[{ required: true }]}><Input /></Form.Item></Col>
                <Col span={12}><Form.Item name="node_control_url" label="节点控制面 URL" rules={[{ required: true }]}><Input placeholder="http://1.2.3.4:8088" /></Form.Item></Col>
                <Col span={12}><Form.Item name="bind_port" label="frps 接入端口"><InputNumber className="full" /></Form.Item></Col>
                <Col span={24}><Form.Item name="write_frps_config" label="frps 配置路径"><Input /></Form.Item></Col>
              </Row>
              <Button type="primary" htmlType="submit" icon={<PlusOutlined />}>生成加入命令</Button>
            </Form>
            {joinCommand && <CommandBlock value={joinCommand} />}
          </Space>
        ) : (
          <Space direction="vertical" className="full" size="large">
            <Alert type="info" showIcon message="客户端会优先粘住当前可用节点；当前节点不可连时按 failover-interval 尝试其他节点。" />
            <Form layout="vertical" form={clientForm} initialValues={{ control_url: window.location.origin, client_id: "local-ssh", proxies: "ssh:tcp:127.0.0.1:22:11022", frpc_bin: "/usr/local/bin/frpc", work_dir: "/var/lib/frp-cluster/frpc.d" }}>
              <Form.Item name="control_url" label="控制面 URL"><Input /></Form.Item>
              <Form.Item name="client_id" label="客户端 ID"><Input /></Form.Item>
              <Form.Item name="proxies" label="端口转发列表"><Input.TextArea rows={3} /></Form.Item>
              <Row gutter={12}>
                <Col span={12}><Form.Item name="frpc_bin" label="frpc 路径"><Input /></Form.Item></Col>
                <Col span={12}><Form.Item name="work_dir" label="配置目录"><Input /></Form.Item></Col>
              </Row>
            </Form>
            <CommandBlock value={clientCommand(clientValues)} />
          </Space>
        )}
      </Drawer>
    </Layout>
  );
}

function CommandBlock({ value }) {
  const { message } = App.useApp();
  return (
    <Card className="command-card">
      <Space align="start" className="command-head">
        <Text code>命令</Text>
        <Button size="small" icon={<CopyOutlined />} onClick={() => navigator.clipboard.writeText(value).then(() => message.success("已复制"))}>复制</Button>
      </Space>
      <pre>{value}</pre>
    </Card>
  );
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
      const data = await api("/api/v1/auth/me");
      setAuth(data);
    } catch {
      setAuth({ auth_enabled: true, authenticated: false });
    }
  };
  useEffect(() => {
    checkAuth();
  }, []);
  if (!auth) return <div className="loading-state">加载登录状态...</div>;
  if (auth.auth_enabled && !auth.authenticated) return <LoginView onLogin={checkAuth} />;
  return <Dashboard />;
}

createRoot(document.getElementById("root")).render(
  <App>
    <Root />
  </App>
);
