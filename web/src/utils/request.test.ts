import type { AxiosAdapter } from 'axios';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('antd', () => ({ message: { error: vi.fn() } }));

import { ApiError, auth, http, request } from './request';

const replace = vi.fn();
let values: Map<string, string>;

beforeEach(() => {
  values = new Map();
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  });
  vi.stubGlobal('location', {
    pathname: '/demo/item',
    search: '?status=1',
    hash: '#selection',
    replace,
  });
  replace.mockReset();
});

function respond(code: number, data?: unknown): AxiosAdapter {
  return async (config) => ({
    status: 200,
    statusText: 'OK',
    headers: {},
    config,
    data: { code, msg: 'result', data },
  });
}

describe('authenticated requests', () => {
  it('keeps the session when a login form explicitly handles authentication failures', async () => {
    auth.set('existing-session');
    http.defaults.adapter = respond(401);
    await expect(
      request({ url: '/login', dedupe: false, ignoreUnauthorized: true }),
    ).rejects.toBeInstanceOf(ApiError);
    expect(auth.get()).toBe('existing-session');
    expect(replace).not.toHaveBeenCalled();
  });

  it('clears an expired session and retains the full intended deep link', async () => {
    auth.set('expired-session');
    http.defaults.adapter = respond(401);
    await expect(
      request({ url: '/getInfo', dedupe: false }),
    ).rejects.toMatchObject({ code: 401 });
    expect(auth.get()).toBe('');
    expect(replace).toHaveBeenCalledWith(
      '/login?redirect=%2Fdemo%2Fitem%3Fstatus%3D1%23selection',
    );
  });

  it('does not share an in-flight result between different accounts', async () => {
    const tokens: string[] = [];
    http.defaults.adapter = async (config) => {
      tokens.push(String(config.headers.Authorization));
      return respond(200, String(config.headers.Authorization))(config);
    };
    auth.set('account-a');
    expect(await request({ url: '/account-isolation' })).toBe(
      'Bearer account-a',
    );
    auth.set('account-b');
    expect(await request({ url: '/account-isolation' })).toBe(
      'Bearer account-b',
    );
    expect(tokens).toEqual(['Bearer account-a', 'Bearer account-b']);
  });
});
