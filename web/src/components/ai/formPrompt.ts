import { t } from '@/i18n';
import type { FormFocus } from './panelState';

export function formPrompt(form: FormFocus, fieldLabel?: string): string {
  const task =
    form.mode === 'create'
      ? t('请帮我新增“{form}”表单', { form: form.title })
      : form.mode === 'update' && form.recordId
        ? t('请帮我修改“{form}”表单（记录 ID：{id}）', {
            form: form.title,
            id: form.recordId,
          })
        : form.mode === 'update'
          ? t('请帮我修改“{form}”表单', { form: form.title })
          : form.mode === 'query'
            ? t('请帮我查询“{form}”表单', { form: form.title })
            : t('请帮我填写“{form}”表单', { form: form.title });
  return (
    task +
    (fieldLabel ? t('，其中“{field}”请填写：', { field: fieldLabel }) : '')
  );
}
