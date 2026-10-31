import { history } from '@umijs/max';
import { App, Button, Space } from 'antd';
import { useRef, useState } from 'react';
import { usePageActive } from '@/contexts/ActivePageContext';
import { invalidateDicts } from '@/hooks/useDict';
import { usePerm } from '@/hooks/usePerm';
import { t } from '@/i18n';
import { ApiError, Code, request } from '@/utils/request';
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
export function useAdminAction(after?: () => void, onClosed?: () => void) {
  const active = usePageActive();
  const canViewChanges = usePerm('system:changeset:list');
  const { message } = App.useApp();
  const pending = useRef(false);
  const [executing, setExecuting] = useState(false);
  // Human page submissions execute directly through the ordinary HTTP capability.
  async function run(command: Command) {
    if (!active || pending.current) return;
    pending.current = true;
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
      command.after?.();
      after?.();
    } catch (error) {
      if (
        error instanceof ApiError &&
        (error.code === Code.StaleData || error.code === Code.RiskExceeded)
      ) {
        message.error(error.message);
      }
    } finally {
      pending.current = false;
      setExecuting(false);
      onClosed?.();
    }
  }
  return { run, executing, modal: null };
}
