// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('antd', () => ({
  App: { useApp: () => ({ message: { success: vi.fn(), error: vi.fn() } }) },
}));
vi.mock('@/contexts/ActivePageContext', () => ({ usePageActive: () => true }));
vi.mock('@/hooks/useDict', () => ({ invalidateDicts: vi.fn() }));
vi.mock('@/utils/request', () => ({
  request: vi.fn(),
  auth: { get: () => 'session-a' },
  ApiError: class extends Error {
    constructor(public code: number) {
      super('API error');
    }
  },
  Code: { StaleData: 412, RiskExceeded: 423 },
}));
import { ApiError, request } from '@/utils/request';
import { useAdminAction } from './useAdminAction';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
let action: ReturnType<typeof useAdminAction>;
function Harness() {
  action = useAdminAction();
  return null;
}
let root: ReturnType<typeof createRoot>;
beforeEach(async () => {
  vi.clearAllMocks();
  root = createRoot(document.createElement('div'));
  await act(async () => root.render(<Harness />));
});
afterEach(async () => {
  await act(async () => root.unmount());
});
const command = {
  title: 'Create',
  url: '/demo/item',
  method: 'POST' as const,
  args: { name: 'A' },
};
describe('mutation retry keys', () => {
  it('reuses an ambiguous attempt key and starts a new intent after success', async () => {
    vi.mocked(request)
      .mockRejectedValueOnce(new Error('connection lost'))
      .mockResolvedValue({});
    await act(async () => {
      expect(await action.run(command)).toBe(false);
    });
    await act(async () => {
      expect(await action.run(command)).toBe(true);
    });
    const first =
      vi.mocked(request).mock.calls[0][0].headers?.['Idempotency-Key'];
    expect(first).toBeTruthy();
    expect(vi.mocked(request).mock.calls[0][0].dedupe).toBe(false);
    expect(
      vi.mocked(request).mock.calls[1][0].headers?.['Idempotency-Key'],
    ).toBe(first);
    await act(async () => {
      await action.run(command);
    });
    expect(
      vi.mocked(request).mock.calls[2][0].headers?.['Idempotency-Key'],
    ).not.toBe(first);
  });
  it('does not reuse a key when the payload changes or a definitive conflict is returned', async () => {
    vi.mocked(request)
      .mockRejectedValueOnce(new Error('network'))
      .mockRejectedValueOnce(new ApiError(409, 'conflict'))
      .mockResolvedValue({});
    await act(async () => {
      await action.run(command);
    });
    const changed = { ...command, args: { name: 'B' } };
    await act(async () => {
      await action.run(changed);
    });
    await act(async () => {
      await action.run(changed);
    });
    const keys = vi
      .mocked(request)
      .mock.calls.map(([config]) => config.headers?.['Idempotency-Key']);
    expect(new Set(keys).size).toBe(3);
  });
});
