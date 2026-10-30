import { t } from '@/i18n';
import type { PlanApproval } from './state';

export const approvalLabels: Record<string, string> = {
  pending: t('待他人审批'),
  approved: t('已批准'),
  rejected: t('已驳回'),
  revoked: t('已撤销审批'),
  expired: t('审批已过期'),
  stale: t('审批已失效'),
  applied: t('已按审批执行'),
};

export function approvalActions(
  approval: PlanApproval,
  userId: number | undefined,
  canApprove: boolean,
  now = Date.now(),
) {
  const active = ['pending', 'approved'].includes(approval.status);
  const expired = active && Date.parse(approval.expiresAt) <= now;
  const proposer = userId === approval.proposerId;
  const reviewer = userId === approval.reviewerId && !proposer && canApprove;
  return {
    expired,
    approve: reviewer && approval.status === 'pending' && !expired,
    reject: reviewer && approval.status === 'pending' && !expired,
    apply: proposer && approval.status === 'approved' && !expired,
    revoke:
      (proposer &&
        approval.status !== 'applied' &&
        approval.status !== 'revoked') ||
      (reviewer && approval.status === 'approved'),
  };
}
