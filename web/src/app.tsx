import { StyleProvider } from '@ant-design/cssinjs';
import {
  ApiOutlined,
  AppstoreOutlined,
  AuditOutlined,
  BarsOutlined,
  BookOutlined,
  CheckSquareOutlined,
  CloudOutlined,
  DashboardOutlined,
  DatabaseOutlined,
  DownOutlined,
  FileTextOutlined,
  FolderOutlined,
  HistoryOutlined,
  KeyOutlined,
  LoginOutlined,
  LogoutOutlined,
  MenuOutlined,
  MonitorOutlined,
  NotificationOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
  ScheduleOutlined,
  SearchOutlined,
  SettingOutlined,
  TeamOutlined,
  UserOutlined,
} from '@ant-design/icons';
import type { RunTimeLayoutConfig } from '@umijs/max';
import { history, Link, Navigate, useLocation, useModel } from '@umijs/max';
import {
  App,
  Avatar,
  Button,
  ConfigProvider,
  Dropdown,
  Result,
  Spin,
  Tooltip,
  Typography,
} from 'antd';
import { type ReactNode, useEffect, useState } from 'react';
import { AiFloatingButton } from '@/components/ai/AiFloatingButton';
import { AiPanel, useAiPanel } from '@/components/ai/AiPanel';
import { NotificationBell } from '@/components/notifications/NotificationBell';
import { Omnibox } from '@/components/omnibox/Omnibox';
import { usePerm } from '@/hooks/usePerm';
import { t } from '@/i18n';
import LocaleSwitcher from '@/i18n/LocaleSwitcher';
import DynamicPage from '@/pages/dynamic';
import { type MenuRoute, visibleMenus } from '@/router/menu';
import { type Identity, useUserStore } from '@/stores/user';
import { api, auth } from '@/utils/request';
import defaultSettings from '../config/defaultSettings';

// Ant Design Pro's initial-state/layout/access conventions are backed by the
// same authenticated user store as API calls and permission-aware business pages.
export async function getInitialState(): Promise<{
  identity: Identity | null;
  menus: MenuRoute[];
}> {
  if (auth.get() && history.location.pathname !== '/login') {
    try {
      await useUserStore.getState().load();
    } catch {
      /* AuthGate offers retry. */
    }
  }
  return {
    identity: useUserStore.getState().identity,
    menus: useUserStore.getState().menus,
  };
}

