import { describe, expect, it, vi } from 'vitest';
import type { MenuRoute } from '@/router/menu';
import { createChatNavigation } from './navigation';
import { pageSuggestions } from './state';

function setup() {
  let menus: MenuRoute[] = [
    {
      path: '/system/user',
      component: 'system/user/index',
      meta: { title: '用户管理' },
    },
  ];
  const actions = {
    getMenus: () => menus,
    navigate: vi.fn(),
    close: vi.fn(),
    notify: vi.fn(),
  };
  return {
    actions,
    flow: createChatNavigation(actions),
    revoke: () => {
      menus = [];
    },
  };
}

describe('chat navigation and optional page suggestions', () => {
  it('displays follow-up destinations without navigating, closing or showing a toast', () => {
    const { actions, flow } = setup();
    expect(
      pageSuggestions([{ route: '/system/user' }], actions.getMenus()),
    ).toEqual([{ url: '/system/user', title: '用户管理' }]);
    flow.complete();
    expect(actions.navigate).not.toHaveBeenCalled();
    expect(actions.close).not.toHaveBeenCalled();
    expect(actions.notify).not.toHaveBeenCalled();
  });
  it('automatically navigates and shows one named toast when the explicit navigation finishes', () => {
    const { actions, flow } = setup();
    flow.request({ route: '/system/user', filters: { status: '1' } });
    expect(actions.navigate).not.toHaveBeenCalled();
    flow.complete();
    flow.complete();
    expect(actions.navigate).toHaveBeenCalledExactlyOnceWith(
      '/system/user?status=1',
    );
    expect(actions.close).toHaveBeenCalledTimes(1);
    expect(actions.notify).toHaveBeenCalledExactlyOnceWith('用户管理');
  });
  it('does not navigate or show success after errors, permission revocation or an invalid target', () => {
    for (const kind of ['error', 'revoked', 'invalid']) {
      const { actions, flow, revoke } = setup();
      flow.request({
        route: kind === 'invalid' ? '//example.com' : '/system/user',
      });
      if (kind === 'error') flow.cancel();
      if (kind === 'revoked') revoke();
      flow.complete();
      expect(actions.navigate).not.toHaveBeenCalled();
      expect(actions.close).not.toHaveBeenCalled();
      expect(actions.notify).not.toHaveBeenCalled();
    }
  });
});
