import { App } from 'antd';
import type { ReactNode } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@umijs/max', () => ({ history: { push: vi.fn() } }));
vi.mock('@/hooks/usePerm', () => ({ usePerm: () => true }));
vi.mock('@/utils/request', () => ({
  api: { get: vi.fn() },
  request: vi.fn(),
  download: vi.fn(),
  auth: { get: () => '' },
  ApiError: class extends Error {},
  Code: {},
}));
vi.mock('antd', async (load) => ({
  ...(await load<typeof import('antd')>()),
  // Render portal content inline so the actual pages can be checked with SSR.
  Modal: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock('@ant-design/pro-components', () => ({
  PageContainer: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  ProForm: () => null,
  ProTable: ({ toolBarRender }: { toolBarRender: () => ReactNode }) => (
    <div>{toolBarRender()}</div>
  ),
}));

import DemoItems from '@/pages/demo/item';
import Roles from '@/pages/system/role';
import Users from '@/pages/system/user';

afterEach(() => vi.unstubAllGlobals());

describe('spreadsheet preparation guidance', () => {
  it.each([
    {
      locale: 'zh-CN',
      common:
        '先下载模板填写，必填列带 *；状态 0 为正常、1 为停用，不支持公式。',
      amount: '金额单位为分。',
      userWarning: '用户使用系统初始密码，不导入密码、角色或额外权限。',
    },
    {
      locale: 'en-US',
      common:
        'Download the template first. Required columns use *. Status: 0 = active, 1 = disabled. Formulas are not supported.',
      amount: 'Amounts are in cents.',
      userWarning: 'Users receive the configured initial password.',
    },
  ])('limits the cents hint to demo items ($locale)', (copy) => {
    vi.stubGlobal('localStorage', { getItem: () => copy.locale });
    for (const Page of [Users, Roles, DemoItems]) {
      const html = renderToStaticMarkup(
        <App>
          <Page />
        </App>,
      );
      expect(html).toContain(copy.common);
      if (Page === DemoItems) {
        expect(html).toContain(copy.amount);
      } else {
        expect(html).not.toContain(copy.amount);
      }
      if (Page === Users) expect(html).toContain(copy.userWarning);
    }
  });
});
