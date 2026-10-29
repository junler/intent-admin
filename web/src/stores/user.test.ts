import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('antd', () => ({ message: { error: vi.fn() } }));

import { resumeSession, useUserStore } from './user';
import { auth, http } from '@/utils/request';

const replace = vi.fn();

beforeEach(() => {
  const values = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  });
  vi.stubGlobal('location', {
    pathname: '/login',
    search: '?redirect=/demo/item',
    hash: '',
    replace,
  });
  replace.mockReset();
  useUserStore.getState().reset();
});

describe('resuming the login session', () => {
  it('stays on login after a stale saved session expires, without a redirect loop', async () => {
    auth.set('stale-login-session');
    http.defaults.adapter = async (config) => ({
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
      data: { code: 401, msg: 'expired' },
    });
    expect(await resumeSession()).toBe(false);
    expect(auth.get()).toBe('');
    expect(useUserStore.getState().ready).toBe(false);
    expect(replace).not.toHaveBeenCalled();
  });

  it('loads the identity and authorized routes before resuming a valid session', async () => {
    auth.set('valid-login-session');
    http.defaults.adapter = async (config) => ({
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
      data: {
        code: 200,
        data:
          config.url === '/getInfo'
            ? {
                user: { userId: 1, userName: 'admin', nickName: 'Admin' },
                roles: ['admin'],
                permissions: ['*:*:*'],
                aiMaxRisk: 'high',
              }
            : [
                {
                  path: 'demo',
                  component: 'Layout',
                  children: [{ path: 'item', component: 'demo/item/index' }],
                },
              ],
      },
    });
    expect(await resumeSession()).toBe(true);
    expect(useUserStore.getState().identity?.user.userName).toBe('admin');
    expect(useUserStore.getState().menus[0].children?.[0].path).toBe(
      '/demo/item',
    );
    expect(replace).not.toHaveBeenCalled();
  });
});
