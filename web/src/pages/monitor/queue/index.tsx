import { DetailDialog } from '@/components/common/DetailDialog';
import {
  CheckCircleOutlined,
  ClockCircleOutlined,
  CloudServerOutlined,
  DeleteOutlined,
  InboxOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  StopOutlined,
  SyncOutlined,
} from '@ant-design/icons';
import {
  type ActionType,
  PageContainer,
  ProCard,
  type ProColumns,
  ProTable,
} from '@ant-design/pro-components';
import {
  Alert,
  App,
  Badge,
  Button,
  Descriptions,
  Empty,
  Select,
  Space,
  Statistic,
  Switch,
  Tag,
  Tooltip,
  Typography,
  theme,
} from 'antd';
import dayjs from 'dayjs';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useAdminAction } from '@/components/admin/useAdminAction';
import { Auth } from '@/components/auth/Auth';
import { usePageActive, usePageLocation } from '@/contexts/ActivePageContext';
import { usePerm } from '@/hooks/usePerm';
import { t } from '@/i18n';
import { usePageContext } from '@/stores/pageContext';
import { api, type PageData } from '@/utils/request';

type State =
  | 'pending'
  | 'active'
  | 'scheduled'
  | 'retry'
  | 'archived'
  | 'completed';
interface QueueInfo {
  queue: string;
  exists: boolean;
  pending: number;
  active: number;
  scheduled: number;
  retry: number;
  archived: number;
  completed: number;
  size: number;
  processed: number;
  failed: number;
  paused: boolean;
  latencySeconds: number;
}
interface Summary {
  queues: QueueInfo[];
  pending: number;
  active: number;
  scheduled: number;
  retry: number;
  archived: number;
  completed: number;
  processed: number;
  failed: number;
  asOf: string;
}
interface Task {
  taskId: string;
  queue: string;
  type: string;
  state: State;
  payload?: unknown;
  result?: unknown;
  lastError?: string;
  maxRetry: number;
  retried: number;
  nextProcessAt: string;
  lastFailedAt: string;
  completedAt: string;
  timeoutSeconds: number;
  retentionSeconds: number;
  internal: boolean;
  managed: boolean;
  canRetry: boolean;
  canArchive: boolean;
  canCancel: boolean;
  canDelete: boolean;
}
const states: { key: State; label: string; color: string }[] = [
  { key: 'pending', label: t('待处理'), color: 'blue' },
  { key: 'active', label: t('运行中'), color: 'processing' },
  { key: 'scheduled', label: t('已调度'), color: 'cyan' },
  { key: 'retry', label: t('等待重试'), color: 'orange' },
  { key: 'archived', label: t('已归档'), color: 'default' },
  { key: 'completed', label: t('已完成'), color: 'success' },
];
const queueLabels: Record<string, string> = {
  critical: t('高优先级'),
  default: t('标准队列'),
  low: t('低优先级'),
};
const actionLabels = {
  retry: t('立即重试'),
  archive: t('归档任务'),
  cancel: t('取消运行'),
  delete: t('永久删除'),
};
const formatTime = (value?: string) =>
  value && !value.startsWith('0001-')
    ? dayjs(value).format('YYYY-MM-DD HH:mm:ss')
    : '—';
const taskLabel = (type: string) =>
  ({
    'intent-admin:job': t('定时任务'),
    'intent-admin:export': t('异步导出'),
    'intent-admin:maintenance': t('系统维护'),
  })[type] ?? type;

