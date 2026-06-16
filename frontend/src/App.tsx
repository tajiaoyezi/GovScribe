import {
  Alert,
  App as AntApp,
  Button,
  Form,
  Input,
  Layout,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
  Typography
} from "antd";
import type { ColumnsType } from "antd/es/table";
import {
  BookOpen,
  ClipboardCheck,
  FileText,
  KeyRound,
  LogOut,
  Search,
  ShieldCheck,
  UserCog,
  Users
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import {
  ApiError,
	ManagedUser,
	assignRole,
	changePassword,
	createUser,
	disableUser,
	listUsers,
	login,
	resetPassword
} from "./api";
import {
  RoleCode,
  SessionPrincipal,
  clearToken,
  hasRole,
  loadToken,
  parseSession,
  saveToken
} from "./auth";

const roleLabels: Record<RoleCode, string> = {
  system_admin: "系统管理员",
  secretary: "文秘",
  business_user: "业务兼职用户",
  auditor: "审计员"
};

type LoginValues = {
  username: string;
  password: string;
};

type PasswordValues = {
  newPassword: string;
};

type UserValues = {
  username: string;
  password: string;
  department: string;
  role: RoleCode;
};

export function App() {
  const [token, setToken] = useState(loadToken);
  const [principal, setPrincipal] = useState<SessionPrincipal | null>(() => parseSession(loadToken()));

  useEffect(() => {
    const parsed = parseSession(token);
    setPrincipal(parsed);
    if (!parsed && token) {
      clearToken();
      setToken("");
    }
  }, [token]);

  const onAuthenticated = (nextToken: string) => {
    saveToken(nextToken);
    setToken(nextToken);
  };

  const onLogout = () => {
    clearToken();
    setToken("");
    setPrincipal(null);
  };

  if (!principal) {
    return (
      <AntApp>
        <LoginPage onAuthenticated={onAuthenticated} />
      </AntApp>
    );
  }

  if (principal.mustChangePassword) {
    return (
      <AntApp>
        <PasswordChangePage token={token} onChanged={onLogout} />
      </AntApp>
    );
  }

  return (
    <AntApp>
      <Layout className="appShell">
        <Layout.Sider className="sideNav" width={248}>
          <div className="brand">
            <ShieldCheck size={22} />
            <span>GovScribe</span>
          </div>
          <RoleSummary principal={principal} />
          <Button className="logoutButton" icon={<LogOut size={16} />} onClick={onLogout}>
            退出
          </Button>
        </Layout.Sider>
        <Layout.Content className="workspace">
          <Tabs
            defaultActiveKey={hasRole(principal, "system_admin") ? "users" : "workbench"}
            items={[
              ...(hasRole(principal, "system_admin")
                ? [
                    {
                      key: "users",
                      label: (
                        <span className="tabLabel">
                          <UserCog size={16} /> 用户角色
                        </span>
                      ),
                      children: <UserAdmin token={token} />
                    }
                  ]
                : []),
              {
                key: "workbench",
                label: (
                  <span className="tabLabel">
                    <FileText size={16} /> 工作入口
                  </span>
                ),
                children: <RoleWorkbench principal={principal} />
              }
            ]}
          />
        </Layout.Content>
      </Layout>
    </AntApp>
  );
}

function LoginPage({ onAuthenticated }: { onAuthenticated: (token: string) => void }) {
  const { message } = AntApp.useApp();
  const [loading, setLoading] = useState(false);

  const onFinish = async (values: LoginValues) => {
    setLoading(true);
    try {
      const response = await login(values.username, values.password);
      onAuthenticated(response.token);
    } catch {
      message.error("用户名或密码错误");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="authPage">
      <div className="authPanel">
        <div className="authHeader">
          <ShieldCheck size={30} />
          <div>
            <Typography.Title level={2}>GovScribe</Typography.Title>
            <Typography.Text type="secondary">统一身份认证</Typography.Text>
          </div>
        </div>
        <Form<LoginValues> layout="vertical" onFinish={onFinish} autoComplete="off">
          <Form.Item name="username" label="用户名" rules={[{ required: true, message: "请输入用户名" }]}>
            <Input autoFocus prefix={<Users size={16} />} />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, message: "请输入密码" }]}>
            <Input.Password prefix={<KeyRound size={16} />} />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={loading} block>
            登录
          </Button>
        </Form>
      </div>
    </div>
  );
}

