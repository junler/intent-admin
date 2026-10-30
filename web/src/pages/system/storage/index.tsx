import { App, Button, Descriptions, Tag } from 'antd';
import { useEffect, useState } from 'react';
import { CrudPage } from '@/components/admin/CrudPage';
import { type Row, statusOptions } from '@/components/admin/types';
import { t } from '@/i18n';
import { api } from '@/utils/request';
export default function Storage() {
  const [presets, setPresets] = useState<Row[]>([]);
  const { modal } = App.useApp();
  useEffect(() => {
    void api.get<Row[]>('/system/storage/presets').then(setPresets);
  }, []);
  return (
    <CrudPage
      batchDelete={false}
      title={t('存储配置')}
      base="/system/storage"
      createUrl="/system/storage/create"
      updateUrl="/system/storage/update"
      idKey="storageId"
      perm="system:storage"
      onValuesChange={(changed) => {
        const preset = presets.find((p) => p.preset === changed.preset);
        return preset
          ? { endpoint: preset.endpoint, pathStyle: preset.pathStyle }
          : {};
      }}
      editorExtra={(form, editing) => (
        <Button
          onClick={() =>
            void form
              .validateFields()
              .then(async (values) => {
                const draft = {
                  ...values,
                  ...(editing ? { storageId: editing.storageId } : {}),
                };
                if (!draft.secretKey) delete draft.secretKey;
                const result = await api.post<Row>(
                  '/system/storage/test',
                  draft,
                );
                modal.info({
                  title: result.ok ? t('配置测试通过') : t('配置测试失败'),
                  width: 700,
                  content: (
                    <pre className="max-h-96 overflow-auto whitespace-pre-wrap">
                      {JSON.stringify(result, null, 2)}
                    </pre>
                  ),
                });
              })
              .catch(() => {})
          }
        >
          {t('测试当前表单')}
        </Button>
      )}
      fields={[
        { name: 'name', label: t('名称'), required: true, search: true },
        {
          name: 'driver',
          label: t('驱动'),
          type: 'select',
          required: true,
          defaultValue: 'local',
          options: [
            { label: t('本地文件'), value: 'local' },
            { label: t('S3 兼容'), value: 's3' },
          ],
          search: true,
        },
        {
          name: 'preset',
          label: t('服务商预设'),
          type: 'select',
          column: false,
          options: presets.map((p) => ({ label: p.name, value: p.preset })),
        },
        {
          name: 'localDir',
          label: t('本地目录'),
          defaultValue: './uploads',
          column: false,
        },
        { name: 'endpoint', label: 'S3 Endpoint', column: false },
        { name: 'region', label: t('区域'), column: false },
        { name: 'bucket', label: t('存储桶') },
        { name: 'accessKeyId', label: 'Access Key ID', column: false },
        {
          name: 'secretKey',
          label: 'Secret Key',
          type: 'password',
          help: t('编辑时留空保留当前密钥。'),
        },
        {
          name: 'useSsl',
          label: 'HTTPS',
          type: 'switch',
          defaultValue: true,
          column: false,
        },
        {
          name: 'pathStyle',
          label: 'Path Style',
          type: 'switch',
          defaultValue: false,
          column: false,
        },
        { name: 'baseUrl', label: t('公网访问前缀') },
        {
          name: 'urlMode',
          label: t('URL 模式'),
          type: 'select',
          defaultValue: 'static',
          options: [
            { label: t('公开固定链接'), value: 'static' },
            { label: t('私有临时签名'), value: 'presigned' },
          ],
        },
        {
          name: 'presignExpireSec',
          label: t('签名有效秒数'),
          type: 'number',
          defaultValue: 3600,
          column: false,
        },
        {
          name: 'maxSize',
          label: t('文件上限（字节）'),
          type: 'number',
          defaultValue: 20971520,
          column: false,
        },
        {
          name: 'allowExt',
          label: t('允许扩展名'),
          type: 'multi',
          column: false,
          help: t('例如 jpg、png、pdf；留空采用服务端默认。'),
        },
        { name: 'isDefault', label: t('默认'), type: 'switch', form: false },
        {
          name: 'status',
          label: t('状态'),
          type: 'select',
          defaultValue: '1',
          options: statusOptions,
          search: true,
          form: false,
        },
        { name: 'remark', label: t('备注'), type: 'textarea' },
      ]}
      actions={[
        {
          label: t('读写测试'),
          perm: 'system:storage:edit',
          readOnly: true,
          url: '/system/storage/test',
          method: 'POST',
          args: (r) => ({ storageId: r.storageId }),
          result: (r) =>
            modal.info({
              title: r.ok ? t('测试通过') : t('测试失败'),
              width: 700,
              content: (
                <Descriptions
                  column={1}
                  items={Object.entries(r).map(([key, value]) => ({
                    key,
                    label: key,
                    children:
                      typeof value === 'boolean' ? (
                        <Tag color={value ? 'green' : 'red'}>
                          {String(value)}
                        </Tag>
                      ) : (
                        JSON.stringify(value)
                      ),
                  }))}
                />
              ),
            }),
        },
        {
          label: t('设为默认'),
          perm: 'system:storage:edit',
          url: '/system/storage/setDefault',
          previewUrl: '/system/storage/setDefault/preview',
          args: (r) => ({ storageId: r.storageId }),
        },
        {
          label: t('启停'),
          perm: 'system:storage:edit',
          url: '/system/storage/changeStatus',
          previewUrl: '/system/storage/changeStatus/preview',
          args: (r) => ({
            storageId: r.storageId,
            status: r.status === '0' ? '1' : '0',
          }),
        },
      ]}
    />
  );
}
