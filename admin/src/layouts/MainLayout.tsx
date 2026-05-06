import React, { useState } from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import { Layout, Menu, Avatar, Dropdown, theme, Tag } from 'antd';
import type { MenuProps } from 'antd';
import {
  DashboardOutlined,
  AppstoreOutlined,
  KeyOutlined,
  DesktopOutlined,
  UserOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  FileTextOutlined,
  BarChartOutlined,
  CrownOutlined,
  StopOutlined,
  DownloadOutlined,
  UsergroupAddOutlined,
  SafetyCertificateOutlined,
  ToolOutlined,
  TeamOutlined,
  CloudSyncOutlined,
  ApiOutlined,
  DollarOutlined,
  TagsOutlined,
  SendOutlined,
} from '@ant-design/icons';
import { useAuthStore } from '../store';

const { Header, Sider, Content } = Layout;

type MenuItem = Required<MenuProps>['items'][number];

// 所有菜单项定义
const allMenuItems: MenuItem[] = [
  {
    key: 'workspace',
    icon: <DashboardOutlined />,
    label: '工作台',
    children: [
      { key: '/', icon: <DashboardOutlined />, label: '仪表盘' },
      { key: '/analytics', icon: <BarChartOutlined />, label: '报表分析' },
    ],
  },
  {
    key: 'customer-auth',
    icon: <UsergroupAddOutlined />,
    label: '客户与授权',
    children: [
      { key: '/customers', icon: <UsergroupAddOutlined />, label: '客户管理' },
      { key: '/licenses', icon: <KeyOutlined />, label: '授权码' },
      { key: '/subscriptions', icon: <CrownOutlined />, label: '订阅' },
      { key: '/user-credits', icon: <DollarOutlined />, label: '用户额度' },
    ],
  },
  {
    key: 'device',
    icon: <DesktopOutlined />,
    label: '设备管理',
    children: [
      { key: '/devices', icon: <DesktopOutlined />, label: '设备列表' },
      { key: '/blacklist', icon: <StopOutlined />, label: '黑名单' },
      { key: '/instructions', icon: <SendOutlined />, label: '实时指令' },
    ],
  },
  {
    key: 'app-management',
    icon: <AppstoreOutlined />,
    label: '应用管理',
    children: [
      { key: '/apps', icon: <AppstoreOutlined />, label: '应用列表' },
      { key: '/secure-scripts', icon: <SafetyCertificateOutlined />, label: '安全脚本' },
    ],
  },
  {
    key: 'ai',
    icon: <ApiOutlined />,
    label: 'AI 转发',
    children: [
      { key: '/provider-credentials', icon: <ApiOutlined />, label: 'Provider 凭证' },
      { key: '/pricing-rules', icon: <TagsOutlined />, label: '计价规则' },
    ],
  },
  {
    key: 'system',
    icon: <ToolOutlined />,
    label: '系统管理',
    children: [
      { key: '/team', icon: <TeamOutlined />, label: '团队管理' },
      { key: '/backups', icon: <CloudSyncOutlined />, label: '数据备份' },
      { key: '/audit', icon: <FileTextOutlined />, label: '操作日志' },
      { key: '/export', icon: <DownloadOutlined />, label: '数据导出' },
    ],
  },
];

// 只读用户需要隐藏的菜单
const viewerHiddenMenus = ['system', 'ai', '/team', '/backups', '/audit', '/export', '/instructions', '/secure-scripts'];
const aiManagerRoles = ['owner', 'admin'];
const appManagerRoles = ['owner', 'admin', 'developer'];

const canShowMenuItem = (key: string, role?: string) => {
  if (role === 'viewer' && viewerHiddenMenus.includes(key)) {
    return false;
  }
  if ((key === 'ai' || key === '/provider-credentials' || key === '/pricing-rules' || key === '/user-credits' || key === '/backups' || key === '/audit') && !aiManagerRoles.includes(role || '')) {
    return false;
  }
  if ((key === '/instructions' || key === '/secure-scripts' || key === '/team' || key === '/export') && !appManagerRoles.includes(role || '')) {
    return false;
  }
  return true;
};

// 根据角色过滤菜单
const getMenuItemsByRole = (role?: string): MenuItem[] => {
  const filterItems = (items: MenuItem[]): MenuItem[] => items
    .map(item => {
      if (!item) return null;
      const key = String((item as any)?.key || '');
      const children = (item as any)?.children as MenuItem[] | undefined;
      if (children) {
        const filteredChildren = filterItems(children);
        if (filteredChildren.length === 0 || !canShowMenuItem(key, role)) {
          return null;
        }
        return { ...(item as any), children: filteredChildren } as MenuItem;
      }
      return canShowMenuItem(key, role) ? item : null;
    })
    .filter(Boolean) as MenuItem[];

  return filterItems(allMenuItems);
};

