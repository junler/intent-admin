import { useMemo, useState } from 'react';
import {
  Avatar,
  Button,
  Layout,
  Menu,
  Tag,
  Typography,
  message,
  type MenuProps,
} from 'antd';
import {
  ApiOutlined,
  AppstoreOutlined,
  FolderOutlined,
  LogoutOutlined,
  ThunderboltOutlined,
  UserOutlined,
} from '@ant-design/icons';
import { Outlet, useLocation, useNavigate } from 'react-router';

import { Omnibox } from '@/components/omnibox/Omnibox';
import { visibleMenus, type MenuRoute } from '@/router/menu';
import { useUserStore } from '@/stores/user';

const { Header, Sider, Content } = Layout;

function menuItems(routes: MenuRoute[]): MenuProps['items'] {
  return visibleMenus(routes).map((route) => ({
    key: route.path,
    label: route.meta?.title ?? route.path,
    icon: route.children?.length ? (
      <FolderOutlined />
    ) : route.component?.startsWith('capabilities') ? (
      <ApiOutlined />
    ) : (
      <AppstoreOutlined />
    ),
    children: route.children?.length ? menuItems(route.children) : undefined,
  }));
}

export function BasicLayout() {
  const navigate = useNavigate();
  const location = useLocation();
  const { menus, identity, signOut } = useUserStore();
  const [loggingOut, setLoggingOut] = useState(false);
  const items = useMemo(() => menuItems(menus), [menus]);

  async function logout() {
    setLoggingOut(true);
    try {
      await signOut();
      navigate('/login', { replace: true });
    } catch {
      message.error('退出失败，请重试');
    } finally {
      setLoggingOut(false);
    }
  }

  return (
    <Layout className="h-full">
      <Sider theme="light" width={208} breakpoint="lg" collapsedWidth={64}>
        <div className="flex h-14 items-center gap-2 px-4">
          <ThunderboltOutlined className="text-xl text-blue-600" />
          <Typography.Text strong className="text-base">
            Intent
          </Typography.Text>
        </div>
        <Menu
          key={menus.map((menu) => menu.path).join('|')}
          mode="inline"
          items={items}
          selectedKeys={[location.pathname]}
          defaultOpenKeys={menus
            .filter((route) => route.children?.length)
            .map((route) => route.path)}
          onClick={({ key }) => navigate(key)}
          className="border-e-0"
        />
      </Sider>
      <Layout>
        <Header className="flex items-center gap-4 border-b border-gray-200 bg-white px-4">
          <Omnibox />
          <div className="ms-auto flex shrink-0 items-center gap-2">
            <Tag color="blue">AI 上限：{identity?.aiMaxRisk}</Tag>
            <Avatar
              size="small"
              src={identity?.user.avatar || undefined}
              icon={<UserOutlined />}
            />
            <Typography.Text>
              {identity?.user.nickName || identity?.user.userName}
            </Typography.Text>
            <Button
              type="text"
              icon={<LogoutOutlined />}
              loading={loggingOut}
              onClick={() => void logout()}
            >
              退出
            </Button>
          </div>
        </Header>
        <Content className="overflow-auto p-4">
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
