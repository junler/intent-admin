import { CrudPage } from '@/components/admin/CrudPage';
import { statusOptions } from '@/components/admin/types';
import { t } from '@/i18n';

export default function DemoItemPage() {
  return (
    <CrudPage
      title={t('示例条目')}
      base="/demo/item"
      idKey="itemId"
      perm="demo:item"
      createPerm="demo:item:edit"
      updateUrl={(row) => `/demo/item/update/${row.itemId}`}
      removeUrl={(ids) => `/demo/item/remove/${ids.join(',')}`}
      detail
      detailPerm="demo:item:list"
      attachments={{ entity: 'demo.item' }}
      import={{ permission: 'demo:item:edit' }}
      export
      fields={[
        {
          name: 'name',
          label: t('名称'),
          required: true,
          search: true,
          rules: [{ max: 100 }],
        },
        {
          name: 'category',
          label: t('分类'),
          search: true,
          rules: [{ max: 32 }],
        },
        {
          name: 'amount',
          label: t('金额（分）'),
          type: 'number',
          defaultValue: 0,
          rules: [{ type: 'number', min: 0 }],
          render: (value) => (Number(value) / 100).toFixed(2),
        },
        {
          name: 'status',
          label: t('状态'),
          type: 'select',
          options: statusOptions,
          defaultValue: '0',
          search: true,
        },
        {
          name: 'remark',
          label: t('备注'),
          type: 'textarea',
          rules: [{ max: 500 }],
        },
        {
          name: 'createdRange',
          label: t('创建时间'),
          type: 'dateRange',
          rangeKeys: ['beginTime', 'endTime'],
          search: true,
          form: false,
          column: false,
        },
        { name: 'createTime', label: t('创建时间'), form: false },
      ]}
    />
  );
}