export default function Queue() {
  const { token } = theme.useToken();
  const { message } = App.useApp();
  const active = usePageActive();
  const location = usePageLocation();
  const query = new URLSearchParams(
    location?.search ?? globalThis.location?.search ?? '',
  );
  const [queue, setQueue] = useState(
    ['critical', 'default', 'low'].includes(query.get('queue') ?? '')
      ? (query.get('queue') ?? 'default')
      : 'default',
  );
  const [state, setState] = useState<State>(
    states.some((x) => x.key === query.get('state'))
      ? (query.get('state') as State)
      : 'pending',
  );
  const [summary, setSummary] = useState<Summary>();
  const [busy, setBusy] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(false);
  const [detail, setDetail] = useState<Task>();
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailOpen, setDetailOpen] = useState(false);
  const actionRef = useRef<ActionType>(undefined);
  const canQuery = usePerm('monitor:queue:query');
  const loadSummary = useCallback(async () => {
    setBusy(true);
    try {
      setSummary(await api.get<Summary>('/monitor/queue/summary'));
    } finally {
      setBusy(false);
    }
  }, []);
  const refresh = useCallback(() => {
    void loadSummary().catch(() => {});
    void actionRef.current?.reload();
  }, [loadSummary]);
  const action = useAdminAction(() => {
    setDetailOpen(false);
    refresh();
  });
  useEffect(() => {
    if (active) void loadSummary().catch(() => {});
  }, [active, loadSummary]);
  useEffect(() => {
    if (!active || !autoRefresh) return;
    const timer = setInterval(refresh, 10000);
    return () => clearInterval(timer);
  }, [active, autoRefresh, refresh]);
  usePageContext({
    capability: 'monitor.queue.tasks',
    filters: { queue, state },
  });
  const current = summary?.queues.find((x) => x.queue === queue);
  async function showDetail(row: Task) {
    setDetailOpen(true);
    setDetailLoading(true);
    setDetail(undefined);
    try {
      setDetail(
        await api.get<Task>('/monitor/queue/task', {
          queue: row.queue,
          taskId: row.taskId,
        }),
      );
    } catch {
      setDetailOpen(false);
    } finally {
      setDetailLoading(false);
    }
  }
  function run(kind: keyof typeof actionLabels, row: Task) {
    return action.run({
      title: t(actionLabels[kind]),
      url: `/monitor/queue/${kind}`,
      method: kind === 'delete' ? 'DELETE' : 'POST',
      previewUrl: `/monitor/queue/${kind}/preview`,
      args: { queue: row.queue, taskId: row.taskId, expectedState: row.state },
      result: (result) => {
        if (result?.status === 'cancellation_requested')
          message.info(t('取消请求已发送，请刷新确认最终状态。'));
      },
    });
  }
  function queueAction() {
    const kind = current?.paused ? 'resume' : 'pause';
    return action.run({
      title: t(current?.paused ? '恢复队列' : '暂停队列'),
      url: `/monitor/queue/${kind}`,
      method: 'POST',
      previewUrl: `/monitor/queue/${kind}/preview`,
      args: { queue },
    });
  }
  const actions = (row: Task) => (
    <Space size={2} wrap>
      {canQuery && (
        <Button type="link" size="small" onClick={() => void showDetail(row)}>
          {t('详情')}
        </Button>
      )}
      {row.canRetry && (
        <Auth perm="monitor:queue:retry">
          <Button
            type="link"
            size="small"
            icon={<ReloadOutlined />}
            onClick={() => void run('retry', row)}
          >
            {t('重试')}
          </Button>
        </Auth>
      )}
      {row.canArchive && (
        <Auth perm="monitor:queue:archive">
          <Button
            type="link"
            size="small"
            onClick={() => void run('archive', row)}
          >
            {t('归档')}
          </Button>
        </Auth>
      )}
      {row.canCancel && (
        <Auth perm="monitor:queue:cancel">
          <Button
            type="link"
            size="small"
            danger
            onClick={() => void run('cancel', row)}
          >
            {t('取消运行')}
          </Button>
        </Auth>
      )}
      {row.canDelete && (
        <Auth perm="monitor:queue:remove">
          <Tooltip title={t('永久删除任务与结果')}>
            <Button
              type="text"
              size="small"
              danger
              aria-label={t('永久删除')}
              icon={<DeleteOutlined />}
              onClick={() => void run('delete', row)}
            />
          </Tooltip>
        </Auth>
      )}
    </Space>
  );
  const columns: ProColumns<Task>[] = [
    {
      title: t('任务'),
      key: 'task',
      render: (_, row) => (
        <Space direction="vertical" size={2}>
          <Space>
            <Typography.Text strong>{taskLabel(row.type)}</Typography.Text>
            {row.internal && (
              <Tag icon={<SafetyCertificateOutlined />}>{t('内部任务')}</Tag>
            )}
          </Space>
          <Typography.Text
            type="secondary"
            copyable={{ text: row.taskId }}
            style={{ fontFamily: 'monospace', fontSize: 12 }}
          >
            {row.taskId}
          </Typography.Text>
        </Space>
      ),
    },
    {
      title: t('状态'),
      dataIndex: 'state',
      width: 120,
      render: (_, row) => (
        <Tag color={states.find((x) => x.key === row.state)?.color}>
          {t(states.find((x) => x.key === row.state)?.label ?? row.state)}
        </Tag>
      ),
    },
    {
      title: t('重试次数'),
      key: 'retried',
      width: 110,
      render: (_, row) => (
        <Typography.Text>
          {row.retried} / {row.maxRetry}
        </Typography.Text>
      ),
    },
    {
      title: t('执行时间'),
      key: 'time',
      width: 190,
      render: (_, row) =>
        formatTime(
          row.completedAt.startsWith('0001-')
            ? row.nextProcessAt
            : row.completedAt,
        ),
    },
    {
      title: t('最近失败'),
      key: 'error',
      width: 200,
      ellipsis: true,
      render: (_, row) =>
        row.lastError ? (
          <Tooltip title={t(row.lastError)}>
            <Typography.Text type="danger">
              {t('执行失败，查看业务日志')}
            </Typography.Text>
          </Tooltip>
        ) : (
          '—'
        ),
    },
    {
      title: t('操作'),
      valueType: 'option',
      width: 235,
      render: (_, row) => actions(row),
    },
  ];
  const metrics = [
    {
      label: t('待处理'),
      value: summary?.pending,
      color: token.colorPrimary,
      icon: <InboxOutlined />,
    },
    {
      label: t('运行中'),
      value: summary?.active,
      color: token.colorInfo,
      icon: <SyncOutlined />,
    },
    {
      label: t('已调度'),
      value: summary?.scheduled,
      color: token.colorInfo,
      icon: <ClockCircleOutlined />,
    },
    {
      label: t('等待重试'),
      value: summary?.retry,
      color: token.colorWarning,
      icon: <ReloadOutlined />,
    },
    {
      label: t('已归档'),
      value: summary?.archived,
      color: token.colorTextSecondary,
      icon: <StopOutlined />,
    },
    {
      label: t('已完成'),
      value: summary?.completed,
      color: token.colorSuccess,
      icon: <CheckCircleOutlined />,
    },
  ];
  return (
    <PageContainer
      title={t('队列控制台')}
      content={t('实时观察任务流转，安全处理积压、失败与运行中的任务。')}
      extra={
        <Space>
          <Typography.Text type="secondary">
            {t('每 10 秒刷新')}
          </Typography.Text>
          <Switch
            checked={autoRefresh}
            onChange={setAutoRefresh}
            aria-label={t('自动刷新')}
          />
          <Button icon={<ReloadOutlined />} loading={busy} onClick={refresh}>
            {t('刷新')}
          </Button>
        </Space>
      }
    >
      <ProCard gutter={[16, 16]} wrap ghost style={{ marginBottom: 20 }}>
        {metrics.map((metric) => (
          <ProCard
            key={metric.label}
            colSpan={{ xs: 12, md: 8, xl: 4 }}
            style={{
              borderRadius: 12,
              border: `1px solid ${token.colorBorderSecondary}`,
            }}
          >
            <Statistic
              title={
                <Space>
                  <span style={{ color: metric.color }}>{metric.icon}</span>
                  {t(metric.label)}
                </Space>
              }
              value={metric.value ?? 0}
              valueStyle={{ fontWeight: 650 }}
            />
          </ProCard>
        ))}
      </ProCard>
      <ProCard
        style={{
          marginBottom: 20,
          borderRadius: 12,
          border: `1px solid ${token.colorBorderSecondary}`,
        }}
      >
        <Space wrap style={{ width: '100%', justifyContent: 'space-between' }}>
          <Space size="middle">
            <CloudServerOutlined
              style={{ fontSize: 24, color: token.colorPrimary }}
            />
            <div>
              <Typography.Text strong>
                {t('当前部署的任务队列')}
              </Typography.Text>
              <div>
                <Typography.Text type="secondary">
                  {t('今日处理 {count} 次，失败 {failed} 次', {
                    count: summary?.processed ?? 0,
                    failed: summary?.failed ?? 0,
                  })}
                </Typography.Text>
              </div>
            </div>
          </Space>
          <Space wrap>
            <Select
              aria-label={t('选择队列')}
              value={queue}
              onChange={setQueue}
              style={{ minWidth: 190 }}
              options={['critical', 'default', 'low'].map((name) => ({
                value: name,
                label: `${t(queueLabels[name])} · ${name}`,
              }))}
            />
            <Badge
              status={current?.paused ? 'warning' : 'processing'}
              text={t(current?.paused ? '已暂停' : '正在消费')}
            />
            <Auth perm="monitor:queue:pause">
              <Button
                icon={
                  current?.paused ? (
                    <PlayCircleOutlined />
                  ) : (
                    <PauseCircleOutlined />
                  )
                }
                onClick={() => void queueAction()}
                disabled={!current?.exists}
              >
                {t(current?.paused ? '恢复队列' : '暂停队列')}
              </Button>
            </Auth>
          </Space>
        </Space>
        <Typography.Paragraph
          type="secondary"
          style={{ marginTop: 16, marginBottom: 0 }}
        >
          {t('待处理延迟 {seconds} 秒 · 当前队列 {count} 个任务', {
            seconds: (current?.latencySeconds ?? 0).toFixed(1),
            count: current?.size ?? 0,
          })}
        </Typography.Paragraph>
      </ProCard>
      <ProTable<Task>
        actionRef={actionRef}
        rowKey="taskId"
        columns={columns}
        search={false}
        params={{ queue, state, active }}
        headerTitle={t('任务流转')}
        cardBordered
        scroll={{ x: 1000 }}
        request={async (params) => {
          if (!params.active) return { data: [], total: 0, success: true };
          const result = await api.get<PageData<Task>>('/monitor/queue/tasks', {
            queue: params.queue,
            state: params.state,
            pageNum: params.current,
            pageSize: params.pageSize,
          });
          return { data: result.rows, total: result.total, success: true };
        }}
        toolbar={{
          menu: {
            type: 'tab',
            activeKey: state,
            items: states.map((item) => ({
              key: item.key,
              label: (
                <Space size={5}>
                  {t(item.label)}
                  <Badge
                    count={current?.[item.key] ?? 0}
                    showZero
                    overflowCount={9999}
                    color={
                      state === item.key
                        ? token.colorPrimary
                        : token.colorTextQuaternary
                    }
                  />
                </Space>
              ),
            })),
            onChange: (key) => setState(key as State),
          },
        }}
        pagination={{
          hideOnSinglePage: true,
          defaultPageSize: 10,
          showSizeChanger: true,
          pageSizeOptions: [10, 20, 50, 100],
        }}
        locale={{
          emptyText: (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={t('此状态下暂无任务')}
            />
          ),
        }}
      />
      <Alert
        style={{ marginTop: 16 }}
        type="info"
        showIcon
        icon={<SafetyCertificateOutlined />}
        title={t('任务身份与参数受到保护')}
        description={t(
          '仅管理当前部署的队列。内部导出参数完全隐藏，任务重试会重新验证原用户权限；已完成的持久业务不会重复执行。',
        )}
      />
      <DetailDialog
        title={t('任务详情')}
        width={720}
        open={active && detailOpen}
        onClose={() => setDetailOpen(false)}
        loading={detailLoading}
        extra={detail && actions(detail)}
        destroyOnHidden
      >
        {detail && (
          <Space direction="vertical" size="large" style={{ width: '100%' }}>
            <Descriptions
              column={2}
              bordered
              size="small"
              items={[
                {
                  key: 'id',
                  label: t('任务编号'),
                  span: 2,
                  children: (
                    <Typography.Text copyable code>
                      {detail.taskId}
                    </Typography.Text>
                  ),
                },
                { key: 'queue', label: t('队列'), children: detail.queue },
                {
                  key: 'type',
                  label: t('处理器'),
                  children: taskLabel(detail.type),
                },
                {
                  key: 'state',
                  label: t('状态'),
                  children: t(
                    states.find((item) => item.key === detail.state)?.label ??
                      detail.state,
                  ),
                },
                {
                  key: 'retry',
                  label: t('重试次数'),
                  children: `${detail.retried} / ${detail.maxRetry}`,
                },
                {
                  key: 'next',
                  label: t('下次执行'),
                  children: formatTime(detail.nextProcessAt),
                },
                {
                  key: 'completed',
                  label: t('完成时间'),
                  children: formatTime(detail.completedAt),
                },
                {
                  key: 'failed',
                  label: t('失败时间'),
                  children: formatTime(detail.lastFailedAt),
                },
                {
                  key: 'timeout',
                  label: t('超时秒数'),
                  children: detail.timeoutSeconds,
                },
              ]}
            />
            {detail.lastError && (
              <Alert type="warning" showIcon title={t(detail.lastError)} />
            )}
            {detail.internal && (
              <Alert
                type="info"
                showIcon
                title={t('内部任务参数与身份信息已隐藏')}
              />
            )}
            {(
              [
                ['payload', t('脱敏参数')],
                ['result', t('脱敏结果')],
              ] as const
            ).map(([key, label]) => (
              <div key={key}>
                <Typography.Title level={5}>{label}</Typography.Title>
                <pre
                  style={{
                    maxHeight: 300,
                    overflow: 'auto',
                    whiteSpace: 'pre-wrap',
                    overflowWrap: 'anywhere',
                    background: token.colorFillAlter,
                    padding: 16,
                    borderRadius: 8,
                    margin: 0,
                  }}
                >
                  {typeof detail[key] === 'string'
                    ? t(detail[key] as string)
                    : JSON.stringify(detail[key] ?? null, null, 2)}
                </pre>
              </div>
            ))}
          </Space>
        )}
      </DetailDialog>
      {action.modal}
    </PageContainer>
  );
}
