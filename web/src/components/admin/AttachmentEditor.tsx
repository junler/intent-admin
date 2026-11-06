import {
  DeleteOutlined,
  DownloadOutlined,
  UploadOutlined,
} from '@ant-design/icons';
import { App, Button, List, Space, Upload } from 'antd';
import { useCallback, useEffect, useState } from 'react';
import { t } from '@/i18n';
import { ApiError, api, request } from '@/utils/request';
import { useAdminAction } from './useAdminAction';

type FileItem = {
  uploadId: string;
  fileName: string;
  mime: string;
  size: number;
};
type FileList = { rows: FileItem[]; revision: string };
export function AttachmentEditor({
  entity,
  recordId,
  readOnly = false,
}: {
  entity: string;
  recordId: string;
  readOnly?: boolean;
}) {
  const [files, setFiles] = useState<FileList>({ rows: [], revision: '' });
  const [loading, setLoading] = useState(false);
  const { message } = App.useApp();
  const load = useCallback(async () => {
    setLoading(true);
    try {
      setFiles(
        await api.get<FileList>(
          '/system/attachment/list',
          {
            entity,
            recordId,
          },
          { dedupe: false },
        ),
      );
    } finally {
      setLoading(false);
    }
  }, [entity, recordId]);
  useEffect(() => {
    void load().catch(() => {});
  }, [load]);
  const action = useAdminAction(() => void load().catch(() => {}));
  async function replace(uploadIds: string[]) {
    const saved = await action.run({
      title: t('更新业务附件'),
      method: 'PUT',
      url: '/system/attachment',
      args: { entity, recordId, revision: files.revision, uploadIds },
    });
    if (!saved) throw new Error(t('附件保存失败，请刷新后重试'));
  }
  async function download(file: FileItem) {
    const response = await request<{
      data: Blob;
      headers?: Record<string, string>;
    }>({
      method: 'GET',
      url: `/system/upload/${file.uploadId}/download`,
      responseType: 'blob',
    });
    const blob = response.data;
    // JSON is a valid attachment format. Only an error envelope signals failure.
    if (
      !response.headers?.['content-disposition'] &&
      blob.type.includes('json')
    ) {
      try {
        const body = JSON.parse(await blob.text());
        if (typeof body.code === 'number' && body.code !== 200)
          throw new ApiError(body.code, body.msg);
      } catch (error) {
        if (error instanceof ApiError) throw error;
      }
    }
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = file.fileName;
    link.click();
    URL.revokeObjectURL(url);
  }
  return (
    <Space orientation="vertical" className="w-full">
      <List
        loading={loading}
        dataSource={files.rows}
        renderItem={(file) => (
          <List.Item
            actions={[
              <Button
                key="download"
                type="link"
                icon={<DownloadOutlined />}
                onClick={() =>
                  void download(file).catch(() => message.error(t('下载失败')))
                }
              >
                {t('下载')}
              </Button>,
              ...(!readOnly
                ? [
                    <Button
                      key="remove"
                      type="link"
                      danger
                      disabled={action.executing}
                      icon={<DeleteOutlined />}
                      onClick={() =>
                        void replace(
                          files.rows
                            .filter((row) => row.uploadId !== file.uploadId)
                            .map((row) => row.uploadId),
                        )
                      }
                    >
                      {t('移除关联')}
                    </Button>,
                  ]
                : []),
            ]}
          >
            {file.fileName}
          </List.Item>
        )}
      />
      {!readOnly && (
        <Upload
          showUploadList={false}
          disabled={loading || action.executing || files.rows.length >= 20}
          customRequest={async (option) => {
            const data = new FormData();
            data.append('file', option.file);
            try {
              const file = await api.post<FileItem>(
                `/system/attachment/upload?${new URLSearchParams({ entity, recordId })}`,
                data,
                { dedupe: false },
              );
              await replace([
                ...files.rows.map((row) => row.uploadId),
                file.uploadId,
              ]);
              option.onSuccess?.(file);
            } catch (error) {
              option.onError?.(error as Error);
            }
          }}
        >
          <Button icon={<UploadOutlined />}>{t('上传附件')}</Button>
        </Upload>
      )}
    </Space>
  );
}
