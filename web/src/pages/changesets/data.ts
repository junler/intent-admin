import type { Change, Preview, Risk } from '@/api/types';
import { annotateDiffField } from '@/components/diff/fieldMetadata';
export interface ChangesetItem {
  itemId: number;
  seq: number;
  undoGroup?: string;
  errorCode?: number;
  conflictFields?: string[];
  rolledBackAt?: string;
  rolledBackBy?: string;
  rollbackChangesetId?: string;
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
  rollbackChangesetIds?: string[];
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

export interface RollbackPreview extends Preview {
  confirmationHash: string;
  selection?: {
    itemIds: number[];
    total: number;
    remaining: number;
    alreadyRolledBack: number;
  };
  changes?: (Change & {
    itemId?: number;
    status?: string;
    conflictFields?: string[];
  })[];
}
export function selectableItem(item: ChangesetItem): boolean {
  return item.op !== 'skip' && item.status !== 'rolled_back';
}
/** Selecting or deselecting any member selects or deselects its whole undo group. */
export function updateRollbackSelection(
  items: ChangesetItem[],
  previous: number[],
  next: number[],
): number[] {
  const selected = new Set(next);
  const removedGroups = new Set(
    items
      .filter(
        (item) => previous.includes(item.itemId) && !selected.has(item.itemId),
      )
      .map((item) => item.undoGroup)
      .filter(Boolean),
  );
  for (const item of items) {
    if (
      !selectableItem(item) ||
      (item.undoGroup && removedGroups.has(item.undoGroup))
    )
      selected.delete(item.itemId);
  }
  const groups = new Set(
    items
      .filter((item) => selected.has(item.itemId))
      .map((item) => item.undoGroup)
      .filter(Boolean),
  );
  for (const item of items)
    if (selectableItem(item) && item.undoGroup && groups.has(item.undoGroup))
      selected.add(item.itemId);
  return [...selected].sort((a, b) => a - b);
}
