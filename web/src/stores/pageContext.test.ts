import { beforeEach, describe, expect, it, vi } from 'vitest';
import { usePageContextStore } from './pageContext';

beforeEach(() => {
  vi.stubGlobal('location', { pathname: '/foreground' });
  usePageContextStore.getState().clear();
});
describe('page context cleanup ownership', () => {
  it('does not let a deactivated or evicted page clear the foreground page context', () => {
    usePageContextStore.getState().set({ route: '/old', selectedIds: [1] });
    usePageContextStore
      .getState()
      .set({ route: '/foreground', selectedIds: [2] });
    usePageContextStore.getState().clear('/old');
    expect(usePageContextStore.getState().context).toMatchObject({
      route: '/foreground',
      selectedIds: [2],
    });
  });
  it('clears its own context without leaving stale row hints', () => {
    usePageContextStore
      .getState()
      .set({ route: '/foreground', visibleIds: [1, 2] });
    usePageContextStore.getState().clear('/foreground');
    expect(usePageContextStore.getState().context).toEqual({
      route: '/foreground',
    });
  });
  it('bounds page hints independently of a large cached selection', () => {
    usePageContextStore.getState().set({
      route: '/foreground',
      selectedIds: Array.from({ length: 700 }, (_, index) => index),
      visibleIds: Array.from({ length: 300 }, (_, index) => index),
    });
    expect(usePageContextStore.getState().context.selectedIds).toHaveLength(
      500,
    );
    expect(usePageContextStore.getState().context.visibleIds).toHaveLength(200);
  });
});
