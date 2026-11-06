// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

let selectProps: any;
vi.mock('antd', () => ({
  Select: (props: any) => {
    selectProps = props;
    return null;
  },
  Spin: () => null,
}));
vi.mock('@/utils/request', () => ({ api: { get: vi.fn() } }));
import { api } from '@/utils/request';
import { RemoteSelect } from './RemoteSelect';
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
let root: ReturnType<typeof createRoot>;
beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  root = createRoot(document.createElement('div'));
});
afterEach(async () => {
  await act(async () => root.unmount());
  vi.useRealTimers();
});
it('ignores older search results arriving after the current search', async () => {
  let first!: (rows: unknown) => void;
  let second!: (rows: unknown) => void;
  vi.mocked(api.get)
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          first = resolve;
        }),
    )
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          second = resolve;
        }),
    );
  const source = { url: '/customers/list', valueKey: 'id', labelKey: 'name' };
  await act(async () => root.render(<RemoteSelect source={source} />));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(250);
  });
  await act(async () => selectProps.onSearch('new'));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(250);
  });
  await act(async () => second({ rows: [{ id: 2, name: 'New' }] }));
  await act(async () => first({ rows: [{ id: 1, name: 'Old' }] }));
  expect(selectProps.options).toEqual([{ value: 2, label: 'New' }]);
});
it('resolves the label of a selected ID outside the search page', async () => {
  vi.mocked(api.get)
    .mockResolvedValueOnce({ id: 99, name: 'Existing' })
    .mockResolvedValueOnce({ rows: [] });
  const source = {
    url: '/customers/list',
    valueKey: 'id',
    labelKey: 'name',
    resolveUrl: '/customers/:id',
  };
  await act(async () =>
    root.render(<RemoteSelect source={source} value={99} />),
  );
  await act(async () => {
    await vi.advanceTimersByTimeAsync(250);
  });
  expect(api.get).toHaveBeenCalledWith('/customers/99');
  expect(selectProps.options).toEqual([{ value: 99, label: 'Existing' }]);
});
