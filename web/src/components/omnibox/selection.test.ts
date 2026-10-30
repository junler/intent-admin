import { describe, expect, it } from 'vitest';
import type { MenuRoute } from '@/router/menu';
import { pageTitle, selectionOf } from './selection';

const menus: MenuRoute[] = [
  {
    path: '/system',
    component: 'Layout',
    meta: { title: '系统管理' },
    children: [
      { path: '/system/user', component: 'system/user', meta: { title: '用户管理' } },
    ],
  },
];

describe('pageTitle', () => {
  it('names the page that owns a route', () => {
    expect(pageTitle(menus, '/system/user')).toBe('用户管理');
  });
  it('returns undefined for routes the menu does not cover', () => {
    expect(pageTitle(menus, '/system/unknown')).toBeUndefined();
  });
});

describe('selectionOf', () => {
  it('stays absent while nothing is checked', () => {
    expect(selectionOf({ route: '/system/user' }, menus)).toBeUndefined();
    expect(
      selectionOf({ route: '/system/user', selectedIds: [] }, menus),
    ).toBeUndefined();
  });

  it('names a single row instead of counting it', () => {
    expect(
      selectionOf(
        {
          route: '/system/user',
          selectedIds: [7],
          selectedLabels: ['  admin  '],
        },
        menus,
      ),
    ).toEqual({
      scope: '用户管理',
      count: 1,
      label: 'admin',
      labels: ['admin'],
    });
  });

  it('counts multiple rows and keeps their labels for the tooltip', () => {
    expect(
      selectionOf(
        {
          route: '/system/user',
          selectedIds: [1, 2],
          selectedLabels: ['admin', '', 'ops'],
        },
        menus,
      ),
    ).toEqual({
      scope: '用户管理',
      count: 2,
      label: undefined,
      labels: ['admin', 'ops'],
    });
  });

  it('still reports a count when the menu cannot name the page', () => {
    const selection = selectionOf(
      { route: '/custom/report', selectedIds: [1, 2, 3] },
      menus,
    );
    expect(selection?.scope).toBeUndefined();
    expect(selection?.count).toBe(3);
    expect(selection?.labels).toEqual([]);
  });
});
