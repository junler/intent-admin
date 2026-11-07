// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { SchedulePreview, type SchedulePreviewData } from './SchedulePreview';

const get = vi.hoisted(() => vi.fn());
vi.mock('@/utils/request', () => ({ api: { get } }));
vi.mock('antd', () => ({
  Typography: {
    Text: ({ children }: { children: React.ReactNode }) => (
      <span>{children}</span>
    ),
  },
}));

const data: SchedulePreviewData = {
  cronExpression: '0 9 * * MON-FRI',
  timeZone: 'Asia/Shanghai',
  baseTime: '2026-11-07T17:00:00+08:00',
  nextExecutions: [9, 10, 11, 12, 13].map(
    (day) => `2026-11-${String(day).padStart(2, '0')}T09:00:00+08:00`,
  ),
  estimated: false,
};
function deferred() {
  let resolve!: (value: SchedulePreviewData) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<SchedulePreviewData>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
let root: Root;
let container: HTMLDivElement;
beforeEach(() => {
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
  vi.useFakeTimers();
  get.mockReset();
  container = document.createElement('div');
  document.body.append(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
  vi.useRealTimers();
});
async function render(expression: string, paused = false) {
  await act(async () =>
    root.render(<SchedulePreview expression={expression} paused={paused} />),
  );
}
async function tick() {
  await act(async () => vi.advanceTimersByTimeAsync(400));
}
describe('schedule preview', () => {
  it('debounces edits and shows server timezone and offset without browser conversion', async () => {
    get.mockResolvedValue(data);
    await render('0 8 * * *');
    await render('0 9 * * MON-FRI');
    expect(get).not.toHaveBeenCalled();
    await tick();
    expect(get).toHaveBeenCalledTimes(1);
    expect(get).toHaveBeenCalledWith(
      '/monitor/job/schedulePreview',
      { cronExpression: '0 9 * * MON-FRI' },
      { silent: true, dedupe: false },
    );
    expect(container.textContent).toContain('Asia/Shanghai');
    expect(container.querySelectorAll('li')).toHaveLength(5);
    expect(container.textContent).toContain('2026-11-09 09:00:00+08:00');
  });
  it('clears old results immediately and ignores an out-of-order successful response', async () => {
    const old = deferred();
    const latest = deferred();
    get.mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise);
    await render('0 8 * * *');
    await tick();
    await render('0 9 * * *');
    await tick();
    await act(async () => latest.resolve(data));
    await act(async () => old.resolve({ ...data, timeZone: 'stale zone' }));
    expect(container.textContent).not.toContain('stale zone');
    await render('99 9 * * *');
    expect(container.textContent).not.toContain('Asia/Shanghai');
  });
  it('shows invalid errors in place, then clears errors and labels paused interval estimates', async () => {
    get
      .mockRejectedValueOnce(new Error('invalid minute'))
      .mockResolvedValueOnce({ ...data, estimated: true });
    await render('99 9 * * *');
    await tick();
    expect(container.querySelector('[role="alert"]')?.textContent).toContain(
      'invalid minute',
    );
    await render('@every 5m', true);
    expect(container.textContent).not.toContain('invalid minute');
    await tick();
    expect(container.textContent).toContain('当前不会执行');
    expect(container.textContent).toContain('不代表队列的实际下次触发时间');
  });
  it('ignores an old failure and cancels pending requests on empty input or unmount', async () => {
    const old = deferred();
    get.mockReturnValueOnce(old.promise).mockResolvedValueOnce(data);
    await render('old');
    await tick();
    await render('new');
    await tick();
    await act(async () => old.reject(new Error('stale failure')));
    expect(container.textContent).not.toContain('stale failure');
    await render('');
    await tick();
    expect(container.textContent).toBe('');
    await render('pending');
    await act(async () => root.unmount());
    root = createRoot(container);
    await tick();
    expect(get).toHaveBeenCalledTimes(2);
  });
});
