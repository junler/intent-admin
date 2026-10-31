import { useSearchParams } from '@umijs/max';
import { CrudPage } from '@/components/admin/CrudPage';
import { statusOptions } from '@/components/admin/types';
import { t } from '@/i18n';
export default function DictData() {
  const [query] = useSearchParams();
  const dictType = query.get('dictType') ?? '';
  return (
    <CrudPage
      title={t('字典数据{value0}', {
        value0: dictType ? ' · ' + dictType : '',
      })}
      base="/system/dict/data"
      idKey="dictCode"
      perm="system:dict"
      export
      import
      params={dictType ? { dictType } : undefined}
      fields={[
        {
          name: 'dictType',
          label: t('字典类型'),
          required: true,
          search: true,
          defaultValue: dictType,
        },
        { name: 'dictLabel', label: t('标签'), required: true, search: true },
        { name: 'dictValue', label: t('值'), required: true },
        { name: 'dictSort', label: t('排序'), type: 'number', defaultValue: 0 },
        {
          name: 'listClass',
          label: t('标签颜色'),
          type: 'select',
          options: [
            'default',
            'success',
            'warning',
            'error',
            'processing',
            'blue',
            'green',
            'red',
            'orange',
            'purple',
            'cyan',
          ].map((value) => ({ label: value, value })),
          defaultValue: 'default',
        },
        { name: 'cssClass', label: t('CSS 类名'), column: false },
        {
          name: 'isDefault',
          label: t('默认项'),
          type: 'select',
          defaultValue: 'N',
          options: [
            { label: t('是'), value: 'Y' },
            { label: t('否'), value: 'N' },
          ],
        },
        {
          name: 'status',
          label: t('状态'),
          type: 'select',
          options: statusOptions,
          defaultValue: '0',
          search: true,
        },
        { name: 'remark', label: t('备注'), type: 'textarea' },
      ]}
    />
  );
}
