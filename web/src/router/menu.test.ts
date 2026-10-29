import { describe, expect, it } from 'vitest';

import {
  firstPage,
  flattenPages,
  normalizeMenus,
  safeRedirect,
  visibleMenus,
  type MenuRoute,
} from './menu';

const routes: MenuRoute[] = [
  {
    path: 'system',
    component: 'Layout',
    meta: { title: '系统' },
    children: [
      { path: 'users', component: 'system/user/index' },
      { path: 'profile', component: 'system/profile/index', hidden: true },
      { path: '/capabilities', component: 'capabilities/index' },
      { path: 'empty', component: 'ParentView', children: [] },
    ],
  },
];

describe('menu projections', () => {
  it('joins relative paths without changing absolute children or the source', () => {
    const normalized = normalizeMenus(routes);
    expect(normalized[0].path).toBe('/system');
    expect(normalized[0].children?.map((route) => route.path)).toEqual([
      '/system/users',
      '/system/profile',
      '/capabilities',
      '/system/empty',
    ]);
    expect(routes[0].children?.[0].path).toBe('users');
  });

  it('registers authorized hidden pages while removing hidden and empty sidebar items', () => {
    const normalized = normalizeMenus(routes);
    expect(flattenPages(normalized).map((route) => route.path)).toContain(
      '/system/profile',
    );
    expect(
      visibleMenus(normalized)[0].children?.map((route) => route.path),
    ).toEqual(['/system/users', '/capabilities']);
    expect(firstPage(routes)).toBe('/system/users');
  });

  it('omits hidden directories from sidebar and defaults to forbidden when no page is available', () => {
    const hidden = [{ ...routes[0], hidden: true }];
    expect(visibleMenus(hidden)).toEqual([]);
    expect(flattenPages(normalizeMenus(hidden))).toHaveLength(3);
    expect(firstPage(hidden)).toBe('/403');
    expect(firstPage([])).toBe('/403');
  });

  it('excludes external URLs and reserved container components from page routes', () => {
    const normalized = normalizeMenus([
      { path: 'https://example.com', component: 'demo/item/index' },
      { path: '//example.com', component: 'demo/item/index' },
      { path: '/external', component: 'InnerLink' },
      { path: '/empty', component: 'Layout' },
    ]);
    expect(flattenPages(normalized)).toEqual([]);
    expect(visibleMenus(normalized)).toEqual([]);
  });
});

describe('safeRedirect', () => {
  it('preserves the intended local page, query, and fragment', () => {
    expect(safeRedirect('/demo/item?status=1#selection')).toBe(
      '/demo/item?status=1#selection',
    );
    expect(safeRedirect('/a/../demo/item')).toBe('/demo/item');
  });

  it.each([
    null,
    '',
    'https://evil.example',
    '//evil.example',
    '/\\evil.example',
    '/%2f%2fevil.example',
    '/%5cevil.example',
    '/login',
    '/a/../login?redirect=/demo/item',
    '/%6cogin',
    '/demo\n/item',
    '/%broken',
  ])('rejects unsafe or recursive redirect %s', (value) => {
    expect(safeRedirect(value, '/demo/item')).toBe('/demo/item');
  });
});
