import { DeleteOutlined } from '@ant-design/icons';
import { App, Button, Descriptions, Space } from 'antd';
import { Auth } from '@/components/auth/Auth';
import { t } from '@/i18n';
import { CrudPage } from './CrudPage';
import type { Field, RowAction } from './types';
import { useAdminAction } from './useAdminAction';
export function LogPage({
  title,
  base,
  idKey,
  perm,
  fields,
  actions = [],
}: {
  title: string;
  base: string;
  idKey: string;
  perm: string;
  fields: Field[];
  actions?: RowAction[];
}) {
  const { modal } = App.useApp();
  const mutation = useAdminAction();
  return (
    <>
      <CrudPage
        title={title}
        base={base}
        idKey={idKey}
        perm={perm}
        canCreate={false}
        canEdit={false}
        export
        fields={fields}
        extra={
          <Space>
            <Auth perm={`${perm}:remove`}>
              <Button
                icon={<DeleteOutlined />}
                danger
                onClick={() =>
                  void mutation.run({
                    title: t('清空{value0}', { value0: title }),
                    url: `${base}/clean`,
                    method: 'DELETE',
                    previewUrl: `${base}/clean/preview`,
                  })
                }
              >
                {t('清空日志')}
              </Button>
            </Auth>
          </Space>
        }
        actions={[
          {
            label: t('详情'),
            perm: `${perm}:list`,
            readOnly: true,
            execute: async (row) => row,
            result: (row) =>
              modal.info({
                title: t('日志详情'),
                width: 920,
                className: 'intent-log-detail',
                content: (
                  <Descriptions
                    column={{ xs: 1, sm: 2 }}
                    layout="vertical"
                    bordered
                    items={Object.entries(row).map(([key, value]) => ({
                      key,
                      label:
                        fields.find((field) => field.name === key)?.label ??
                        t(key),
                      span:
                        typeof value === 'object' ||
                        String(value ?? '').length > 100
                          ? 2
                          : 1,
                      children: (
                        <pre
                          className={`intent-detail-value ${typeof value === 'object' ? 'intent-detail-json' : ''}`}
                        >
                          {typeof value === 'object'
                            ? JSON.stringify(value, null, 2)
                            : key === 'title'
                              ? t(String(value ?? ''))
                              : String(value ?? '')}
                        </pre>
                      ),
                    }))}
                  />
                ),
              }),
          },
          ...actions,
        ]}
      />
      {mutation.modal}
    </>
  );
}
