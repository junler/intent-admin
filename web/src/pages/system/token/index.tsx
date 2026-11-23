import { Alert, App, Tag, Typography } from 'antd';
import { CrudPage } from '@/components/admin/CrudPage';
import { riskOptions } from '@/components/admin/types';
import { getLocale, t } from '@/i18n';
import { parseTokenTimestamp } from './timestamps';

function renderExpiry(value: unknown) {
  if (value === null) return t('不过期');
  const expiry = parseTokenTimestamp(value);
  if (!expiry) return t('时间不可用');
  const formatted = expiry.date.toLocaleString(getLocale());
  if (expiry.cutoffMs > Date.now()) return formatted;
  return (
    <>
      <Tag color="default">{t('已过期')}</Tag>
      {formatted}
    </>
  );
}

function renderLastUsed(value: unknown) {
  if (value == null) return t('未记录');
  return (
    parseTokenTimestamp(value)?.date.toLocaleString(getLocale()) ??
    t('时间不可用')
  );
}

export default function Tokens() {
  const { modal } = App.useApp();
  return (
    <CrudPage
      title={t('个人访问令牌')}
      base="/system/token"
      removePreviewUrl="/system/token/revoke/preview"
      idKey="tokenId"
      perm=""
      canEdit={false}
      onCreateResult={(data) =>
        modal.info({
          title: t('请立即保存令牌'),
          width: 650,
          content: (
            <>
              <Alert
                type="warning"
                title={t('明文只显示这一次，关闭后无法再次读取。')}
                className="mb-3"
              />
              <Typography.Paragraph copyable code>
                {data.token}
              </Typography.Paragraph>
            </>
          ),
        })
      }
      fields={[
        {
          name: 'name',
          label: t('名称'),
          required: true,
          search: true,
          aiDefaultValue: t('个人访问令牌'),
        },
        { name: 'prefix', label: t('令牌前缀'), form: false },
        {
          name: 'scopes',
          label: t('权限白名单'),
          type: 'multi',
          column: false,
          help: t('填写完整权限字符；留空表示不额外收窄当前用户权限。'),
        },
        {
          name: 'maxRisk',
          label: t('风险上限'),
          type: 'select',
          options: riskOptions,
          defaultValue: 'low',
        },
        {
          name: 'expiresAt',
          label: t('过期时间'),
          type: 'date',
          help: t('留空表示不过期。'),
          render: renderExpiry,
        },
        {
          name: 'lastUsedAt',
          label: t('最近使用'),
          form: false,
          render: renderLastUsed,
        },
        { name: 'createTime', label: t('创建时间'), form: false },
      ]}
    />
  );
}
