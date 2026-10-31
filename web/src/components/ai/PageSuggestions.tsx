import { history } from '@umijs/max';
import { Button, Space, Typography } from 'antd';
import type { Evidence } from '@/api/types';
import { t } from '@/i18n';
import { useUserStore } from '@/stores/user';
import { navigationURL, pageSuggestions } from './state';

/** Suggestions wait for the user's choice and follow the current authorized menus. */
export function PageSuggestions({
  evidence,
  onNavigate,
}: {
  evidence: Evidence[];
  onNavigate?: () => void;
}) {
  const menus = useUserStore((state) => state.menus);
  const suggestions = pageSuggestions(evidence, menus);
  if (!suggestions.length) return null;
  return (
    <div className="mt-4">
      <Typography.Paragraph type="secondary" className="mb-2">
        {t('是否前往相关页面查看？')}
      </Typography.Paragraph>
      <Space wrap>
        {suggestions.map(({ url, title }) => (
          <Button
            key={url}
            size="small"
            onClick={() => {
              // Permissions may change while a saved conversation stays open.
              if (!navigationURL({ route: url }, useUserStore.getState().menus))
                return;
              history.push(url);
              onNavigate?.();
            }}
          >
            {t('查看{page}', { page: t(title) })}
          </Button>
        ))}
      </Space>
    </div>
  );
}