function UserActions() {
  const identity = useUserStore((s) => s.identity);
  const { setInitialState } = useModel('@@initialState');
  const [busy, setBusy] = useState(false);
  async function signOut() {
    setBusy(true);
    try {
      await useUserStore.getState().signOut();
      await setInitialState((s) => ({ ...s, identity: null, menus: [] }));
      history.replace('/login');
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dropdown
      placement="bottomRight"
      trigger={['click']}
      menu={{
        items: [
          {
            key: 'identity',
            disabled: true,
            label: (
              <div className="intent-user-menu-heading">
                <strong>
                  {identity?.user.nickName || identity?.user.userName}
                </strong>
                <span>{identity?.user.userName}</span>
              </div>
            ),
          },
          { type: 'divider' },
          {
            key: 'profile',
            icon: <UserOutlined />,
            label: t('个人中心'),
            onClick: () => history.push('/system/profile'),
          },
          {
            key: 'token',
            icon: <KeyOutlined />,
            label: t('访问令牌'),
            onClick: () => history.push('/system/token'),
          },
          {
            key: 'mcp-grants',
            icon: <SafetyCertificateOutlined />,
            label: t('MCP 授权'),
            onClick: () => history.push('/system/mcp-grants'),
          },
          { type: 'divider' },
          {
            key: 'logout',
            icon: <LogoutOutlined />,
            label: t('退出登录'),
            danger: true,
            disabled: busy,
            onClick: () => void signOut(),
          },
        ],
      }}
    >
      <Button
        type="text"
        className="intent-avatar-button"
        loading={busy}
        aria-label={t('账号菜单')}
      >
        <Avatar size={30} src={identity?.user.avatar} icon={<UserOutlined />} />
        <span className="intent-avatar-name">
          {identity?.user.nickName || identity?.user.userName}
        </span>
        <DownOutlined className="intent-avatar-chevron" />
      </Button>
    </Dropdown>
  );
}

function HeaderSearch() {
  const allowed = usePerm('ai:agent:use');
  const reveal = useAiPanel((state) => state.reveal);
  return (
    <>
      <div className="intent-header-search">
        <Omnibox />
      </div>
      <div className="intent-mobile-search">
        <Tooltip title={t('AI 会话')}>
          <Button
            type="text"
            icon={<SearchOutlined />}
            aria-label={t('AI 会话')}
            disabled={!allowed}
            onClick={reveal}
          />
        </Tooltip>
      </div>
    </>
  );
}

function AuthGate({ children }: { children: ReactNode }) {
  const { ready, load, reset } = useUserStore();
  const location = useLocation();
  const { setInitialState } = useModel('@@initialState');
  const [failed, setFailed] = useState(false);
  const token = auth.get();
  useEffect(() => {
    if (token && !ready) void load().catch(() => setFailed(true));
  }, [token, ready, load]);
  useEffect(() => {
    if (!ready || !token) return;
    let lastActivity = Date.now();
    const activity = () => {
      lastActivity = Date.now();
    };
    const refresh = () => {
      if (!document.hidden && Date.now() - lastActivity < 5 * 60_000)
        void api
          .post('/refreshToken', undefined, { silent: true })
          .catch(() => {});
    };
    const storage = (event: StorageEvent) => {
      if (event.key === 'intent-admin:token' && event.newValue !== token) {
        reset();
        window.location.replace('/login');
      }
    };
    const timer = window.setInterval(refresh, 60_000);
    window.addEventListener('pointerdown', activity);
    window.addEventListener('keydown', activity);
    window.addEventListener('focus', refresh);
    window.addEventListener('storage', storage);
    return () => {
      clearInterval(timer);
      window.removeEventListener('pointerdown', activity);
      window.removeEventListener('keydown', activity);
      window.removeEventListener('focus', refresh);
      window.removeEventListener('storage', storage);
    };
  }, [ready, token, reset]);
  useEffect(() => {
    const changed = () => {
      reset();
      void load()
        .then(() =>
          setInitialState((state) => ({
            ...state,
            identity: useUserStore.getState().identity,
            menus: useUserStore.getState().menus,
          })),
        )
        .catch(() => setFailed(true));
    };
    window.addEventListener('intent-admin:permissions-changed', changed);
    return () =>
      window.removeEventListener('intent-admin:permissions-changed', changed);
  }, [reset, load, setInitialState]);
  if (!token)
    return (
      <Navigate
        to={`/login?redirect=${encodeURIComponent(location.pathname + location.search + location.hash)}`}
        replace
      />
    );
  if (failed && !ready)
    return (
      <Result
        status="error"
        title={t('加载账号失败')}
        extra={
          <Button
            onClick={() => {
              setFailed(false);
              void load().catch(() => setFailed(true));
            }}
          >
            {t('重试')}
          </Button>
        }
      />
    );
  if (!ready)
    return (
      <div className="intent-page-loading">
        <Spin size="large" />
        <Typography.Text type="secondary">
          {t('正在加载工作空间')}
        </Typography.Text>
      </div>
    );
  return (
    <>
      {children}
      <AiPanel />
      <AiFloatingButton />
    </>
  );
}

const menuIcons: Record<string, ReactNode> = {
  DashboardOutlined: <DashboardOutlined />,
  UserOutlined: <UserOutlined />,
  TeamOutlined: <TeamOutlined />,
  MenuOutlined: <MenuOutlined />,
  BookOutlined: <BookOutlined />,
  SettingOutlined: <SettingOutlined />,
  NotificationOutlined: <NotificationOutlined />,
  FolderOutlined: <FolderOutlined />,
  ApiOutlined: <ApiOutlined />,
  CloudOutlined: <CloudOutlined />,
  KeyOutlined: <KeyOutlined />,
  ScheduleOutlined: <ScheduleOutlined />,
  FileTextOutlined: <FileTextOutlined />,
  LoginOutlined: <LoginOutlined />,
  MonitorOutlined: <MonitorOutlined />,
  DatabaseOutlined: <DatabaseOutlined />,
  HistoryOutlined: <HistoryOutlined />,
  AuditOutlined: <AuditOutlined />,
  RobotOutlined: <RobotOutlined />,
  AppstoreOutlined: <AppstoreOutlined />,
  BarsOutlined: <BarsOutlined />,
  SafetyCertificateOutlined: <SafetyCertificateOutlined />,
  CheckSquareOutlined: <CheckSquareOutlined />,
};
function proMenus(
  routes: MenuRoute[],
  depth = 0,
): Array<{
  path: string;
  name: string;
  icon: ReactNode;
  menuDepth: number;
  isUrl?: boolean;
  target?: string;
  children?: ReturnType<typeof proMenus>;
}> {
  return visibleMenus(routes).map((route) => ({
    path: route.path,
    name: route.meta?.title ? t(route.meta.title) : route.path,
    icon: menuIcons[route.meta?.icon ?? ''] ?? <AppstoreOutlined />,
    menuDepth: depth,
    isUrl: route.external,
    target: route.external ? '_blank' : undefined,
    children: route.children?.length
      ? proMenus(route.children, depth + 1)
      : undefined,
  }));
}

// ProLayout omits nested icons by default; keep its root rendering for the collapsed rail.
function menuTitle(
  item: { menuDepth?: number; icon?: ReactNode; name?: string },
  dom: ReactNode,
) {
  return item.menuDepth ? (
    <span className="intent-submenu-title">
      <span className="intent-submenu-icon">{item.icon}</span>
      <span className="intent-submenu-text">{item.name}</span>
    </span>
  ) : (
    dom
  );
}

export const layout: RunTimeLayoutConfig = ({ initialState }) => ({
  ...defaultSettings,
  menu: {
    locale: false,
    params: { userId: initialState?.identity?.user.userId },
    request: async () => proMenus(useUserStore.getState().menus),
  },
  menuDataRender: () => proMenus(useUserStore.getState().menus),
  subMenuItemRender: menuTitle,
  menuItemRender: (item, dom) =>
    item.isUrl && item.path ? (
      <a href={item.path} target="_blank" rel="noopener noreferrer">
        {menuTitle(item, dom)}
      </a>
    ) : item.path ? (
      <Link to={item.path}>{menuTitle(item, dom)}</Link>
    ) : (
      menuTitle(item, dom)
    ),
  headerContentRender: () => <HeaderSearch />,
  actionsRender: () => [
    <NotificationBell key="notifications" />,
    <LocaleSwitcher key="locale" />,
    <UserActions key="user" />,
  ],
  footerRender: () => (
    <div className="intent-footer">
      <span>Intent</span>
      <span>Ant Design Pro</span>
    </div>
  ),
  childrenRender: () => (
    <AuthGate>
      <DynamicPage />
    </AuthGate>
  ),
});

export function rootContainer(container: ReactNode) {
  return (
    <StyleProvider layer>
      <ConfigProvider
        modal={{ centered: true }}
        pagination={{ styles: { root: { justifyContent: 'center' } } }}
        theme={{
          token: {
            colorPrimary: '#2563eb',
            colorInfo: '#2563eb',
            colorText: '#172b4d',
            colorTextSecondary: '#66758c',
            colorBgLayout: '#f4f7fb',
            colorBorderSecondary: '#edf0f5',
            borderRadius: 8,
            fontSize: 14,
            controlHeight: 36,
            fontFamily:
              "Inter, -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif",
          },
          components: {
            Card: { headerFontSize: 15, headerHeight: 56 },
            Table: {
              headerBg: '#f8fafc',
              headerColor: '#56657b',
              rowHoverBg: '#f6f9ff',
              cellPaddingBlock: 15,
            },
            Form: { itemMarginBottom: 22 },
            Button: { primaryShadow: '0 2px 4px rgb(37 99 235 / 12%)' },
            Modal: { borderRadiusLG: 12 },
          },
        }}
      >
        <App>{container}</App>
      </ConfigProvider>
    </StyleProvider>
  );
}
