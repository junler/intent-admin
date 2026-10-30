import { describe, expect, it } from 'vitest';
import { approvalActions } from './approval';
import type { PlanApproval } from './state';

const record: PlanApproval = {
  id: 'approval',
  planId: 'plan',
  proposerId: 1,
  proposerName: 'requester',
  reviewerId: 2,
  reviewerName: 'reviewer',
  status: 'pending',
  fingerprint: 'hash',
  requestComment: '',
  decisionComment: '',
  createTime: '2026-01-01T00:00:00Z',
  updateTime: '2026-01-01T00:00:00Z',
  expiresAt: '2026-01-02T00:00:00Z',
};
const now = Date.parse('2026-01-01T01:00:00Z');
describe('approval participant controls', () => {
  it('only the assigned distinct permitted reviewer may decide', () => {
    expect(approvalActions(record, 2, true, now).approve).toBe(true);
    expect(approvalActions(record, 1, true, now).approve).toBe(false);
    expect(approvalActions(record, 3, true, now).approve).toBe(false);
    expect(approvalActions(record, 2, false, now).approve).toBe(false);
  });
  it('only the proposer executes an unexpired approved preview', () => {
    const approved = { ...record, status: 'approved' };
    expect(approvalActions(approved, 1, false, now).apply).toBe(true);
    expect(approvalActions(approved, 2, true, now).apply).toBe(false);
    expect(
      approvalActions(approved, 1, true, Date.parse('2026-01-03')).apply,
    ).toBe(false);
    expect(
      approvalActions({ ...record, status: 'stale' }, 1, true, now).apply,
    ).toBe(false);
  });
  it('allows explicit withdrawal but never re-executes applied history', () => {
    expect(approvalActions(record, 1, false, now).revoke).toBe(true);
    expect(
      approvalActions({ ...record, status: 'applied' }, 1, true, now).revoke,
    ).toBe(false);
    expect(
      approvalActions({ ...record, proposerId: 2 }, 2, true, now).approve,
    ).toBe(false);
  });
});
