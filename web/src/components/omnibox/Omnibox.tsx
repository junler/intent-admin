import { ArrowUpOutlined } from '@ant-design/icons';
import { Input, Tooltip } from 'antd';
import { useEffect, useId, useRef, useState } from 'react';
import { useAiPanel } from '@/components/ai/AiPanel';
import { usePerm } from '@/hooks/usePerm';
import { t } from '@/i18n';
import { usePageContextStore } from '@/stores/pageContext';
import { useUserStore } from '@/stores/user';
import { selectionOf } from './selection';

/** macOS writes the shortcut as ⌘K; every other platform writes Ctrl K. */
function isApple(): boolean {
  if (typeof navigator === 'undefined') return false;
  return /mac|iphone|ipad|ipod/i.test(navigator.userAgent);
}

/**
 * The intent mark. A sparkle rather than a magnifier, because this bar does not
 * search the console — it states what should happen in it.
 */
function IntentMark() {
  return (
    <svg
      viewBox="0 0 16 16"
      className="intent-omnibox-mark"
      aria-hidden="true"
      focusable="false"
    >
      <path d="M6.6 2.8 8.6 7l4.2 2-4.2 2-2 4.2-2-4.2L.4 9l4.2-2z" />
      <path d="M12.9.8l.95 1.65 1.65.95-1.65.95-.95 1.65-.95-1.65L10.3 3.4l1.65-.95z" />
    </svg>
  );
}

export function Omnibox({ compact = false }: { compact?: boolean }) {
  const ref = useRef<React.ComponentRef<typeof Input>>(null);
  const allowed = usePerm('ai:agent:use');
  const show = useAiPanel((s) => s.show);
  const context = usePageContextStore((s) => s.context);
  const menus = useUserStore((s) => s.menus);
  const [text, setText] = useState('');
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
        ref.current?.focus();
      }
    };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  }, [allowed]);

  const selection = selectionOf(context, menus);
  const ready = Boolean(text.trim());

  function send() {
    if (!allowed || !ready) return;
    show(text.trim());
    setText('');
    ref.current?.blur();
  }

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
    <Tooltip title={hint} placement="bottom" mouseEnterDelay={0.4}>
      <div
        className={[
          'intent-omnibox',
          focused && 'intent-omnibox-focused',
          !allowed && 'intent-omnibox-disabled',
          compact && 'intent-omnibox-compact',
        ]
          .filter(Boolean)
          .join(' ')}
      >
        <IntentMark />
        <Input
          ref={ref}
          variant="borderless"
          value={text}
          disabled={!allowed}
          aria-label={t('描述你的意图')}
          aria-describedby={selection ? chipId : undefined}
          placeholder={t('描述你的意图，例如：停用选中的用户')}
          onChange={(event) => setText(event.target.value)}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          onPressEnter={(event) => {
            if (event.nativeEvent.isComposing) return;
            event.preventDefault();
            send();
          }}
          onKeyDown={(event) => {
            if (event.key === 'Escape' && !event.nativeEvent.isComposing) {
              setText('');
              ref.current?.blur();
            }
          }}
        />
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
        {ready ? (
          <button
            type="button"
            className="intent-omnibox-send"
            aria-label={t('发送给 Intent')}
            onMouseDown={(event) => event.preventDefault()}
            onClick={send}
          >
            <ArrowUpOutlined />
          </button>
        ) : (
          !compact &&
          allowed && (
            <kbd className="intent-omnibox-kbd">
              {isApple() ? '⌘K' : 'Ctrl K'}
            </kbd>
          )
        )}
      </div>
    </Tooltip>
  );
}
