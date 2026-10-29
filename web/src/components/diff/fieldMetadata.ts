import type { FieldDiff } from '@/api/types';
import { t } from '@/i18n';

// These bindings mirror the system schema's dictionary fields. Labels and
// values themselves are loaded through the shared dictionary API/cache.
const normalTables = new Set([
  'sys_user',
  'sys_role',
  'sys_menu',
  'sys_dict_type',
  'sys_dict_data',
  'sys_config',
  'sys_ai_provider',
  'sys_storage_config',
  'sys_upload',
  'demo_item',
]);
const labels: Record<string, string> = {
  status: t('状态'),
  sex: t('性别'),
  visible: t('菜单显示'),
};
export function annotateDiffField(entity: string, field: FieldDiff): FieldDiff {
  const name = field.field.replace(
    /[A-Z]/g,
    (char) => `_${char.toLowerCase()}`,
  );
  const dictionary =
    name === 'status'
      ? entity === 'sys_job'
        ? 'sys_job_status'
        : entity === 'sys_notice'
          ? 'sys_notice_status'
          : normalTables.has(entity)
            ? 'sys_normal_disable'
            : undefined
      : entity === 'sys_user' && name === 'sex'
        ? 'sys_user_sex'
        : entity === 'sys_menu' && name === 'visible'
          ? 'sys_show_hide'
          : undefined;
  const text = (value: unknown) =>
    dictionary && value !== undefined && value !== null && value !== ''
      ? `${dictionary}:${String(value)}`
      : undefined;
  return {
    ...field,
    label: field.label || labels[name] || field.field,
    beforeText: field.beforeText ?? text(field.before),
    afterText: field.afterText ?? text(field.after),
  };
}
