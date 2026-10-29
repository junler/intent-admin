import { CrudPage } from '@/components/admin/CrudPage';
import { statusOptions } from '@/components/admin/types';
import { t } from '@/i18n';
export default function Notices() {
  return (
    <CrudPage
      title={t('通知公告')}
      base="/system/notice"
      idKey="noticeId"
      perm="system:notice"
      fields={[
        { name: 'noticeTitle', label: t('标题'), required: true, search: true },
        {
          name: 'noticeType',
          label: t('类型'),
          type: 'select',
          required: true,
          defaultValue: '1',
          options: [
            { label: t('通知'), value: '1' },
            { label: t('公告'), value: '2' },
          ],
          search: true,
        },
        {
          name: 'noticeContent',
          label: t('正文'),
          type: 'textarea',
          required: true,
        },
        {
          name: 'status',
          label: t('状态'),
          type: 'select',
          defaultValue: '0',
          options: statusOptions,
          search: true,
        },
        { name: 'remark', label: t('备注'), type: 'textarea' },
      ]}
    />
  );
}
