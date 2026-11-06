import { App } from 'antd';
import { useRef, useState } from 'react';
import { usePageActive } from '@/contexts/ActivePageContext';
import { invalidateDicts } from '@/hooks/useDict';
import { t } from '@/i18n';
import { auth, ApiError, Code, request } from '@/utils/request';
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
  const { message } = App.useApp();
  const pending = useRef(false);
  const attempt = useRef<
    { signature: string; key: string; time: number } | undefined
  >(undefined);
  const [executing, setExecuting] = useState(false);
  // Human page submissions execute directly through the ordinary HTTP capability.
  async function run(command: Command) {
    if (!active || pending.current) return false;
    const signature = JSON.stringify([
      auth.get(),
      command.url,
      command.method,
      command.args,
    ]);
    if (
      !attempt.current ||
      attempt.current.signature !== signature ||
      Date.now() - attempt.current.time > 300_000
    )
      attempt.current = {
        signature,
        key:
          globalThis.crypto.randomUUID?.() ??
          Array.from(
            globalThis.crypto.getRandomValues(new Uint8Array(16)),
            (byte) => byte.toString(16).padStart(2, '0'),
          ).join(''),
        time: Date.now(),
      };
    pending.current = true;
    setExecuting(true);
    try {
      const value = await (command.execute
        ? command.execute()
        : request({
            url: command.url,
            method: command.method,
            dedupe: false,
            data: command.args,
            headers: { 'Idempotency-Key': attempt.current.key },
          }));
      attempt.current = undefined;
      if (command.url.startsWith('/system/dict')) invalidateDicts();
      if (
        /^\/system\/(?:menu|role)(?:\/|$)|^\/system\/user\/(?:authRole|authPerm|aiRisk)$/.test(
          command.url,
        )
      )
        window.dispatchEvent(new Event('intent-admin:permissions-changed'));
      command.result?.(value);
      message.success(t('操作成功'));
      command.after?.();
      after?.();
      return true;
    } catch (error) {
      // Retain the key only for an ambiguous network failure.
      if (error instanceof ApiError && error.code < 500)
        attempt.current = undefined;
      if (
        error instanceof ApiError &&
        (error.code === Code.StaleData || error.code === Code.RiskExceeded)
      ) {
        message.error(error.message);
      }
      return false;
    } finally {
      pending.current = false;
      setExecuting(false);
      onClosed?.();
    }
  }
  return { run, executing, modal: null };
}