const getParentKeyByPath = (path: string) => {
  if (['/', '/analytics'].includes(path)) return 'workspace';
  if (['/customers', '/licenses', '/subscriptions', '/user-credits'].includes(path)) return 'customer-auth';
  if (['/devices', '/blacklist', '/instructions'].includes(path)) return 'device';
  if (['/apps', '/secure-scripts'].includes(path) || path.startsWith('/apps/')) return 'app-management';
  if (['/provider-credentials', '/pricing-rules'].includes(path)) return 'ai';
  if (['/team', '/backups', '/audit', '/export', '/settings'].includes(path)) return 'system';
  return '';
};

const getSelectedKey = (path: string) => {
  if (path.startsWith('/apps/')) return '/apps';
  return path;
};

const getOpenKeysByPath = (path: string) => {
  const parentKey = getParentKeyByPath(path);
  return parentKey ? [parentKey] : [];
};

const isMenuLeafKey = (key: string) => key.startsWith('/');

const getFirstLeafKey = (item: MenuItem): string | null => {
  const key = String((item as any)?.key || '');
  if (isMenuLeafKey(key)) return key;
  const children = (item as any)?.children as MenuItem[] | undefined;
  if (!children) return null;
  for (const child of children) {
    const childKey = getFirstLeafKey(child);
    if (childKey) return childKey;
  }
  return null;
};

const findMenuItemByKey = (items: MenuItem[], targetKey: string): MenuItem | null => {
  for (const item of items) {
    if (!item) continue;
    const key = (item as any)?.key;
    if (key === targetKey) return item;
    const children = (item as any)?.children as MenuItem[] | undefined;
    if (children) {
      const found = findMenuItemByKey(children, targetKey);
      if (found) return found;
    }
  }
  return null;
};

const MainLayout: React.FC = () => {
  const [collapsed, setCollapsed] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();
  const { user, tenant, logout } = useAuthStore();
  const { token: { colorBgContainer, borderRadiusLG } } = theme.useToken();

  // 根据用户角色获取菜单
  const menuItems = getMenuItemsByRole(user?.role);

  // 根据当前路径获取展开的菜单
  const getOpenKeys = () => {
    return getOpenKeysByPath(location.pathname);
  };

  const [openKeys, setOpenKeys] = useState<string[]>(getOpenKeys());

  const handleMenuClick = ({ key }: { key: string }) => {
    if (isMenuLeafKey(key)) {
      navigate(key);
      return;
    }
    const item = findMenuItemByKey(menuItems, key);
    const firstLeafKey = item ? getFirstLeafKey(item) : null;
    if (firstLeafKey) {
      navigate(firstLeafKey);
    }
  };

  const handleOpenChange = (keys: string[]) => {
    setOpenKeys(keys);
  };

  const handleLogout = () => {
    logout();
    navigate('/login');
  };

  const handleUserMenuClick = ({ key }: { key: string }) => {
    if (key === 'profile') {
      navigate('/profile');
    } else if (key === 'logout') {
      handleLogout();
    }
  };

  const getRoleLabel = (role?: string) => {
    const roleMap: Record<string, { color: string; text: string }> = {
      owner: { color: 'gold', text: '所有者' },
      admin: { color: 'red', text: '管理员' },
      developer: { color: 'blue', text: '开发者' },
      viewer: { color: 'default', text: '只读' },
    };
    const info = roleMap[role || ''] || { color: 'default', text: role };
    return <Tag color={info.color} style={{ marginLeft: 8 }}>{info.text}</Tag>;
  };

  const userMenuItems = [
    { key: 'profile', icon: <UserOutlined />, label: '个人中心' },
    { type: 'divider' as const },
    { key: 'logout', icon: <LogoutOutlined />, label: '退出登录' },
  ];

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider trigger={null} collapsible collapsed={collapsed} theme="dark" width={200}>
        <div style={{
          height: 64,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: '#fff',
          fontSize: collapsed ? 16 : 18,
          fontWeight: 'bold',
        }}>
          {collapsed ? 'LS' : (tenant?.name || '授权管理平台')}
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[getSelectedKey(location.pathname)]}
          openKeys={collapsed ? [] : openKeys}
          onOpenChange={handleOpenChange}
          items={menuItems}
          onClick={handleMenuClick}
        />
      </Sider>
      <Layout>
        <Header style={{
          padding: '0 24px',
          background: colorBgContainer,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}>
          <div
            onClick={() => setCollapsed(!collapsed)}
            style={{ cursor: 'pointer', fontSize: 18 }}
          >
            {collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
          </div>
          <Dropdown menu={{ items: userMenuItems, onClick: handleUserMenuClick }} placement="bottomRight">
            <div style={{ cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 8 }}>
              <Avatar icon={<UserOutlined />} />
              <span>{user?.name || '用户'}</span>
              {getRoleLabel(user?.role)}
            </div>
          </Dropdown>
        </Header>
        <Content style={{
          margin: 24,
          padding: 24,
          background: colorBgContainer,
          borderRadius: borderRadiusLG,
          minHeight: 280,
          overflow: 'auto',
        }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
};

export default MainLayout;
