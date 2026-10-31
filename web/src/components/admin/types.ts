import type { FormInstance } from 'antd';
import type { ReactNode } from 'react';
import { t } from '@/i18n';
export type Row = Record<string, any>;
export interface Field {
  name: string;
  label: string;
  type?:
    | 'text'
    | 'textarea'
    | 'number'
    | 'select'
    | 'multi'
    | 'password'
    | 'json'
    | 'switch'
    | 'date'
    | 'tree'
    | 'cron';
  required?: boolean;
  createRequired?: boolean;
  search?: boolean;
  hidden?: boolean;
  createOnly?: boolean;
  defaultValue?: unknown;
  /** Suggested AI draft value; applied only to empty controls. */
  aiDefaultValue?: unknown;
  treeData?: any[];
  options?: { label: string; value: any }[];
  rules?: any[];
  help?: string;
  column?: boolean;
  form?: boolean;
  render?: (value: any, row: Row) => ReactNode;
}
export interface RowAction {
  readOnly?: boolean;
  label: string;
  perm?: string;
  danger?: boolean;
  url?: string | ((row: Row) => string);
  method?: 'POST' | 'PUT' | 'DELETE';
  previewUrl?: string | ((row: Row) => string);
  fields?: Field[];
  defaults?: (row: Row) => Promise<Row> | Row;
  args?: (row: Row, values: Row) => Row;
  execute?: (row: Row, values: Row) => Promise<unknown>;
  result?: (value: any) => void;
}
export interface CrudConfig {
  title: string;
  base: string;
  idKey: string;
  perm: string;
  fields: Field[];
  createUrl?: string;
  updateUrl?: string;
  removePreviewUrl?: string;
  canCreate?: boolean;
  canEdit?: boolean;
  canDelete?: boolean;
  batchDelete?: boolean;
  export?: boolean;
  /**
   * Show the XLSX import action. The entity also needs a column specification
   * in internal/feature/importer. Defaults to the `{perm}:add` permission;
   * pass an object to require a different one.
   */
  import?: boolean | { permission?: string };
  actions?: RowAction[];
  extra?: ReactNode;
  params?: Row;
  tree?: boolean;
  onValuesChange?: (changed: Row, values: Row) => Row;
  prepare?: (values: Row, editing: boolean) => Row;
  editorExtra?: (form: FormInstance, editing: Row | null) => ReactNode;
  onCreateResult?: (value: any) => void;
}
export const statusOptions = [
  { label: t('正常'), value: '0' },
  { label: t('停用'), value: '1' },
];
export const riskOptions = [
  { label: t('低风险'), value: 'low' },
  { label: t('中风险'), value: 'medium' },
  { label: t('高风险'), value: 'high' },
  { label: t('极高风险'), value: 'critical' },
];
