import { App, Button, Descriptions, Tag } from 'antd';
import { useEffect, useState } from 'react';
import { CrudPage } from '@/components/admin/CrudPage';
import { type Row, statusOptions } from '@/components/admin/types';
import { t } from '@/i18n';
import { api } from '@/utils/request';
export default function Providers() {
  const [presets, setPresets] = useState<Row[]>([]);
  const { modal } = App.useApp();
  useEffect(() => {
    void api.get<Row[]>('/system/ai-provider/presets').then(setPresets);
  }, []);
  return (
    <CrudPage
      batchDelete={false}
      title={t('模型服务')}
      base="/system/ai-provider"
      createUrl="/system/ai-provider/create"
      updateUrl="/system/ai-provider/update"
      idKey="providerId"
      perm="system:aiprovider"
      onValuesChange={(changed) => {
        const preset = presets.find((p) => p.vendor === changed.vendor);
        return preset
          ? {
              baseUrl: preset.baseUrl,
              adapter: preset.adapter,
              defaultModel: preset.models?.[0],
            }
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
                  ...(editing ? { providerId: editing.providerId } : {}),
                };
                if (!draft.apiKey) delete draft.apiKey;
                const result = await api.post<Row>(
                  '/system/ai-provider/test',
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
          name: 'vendor',
          label: t('模型厂商'),
          type: 'select',
          required: true,
          options: presets.map((p) => ({ label: p.name, value: p.vendor })),
          defaultValue: 'openai',
          search: true,
        },
        {
          name: 'adapter',
          label: t('协议适配器'),
          form: false,
          type: 'select',
          defaultValue: 'openai',
          options: [
            { label: t('OpenAI 兼容'), value: 'openai' },
            { label: t('火山方舟'), value: 'ark' },
          ],
          column: false,
        },
        {
          name: 'baseUrl',
          label: t('API 地址'),
          help: t('留空采用厂商默认地址。'),
        },
        {
          name: 'apiKey',
          label: 'API Key',
          createRequired: true,
          type: 'password',
          help: t('编辑时留空保留当前密钥。'),
        },
        { name: 'defaultModel', label: t('默认模型'), required: true },
        { name: 'models', label: t('可用模型'), type: 'multi', form: false },
        {
          name: 'temperature',
          label: t('温度'),
          type: 'number',
          column: false,
        },
        {
          name: 'maxTokens',
          label: t('最大输出 token'),
          type: 'number',
          column: false,
        },
        {
          name: 'timeoutSec',
          label: t('超时秒数'),
          type: 'number',
          defaultValue: 60,
        },
        { name: 'sort', label: t('排序'), type: 'number', defaultValue: 0 },
        {
          name: 'supportsToolCalling',
          label: t('工具调用'),
          type: 'switch',
          form: false,
        },
        { name: 'isDefault', label: t('默认'), type: 'switch', form: false },
        {
          name: 'status',
          label: t('状态'),
          type: 'select',
          options: statusOptions,
          defaultValue: '1',
          search: true,
          form: false,
        },
        { name: 'remark', label: t('备注'), type: 'textarea' },
      ]}
      actions={[
        {
          label: t('连通测试'),
          perm: 'system:aiprovider:edit',
          readOnly: true,
          url: '/system/ai-provider/test',
          args: (r) => ({ providerId: r.providerId }),
          method: 'POST',
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
          perm: 'system:aiprovider:edit',
          url: '/system/ai-provider/setDefault',
          previewUrl: '/system/ai-provider/setDefault/preview',
          args: (r) => ({ providerId: r.providerId }),
        },
        {
          label: t('启停'),
          perm: 'system:aiprovider:edit',
          url: '/system/ai-provider/changeStatus',
          previewUrl: '/system/ai-provider/changeStatus/preview',
          args: (r) => ({
            providerId: r.providerId,
            status: r.status === '0' ? '1' : '0',
          }),
        },
      ]}
    />
  );
}
