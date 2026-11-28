// @vitest-environment jsdom
import { act, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

const get = vi.hoisted(() => vi.fn());

vi.mock('@/utils/request', () => ({ api: { get } }));
vi.mock('@/hooks/usePageRefresh', () => ({ usePageRefresh: () => {} }));
vi.mock('@/components/admin/useAdminAction', () => ({
  useAdminAction: () => ({ run: vi.fn(), modal: null }),
}));
vi.mock('@/components/admin/InfoPanel', () => ({ InfoPanel: () => null }));
vi.mock('@/components/auth/Auth', () => ({ Auth: () => null }));
vi.mock('@ant-design/pro-components', () => ({
  PageContainer: ({ children }: { children: ReactNode }) => (
    <main>{children}</main>
  ),
}));
vi.mock('antd', async (original) => {
  const antd = await original<typeof import('antd')>();
  return {
    ...antd,
    Select: ({
      options,
      value,
      onChange,
    }: {
      options: { value: string; label: string }[];
      value?: string;
      onChange: (name: string) => void;
    }) => (
      <select
        aria-label="Cache prefix"
        value={value ?? ''}
        onChange={(event) => onChange(event.target.value)}
      >
        <option value="" />
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    ),
  };
});

import Cache from './index';

let root: Root;
let container: HTMLDivElement;
let keys: string[];
const keyPath = '/monitor/cache/keys';

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: false,
    media: query,
    addListener: vi.fn(),
    removeListener: vi.fn(),
  }));
  const getStyle = window.getComputedStyle.bind(window);
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) =>
    getStyle(element),
  );
  localStorage.clear();
  keys = ['first-key', 'other-key'];
  get.mockReset().mockImplementation(async (path: string) => {
    if (path === keyPath) return keys;
    if (path === '/monitor/cache/names') return ['config', 'dict'];
    return {};
  });
  container = document.createElement('div');
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  container.remove();
});

async function render() {
  await act(async () => root.render(<Cache />));
}

function filter(): HTMLInputElement {
  return container.querySelector<HTMLInputElement>('input[aria-label]')!;
}

async function typeAndEnter(value: string) {
  const input = filter();
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      HTMLInputElement.prototype,
      'value',
    )!.set!.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(async () => {
    input.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }),
    );
    input.dispatchEvent(
      new KeyboardEvent('keyup', { key: 'Enter', bubbles: true }),
    );
  });
}

async function click(label: string) {
  const button = Array.from(container.querySelectorAll('button')).find(
    (item) => item.textContent === label,
  );
  expect(button).toBeDefined();
  await act(async () => button!.click());
}

function keyRequests() {
  return get.mock.calls.filter(([path]) => path === keyPath);
}

it('filters loaded keys without fetching, then refreshes explicitly without losing the keyword', async () => {
  await render();
  expect(keyRequests()).toEqual([[keyPath, { cacheName: 'config' }]]);
  await typeAndEnter('first');
  expect(keyRequests()).toHaveLength(1);
  expect(container.textContent).toContain('匹配 1 / 已加载 2');
  expect(container.textContent).not.toContain('other-key');

  keys = ['first-new-key', 'another-key', 'third-key'];
  await click('刷新键列表');
  expect(keyRequests()).toEqual([
    [keyPath, { cacheName: 'config' }],
    [keyPath, { cacheName: 'config' }],
  ]);
  expect(filter().value).toBe('first');
  expect(container.textContent).toContain('first-new-key');
  expect(container.textContent).toContain('匹配 1 / 已加载 3');
});

it('explains a bounded sample and an empty match in English', async () => {
  localStorage.setItem('umi_locale', 'en-US');
  keys = Array.from({ length: 500 }, (_, index) => `key-${index}`);
  await render();
  expect(filter().getAttribute('aria-label')).toBe('Filter loaded keys');
  await typeAndEnter('missing');
  expect(keyRequests()).toHaveLength(1);
  expect(container.textContent).toContain('Matched 0 / Loaded 500');
  expect(container.textContent).toContain('No matches among the loaded keys');
  expect(container.textContent).toContain(
    'Load limit reached. There may be more keys.',
  );
});

it('keeps the sample on refresh failure and ignores a late response from another prefix', async () => {
  await render();
  get.mockRejectedValueOnce(new Error('unavailable'));
  await click('刷新键列表');
  expect(container.textContent).toContain('刷新失败，仍显示上次加载的键。');
  expect(container.textContent).toContain('匹配 2 / 已加载 2');

  let settle!: (value: string[]) => void;
  get.mockReturnValueOnce(
    new Promise<string[]>((resolve) => {
      settle = resolve;
    }),
  );
  await click('刷新键列表');
  keys = ['dict-key'];
  const select = container.querySelector('select')!;
  await act(async () => {
    select.value = 'dict';
    select.dispatchEvent(new Event('change', { bubbles: true }));
  });
  await act(async () => settle(['late-config-key']));
  expect(keyRequests().at(-1)).toEqual([keyPath, { cacheName: 'dict' }]);
  expect(container.textContent).toContain('dict-key');
  expect(container.textContent).not.toContain('late-config-key');
  expect(container.textContent).toContain('匹配 1 / 已加载 1');
});
