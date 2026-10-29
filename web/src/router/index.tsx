import { lazy, Suspense, type ComponentType } from 'react';
import { createBrowserRouter, Navigate } from 'react-router';
import { Spin } from 'antd';

import { BasicLayout } from '@/layouts/BasicLayout';

/**
 * Component registry.
 *
 * The backend sends menus with a `component` string (sys_menu.component) and
 * this glob maps it to a page. Menus are therefore configurable at runtime
 * without a frontend release — which is the point of keeping the menu tree in
 * the database.
 *
 * Phase 1 replaces the static route table below with routes derived from
 * GET /api/getRouters; resolveComponent is already the shape that needs.
 */
const modules = import.meta.glob('../pages/**/*.tsx');

export function resolveComponent(component: string): ComponentType {
  const loader =
    modules[`../pages/${component}.tsx`] ?? modules[`../pages/${component}/index.tsx`];
  if (!loader) {
    // A mistyped `component` must not blank the screen. Showing "page not
    // found" tells the administrator exactly what to fix.
    return lazy(() => import('@/pages/error/404'));
  }
  return lazy(loader as never);
}

function Loading() {
  return (
    <div className="flex h-full items-center justify-center py-20">
      <Spin size="large" />
    </div>
  );
}

function withSuspense(component: string) {
  const C = resolveComponent(component);
  return (
    <Suspense fallback={<Loading />}>
      <C />
    </Suspense>
  );
}

export const router = createBrowserRouter([
  {
    path: '/',
    element: <BasicLayout />,
    children: [
      { index: true, element: <Navigate to="/demo/item" replace /> },
      { path: 'demo/item', element: withSuspense('demo/item/index') },
      { path: 'capabilities', element: withSuspense('capabilities/index') },
      { path: '403', element: withSuspense('error/403') },
      { path: '*', element: withSuspense('error/404') },
    ],
  },
]);
