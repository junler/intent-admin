import { annotateDiffField } from '@/components/diff/fieldMetadata';
import type { Change, Preview, Risk } from '@/api/types';
export interface ChangesetItem {
  itemId: number;
  seq: number;
  capability: string;
  entity: string;
  entityId: string;
  entityLabel: string;
  op: Change['op'];
  before?: Record<string, unknown>;
  after?: Record<string, unknown>;
  status: string;
  errorMsg?: string;
}
export interface Changeset {
  changesetId: string;
  actorType: string;
  actorName: string;
  source: string;
  intent: string;
  planId?: string;
  status: string;
  risk: Risk;
  summary: string;
  affectedCount: number;
  approvalMode: string;
  approvedBy: string;
  createTime: string;
  rolledBackAt?: string;
  rolledBackBy?: string;
  rollbackOf?: string;
  rollbackChangesetId?: string;
  items?: ChangesetItem[];
}
export function changesetPreview(set: Changeset): Preview {
  const changes = (set.items ?? []).map((item): Change => {
    const before = item.before ?? {},
      after = item.after ?? {};
    const fields = [...new Set([...Object.keys(before), ...Object.keys(after)])]
      .filter(
        (key) => JSON.stringify(before[key]) !== JSON.stringify(after[key]),
      )
      .map((field) =>
        annotateDiffField(item.entity, {
          field,
          before: before[field],
          after: after[field],
        }),
      );

    return {
      entity: item.entity,
      id: item.entityId,
      label: item.entityLabel || `${item.entity} #${item.entityId}`,
      op: item.op,
      fields,
      reason: item.errorMsg,
    };
  });
  return {
    summary: set.summary,
    affected: set.affectedCount,
    skipped: (set.items ?? []).filter((item) => item.status === 'skipped')
      .length,
    changes,
    reversible: true,
    risk: set.risk,
  };
}
