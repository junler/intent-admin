import { usePageRefresh } from '@/hooks/usePageRefresh';
import { DetailDialog } from '@/components/common/DetailDialog';
import { DeleteOutlined, SafetyCertificateOutlined } from '@ant-design/icons';
import {
  type ActionType,
  PageContainer,
  type ProColumns,
  ProTable,
} from '@ant-design/pro-components';
import {
  Alert,
  App,
  Button,
  Descriptions,
  Empty,
  Modal,
  Space,
  Tag,
  Typography,
} from 'antd';
import { useRef, useState } from 'react';
import { usePageActive } from '@/contexts/ActivePageContext';
import { getLocale, t } from '@/i18n';
import { api } from '@/utils/request';

interface Grant {
  grantId: number;
  clientName: string;
  scopes: string[];
  createTime: string;
  expiresAt: string;
  revokedAt?: string;
}
const scopeLabels: Record<string, string> = {
  'mcp:read': t('读取 MCP 功能与知识'),
  'mcp:write': t('确认后修改数据'),
  offline_access: t('后台保持授权'),
};
function status(grant: Grant) {
  if (grant.revokedAt) return { text: t('已撤销'), color: 'default' };
  if (Date.parse(grant.expiresAt) <= Date.now())
    return { text: t('已过期'), color: 'orange' };
  return { text: t('有效'), color: 'green' };
}
export default function OAuthGrantsPage() {
  const active = usePageActive();
  const action = useRef<ActionType>(undefined);
  usePageRefresh(() => action.current?.reload());
  const { message } = App.useApp();
  const [detail, setDetail] = useState<Grant>();
  const [revoking, setRevoking] = useState<Grant>();
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  async function revoke() {
    if (!revoking || pending.current) return;
    pending.current = true;
    setBusy(true);
    try {
      await api.del(`/oauth/grants/${revoking.grantId}`);
      message.success(t('客户端授权已撤销'));
      setRevoking(undefined);
      setDetail(undefined);
      action.current?.reload();
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }
  const columns: ProColumns<Grant>[] = [
    { title: t('客户端'), dataIndex: 'clientName', ellipsis: true, width: 180 },
    {
      title: t('授权范围'),
      dataIndex: 'scopes',
      width: 260,
      search: false,
      render: (_, row) => (
        <Space wrap style={{ minWidth: 0, maxWidth: '100%' }}>
          {row.scopes.map((scope) => (
            <Tag
              key={scope}
              style={{
                maxWidth: '100%',
                whiteSpace: 'normal',
                overflowWrap: 'anywhere',
              }}
            >
              {t(scopeLabels[scope] ?? scope)}
            </Tag>
          ))}
        </Space>
      ),
    },
    {
      title: t('状态'),
      key: 'status',
      width: 110,
      search: false,
      render: (_, row) => (
        <Tag color={status(row).color}>{status(row).text}</Tag>
      ),
    },
    {
      title: t('授权时间'),
      dataIndex: 'createTime',
      width: 190,
      valueType: 'dateTime',
      search: false,
    },
    {
      title: t('到期时间'),
      dataIndex: 'expiresAt',
      width: 190,
      valueType: 'dateTime',
      search: false,
    },
    {
      title: t('操作'),
      valueType: 'option',
      width: 220,
      render: (_, row) => [
        <Button type="link" key="details" onClick={() => setDetail(row)}>
          {t('详情')}
        </Button>,
        <Button
          type="link"
          danger
          key="revoke"
          icon={<DeleteOutlined />}
          disabled={Boolean(row.revokedAt)}
          onClick={() => setRevoking(row)}
        >
          {t('撤销授权')}
        </Button>,
      ],
    },
  ];
  return (
    <PageContainer title={t('MCP 客户端授权')}>
      <Alert
        type="info"
        showIcon
        icon={<SafetyCertificateOutlined />}
        message={t('你可以随时撤销已授权的客户端')}
        description={t(
          '撤销后，该客户端的访问令牌和刷新令牌立即失效。所有调用仍受你当前的账号权限及风险上限约束。',
        )}
        style={{ marginBottom: 20 }}
      />
      <ProTable<Grant>
        rowKey="grantId"
        style={{ minWidth: 0 }}
        scroll={{ x: 1200 }}
        actionRef={action}
        columns={columns}
        search={false}
        headerTitle={t('已授权客户端')}
        request={async () => {
          const rows = await api.get<Grant[]>('/oauth/grants', undefined, {
            dedupe: false,
          });
          return { data: rows, success: true, total: rows.length };
        }}
        pagination={{ pageSize: 10, hideOnSinglePage: true }}
        locale={{
          emptyText: <Empty description={t('尚未授权任何 MCP 客户端')} />,
        }}
      />
      <DetailDialog
        open={active && Boolean(detail)}
        title={t('授权详情')}
        width={580}
        onClose={() => setDetail(undefined)}
      >
        {detail && (
          <Descriptions
            column={1}
            bordered
            items={[
              {
                key: 'client',
                label: t('客户端'),
                children: detail.clientName,
              },
              {
                key: 'scopes',
                label: t('授权范围'),
                children: (
                  <Space wrap>
                    {detail.scopes.map((scope) => (
                      <Tag key={scope}>{t(scopeLabels[scope] ?? scope)}</Tag>
                    ))}
                  </Space>
                ),
              },
              {
                key: 'status',
                label: t('状态'),
                children: status(detail).text,
              },
              {
                key: 'created',
                label: t('授权时间'),
                children: new Date(detail.createTime).toLocaleString(
                  getLocale(),
                ),
              },
              {
                key: 'expires',
                label: t('到期时间'),
                children: new Date(detail.expiresAt).toLocaleString(
                  getLocale(),
                ),
              },
              ...(detail.revokedAt
                ? [
                    {
                      key: 'revoked',
                      label: t('撤销时间'),
                      children: new Date(detail.revokedAt).toLocaleString(
                        getLocale(),
                      ),
                    },
                  ]
                : []),
            ]}
          />
        )}
      </DetailDialog>
      <Modal
        open={active && Boolean(revoking)}
        title={t('撤销客户端授权')}
        okText={t('撤销授权')}
        cancelText={t('取消')}
        okButtonProps={{ danger: true }}
        confirmLoading={busy}
        onOk={revoke}
        onCancel={() => !busy && setRevoking(undefined)}
      >
        <Typography.Paragraph>
          {t('撤销后需要在客户端重新发起授权才能继续访问。')}
        </Typography.Paragraph>
        <Typography.Text strong>{revoking?.clientName}</Typography.Text>
      </Modal>
    </PageContainer>
  );
}
