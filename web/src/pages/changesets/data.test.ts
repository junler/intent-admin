import { describe, expect, it } from 'vitest';
import { changesetPreview, type Changeset } from './data';
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
