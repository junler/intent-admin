import { afterEach, describe, expect, it, vi } from 'vitest';
import { useFormDraftStore } from './formDraft';

afterEach(() => {
  useFormDraftStore.getState().clear();
  vi.useRealTimers();
});

describe('ephemeral AI form drafts', () => {
  it('delivers a generic create or update draft only to its target page and only once', () => {
    const store = useFormDraftStore.getState();
    store.stage(
      7,
      {
        name: 'Storage',
        maxSize: 20,
        allowExt: ['png', 'pdf'],
        useSSL: true,
        extra: { region: 'test' },
      },
      {
        route: '/system/storage',
        mode: 'update',
        recordId: '12',
        path: '/system/storage/update',
        permission: 'system:storage:edit',
      },
    );
    expect(store.take(7, '/system/user')).toBeUndefined();
    const draft = store.take(7, '/system/storage');
    expect(draft?.target.mode).toBe('update');
    expect(draft?.target.recordId).toBe('12');
    expect(draft?.values.allowExt).toEqual(['png', 'pdf']);
    expect(draft?.values.extra).toEqual({ region: 'test' });
    expect(store.take(7, '/system/storage')).toBeUndefined();
  });
  it('does not expose another account’s draft', () => {
    const store = useFormDraftStore.getState();
    store.stage(7, { password: 'test-pass-42' });
    expect(store.take(8)).toBeUndefined();
    expect(useFormDraftStore.getState().draft).toBeUndefined();
  });
  it('removes expired credentials even when a page never consumes them', () => {
    vi.useFakeTimers();
    useFormDraftStore.getState().stage(7, { password: 'test-pass-42' });
    vi.advanceTimersByTime(120_001);
    expect(useFormDraftStore.getState().draft).toBeUndefined();
  });
});
