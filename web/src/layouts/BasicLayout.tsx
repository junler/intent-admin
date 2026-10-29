import { Layout, Menu, Tag, Typography } from 'antd';
import { ApiOutlined, AppstoreOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { Outlet, useLocation, useNavigate } from 'react-router';

import { Omnibox } from '@/components/omnibox/Omnibox';

const { Header, Sider, Content } = Layout;

/**
 * BasicLayout is the shell: sidebar menu, header with the global natural
 * language input, content outlet.
 *
 * The Omnibox lives in the header rather than behind a sidebar "AI assistant"
 * item on purpose — it is a primary entry point equal to the menu, not a
 * feature page you navigate to.
 *
 * Phase 1 replaces the hardcoded menu with the tree from GET /api/getRouters.
 */
export function BasicLayout() {
  const navigate = useNavigate();
  const location = useLocation();

  const items = [
    {
      key: '/demo/item',
      icon: <AppstoreOutlined />,
      label: '示例条目',
    },
    {
      key: '/capabilities',
      icon: <ApiOutlined />,
      label: '能力浏览器',
    },
  ];

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
          mode="inline"
          items={items}
          selectedKeys={[location.pathname]}
          onClick={({ key }) => navigate(key)}
          className="border-e-0"
        />
      </Sider>

      <Layout>
        <Header className="flex items-center gap-4 border-b border-gray-200 bg-white px-4">
          <Omnibox />
          <Tag color="orange">Phase 0 · 认证未接入</Tag>
        </Header>
        <Content className="overflow-auto p-4">
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
