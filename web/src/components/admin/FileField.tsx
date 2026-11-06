import { UploadOutlined } from '@ant-design/icons';
import { Button, Upload } from 'antd';
import { useEffect, useState } from 'react';
import type { UploadFile } from 'antd';
import { t } from '@/i18n';
import { api } from '@/utils/request';

const EMPTY_UPLOADS: string[] = [];

// Business modules bind these opaque upload IDs in their own write contract.
export function FileField({
  value = EMPTY_UPLOADS,
  onChange,
  image,
  maxCount = 10,
}: {
  value?: string[];
  onChange?: (ids: string[]) => void;
  image?: boolean;
  maxCount?: number;
}) {
  const [files, setFiles] = useState<UploadFile[]>([]);
  useEffect(() => {
    let active = true;
    void Promise.all(
      value.map(async (id) => {
        try {
          const file = await api.get<{ fileName: string; url?: string }>(
            `/system/upload/${id}`,
          );
          return {
            uid: id,
            name: file.fileName,
            url: file.url,
            status: 'done' as const,
          };
        } catch {
          return { uid: id, name: id, status: 'done' as const };
        }
      }),
    ).then((rows) => {
      if (active) setFiles(rows);
    });
    return () => {
      active = false;
    };
  }, [value]);
  return (
    <Upload
      listType={image ? 'picture' : 'text'}
      fileList={files}
      maxCount={maxCount}
      accept={image ? 'image/png,image/jpeg,image/webp' : undefined}
      beforeUpload={
        image
          ? (file) =>
              /image\/(png|jpeg|webp)/.test(file.type) || Upload.LIST_IGNORE
          : undefined
      }
      customRequest={async (option) => {
        const body = new FormData();
        body.append('file', option.file);
        body.append('bizType', 'attach');
        try {
          const result = await api.post<{ uploadId: string; fileName: string }>(
            '/system/upload',
            body,
            { dedupe: false },
          );
          setFiles((rows) => [
            ...rows,
            { uid: result.uploadId, name: result.fileName, status: 'done' },
          ]);
          onChange?.([...value, result.uploadId]);
          option.onSuccess?.(result);
        } catch (error) {
          option.onError?.(error as Error);
        }
      }}
      onRemove={(file) => {
        onChange?.(value.filter((id) => id !== file.uid));
        return true;
      }}
    >
      {files.length < maxCount && (
        <Button icon={<UploadOutlined />}>{t('上传文件')}</Button>
      )}
    </Upload>
  );
}
