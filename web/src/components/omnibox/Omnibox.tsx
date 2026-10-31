import { CommentOutlined } from '@ant-design/icons';
import { Tooltip } from 'antd';
import { useEffect, useId, useState } from 'react';
import { useAiPanel } from '@/components/ai/AiPanel';
import { usePerm } from '@/hooks/usePerm';
import { t } from '@/i18n';
import { usePageContextStore } from '@/stores/pageContext';
import { useUserStore } from '@/stores/user';
import { selectionOf } from './selection';

/** macOS uses the command symbol; every other platform uses Ctrl. */
function isApple(): boolean {
  if (typeof navigator === 'undefined') return false;
  return /mac|iphone|ipad|ipod/i.test(navigator.userAgent);
}

export function Omnibox({ compact = false }: { compact?: boolean }) {
  const allowed = usePerm('ai:agent:use');
  const reveal = useAiPanel((s) => s.reveal);
  const context = usePageContextStore((s) => s.context);
  const menus = useUserStore((s) => s.menus);
  const [focused, setFocused] = useState(false);
  const chipId = useId();

  useEffect(() => {
    const key = (event: KeyboardEvent) => {
      if (
        allowed &&
        (event.metaKey || event.ctrlKey) &&
        event.key.toLowerCase() === 'k'
      ) {
        event.preventDefault();
        reveal();
      }
    };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  }, [allowed, reveal]);

  const selection = selectionOf(context, menus);
  // One tooltip for the whole bar: a second one on the chip would stack on top
  // of it, so the selected rows are named here instead.
  const hint = !allowed ? (
    t('当前账号没有 AI 使用权限')
  ) : (
    <>
      {t('Intent 会读取当前页面的筛选与勾选项')}
      {!!selection?.labels.length && (
        <div className="intent-omnibox-hint-rows">
          {selection.labels.slice(0, 8).join('、')}
          {selection.labels.length > 8 && '…'}
        </div>
      )}
    </>
  );

  return (
    <Tooltip
      title={hint}
      placement="bottomLeft"
      mouseEnterDelay={0.5}
      open={focused ? false : undefined}
    >
      <button
        type="button"
        disabled={!allowed}
        aria-label={t('AI 会话')}
        aria-haspopup="dialog"
        aria-describedby={selection ? chipId : undefined}
        onClick={reveal}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        className={[
          'intent-omnibox',
          !allowed && 'intent-omnibox-disabled',
          compact && 'intent-omnibox-compact',
        ]
          .filter(Boolean)
          .join(' ')}
      >
        <CommentOutlined className="intent-omnibox-mark" aria-hidden="true" />
        <span className="intent-omnibox-placeholder">
          {t('描述你的目标，例如：停用选中的用户')}
        </span>
        {selection && (
          <span className="intent-omnibox-chip" id={chipId}>
            {selection.scope && (
              <span className="intent-omnibox-chip-scope">
                {t(selection.scope)}
              </span>
            )}
            <span className="intent-omnibox-chip-detail">
              {selection.label ??
                t('已选 {count} 项', { count: selection.count })}
            </span>
          </span>
        )}
        {!compact && allowed && (
          <kbd className="intent-omnibox-kbd">
            <span className="intent-omnibox-modifier">
              {isApple() ? '⌘' : 'Ctrl'}
            </span>
            <span>K</span>
          </kbd>
        )}
      </button>
    </Tooltip>
  );
}
