import {
  lazy,
  Suspense,
  useEffect,
  useMemo,
  useState,
  type ComponentType,
} from 'react';
import {
  createBrowserRouter,
  Navigate,
  useLocation,
  useRoutes,
  type RouteObject,
} from 'react-router';
import { Button, Result, Spin } from 'antd';

import { BasicLayout } from '@/layouts/BasicLayout';
import LoginPage from '@/pages/login';
import { firstPage, flattenPages } from '@/router/menu';
import { useUserStore } from '@/stores/user';
import { api, auth } from '@/utils/request';

const modules = import.meta.glob([
  '../pages/**/*.tsx',
  '!../pages/login/**/*.tsx',
]);
const components = new Map<string, ComponentType>();

/** Resolve only bundled page modules; backend strings never become import URLs. */
export function resolveComponent(component: string): ComponentType {
  const cached = components.get(component);
  if (cached) return cached;
  const loader =
    modules[`../pages/${component}.tsx`] ??
    modules[`../pages/${component}/index.tsx`];
  const page = loader
    ? lazy(loader as never)
    : lazy(() => import('@/pages/error/404'));
  components.set(component, page);
  return page;
}

function Loading() {
  return (
    <div className="flex h-full min-h-64 items-center justify-center">
      <Spin size="large" />
    </div>
  );
}

function page(component: string) {
  const Component = resolveComponent(component);
  return (
    <Suspense fallback={<Loading />}>
      <Component />
    </Suspense>
  );
}

function ProtectedApplication() {
  const location = useLocation();
  const { ready, menus, load, reset } = useUserStore();
  const [error, setError] = useState(false);
  const token = auth.get();

  useEffect(() => {
    if (!token || ready) return;
    let active = true;
    void load().catch(() => {
      if (active) setError(true);
    });
    return () => {
      active = false;
    };
  }, [token, ready, load]);

  useEffect(() => {
    if (!token || !ready) return;
    let lastActivity = Date.now();
    const activity = () => {
      lastActivity = Date.now();
    };
    const refresh = () => {
      if (!document.hidden && Date.now() - lastActivity < 5 * 60_000) {
        void api
          .post('/refreshToken', undefined, { silent: true })
          .catch(() => {});
      }
    };
    const onStorage = (event: StorageEvent) => {
      if (event.key === 'intent-admin:token' && event.newValue !== token) {
        reset();
        window.location.replace('/login');
      }
    };
    window.addEventListener('pointerdown', activity);
    window.addEventListener('keydown', activity);
    window.addEventListener('focus', refresh);
    window.addEventListener('storage', onStorage);
    const timer = window.setInterval(refresh, 60_000);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener('pointerdown', activity);
      window.removeEventListener('keydown', activity);
      window.removeEventListener('focus', refresh);
      window.removeEventListener('storage', onStorage);
    };
  }, [token, ready, reset]);

  const routes = useMemo<RouteObject[]>(() => {
    const pages = flattenPages(menus);
    const seen = new Set<string>();
    return [
      {
        path: '/',
        element: <BasicLayout />,
        children: [
          { index: true, element: <Navigate to={firstPage(menus)} replace /> },
          ...pages
            .filter((entry) => {
              if (
                entry.path === '/' ||
                entry.path === '/login' ||
                seen.has(entry.path)
              )
                return false;
              seen.add(entry.path);
              return true;
            })
            .map((entry) => ({
              path: entry.path.slice(1),
              element: page(entry.component!),
            })),
          { path: '403', element: page('error/403') },
          {
            path: '*',
            element: page(pages.length ? 'error/404' : 'error/403'),
          },
        ],
      },
    ];
  }, [menus]);
  const content = useRoutes(routes);

  if (!token) {
    const redirect = encodeURIComponent(
      location.pathname + location.search + location.hash,
    );
    return <Navigate to={`/login?redirect=${redirect}`} replace />;
  }
  if (error && !ready) {
    return (
      <Result
        status="error"
        title="加载账号信息失败"
        subTitle="请检查服务连接后重试。"
        extra={
          <Button
            type="primary"
            onClick={() => {
              setError(false);
              void load().catch(() => setError(true));
            }}
          >
            重试
          </Button>
        }
      />
    );
  }
  return ready ? content : <Loading />;
}

export const router = createBrowserRouter([
  { path: '/login', element: <LoginPage /> },
  { path: '*', element: <ProtectedApplication /> },
]);
