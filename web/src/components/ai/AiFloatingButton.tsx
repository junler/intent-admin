import { RobotOutlined } from '@ant-design/icons';
import { useLocation } from '@umijs/max';
import { FloatButton } from 'antd';
import { createPortal } from 'react-dom';
import { usePerm } from '@/hooks/usePerm';
import { t } from '@/i18n';
import { flattenPages } from '@/router/menu';
import { useUserStore } from '@/stores/user';
import { detectAssistantContext } from './floatingContext';
import { useAiPanel } from './panelState';

export function AiFloatingButton() {
  const location = useLocation();
  const allowed = usePerm('ai:agent:use');
  const open = useAiPanel((state) => state.open);
  const menus = useUserStore((state) => state.menus);
  if (!allowed || open || typeof document === 'undefined') return null;
  return createPortal(
    <FloatButton
      className="intent-ai-floating-entry"
      type="primary"
      icon={<RobotOutlined />}
      tooltip={{ title: t('AI 助手'), placement: 'left', zIndex: 20001 }}
      aria-label={t('打开 AI 助手')}
      style={{ zIndex: 20000 }}
      onClick={() => {
        const panel = useAiPanel.getState();
        const title = flattenPages(menus).find(
          (page) => page.path === location.pathname,
        )?.meta?.title;
        const context = detectAssistantContext(
          location.pathname,
          title ? t(title) : t('当前页面'),
        );
        if (panel.open && context.layer <= (panel.request?.layer ?? 1800)) {
          panel.reveal();
          return;
        }
        panel.compose(context.prompt, context.form, context.layer);
      }}
    />,
    document.body,
  );
}
