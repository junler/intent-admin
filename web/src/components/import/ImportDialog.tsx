import {
  CheckCircleOutlined,
  DownloadOutlined,
  InboxOutlined,
  UploadOutlined,
} from '@ant-design/icons';
import { history } from '@umijs/max';
import {
  Alert,
  App,
  Button,
  Modal,
  Space,
  Steps,
  Table,
  Typography,
  Upload,
} from 'antd';
import { useEffect, useRef, useState } from 'react';

import type { Preview } from '@/api/types';
import { Auth } from '@/components/auth/Auth';
import { DiffTable } from '@/components/diff/DiffTable';
import { usePageActive } from '@/contexts/ActivePageContext';
import { invalidateDicts } from '@/hooks/useDict';
import { usePerm } from '@/hooks/usePerm';
import { localizeMessage, t } from '@/i18n';
import { ApiError, api, Code } from '@/utils/request';
import { downloadImportFile } from './files';

export interface ImportDialogProps {
  base: string;
  permission: string;
  onSuccess?: () => void;
  label?: string;
}
interface Report {
  confirmationId: string;
  fingerprint?: string;
  expiresAt: string;
  total: number;
  valid: boolean;
  errors: { row: number; message: string }[];
  preview: Preview;
}

/** Uploads are reviewed before the server accepts a single-use confirmation. */
export default function ImportDialog({
  base,
  permission,
  onSuccess,
  label,
}: ImportDialogProps) {
  const active = usePageActive();
  const { message } = App.useApp();
  const canViewChanges = usePerm('system:changeset:list');
  const [open, setOpen] = useState(false);
  const [file, setFile] = useState<File>();
  const [report, setReport] = useState<Report>();
  const [busy, setBusy] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState('');
  const [now, setNow] = useState(Date.now());
  const pending = useRef<AbortController | undefined>(undefined);

  useEffect(() => {
    if (!open || !report) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [open, report]);
  useEffect(() => () => pending.current?.abort(), []);
  const expired = !!report && new Date(report.expiresAt).getTime() <= now;

  async function discard(current?: Report) {
    if (!current) return;
    try {
      await api.del(
        `${base}/import/${encodeURIComponent(current.confirmationId)}`,
        { silent: true },
      );
    } catch {
      /* Expiry also releases abandoned previews. */
    }
  }

  function close() {
    if (confirming) return;
    pending.current?.abort();
    void discard(report);
    setOpen(false);
    setFile(undefined);
    setReport(undefined);
    setError('');
    setBusy(false);
  }
  function choose(next: File) {
    pending.current?.abort();
    void discard(report);
    setReport(undefined);
    setError('');
    setBusy(false);
    if (
      !next.name.toLowerCase().endsWith('.xlsx') ||
      next.size > 5 * 1024 * 1024
    ) {
      setFile(undefined);
      setError(t('请选择不超过 5 MB 的 XLSX 文件'));
    } else setFile(next);
    return false;
  }
  async function preview() {
    if (!file || busy) return;
    const controller = new AbortController();
    pending.current = controller;
    setBusy(true);
    setReport(undefined);
    setError('');
    const body = new FormData();
    body.append('file', file);
    try {
      await discard(report);
      if (controller.signal.aborted) return;
      const next = await api.post<Report>(`${base}/import/preview`, body, {
        dedupe: false,
        silent: true,
        signal: controller.signal,
      });
      if (!controller.signal.aborted) {
        setReport(next);
        setNow(Date.now());
      }
    } catch (cause) {
      if (!controller.signal.aborted)
        setError(
          localizeMessage(
            cause instanceof Error ? cause.message : t('文件校验失败'),
          ),
        );
    } finally {
      if (pending.current === controller) {
        setBusy(false);
        pending.current = undefined;
      }
    }
  }
  async function confirm() {
    if (!report?.valid || !report.fingerprint || expired || confirming) return;
    setConfirming(true);
    setError('');
    try {
      const result = await api.post<{ affected: number; changesetId: string }>(
        `${base}/import/confirm`,
        {
          confirmationId: report.confirmationId,
          fingerprint: report.fingerprint,
        },
        { dedupe: false, silent: true },
      );
      if (base.startsWith('/system/dict')) invalidateDicts();
      if (base === '/system/role')
        window.dispatchEvent(new Event('intent-admin:permissions-changed'));
      message.success({
        duration: 8,
        content: (
          <Space>
            {t('已导入 {count} 条记录', { count: result.affected })}
            {canViewChanges && (
              <Button
                type="link"
                size="small"
                onClick={() =>
                  history.push(
                    `/changesets?changesetId=${encodeURIComponent(result.changesetId)}`,
                  )
                }
              >
                {t('查看变更')}
              </Button>
            )}
          </Space>
        ),
      });
      setOpen(false);
      setFile(undefined);
      setReport(undefined);
      onSuccess?.();
    } catch (cause) {
      const stale = cause instanceof ApiError && cause.code === Code.StaleData;
      setError(
        stale
          ? t('数据已变化或预览已失效，请重新校验文件后确认。')
          : localizeMessage(
              cause instanceof Error ? cause.message : t('导入失败'),
            ),
      );
      // Every confirmation is one-use, including a rejected attempt.
      setReport(undefined);
    } finally {
      setConfirming(false);
    }
  }
  async function download(url: string, filename: string) {
    try {
      await downloadImportFile(url, filename);
    } catch (cause) {
      setError(
        localizeMessage(cause instanceof Error ? cause.message : t('下载失败')),
      );
    }
  }

  return (
    <Auth perm={permission}>
      <Button icon={<UploadOutlined />} onClick={() => setOpen(true)}>
        {label ?? t('导入')}
      </Button>
      <Modal
        title={t('批量导入')}
        open={active && open}
        onCancel={close}
        width={980}
        destroyOnHidden
        closable={!confirming}
        maskClosable={!confirming}
        footer={
          <Space>
            <Button disabled={confirming} onClick={close}>
              {t('取消')}
            </Button>
            <Button
              loading={busy}
              disabled={!file || confirming}
              onClick={() => void preview()}
            >
              {report ? t('重新校验') : t('校验并预览')}
            </Button>
            <Button
              type="primary"
              icon={<CheckCircleOutlined />}
              loading={confirming}
              disabled={!report?.valid || expired || busy}
              onClick={() => void confirm()}
            >
              {t('确认整批导入')}
            </Button>
          </Space>
        }
      >
        <Space orientation="vertical" size="large" style={{ width: '100%' }}>
          <Steps
            size="small"
            current={report ? 2 : file ? 1 : 0}
            items={[
              { title: t('准备文件') },
              { title: t('校验预览') },
              { title: t('整批确认') },
            ]}
          />
          <Alert
            type="info"
            showIcon
            title={t(
              '仅新增记录，一次最多 200 行。任何一行失败，整批都会回滚。',
            )}
            description={
              <Space wrap>
                <span>
                  {t(
                    '先下载模板填写，必填列带 *；状态 0 为正常、1 为停用，金额单位为分，不支持公式。',
                  )}
                </span>
                <Button
                  type="link"
                  size="small"
                  icon={<DownloadOutlined />}
                  onClick={() =>
                    void download(
                      `${base}/import/template`,
                      'import-template.xlsx',
                    )
                  }
                >
                  {t('下载导入模板')}
                </Button>
              </Space>
            }
          />
          {base === '/system/user' && (
            <Alert
              type="warning"
              showIcon
              title={t(
                '用户使用系统初始密码，不导入密码、角色或额外权限。请通过安全渠道通知用户并要求修改密码。',
              )}
            />
          )}
          {base === '/system/config' && (
            <Alert
              type="warning"
              showIcon
              title={t('密码、密钥、令牌等凭据类参数不能通过批量导入管理。')}
            />
          )}
          <Upload.Dragger
            accept=".xlsx"
            maxCount={1}
            multiple={false}
            disabled={confirming}
            beforeUpload={choose}
            fileList={
              file
                ? [{ uid: 'import-file', name: file.name, status: 'done' }]
                : []
            }
            onRemove={() => {
              pending.current?.abort();
              void discard(report);
              setFile(undefined);
              setReport(undefined);
              setError('');
              setBusy(false);
            }}
          >
            <p className="ant-upload-drag-icon">
              <InboxOutlined />
            </p>
            <p className="ant-upload-text">{t('点击或拖拽 XLSX 文件到这里')}</p>
            <p className="ant-upload-hint">
              {t('单个文件，最大 5 MB；使用上方模板可避免表头错误。')}
            </p>
          </Upload.Dragger>
          {error && <Alert type="error" showIcon title={error} />}
          {report && (
            <>
              <Alert
                showIcon
                type={report.valid && !expired ? 'success' : 'warning'}
                title={
                  expired
                    ? t('预览已过期，请重新校验。')
                    : report.valid
                      ? t('全部 {count} 行校验通过', { count: report.total })
                      : t('共 {count} 行，发现 {errors} 处错误', {
                          count: report.total,
                          errors: report.errors.length,
                        })
                }
                description={t('确认有效期为 10 分钟；请核对下方变更后执行。')}
              />
              {report.errors.length > 0 && (
                <>
                  <Button
                    icon={<DownloadOutlined />}
                    onClick={() =>
                      void download(
                        `${base}/import/${encodeURIComponent(report.confirmationId)}/errors`,
                        'import-errors.xlsx',
                      )
                    }
                  >
                    {t('下载错误清单')}
                  </Button>
                  <Table
                    rowKey={(row) => `${row.row}:${row.message}`}
                    size="small"
                    dataSource={report.errors}
                    pagination={{ pageSize: 5, hideOnSinglePage: true }}
                    columns={[
                      { title: t('文件行号'), dataIndex: 'row', width: 100 },
                      {
                        title: t('校验错误'),
                        dataIndex: 'message',
                        render: (value: string) => localizeMessage(value),
                      },
                    ]}
                  />
                </>
              )}
              <Typography.Text type="secondary">
                {t('本次预览不会写入业务数据。')}
              </Typography.Text>
              <DiffTable preview={report.preview} />
            </>
          )}
        </Space>
      </Modal>
    </Auth>
  );
}
