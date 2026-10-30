import { UploadOutlined } from '@ant-design/icons';
import { useSearchParams } from '@umijs/max';
import { App, Button, Typography, Upload } from 'antd';
import { useState } from 'react';
import { CrudPage } from '@/components/admin/CrudPage';
import { Auth } from '@/components/auth/Auth';
import { t } from '@/i18n';
import { ApiError, api, request } from '@/utils/request';
export default function Files() {
  const [search] = useSearchParams();
  const bizType = search.get('bizType');
  const [version, setVersion] = useState(0);
  const { message, modal } = App.useApp();
  return (
    <CrudPage
      key={version}
      batchDelete={false}
      params={bizType ? { bizType } : undefined}
      title={t('文件管理')}
      base="/system/upload"
      idKey="uploadId"
      perm="system:upload"
      canCreate={false}
      canEdit={false}
      extra={
        <Auth perm="system:upload:add">
          <Upload
            showUploadList={false}
            customRequest={async (option) => {
              const body = new FormData();
              body.append('file', option.file);
              body.append('bizType', 'attach');
              try {
                await api.post('/system/upload', body);
                option.onSuccess?.({});
                message.success(t('上传成功'));
                setVersion((v) => v + 1);
              } catch (e) {
                option.onError?.(e as Error);
              }
            }}
          >
            <Button type="primary" icon={<UploadOutlined />}>
              {t('上传文件')}
            </Button>
          </Upload>
        </Auth>
      }
      fields={[
        { name: 'fileName', label: t('文件名'), search: true },
        { name: 'ext', label: t('扩展名') },
        {
          name: 'size',
          label: t('大小'),
          type: 'number',
          render: (value) =>
            value > 1048576
              ? `${(value / 1048576).toFixed(2)} MB`
              : `${Math.ceil(value / 1024)} KB`,
        },
        { name: 'mime', label: t('类型') },
        { name: 'bizType', label: t('业务'), search: true },
        { name: 'storageId', label: t('存储编号'), type: 'number' },
        { name: 'createTime', label: t('上传时间') },
      ]}
      actions={[
        {
          label: t('下载'),
          perm: 'system:upload:query',
          readOnly: true,
          execute: async (row) => {
            const response = await request<{ data: Blob }>({
              url: `/system/upload/${row.uploadId}/download`,
              method: 'GET',
              responseType: 'blob',
            });
            if (response.data.type.includes('json')) {
              const body = JSON.parse(await response.data.text());
              throw new ApiError(body.code, body.msg, body.data);
            }
            const url = URL.createObjectURL(response.data as Blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = row.fileName;
            a.click();
            URL.revokeObjectURL(url);
          },
        },
        {
          label: t('获取链接'),
          perm: 'system:upload:query',
          readOnly: true,
          execute: (row) => api.get(`/system/upload/${row.uploadId}/url`),
          result: (result) => {
            const url = typeof result === 'string' ? result : result.url;
            if (url)
              modal.info({
                title: t('文件访问链接'),
                width: 700,
                content: (
                  <Typography.Paragraph copyable className="break-all">
                    {url}
                  </Typography.Paragraph>
                ),
              });
          },
        },
      ]}
    />
  );
}
