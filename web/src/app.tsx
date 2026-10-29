import type { RunTimeLayoutConfig } from '@umijs/max';
import { history, Link, Navigate, useLocation, useModel } from '@umijs/max';
import { StyleProvider } from '@ant-design/cssinjs';
import { App, Avatar, Button, Result, Space, Spin, Typography } from 'antd';
import { LogoutOutlined, UserOutlined, AppstoreOutlined } from '@ant-design/icons';
import { useEffect, useState, type ReactNode } from 'react';
import defaultSettings from '../config/defaultSettings';
import { useUserStore, type Identity } from '@/stores/user';
import { auth, api } from '@/utils/request';
import { visibleMenus, type MenuRoute } from '@/router/menu';
import { Omnibox } from '@/components/omnibox/Omnibox';
import { AiPanel } from '@/components/ai/AiPanel';
import { NotificationBell } from '@/components/notifications/NotificationBell';
import DynamicPage from '@/pages/dynamic';

// Ant Design Pro's initial-state/layout/access conventions are backed by the
// same authenticated user store as API calls and permission-aware business pages.
export async function getInitialState(): Promise<{ identity: Identity | null; menus: MenuRoute[] }> {
  if (auth.get() && history.location.pathname !== '/login') {
    try { await useUserStore.getState().load(); } catch { /* AuthGate offers retry. */ }
  }
  return { identity: useUserStore.getState().identity, menus: useUserStore.getState().menus };
}

function UserActions() {
  const identity = useUserStore((s) => s.identity);
  const { setInitialState } = useModel('@@initialState');
  const [busy, setBusy] = useState(false);
  return <Space>
    <Button type="text" onClick={() => history.push('/system/profile')}>
      <Avatar size="small" src={identity?.user.avatar} icon={<UserOutlined />} />{' '}
      {identity?.user.nickName || identity?.user.userName}
    </Button>
    <Button type="text" icon={<LogoutOutlined />} loading={busy} onClick={async () => {
      setBusy(true);
      try {
        await useUserStore.getState().signOut();
        await setInitialState((s) => ({ ...s, identity: null, menus: [] }));
        history.replace('/login');
      } finally { setBusy(false); }
    }}>退出</Button>
  </Space>;
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
    const activity = () => { lastActivity = Date.now(); };
    const refresh = () => {
      if (!document.hidden && Date.now() - lastActivity < 5 * 60_000) void api.post('/refreshToken', undefined, { silent: true }).catch(() => {});
    };
    const storage = (event: StorageEvent) => {
      if (event.key === 'intent-admin:token' && event.newValue !== token) { reset(); window.location.replace('/login'); }
    };
    const timer = window.setInterval(refresh, 60_000);
    window.addEventListener('pointerdown', activity); window.addEventListener('keydown', activity);
    window.addEventListener('focus', refresh); window.addEventListener('storage', storage);
    return () => {
      clearInterval(timer); window.removeEventListener('pointerdown', activity); window.removeEventListener('keydown', activity);
      window.removeEventListener('focus', refresh); window.removeEventListener('storage', storage);
    };
  }, [ready, token, reset]);
  useEffect(() => {
    const changed = () => {
      reset();
      void load().then(() => setInitialState((state) => ({ ...state, identity: useUserStore.getState().identity, menus: useUserStore.getState().menus }))).catch(() => setFailed(true));
    };
    window.addEventListener('intent-admin:permissions-changed', changed);
    return () => window.removeEventListener('intent-admin:permissions-changed', changed);
  }, [reset, load, setInitialState]);
  if (!token) return <Navigate to={`/login?redirect=${encodeURIComponent(location.pathname + location.search + location.hash)}`} replace />;
  if (failed && !ready) return <Result status="error" title="加载账号失败" extra={<Button onClick={() => { setFailed(false); void load().catch(() => setFailed(true)); }}>重试</Button>} />;
  if (!ready) return <div className="p-12 text-center"><Spin size="large" /></div>;
  return <>{children}<AiPanel /></>;
}

function proMenus(routes: MenuRoute[]): Array<{ path: string; name: string; icon: ReactNode; isUrl?: boolean; target?: string; children?: ReturnType<typeof proMenus> }> {
  return visibleMenus(routes).map((route) => ({ path: route.path, name: route.meta?.title ?? route.path,
    icon: <AppstoreOutlined />, isUrl: route.external, target: route.external ? '_blank' : undefined,
    children: route.children?.length ? proMenus(route.children) : undefined }));
}

export const layout: RunTimeLayoutConfig = ({ initialState }) => ({
  ...defaultSettings,
  menu: { locale: false, params: { userId: initialState?.identity?.user.userId }, request: async () => proMenus(useUserStore.getState().menus) },
  menuDataRender: () => proMenus(useUserStore.getState().menus),
  menuItemRender: (item, dom) => item.isUrl && item.path ? <a href={item.path} target="_blank" rel="noopener noreferrer">{dom}</a> : item.path ? <Link to={item.path}>{dom}</Link> : dom,
  headerContentRender: () => <div className="min-w-0 flex-1 px-4"><Omnibox /></div>,
  actionsRender: () => [<NotificationBell key="notifications" />, <UserActions key="user" />],
  footerRender: () => <Typography.Text type="secondary" className="block py-4 text-center">Intent · Ant Design Pro</Typography.Text>,
  childrenRender: () => <AuthGate><DynamicPage /></AuthGate>,
});

export function rootContainer(container: ReactNode) {
  return <StyleProvider layer><App>{container}</App></StyleProvider>;
}