function PasswordChangePage({ token, onChanged }: { token: string; onChanged: () => void }) {
  const { message } = AntApp.useApp();
  const [loading, setLoading] = useState(false);

  const onFinish = async (values: PasswordValues) => {
    setLoading(true);
    try {
      await changePassword(token, values.newPassword);
      message.success("密码已更新，请重新登录");
      onChanged();
    } catch {
      message.error("密码更新失败");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="authPage">
      <div className="authPanel">
        <div className="authHeader">
          <KeyRound size={30} />
          <div>
            <Typography.Title level={2}>首次改密</Typography.Title>
            <Typography.Text type="secondary">完成后重新登录</Typography.Text>
          </div>
        </div>
        <Form<PasswordValues> layout="vertical" onFinish={onFinish}>
          <Form.Item
            name="newPassword"
            label="新密码"
            rules={[
              { required: true, message: "请输入新密码" },
              { min: 10, message: "至少 10 个字符" }
            ]}
          >
            <Input.Password prefix={<KeyRound size={16} />} />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={loading} block>
            更新密码
          </Button>
        </Form>
      </div>
    </div>
  );
}

function RoleSummary({ principal }: { principal: SessionPrincipal }) {
  return (
    <div className="roleSummary">
      <Typography.Text strong>{principal.username}</Typography.Text>
      <div className="roleTags">
        {principal.roles.map((role) => (
          <Tag key={role}>{roleLabels[role]}</Tag>
        ))}
      </div>
    </div>
  );
}

function RoleWorkbench({ principal }: { principal: SessionPrincipal }) {
  const entries = useMemo(() => {
    if (hasRole(principal, "auditor")) {
      return [{ key: "audit", label: "审计只读", icon: <ShieldCheck size={18} /> }];
    }
    const items = [];
    if (hasRole(principal, "secretary") || hasRole(principal, "business_user")) {
      items.push({ key: "draft", label: "起草", icon: <FileText size={18} /> });
      items.push({ key: "template", label: "范文检索", icon: <Search size={18} /> });
      items.push({ key: "document", label: "文档打开", icon: <BookOpen size={18} /> });
    }
    if (hasRole(principal, "secretary")) {
      items.push({ key: "review", label: "在线核稿", icon: <ClipboardCheck size={18} /> });
      items.push({ key: "ingest", label: "范文入库", icon: <ShieldCheck size={18} /> });
    }
    return items;
  }, [principal]);

  return (
    <section className="panel">
      <Typography.Title level={3}>工作入口</Typography.Title>
      <div className="entryGrid">
        {entries.map((entry) => (
          <button className="entryButton" key={entry.key} type="button">
            {entry.icon}
            <span>{entry.label}</span>
          </button>
        ))}
      </div>
    </section>
  );
}

function UserAdmin({ token }: { token: string }) {
  const { message } = AntApp.useApp();
  const [users, setUsers] = useState<ManagedUser[]>([]);
  const [loading, setLoading] = useState(false);
  const [creating, setCreating] = useState(false);
  const [form] = Form.useForm<UserValues>();

  const refresh = async () => {
    setLoading(true);
    try {
      setUsers(await listUsers(token));
    } catch (error) {
      if (error instanceof ApiError && error.status === 403) {
        message.error("没有用户管理权限");
      } else {
        message.error("用户列表加载失败");
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void refresh();
  }, []);

  const onCreate = async (values: UserValues) => {
    setCreating(true);
    try {
      const created = await createUser(token, values);
      setUsers((current) => [created, ...current]);
      form.resetFields();
      message.success("账号已创建");
    } catch {
      message.error("创建失败");
    } finally {
      setCreating(false);
    }
  };

  const columns: ColumnsType<ManagedUser> = [
    { title: "用户名", dataIndex: "username", key: "username" },
    { title: "部门", dataIndex: "department", key: "department" },
    {
      title: "角色",
      key: "roles",
      render: (_, record) => (
        <Select<RoleCode>
          className="roleSelect"
          value={record.roles[0]}
          options={roleOptions()}
          onChange={async (role) => {
            await assignRole(token, record.id, role);
            setUsers((current) => current.map((item) => (item.id === record.id ? { ...item, roles: [role] } : item)));
            message.success("角色已更新");
          }}
        />
      )
    },
    {
      title: "启用",
      key: "isActive",
      width: 92,
      render: (_, record) => (
        <Switch
          checked={record.isActive}
          disabled={!record.isActive}
          onChange={async () => {
            await disableUser(token, record.id);
            setUsers((current) => current.map((item) => (item.id === record.id ? { ...item, isActive: false } : item)));
          }}
        />
      )
    },
    {
      title: "首次改密",
      key: "mustChangePassword",
      width: 110,
      render: (_, record) => (record.mustChangePassword ? <Tag color="gold">是</Tag> : <Tag>否</Tag>)
    },
    {
      title: "操作",
      key: "action",
      width: 120,
      render: (_, record) => (
        <Button
          size="small"
          icon={<KeyRound size={14} />}
          onClick={() => {
            Modal.confirm({
              title: `重置 ${record.username} 的密码`,
              content: <ResetPasswordForm token={token} userId={record.id} onDone={() => void refresh()} />
            });
          }}
        >
          重置
        </Button>
      )
    }
  ];

  return (
    <section className="panel">
      <div className="panelHeader">
        <Typography.Title level={3}>用户角色</Typography.Title>
        <Button onClick={() => void refresh()}>刷新</Button>
      </div>
      <Form<UserValues> className="createUserForm" form={form} layout="inline" onFinish={onCreate}>
        <Form.Item name="username" rules={[{ required: true, message: "用户名必填" }]}>
          <Input placeholder="用户名" />
        </Form.Item>
        <Form.Item name="department">
          <Input placeholder="部门" />
        </Form.Item>
        <Form.Item
          name="password"
          rules={[
            { required: true, message: "初始密码必填" },
            { min: 10, message: "至少 10 个字符" }
          ]}
        >
          <Input.Password placeholder="初始密码" minLength={10} />
        </Form.Item>
        <Form.Item name="role" initialValue="business_user">
          <Select<RoleCode> className="roleSelect" options={roleOptions()} />
        </Form.Item>
        <Button type="primary" htmlType="submit" loading={creating}>
          创建账号
        </Button>
      </Form>
      <Alert className="thinAlert" type="info" showIcon message="停用、分配角色、重置密码均由后端统一判定点校验。" />
      <Table rowKey="id" loading={loading} columns={columns} dataSource={users} pagination={{ pageSize: 8 }} />
    </section>
  );
}

function ResetPasswordForm({ token, userId, onDone }: { token: string; userId: string; onDone: () => void }) {
  const { message } = AntApp.useApp();
  const [loading, setLoading] = useState(false);
  return (
    <Form<{ password: string }>
      layout="vertical"
      onFinish={async ({ password }) => {
        setLoading(true);
        try {
          await resetPassword(token, userId, password);
          message.success("密码已重置");
          onDone();
          Modal.destroyAll();
        } catch {
          message.error("重置失败");
        } finally {
          setLoading(false);
        }
      }}
    >
      <Form.Item
        name="password"
        label="新密码"
        rules={[
          { required: true, message: "请输入新密码" },
          { min: 10, message: "至少 10 个字符" }
        ]}
      >
        <Input.Password placeholder="新密码" minLength={10} />
      </Form.Item>
      <Button type="primary" htmlType="submit" loading={loading}>
        确认
      </Button>
    </Form>
  );
}

function roleOptions() {
  return (Object.keys(roleLabels) as RoleCode[]).map((role) => ({
    value: role,
    label: roleLabels[role]
  }));
}
