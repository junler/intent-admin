import { t } from '@/i18n';
/** Mirrors internal/capability — keep in sync with the Go types. */

export type Risk = 'low' | 'medium' | 'high' | 'critical';

export const RiskLabel: Record<Risk, string> = {
  low: t('低风险'),
  medium: t('中风险'),
  high: t('高风险'),
  critical: t('极高风险'),
};

/** antd Tag colours per risk level. */
export const RiskColor: Record<Risk, string> = {
  low: 'green',
  medium: 'blue',
  high: 'orange',
  critical: 'red',
};

export type ChangeOp = 'create' | 'update' | 'delete' | 'skip';

export const OpLabel: Record<ChangeOp, string> = {
  create: t('新增'),
  update: t('修改'),
  delete: t('删除'),
  skip: t('跳过'),
};

export interface FieldDiff {
  field: string;
  label?: string;
  before: unknown;
  after: unknown;
  /** Present for dictionary-backed fields, as "dictType:rawValue". */
  beforeText?: string;
  afterText?: string;
}

export interface Change {
  entity: string;
  id: string;
  label?: string;
  op: ChangeOp;
  fields?: FieldDiff[];
  reason?: string;
}

export interface Evidence {
  route: string;
  filters?: Record<string, unknown>;
}

/**
 * Preview is the dry-run result: what a write would do, before it does it.
 * The same shape is rendered by the page's confirmation modal and by the AI
 * plan card, which is what keeps the two entry points consistent.
 */
export interface Preview {
  summary: string;
  affected: number;
  skipped: number;
  changes?: Change[];
  truncated?: boolean;
  warnings?: string[];
  evidence?: Evidence;
  reversible: boolean;
  capability?: string;
  risk?: Risk;
}

/** Capability descriptor from the catalogue. */
export interface CapabilityDescriptor {
  name: string;
  summary: string;
  tags?: string[];
  risk: Risk;
  mutating: boolean;
  reversible: boolean;
  needConfirm: boolean;
  method?: string;
  path?: string;
  examples?: { utterance: string; args: string }[];
  inputSchema?: unknown;
}
