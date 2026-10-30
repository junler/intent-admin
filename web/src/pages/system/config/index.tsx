import { Button } from 'antd';
import { CrudPage } from '@/components/admin/CrudPage';
import { statusOptions } from '@/components/admin/types';
import { useAdminAction } from '@/components/admin/useAdminAction';
import { Auth } from '@/components/auth/Auth';
import { t } from '@/i18n';
export default function Config() {
  const action = useAdminAction();
  return (
    <>
      <CrudPage
        title={t('系统参数')}
        base="/system/config"
        idKey="configId"
        perm="system:config"
        export
        extra={
          <Auth perm="system:config:edit">
            <Button
              onClick={() =>
                void action.run({
                  title: t('刷新参数缓存'),
                  url: '/system/config/refreshCache',
                  method: 'DELETE',
                  previewUrl: '/system/config/refreshCache/preview',
                })
              }
            >
              {t('刷新缓存')}
            </Button>
          </Auth>
        }
        fields={[
          {
            name: 'configName',
            label: t('名称'),
            required: true,
            search: true,
          },
          { name: 'configKey', label: t('键'), required: true, search: true },
          { name: 'configValue', label: t('值'), required: true },
          {
            name: 'configType',
            label: t('内置'),
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
      {action.modal}
    </>
  );
}
