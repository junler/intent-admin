import {
  type ActionType,
  PageContainer,
  type ProColumns,
  ProTable,
} from '@ant-design/pro-components';
import { history, useLocation } from '@umijs/max';
import {
  Alert,
  Button,
  Descriptions,
  Drawer,
  Space,
  Tag,
  Typography,
} from 'antd';
import dayjs from 'dayjs';
import { useRef, useState } from 'react';
import { Auth } from '@/components/auth/Auth';
import { DictTag } from '@/components/dict/DictTag';
import { usePageActive } from '@/contexts/ActivePageContext';
import { getLocale, t } from '@/i18n';
import { api, type PageData } from '@/utils/request';

interface AuditRow {
  operId: number;
  operName: string;
  actorType: string;
  source: string;
  intent: string;
  risk: string;
  approvalMode: string;
  approvedBy: string;
  capability: string;
  status: string;
  errorMsg: string;
  costTime: number;
  operTime: string;
  operIp: string;
  operParam?: string;
  jsonResult?: string;
  planId?: string;
  changesetId?: string;
  tokenId?: number;
}
function pretty(value: unknown) {
  if (typeof value === 'string') {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      return value;
    }
  }
  return JSON.stringify(value, null, 2) ?? '—';
}

export default function AuditPage() {
  const active = usePageActive();
  const location = useLocation();
  const actionRef = useRef<ActionType | undefined>(undefined);
  const [detail, setDetail] = useState<AuditRow>();
  const query = Object.fromEntries(new URLSearchParams(location.search));
  const columns: ProColumns<AuditRow>[] = [
    {
      title: t('时间'),
      dataIndex: 'operTime',
      valueType: 'dateTime',
      width: 170,
      search: false,
    },
    {
      title: t('时间范围'),
      dataIndex: 'dateRange',
      valueType: 'dateTimeRange',
      hideInTable: true,
      search: {
        transform: (value: string[]) =>
          value?.length === 2
            ? {
                beginTime: new Date(value[0]).toISOString(),
                endTime: new Date(value[1]).toISOString(),
              }
            : {},
      },
    },
    {
      title: t('主体'),
      dataIndex: 'actorType',
      valueType: 'select',
      valueEnum: {
        human: t('人'),
        agent: 'AI',
        mcp_client: 'MCP',
        system: t('系统任务'),
      },
      render: (_, row) => (
        <DictTag type="sys_actor_type" value={row.actorType} />
      ),
    },
    { title: t('操作人'), dataIndex: 'operName', width: 120 },
    {
      title: t('来源'),
      dataIndex: 'source',
      valueType: 'select',
      valueEnum: { http: t('页面'), ai: 'AI', mcp: 'MCP', task: t('任务') },
      render: (_, row) => <DictTag type="sys_call_source" value={row.source} />,
    },
    { title: t('原始意图'), dataIndex: 'intent', ellipsis: true, width: 250 },
    { title: t('功能'), dataIndex: 'capability', ellipsis: true, width: 190 },
    {
      title: t('风险'),
      dataIndex: 'risk',
      valueType: 'select',
      valueEnum: {
        low: t('低风险'),
        medium: t('中风险'),
        high: t('高风险'),
        critical: t('极高风险'),
      },
      render: (_, row) => <DictTag type="sys_risk_level" value={row.risk} />,
    },
    {
      title: t('结果'),
      dataIndex: 'status',
      valueType: 'select',
      valueEnum: {
        '0': { text: t('成功'), status: 'Success' },
        '1': { text: t('失败'), status: 'Error' },
      },
    },
    {
      title: t('已回滚'),
      dataIndex: 'rolledBack',
      valueType: 'select',
      valueEnum: { true: t('是'), false: t('否') },
      hideInTable: true,
    },
    { title: 'Plan ID', dataIndex: 'planId', hideInTable: true },
    { title: t('变更集 ID'), dataIndex: 'changesetId', hideInTable: true },
    {
      title: t('耗时'),
      dataIndex: 'costTime',
      search: false,
      renderText: (value: number) => `${value} ms`,
    },
    {
      title: t('链条与详情'),
      valueType: 'option',
      width: 230,
      render: (_, row) => (
        <Space wrap>
          <Button type="link" onClick={() => setDetail(row)}>
            {t('详情')}
          </Button>
          {row.planId && (
            <Auth perm="ai:agent:use">
              <Button
                type="link"
                onClick={() =>
                  history.push(
                    `/ai/sessions?planId=${encodeURIComponent(row.planId ?? '')}`,
                  )
                }
              >
                {t('计划')}
              </Button>
            </Auth>
          )}
          {row.changesetId && (
            <Auth perm="system:changeset:query">
              <Button
                type="link"
                onClick={() =>
                  history.push(
                    `/changesets?id=${encodeURIComponent(row.changesetId ?? '')}`,
                  )
                }
              >
                {t('变更与回滚')}
              </Button>
            </Auth>
          )}
        </Space>
      ),
    },
  ];
  return (
    <PageContainer
      title={t('行为溯源')}
      content={t(
        '从原始意图追踪到计划、审批、实际修改与回滚，区分页面操作、AI、MCP 和系统任务。',
      )}
    >
      <Alert
        type="info"
        showIcon
        title={t(
          '审计参数与结果已在后端脱敏；凭证和密码不会作为审计详情返回。',
        )}
        className="mb-4"
      />
      <ProTable<AuditRow>
        key={JSON.stringify(query)}
        rowKey="operId"
        actionRef={actionRef}
        params={query}
        form={{
          initialValues: {
            ...query,
            ...(query.beginTime && query.endTime
              ? { dateRange: [dayjs(query.beginTime), dayjs(query.endTime)] }
              : {}),
          },
        }}
        onSubmit={(values) => {
          const params = new URLSearchParams();
          for (const [key, value] of Object.entries(values)) {
            if (
              key === 'dateRange' &&
              Array.isArray(value) &&
              value.length === 2
            ) {
              params.set('beginTime', new Date(String(value[0])).toISOString());
              params.set('endTime', new Date(String(value[1])).toISOString());
            } else if (value !== undefined && value !== null && value !== '')
              params.set(key, String(value));
          }
          history.replace(
            `${location.pathname}${params.size ? `?${params}` : ''}`,
          );
        }}
        onReset={() => history.replace(location.pathname)}
        columns={columns}
        scroll={{ x: 1800 }}
        search={{ labelWidth: 'auto' }}
        pagination={{ pageSize: 20, showSizeChanger: true }}
        request={async (params) => {
          const result = await api.get<PageData<AuditRow>>(
            '/system/audit/list',
            {
              ...query,
              ...params,
              pageNum: params.current,
              pageSize: params.pageSize,
            },
          );
          return { data: result.rows, total: result.total, success: true };
        }}
      />
      <Drawer
        title={t('行为溯源详情')}
        open={active && !!detail}
        width={950}
        onClose={() => setDetail(undefined)}
      >
        {detail && (
          <>
            <Descriptions
              bordered
              column={2}
              size="small"
              items={[
                {
                  key: 'intent',
                  label: t('原始意图'),
                  children: detail.intent || t('页面操作'),
                  span: 2,
                },
                {
                  key: 'actor',
                  label: t('主体'),
                  children: (
                    <Space>
                      <DictTag type="sys_actor_type" value={detail.actorType} />
                      {detail.operName}
                    </Space>
                  ),
                },
                {
                  key: 'source',
                  label: t('来源'),
                  children: (
                    <DictTag type="sys_call_source" value={detail.source} />
                  ),
                },
                {
                  key: 'capability',
                  label: t('功能'),
                  children: detail.capability,
                  span: 2,
                },
                {
                  key: 'risk',
                  label: t('风险'),
                  children: (
                    <DictTag type="sys_risk_level" value={detail.risk} />
                  ),
                },
                {
                  key: 'approval',
                  label: t('审批'),
                  children: (
                    <Space>
                      <DictTag
                        type="sys_approval_mode"
                        value={detail.approvalMode}
                      />
                      {detail.approvedBy}
                    </Space>
                  ),
                },
                {
                  key: 'time',
                  label: t('时间'),
                  children: new Date(detail.operTime).toLocaleString(
                    getLocale(),
                  ),
                },
                {
                  key: 'cost',
                  label: t('耗时'),
                  children: `${detail.costTime} ms`,
                },
                { key: 'ip', label: 'IP', children: detail.operIp || '—' },
                {
                  key: 'token',
                  label: t('个人令牌编号'),
                  children: detail.tokenId || '—',
                },
                {
                  key: 'status',
                  label: t('状态'),
                  children: (
                    <Tag color={detail.status === '0' ? 'green' : 'red'}>
                      {detail.status === '0' ? t('成功') : t('失败')}
                    </Tag>
                  ),
                },
                {
                  key: 'error',
                  label: t('失败原因'),
                  children: detail.errorMsg || '—',
                },
              ]}
            />
            <Space wrap className="my-4">
              {detail.planId && (
                <Auth perm="ai:agent:use">
                  <Button
                    onClick={() =>
                      history.push(
                        `/ai/sessions?planId=${encodeURIComponent(detail.planId ?? '')}`,
                      )
                    }
                  >
                    {t('查看计划 #')}
                    {detail.planId}
                  </Button>
                </Auth>
              )}
              {detail.changesetId && (
                <Auth perm="system:changeset:query">
                  <Button
                    onClick={() =>
                      history.push(
                        `/changesets?id=${encodeURIComponent(detail.changesetId ?? '')}`,
                      )
                    }
                  >
                    {t('查看变更集与回滚')}
                  </Button>
                </Auth>
              )}
            </Space>
            <Typography.Title level={5}>{t('入参')}</Typography.Title>
            <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-all rounded bg-slate-50 p-3">
              {pretty(detail.operParam)}
            </pre>
            <Typography.Title level={5}>{t('响应摘要')}</Typography.Title>
            <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-all rounded bg-slate-50 p-3">
              {pretty(detail.jsonResult)}
            </pre>
          </>
        )}
      </Drawer>
    </PageContainer>
  );
}
