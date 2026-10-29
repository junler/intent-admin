import { SearchOutlined } from '@ant-design/icons';
import { Input, Tooltip } from 'antd';
import { useEffect, useRef } from 'react';
import { useAiPanel } from '@/components/ai/AiPanel';
import { usePerm } from '@/hooks/usePerm';
import { t } from '@/i18n';

export function Omnibox() {
  const ref = useRef<React.ComponentRef<typeof Input>>(null);
  const allowed = usePerm('ai:agent:use');
  const show = useAiPanel((s) => s.show);
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
  return (
    <Tooltip
      title={
        allowed
          ? t('⌘K / Ctrl+K · 感知当前筛选与勾选项')
          : t('当前账号没有 AI 使用权限')
      }
    >
      <Input.Search
        ref={ref}
        prefix={<SearchOutlined />}
        placeholder={t('描述你的意图，例如：把选中的用户停用')}
        disabled={!allowed}
        allowClear
        onSearch={(text) => show(text.trim() || undefined)}
        className="max-w-xl"
      />
    </Tooltip>
  );
}
