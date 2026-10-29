import { useEffect, useRef } from 'react';
import { App, Input, Tooltip } from 'antd';
import { SearchOutlined } from '@ant-design/icons';

import { usePageContextStore } from '@/stores/pageContext';

/**
 * Omnibox is the global natural-language input.
 *
 * It is page-context aware: every submission carries the current route, the
 * active filters and the selected rows, so "停用这几个" resolves to the rows
 * the user is actually looking at. The context is collected by the
 * usePageContext hook on each list page.
 *
 * Phase 6 wires this to POST /api/ai/chat/stream and renders the three
 * response shapes (answer / navigate / plan). Until then it reports what it
 * would send, which keeps the context plumbing honest and testable.
 */
export function Omnibox() {
  const { message } = App.useApp();
  const ref = useRef<React.ComponentRef<typeof Input>>(null);
  const context = usePageContextStore((s) => s.context);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        ref.current?.focus();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const onSubmit = (value: string) => {
    if (!value.trim()) return;
    message.info(
      `AI 会话将在 Phase 6 接入。当前页面上下文：${
        context.capability ?? '未上报'
      }，已选 ${context.selectedIds?.length ?? 0} 行`,
    );
  };

  return (
    <Tooltip title="⌘K / Ctrl+K 聚焦" placement="bottomLeft">
      <Input
        ref={ref}
        prefix={<SearchOutlined className="text-gray-400" />}
        placeholder="用自然语言操作后台，例如：把这几个条目停用"
        allowClear
        onPressEnter={(e) => onSubmit(e.currentTarget.value)}
        className="max-w-xl"
      />
    </Tooltip>
  );
}
