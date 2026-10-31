// @vitest-environment jsdom
import { act, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { StreamEvent } from '@/utils/sse';

const mocks = vi.hoisted(() => ({
  allowed: true,
  get: vi.fn(),
  post: vi.fn(),
  stream: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock('@umijs/max', () => ({ history: { push: mocks.navigate } }));
vi.mock('@/hooks/usePerm', () => ({ usePerm: () => mocks.allowed }));
vi.mock('@/i18n', () => ({
  t: (key: string) => key,
  getLocale: () => 'zh-CN',
}));
vi.mock('@/utils/request', () => ({
  api: { get: mocks.get, post: mocks.post },
}));
vi.mock('@/utils/sse', () => ({ streamChat: mocks.stream }));
vi.mock('./PlanCard', () => ({ PlanCard: () => null }));
vi.mock('./PageSuggestions', () => ({ PageSuggestions: () => null }));
vi.mock('antd', async () => {
  const { forwardRef } = await import('react');
  const Surface = ({ children }: { children?: ReactNode }) => (
    <div>{children}</div>
  );
  return {
    App: { useApp: () => ({ message: { info: vi.fn() }, notification: {} }) },
    Alert: () => null,
    Button: ({
      children,
      onClick,
    }: {
      children?: ReactNode;
      onClick?: () => void;
    }) => (
      <button type="button" onClick={onClick}>
        {children}
      </button>
    ),
    // Drawer visibility must not unmount the conversation or its request owner.
    Drawer: ({
      open,
      onClose,
      children,
      title,
    }: {
      open: boolean;
      onClose: () => void;
      children: ReactNode;
      title: ReactNode;
    }) => (
      <div data-panel hidden={!open}>
        <button type="button" onClick={onClose}>
          关闭窗口
        </button>
        {title}
        {children}
      </div>
    ),
    Dropdown: Surface,
    Empty: () => null,
    Input: {
      TextArea: forwardRef<HTMLTextAreaElement, { value: string }>(
        ({ value }, ref) => <textarea ref={ref} value={value} readOnly />,
      ),
    },
    Select: () => null,
    Space: Surface,
    Spin: () => null,
    Typography: { Text: Surface },
  };
});

import { AiPanel } from './AiPanel';
import { useAiPanel } from './panelState';
import { useUiForms } from '@/hooks/useAiForm';
import { useUserStore } from '@/stores/user';
import { useFormDraftStore } from '@/stores/formDraft';

let container: HTMLDivElement;
let root: Root;
let emit: (event: StreamEvent) => void;
let finish: () => void;
let signal: AbortSignal;

beforeEach(async () => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  vi.clearAllMocks();
  mocks.allowed = true;
  mocks.get.mockImplementation(async (path: string) =>
    path === '/ai/models'
      ? [
          {
            providerId: 1,
            name: 'Test',
            defaultModel: 'test',
            models: ['test'],
          },
        ]
      : [],
  );
  mocks.post.mockResolvedValue({});
  mocks.stream.mockImplementation((_input, onEvent, abortSignal) => {
    emit = onEvent;
    signal = abortSignal;
    return new Promise<void>((resolve) => {
      finish = resolve;
      signal.addEventListener('abort', () => resolve(), { once: true });
    });
  });
  useAiPanel.setState({ open: false, request: undefined });
  container = document.createElement('div');
  document.body.append(container);
  root = createRoot(container);
  await act(async () => root.render(<AiPanel />));
  await act(async () => useAiPanel.getState().show('查询用户'));
  expect(mocks.stream).toHaveBeenCalledTimes(1);
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  useFormDraftStore.getState().clear();
  useUiForms.setState({ forms: {} });
  useUserStore.setState({ identity: null });
});

it('fills the current token editor with defaults without opening or navigating', async () => {
  await act(async () => {
    useUserStore.setState({
      identity: {
        user: { userId: 7, userName: 'test', nickName: 'Test' },
        permissions: [],
        roles: [],
        aiMaxRisk: 'low',
      },
    });
    useUiForms.setState({
      forms: {
        'ui.tokenEditor': {
          name: 'ui.tokenEditor',
          summary: '个人访问令牌',
          route: '/system/token',
          permission: '',
          fields: ['name', 'maxRisk'],
          mode: 'create',
        },
      },
    });
    emit({
      type: 'form_fill',
      route: '/system/token',
      formId: 'ui.tokenEditor',
      permission: '',
      mode: 'create',
      values: {},
      defaults: { name: '个人访问令牌', maxRisk: 'low' },
    });
    emit({ type: 'done' });
    finish();
  });
  expect(mocks.navigate).not.toHaveBeenCalled();
  expect(useAiPanel.getState().open).toBe(true);
  expect(container.textContent).toContain('已填充表单');
  expect(container.textContent).not.toContain('已打开并填写');
  const draft = useFormDraftStore.getState().take(7, '/system/token');
  expect(draft?.defaults).toEqual({ name: '个人访问令牌', maxRisk: 'low' });
});

async function click(label: string) {
  const button = Array.from(container.querySelectorAll('button')).find(
    (item) => item.textContent === label,
  );
  expect(button).toBeDefined();
  await act(async () => button?.click());
}

describe('AI conversation visibility during generation', () => {
  it.each([false, true])(
    'keeps receiving the reply after closing (started: %s)',
    async (started) => {
      if (started) {
        await act(async () => {
          emit({ type: 'start', sessionId: 'session-1' });
          emit({ type: 'delta', text: '正在查询。' });
        });
      }
      await click('关闭窗口');
      expect(useAiPanel.getState().open).toBe(false);
      expect(signal.aborted).toBe(false);
      expect(mocks.post).not.toHaveBeenCalled();

      await act(async () => {
        if (!started) emit({ type: 'start', sessionId: 'session-1' });
        emit({ type: 'delta', text: '查询完成。' });
      });
      await act(async () => useAiPanel.getState().reveal());
      expect(container.textContent).toContain('查询完成。');
      expect(container.textContent).toContain('正在生成…');
      expect(mocks.stream).toHaveBeenCalledTimes(1);

      await click('关闭窗口');
      await act(async () => {
        emit({ type: 'answer', text: '共找到 3 位用户。' });
        emit({ type: 'done' });
        finish();
      });
      await act(async () => useAiPanel.getState().reveal());
      expect(container.textContent).toContain('共找到 3 位用户。');
      expect(container.textContent).not.toContain('正在生成…');
      expect(signal.aborted).toBe(false);
      expect(mocks.post).not.toHaveBeenCalled();
    },
  );

  it('still cancels when the user explicitly stops', async () => {
    await act(async () => emit({ type: 'start', sessionId: 'session-1' }));
    await click('停止');
    expect(signal.aborted).toBe(true);
    expect(mocks.post).toHaveBeenCalledWith(
      '/ai/stop',
      { sessionId: 'session-1' },
      { silent: true, dedupe: false },
    );
    await act(async () => emit({ type: 'answer', text: '已停止的回复' }));
    expect(container.textContent).not.toContain('已停止的回复');
  });

  it('still cancels if AI permission is revoked while hidden', async () => {
    await act(async () => emit({ type: 'start', sessionId: 'session-1' }));
    await click('关闭窗口');
    mocks.allowed = false;
    await act(async () => root.render(<AiPanel />));
    expect(signal.aborted).toBe(true);
    expect(mocks.post).toHaveBeenCalledWith(
      '/ai/stop',
      { sessionId: 'session-1' },
      { silent: true, dedupe: false },
    );
  });
});

// jsdom does not calculate layout, so supply the scroll geometry of a message list.
function messageScrollArea() {
  const viewport = container.querySelector<HTMLDivElement>(
    '.intent-ai-messages',
  );
  if (!viewport) throw new Error('Message list is missing');
  const geometry = { height: 1000, top: 800 };
  Object.defineProperties(viewport, {
    scrollHeight: { get: () => geometry.height },
    clientHeight: { get: () => 200 },
    scrollTop: {
      get: () => geometry.top,
      set: (value: number) => {
        geometry.top = Math.min(value, geometry.height - 200);
      },
    },
  });
  return { viewport, geometry };
}

describe('AI reply scroll following', () => {
  it('follows streamed replies until the user scrolls', async () => {
    const { geometry } = messageScrollArea();
    geometry.height = 1200;
    await act(async () => emit({ type: 'delta', text: '第一段' }));
    expect(geometry.top).toBe(1000);
    geometry.height = 1400;
    await act(async () => emit({ type: 'delta', text: '第二段' }));
    expect(geometry.top).toBe(1200);
  });

  it.each(['wheel', 'touchmove'])(
    'pauses immediately on %s and resumes only at the bottom',
    async (type) => {
      const { viewport, geometry } = messageScrollArea();
      await act(async () =>
        viewport.dispatchEvent(new Event(type, { bubbles: true })),
      );
      // Pause before the browser has even dispatched the resulting scroll event.
      geometry.height = 1200;
      await act(async () => emit({ type: 'delta', text: '继续生成' }));
      expect(geometry.top).toBe(800);

      geometry.top = 100;
      await act(async () => viewport.dispatchEvent(new Event('scroll')));
      geometry.height = 1400;
      await act(async () => emit({ type: 'delta', text: '保留阅读位置' }));
      expect(geometry.top).toBe(100);

      geometry.top = 1200;
      await act(async () => viewport.dispatchEvent(new Event('scroll')));
      geometry.height = 1600;
      await act(async () => emit({ type: 'delta', text: '恢复跟随' }));
      expect(geometry.top).toBe(1400);
    },
  );

  it('also respects scrolling with the scrollbar or keyboard', async () => {
    const { viewport, geometry } = messageScrollArea();
    geometry.top = 300;
    await act(async () => viewport.dispatchEvent(new Event('scroll')));
    geometry.height = 1200;
    await act(async () => emit({ type: 'delta', text: '新回复' }));
    expect(geometry.top).toBe(300);
  });

  it('preserves a paused reading position when the panel is closed and reopened', async () => {
    const { viewport, geometry } = messageScrollArea();
    geometry.top = 300;
    await act(async () => viewport.dispatchEvent(new Event('scroll')));
    await click('关闭窗口');
    geometry.height = 1200;
    await act(async () => emit({ type: 'delta', text: '后台继续生成' }));
    await act(async () => useAiPanel.getState().reveal());
    expect(geometry.top).toBe(300);
    expect(signal.aborted).toBe(false);
  });

  it('resumes following when the user sends a new message', async () => {
    const { viewport, geometry } = messageScrollArea();
    geometry.top = 300;
    await act(async () => viewport.dispatchEvent(new Event('scroll')));
    await act(async () => {
      emit({ type: 'done' });
      finish();
    });
    geometry.height = 1200;
    await act(async () => useAiPanel.getState().show('下一个问题'));
    expect(geometry.top).toBe(1000);
    expect(mocks.stream).toHaveBeenCalledTimes(2);
  });
});
