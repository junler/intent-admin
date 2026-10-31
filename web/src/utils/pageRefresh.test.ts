import { describe, expect, it, vi } from 'vitest';
import { refreshCurrentPage, subscribePageRefresh } from './pageRefresh';

describe('refresh after an AI operation', () => {
  it('reloads current subscribers for every completed operation and removes inactive pages', async () => {
    const current = vi.fn();
    const inactive = vi.fn();
    const unsubscribe = subscribePageRefresh(current);
    const deactivate = subscribePageRefresh(inactive);
    deactivate();
    try {
      await refreshCurrentPage();
      await refreshCurrentPage();
      expect(current).toHaveBeenCalledTimes(2);
      expect(inactive).not.toHaveBeenCalled();
    } finally {
      unsubscribe();
    }
    await refreshCurrentPage();
    expect(current).toHaveBeenCalledTimes(2);
  });

  it('still reloads other panels when a refresh fails', async () => {
    const next = vi.fn();
    const a = subscribePageRefresh(() => {
      throw new Error('request failed');
    });
    const b = subscribePageRefresh(next);
    try {
      await expect(refreshCurrentPage()).resolves.toBeUndefined();
      expect(next).toHaveBeenCalledOnce();
    } finally {
      a();
      b();
    }
  });
});
