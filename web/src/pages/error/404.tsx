import { useNavigate } from '@umijs/max';
import { Button, Result } from 'antd';
import { t } from '@/i18n';

export default function NotFound() {
  const navigate = useNavigate();
  return (
    <Result
      status="404"
      title="404"
      subTitle={t(
        '页面不存在。如果是从菜单进来的，可能是菜单的 component 字段配错了。',
      )}
      extra={
        <Button type="primary" onClick={() => navigate('/')}>
          {t('返回首页')}
        </Button>
      }
    />
  );
}
