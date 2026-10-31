import { Navigate, useLocation } from '@umijs/max';
import { Result } from 'antd';
import { Loading } from '@/components/common/Loading';
import {
  type ComponentType,
  type LazyExoticComponent,
  lazy,
  Suspense,
  useLayoutEffect,
  useState,
} from 'react';
import {
  FrozenPageBoundary,
  type PageLocation,
} from '@/contexts/ActivePageContext';
import { pageRegistry } from '@/generated/pageRegistry';
import { t } from '@/i18n';
import { firstPage, flattenPages } from '@/router/menu';
import {
  type CachedPage,
  cacheSignature,
  reconcilePageCache,
  withDefaultQuery,
} from '@/router/pageCache';
import { usePageContextStore } from '@/stores/pageContext';
import { useUserStore } from '@/stores/user';

const components = new Map<string, LazyExoticComponent<ComponentType>>();
function resolveComponent(component: string) {
  const normalized = component
    .replace(/^\/?(?:pages\/)?/, '')
    .replace(/\.tsx$/, '');
  const registry = pageRegistry as Record<
    string,
    () => Promise<{ default: ComponentType }>
  >;
  const loader = registry[normalized] ?? registry[`${normalized}/index`];
  if (!loader) return undefined;
  let Page = components.get(normalized);
  if (!Page) {
    Page = lazy(loader);
    components.set(normalized, Page);
  }
  return Page;
}
export default function DynamicPage() {
  const location = useLocation();
  const { pathname } = location;
  const menus = useUserStore((s) => s.menus);
  const owner = useUserStore((s) => String(s.identity?.user.userId ?? ''));
  const routes = flattenPages(menus);
  const selected = routes.find((route) => route.path === pathname);
  const Page = selected?.component
    ? resolveComponent(selected.component)
    : undefined;
  const defaultSearch = withDefaultQuery(location.search, selected?.query);
  const redirecting = defaultSearch !== location.search;
  const [cache, setCache] = useState<{ owner: string; entries: CachedPage[] }>({
    owner,
    entries: [],
  });
  const base = cache.owner === owner ? cache.entries : [];
  const next = reconcilePageCache(
    base,
    routes,
    Page && !redirecting ? selected : undefined,
    location as PageLocation,
  );
  const signature = cacheSignature(next);
  useLayoutEffect(() => {
    setCache({ owner, entries: next });
  }, [owner, signature]);
  useLayoutEffect(() => {
    usePageContextStore.getState().set({ route: pathname });
  }, [pathname]);
  const active = Page && selected && !redirecting;
  const entries =
    selected?.meta?.noCache && active
      ? [
          ...next,
          {
            path: pathname,
            component: selected.component!,
            query: selected.query ?? '',
            location: location as PageLocation,
          },
        ]
      : next;
  let status: React.ReactNode = null;
  if (redirecting)
    status = (
      <Navigate
        to={{ pathname, search: defaultSearch, hash: location.hash }}
        replace
        state={location.state}
      />
    );
  else if (pathname === '/' && !selected)
    status = <Navigate to={firstPage(menus)} replace />;
  else if (!selected)
    status = (
      <Result
        status="403"
        title={t('没有访问权限')}
        subTitle={t('当前账号未获得此页面的访问权限。')}
      />
    );
  else if (!Page)
    status = (
      <Result
        status="404"
        title={t('页面不存在')}
        subTitle={t('请检查菜单的组件配置。')}
      />
    );
  return (
    <>
      {status}
      {entries.map((entry) => {
        const Cached = resolveComponent(entry.component);
        return Cached ? (
          <FrozenPageBoundary
            key={`${selected?.meta?.noCache && entry.path === pathname ? 'uncached' : 'cached'}:${entry.path}`}
            active={Boolean(active && entry.path === pathname)}
            location={entry.location}
          >
            {/* A bare Spin is inline, so it lands in the top-left corner of
                the content area instead of where the page is about to be. */}
            <Suspense fallback={<Loading />}>
              <Cached />
            </Suspense>
          </FrozenPageBoundary>
        ) : null;
      })}
    </>
  );
}
