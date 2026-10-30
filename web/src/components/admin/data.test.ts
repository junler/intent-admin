import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/utils/request', () => ({ api: { get: vi.fn() } }));

import { api } from '@/utils/request';
import { allRows } from './data';

beforeEach(() => vi.clearAllMocks());
describe('complete menu choices', () => {
  it('loads later pages so grants beyond the first100 menus remain selectable', async () => {
    vi.mocked(api.get)
      .mockResolvedValueOnce({ rows: [{ menuId: 1 }], total: 2 })
      .mockResolvedValueOnce({ rows: [{ menuId: 101 }], total: 2 });
    expect(await allRows('/system/menu')).toEqual([
      { menuId: 1 },
      { menuId: 101 },
    ]);
    expect(api.get).toHaveBeenNthCalledWith(2, '/system/menu/list', {
      pageNum: 2,
      pageSize: 100,
    });
  });
  it('stops when server returns no more records even if total changes during listing', async () => {
    vi.mocked(api.get).mockResolvedValueOnce({ rows: [], total: 10 });
    expect(await allRows('/system/menu')).toEqual([]);
    expect(api.get).toHaveBeenCalledTimes(1);
  });
});
