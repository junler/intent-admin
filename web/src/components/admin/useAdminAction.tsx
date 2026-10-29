import { history } from '@umijs/max';
import { Alert, App, Button, Modal, Space } from 'antd';
import { useState } from 'react';
import type { Preview } from '@/api/types';
import { DiffTable } from '@/components/diff/DiffTable';
import { usePageActive } from '@/contexts/ActivePageContext';
import { invalidateDicts } from '@/hooks/useDict';
import { usePerm } from '@/hooks/usePerm';
import { t } from '@/i18n';
import { ApiError, api, Code, request } from '@/utils/request';
import type { Row } from './types';

interface Command {
  title: string;
  url: string;
  method: 'POST' | 'PUT' | 'DELETE';
  args?: Row;
  previewUrl?: string;
  execute?: () => Promise<unknown>;
  result?: (value: any) => void;
  after?: () => void;
}
export function useAdminAction(after?: () => void) {
  const active = usePageActive();
  const canViewChanges = usePerm('system:changeset:list');
  const { message, modal: dialogs } = App.useApp();
  const [command, setCommand] = useState<Command>();
  const [preview, setPreview] = useState<Preview>();
  const [loading, setLoading] = useState(false);
  const [executing, setExecuting] = useState(false);
  async function run(next: Command) {
    setCommand(next);
    setPreview(undefined);
    setLoading(true);
    try {
      if (next.previewUrl)
        setPreview(await api.post<Preview>(next.previewUrl, next.args));
      else
        setPreview({
          summary: next.title,
          affected: 1,
          skipped: 0,
          reversible: false,
          warnings: [t('此操作执行后可能无法撤销，请确认。')],
        });
    } catch {
      setCommand(undefined);
    } finally {
      setLoading(false);
    }
  }
  async function confirm() {
    if (!command) return;
    setExecuting(true);
    try {
      const value = await (command.execute
        ? command.execute()
        : request({
            url: command.url,
            method: command.method,
            data: command.args,
          }));
      if (command.url.startsWith('/system/dict')) invalidateDicts();
      if (
        /^\/system\/(?:menu|role)(?:\/|$)|^\/system\/user\/(?:authRole|authPerm|aiRisk)$/.test(
          command.url,
        )
      )
        window.dispatchEvent(new Event('intent-admin:permissions-changed'));
      command.result?.(value);
      const changesetId = (value as Row | undefined)?.changesetId;
      message.success({
        duration: changesetId ? 8 : 3,
        content: (
          <Space>
            {t('操作成功')}
            {changesetId && canViewChanges && (
              <Button
                type="link"
                size="small"
                onClick={() =>
                  history.push(
                    `/changesets?changesetId=${encodeURIComponent(changesetId)}`,
                  )
                }
              >
                {t('查看变更')}
              </Button>
            )}
          </Space>
        ),
      });
      setCommand(undefined);
      command.after?.();
      after?.();
    } catch (error) {
      if (error instanceof ApiError && error.code === Code.StaleData) {
        dialogs.warning({
          title: t('数据已变化'),
          content: t('请重新预览并确认最新的变更范围。'),
          onOk: () => run(command),
        });
      } else if (
        error instanceof ApiError &&
        error.code === Code.RiskExceeded
      ) {
        dialogs.info({ title: t('风险上限限制'), content: error.message });
      }
    } finally {
      setExecuting(false);
    }
  }
  const modal = (
    <Modal
      title={command?.title}
      open={active && !!command}
      onCancel={() => !executing && setCommand(undefined)}
      onOk={() => void confirm()}
      confirmLoading={executing}
      okButtonProps={{ disabled: loading || !preview }}
      cancelButtonProps={{ disabled: executing }}
      width={850}
      destroyOnHidden
    >
      {loading ? (
        <Alert type="info" title={t('正在生成变更预览…')} />
      ) : (
        preview && <DiffTable preview={preview} />
      )}
    </Modal>
  );
  return { run, modal };
}
