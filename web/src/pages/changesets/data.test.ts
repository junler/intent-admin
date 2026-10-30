import { describe, expect, it } from 'vitest';
import {
  type Changeset,
  type ChangesetItem,
  changesetPreview,
  selectableItem,
  updateRollbackSelection,
} from './data';

const base: Changeset = {
  changesetId: '1',
  actorType: 'human',
  actorName: 'admin',
  source: 'http',
  intent: '修改用户',
  status: 'applied',
  risk: 'medium',
  summary: '更新用户',
  affectedCount: 1,
  approvalMode: 'manual',
  approvedBy: '',
  createTime: '2026-01-01',
  items: [
    {
      itemId: 1,
      seq: 1,
      capability: 'system.user.update',
      entity: 'sys_user',
      entityId: '2',
      entityLabel: 'test',
      op: 'update',
      before: { status: '0', nick_name: 'same' },
      after: { status: '1', nick_name: 'same' },
      status: 'applied',
    },
  ],
};
describe('stored changeset diff rendering', () => {
  it('shows only changed fields with dictionary semantics', () => {
    const preview = changesetPreview(base);
    expect(preview.changes?.[0].fields).toEqual([
      {
        field: 'status',
        label: '状态',
        before: '0',
        after: '1',
        beforeText: 'sys_normal_disable:0',
        afterText: 'sys_normal_disable:1',
      },
    ]);
  });
  it('keeps skipped and failed item reasons visible', () => {
    const item = base.items?.[0];
    if (!item) throw new Error('missing item fixture');
    const preview = changesetPreview({
      ...base,
      items: [
        {
          ...item,
          op: 'skip',
          status: 'skipped',
          errorMsg: '操作不可逆',
        },
      ],
    });
    expect(preview.skipped).toBe(1);
    expect(preview.changes?.[0].reason).toBe('操作不可逆');
  });
});

describe('selective rollback selection', () => {
  const fixture = base.items?.[0];
  if (!fixture) throw new Error('missing item fixture');
  const items: ChangesetItem[] = [
    { ...fixture, itemId: 1, undoGroup: 'group-a' },
    { ...fixture, itemId: 2, undoGroup: 'group-a' },
    { ...fixture, itemId: 3 },
    { ...fixture, itemId: 4, status: 'rolled_back' },
    { ...fixture, itemId: 5, op: 'skip', status: 'skipped' },
    { ...fixture, itemId: 6, status: 'failed' },
  ];
  it('selects and deselects atomic groups together without dropping independent rows', () => {
    expect(updateRollbackSelection(items, [], [1, 3])).toEqual([1, 2, 3]);
    expect(updateRollbackSelection(items, [1, 2, 3], [2, 3])).toEqual([3]);
    expect(updateRollbackSelection(items, [1, 2], [1, 2, 3])).toEqual([
      1, 2, 3,
    ]);
  });
  it('excludes completed and skipped writes while allowing conflict retries', () => {
    expect(
      updateRollbackSelection(
        items,
        [],
        items.map((item) => item.itemId),
      ),
    ).toEqual([1, 2, 3, 6]);
    expect(selectableItem(items[3])).toBe(false);
    expect(selectableItem(items[4])).toBe(false);
    expect(selectableItem(items[5])).toBe(true);
  });
});
