import { useNavigate } from '@umijs/max';
import { Button, Result } from 'antd';
import { t } from '@/i18n';

export default function Forbidden() {
  const navigate = useNavigate();
  return (
    <Result
      status="403"
      title="403"
      subTitle={t(
        '没有访问该页面的权限。可联系管理员在「角色管理」或「单独授权」中分配。',
      )}
      extra={
        <Button type="primary" onClick={() => navigate('/')}>
          {t('返回首页')}
        </Button>
      }
    />
  );
}
