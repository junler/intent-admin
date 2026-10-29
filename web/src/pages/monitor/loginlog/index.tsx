import { LogPage } from '@/components/admin/LogPage';
import { t } from '@/i18n';
export default function LoginLog() {
  return (
    <LogPage
      title={t('登录日志')}
      base="/monitor/loginlog"
      idKey="infoId"
      perm="monitor:loginlog"
      fields={[
        { name: 'userName', label: t('账号'), search: true },
        { name: 'ipaddr', label: 'IP', search: true },
        { name: 'browser', label: t('浏览器') },
        { name: 'os', label: t('系统') },
        {
          name: 'status',
          label: t('结果'),
          type: 'select',
          search: true,
          options: [
            { label: t('成功'), value: '0' },
            { label: t('失败'), value: '1' },
          ],
        },
        { name: 'msg', label: t('说明') },
        { name: 'loginTime', label: t('时间') },
      ]}
      actions={[
        {
          label: t('解锁账号'),
          perm: 'monitor:loginlog:unlock',
          url: '/monitor/loginlog/unlock',
          previewUrl: '/monitor/loginlog/unlock/preview',
          args: (row) => ({ userName: row.userName }),
        },
      ]}
    />
  );
}
