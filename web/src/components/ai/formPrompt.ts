import { t } from '@/i18n';
import type { FormFocus } from './panelState';

export function formPrompt(form: FormFocus, fieldLabel?: string): string {
  const task =
    form.mode === 'create'
      ? t('帮我填写“{form}”的新增表单', { form: form.title })
      : form.mode === 'update' && form.recordId
        ? t('帮我修改“{form}”中 ID 为 {id} 的记录', {
            form: form.title,
            id: form.recordId,
          })
        : form.mode === 'update'
          ? t('帮我填写“{form}”的修改表单', { form: form.title })
          : t('帮我填写“{form}”表单', { form: form.title });
  return (
    task +
    (fieldLabel
      ? t('，其中“{field}”请填写：', { field: fieldLabel })
      : t('，具体要求：'))
  );
}
