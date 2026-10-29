import { App } from 'antd';
import { useCallback, useState } from 'react';
import type { Preview } from '@/api/types';
import { t } from '@/i18n';
import { ApiError, api, Code, type RiskExceededDetail } from '@/utils/request';

export interface PreviewActionConfig {
  /** Capability name, for messages. */
  capability: string;
  /** The generated preview endpoint, e.g. /demo/item/preview. */
  previewUrl: string;
  /** Builds the execution request from the same arguments. */
  execute: (args: unknown) => Promise<unknown>;
  /** Called after a successful execution. */
  onDone?: () => void;
  /**
   * Skip the preview for low-risk single-record edits. The change still goes
   * through the changeset and is still undoable; only the confirmation step
   * is omitted.
   */
  skipPreview?: boolean;
}

export interface PreviewState {
  open: boolean;
  loading: boolean;
  executing: boolean;
  preview?: Preview;
}

/**
 * usePreviewAction implements the page-side half of
 * preview → confirm → execute → undo.
 *
 * The page calls the capability's generated /preview endpoint, shows the same
 * DiffTable the AI plan card uses, and on confirmation executes. This is why
 * the reversibility work is not an "AI tax": a human mis-click gets the same
 * safety net.
 */
export function usePreviewAction(cfg: PreviewActionConfig) {
  const { message, modal } = App.useApp();
  const [state, setState] = useState<PreviewState>({
    open: false,
    loading: false,
    executing: false,
  });
  const [args, setArgs] = useState<unknown>(null);

  const close = useCallback(() => {
    setState({ open: false, loading: false, executing: false });
  }, []);

  const run = useCallback(
    async (nextArgs: unknown) => {
      setArgs(nextArgs);

      if (cfg.skipPreview) {
        await doExecute(nextArgs);
        return;
      }

      setState({ open: true, loading: true, executing: false });
      try {
        const preview = await api.post<Preview>(cfg.previewUrl, nextArgs);
        setState({ open: true, loading: false, executing: false, preview });
      } catch (err) {
        close();
        reportRisk(err);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [cfg.previewUrl, cfg.skipPreview],
  );

  const doExecute = useCallback(
    async (execArgs: unknown) => {
      setState((s) => ({ ...s, executing: true }));
      try {
        await cfg.execute(execArgs);
        close();
        // The undo affordance belongs here, next to the result, not buried in
        // a history page: the moment right after an accidental action is when
        // a user actually wants it.
        message.success({
          content: t('操作成功'),
          duration: 4,
        });
        cfg.onDone?.();
      } catch (err) {
        setState((s) => ({ ...s, executing: false }));
        if (err instanceof ApiError && err.code === Code.StaleData) {
          // The data moved between preview and apply. Re-previewing is the
          // only honest response — silently applying would execute something
          // the user never saw.
          modal.warning({
            title: t('数据已变化'),
            content: t('预览之后数据发生了变化，请重新确认影响范围。'),
            onOk: () => void run(execArgs),
          });
          return;
        }
        reportRisk(err);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [cfg.execute, cfg.onDone],
  );

  const confirm = useCallback(() => doExecute(args), [args, doExecute]);

  function reportRisk(err: unknown) {
    if (!(err instanceof ApiError) || err.code !== Code.RiskExceeded) return;
    const detail = err.detail as RiskExceededDetail | undefined;
    modal.info({
      title: t('需要人工在页面执行'),
      content: detail?.hint ?? err.message,
    });
  }

  return { state, run, confirm, close };
}
