import { CrudPage } from '@/components/admin/CrudPage';
import { t } from '@/i18n';
export default function Online() {
  return (
    <CrudPage
      title={t('在线用户')}
      base="/monitor/online"
      idKey="tokenId"
      perm="monitor:online"
      canCreate={false}
      canEdit={false}
      canDelete={false}
      fields={[
        { name: 'userName', label: t('账号'), search: true },
        {
          name: 'ipaddr',
          label: 'IP',
          search: true,
          render: (_, row) => row.ip,
        },
        { name: 'loginTime', label: t('登录时间') },
        { name: 'userAgent', label: t('客户端') },
      ]}
      actions={[
        {
          label: t('强制下线'),
          perm: 'monitor:online:forceLogout',
          danger: true,
          url: (r) => `/monitor/online/${encodeURIComponent(r.tokenId)}`,
          method: 'DELETE',
          previewUrl: '/monitor/online/forceLogout/preview',
          args: (r) => ({ tokenId: r.tokenId, ids: [r.tokenId] }),
        },
      ]}
    />
  );
}
