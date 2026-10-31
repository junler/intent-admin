import type { FieldDiff } from '@/api/types';
import { t } from '@/i18n';

/**
 * Which dictionary decodes a coded column, keyed as `entity.column` to match
 * the `usedBy` bindings the backend knowledge registry already publishes.
 *
 * This is the one place a new module registers its coded columns. It is a
 * fallback only: the server may send `label`/`beforeText`/`afterText` on a
 * FieldDiff and those always win.
 *
 * `status` is not globally uniform — it means enabled/disabled on business
 * tables but success/failure on the log tables — so it is listed per entity
 * rather than defaulted.
 */
const dictionaries: Record<string, string> = {
  'sys_user.status': 'sys_normal_disable',
  'sys_user.sex': 'sys_user_sex',
  'sys_role.status': 'sys_normal_disable',
  'sys_menu.status': 'sys_normal_disable',
  'sys_menu.visible': 'sys_show_hide',
  'sys_dict_type.status': 'sys_normal_disable',
  'sys_dict_data.status': 'sys_normal_disable',
  'sys_config.status': 'sys_normal_disable',
  'sys_ai_provider.status': 'sys_normal_disable',
  'sys_storage_config.status': 'sys_normal_disable',
  'sys_upload.status': 'sys_normal_disable',
  'sys_job.status': 'sys_job_status',
  'sys_notice.status': 'sys_notice_status',
  'demo_item.status': 'sys_normal_disable',
};
const labels: Record<string, string> = {
  status: t('状态'),
  sex: t('性别'),
  visible: t('菜单显示'),
  remark: t('备注'),
};
const entityLabels: Record<string, Record<string, string>> = {
  sys_job: {
    job_name: t('任务名称'),
    job_group: t('队列'),
    invoke_target: t('处理器'),
    job_params: t('参数 JSON'),
    cron_expression: t('执行计划'),
    misfire_policy: t('补偿策略'),
    concurrent: t('并发'),
  },
};
const valueLabels: Record<string, Record<string, string>> = {
  'sys_job.misfire_policy': {
    '1': t('立即补一次'),
    '2': t('全部补偿'),
    '3': t('忽略错过的执行'),
  },
  'sys_job.concurrent': { '0': t('允许并发'), '1': t('禁止并发') },
};
export function annotateDiffField(entity: string, field: FieldDiff): FieldDiff {
  const name = field.field.replace(
    /[A-Z]/g,
    (char) => `_${char.toLowerCase()}`,
  );
  const dictionary = dictionaries[`${entity}.${name}`];
  const text = (value: unknown) =>
    dictionary && value !== undefined && value !== null && value !== ''
      ? `${dictionary}:${String(value)}`
      : valueLabels[`${entity}.${name}`]?.[String(value)];
  return {
    ...field,
    label:
      field.label ||
      entityLabels[entity]?.[name] ||
      labels[name] ||
      field.field,
    beforeText: field.beforeText ?? text(field.before),
    afterText: field.afterText ?? text(field.after),
  };
}
