import { usePageRefresh } from '@/hooks/usePageRefresh';
import {
  AppstoreOutlined,
  LoginOutlined,
  ReloadOutlined,
  TeamOutlined,
  UserOutlined,
} from '@ant-design/icons';
import { PageContainer } from '@ant-design/pro-components';
import {
  Alert,
  Badge,
  Button,
  Card,
  Col,
  Empty,
  Row as GridRow,
  Segmented,
  Space,
  Statistic,
  Table,
  Tabs,
  Tag,
  Typography,
} from 'antd';
import ReactECharts from 'echarts-for-react';
import { useEffect, useRef, useState } from 'react';
import type { Row } from '@/components/admin/types';
import { getLocale, t } from '@/i18n';
import { useUserStore } from '@/stores/user';
import { api } from '@/utils/request';

const palette = ['#2563eb', '#8b5cf6', '#06b6d4', '#f59e0b'];
const actorLabels: Record<string, string> = {
  human: t('人'),
  agent: 'AI',
  mcp_client: 'MCP',
  system: t('系统任务'),
};
const number = (value: unknown) =>
  value === undefined ? '—' : Number(value).toLocaleString(getLocale());
const time = (value: string | undefined) =>
  value ? new Date(value).toLocaleString(getLocale()) : '—';
const chartBase = {
  color: palette,
  textStyle: { fontFamily: 'inherit', color: '#66758c' },
  tooltip: {
    trigger: 'axis',
    backgroundColor: '#fff',
    borderColor: '#edf0f5',
    padding: [10, 14],
  },
  grid: { top: 36, right: 16, bottom: 32, left: 44, containLabel: false },
};
export default function Dashboard() {
  const identity = useUserStore((state) => state.identity);
  const [days, setDays] = useState(7);
  const [data, setData] = useState<Row>({});
  const [busy, setBusy] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [trend, setTrend] = useState('operations');
  const requestId = useRef(0);
  async function load(refresh = false) {
    const id = ++requestId.current;
    setBusy(true);
    try {
      const next = await api.get<Row>('/dashboard/overview', { days, refresh });
      if (requestId.current === id) {
        setData(next);
        setLoaded(true);
      }
    } finally {
      if (requestId.current === id) setBusy(false);
    }
  }
  useEffect(() => {
    void load().catch(() => {});
  }, [days]);
  usePageRefresh(() => load(true));
  const primary = [
    {
      title: t('用户总数'),
      value: data.userCount,
      icon: <UserOutlined />,
      tone: 'blue',
      detail: t('正常用户 {count}', { count: number(data.activeUsers) }),
    },
    {
      title: t('当前在线'),
      value: data.onlineCount ?? data.onlineUsers,
      icon: <TeamOutlined />,
      tone: 'cyan',
      detail: t('实时会话'),
    },
    {
      title: t('今日登录'),
      value: data.todayLogins,
      icon: <LoginOutlined />,
      tone: 'purple',
      detail: t('今日登录活动'),
    },
    {
      title: t('今日操作'),
      value: data.todayOperations,
      icon: <AppstoreOutlined />,
      tone: 'amber',
      detail: t('所有执行主体'),
    },
  ];
  const secondary = [
    [t('角色'), data.roleCount],
    [t('停用用户'), data.disabledUsers],
    [t('任务成功'), data.jobSuccess],
    [t('任务失败'), data.jobFailed],
    [
      t('队列积压'),
      typeof data.queueBacklog === 'object'
        ? Object.values(data.queueBacklog ?? {}).reduce<number>(
            (sum, value) => sum + Number(value),
            0,
          )
        : data.queueBacklog,
    ],
  ];
  const loginTrend: Row[] = data.loginTrend ?? [];
  const operations: Row[] = data.operationTrend ?? [];
  const actors: Row[] = data.byActor ?? [];
  const points = trend === 'operations' ? operations : loginTrend;
  const dateAxis = {
    type: 'category',
    boundaryGap: trend === 'operations',
    data: points.map((row) => row.date ?? row.day),
    axisLine: { lineStyle: { color: '#e6ebf2' } },
    axisTick: { show: false },
    axisLabel: {
      color: '#8390a2',
      formatter: (value: string) => value.slice(5),
    },
  };
  const valueAxis = {
    type: 'value',
    minInterval: 1,
    splitLine: { lineStyle: { color: '#edf1f7', type: 'dashed' } },
    axisLabel: { color: '#8390a2' },
  };
  const trendOption = {
    ...chartBase,
    xAxis: dateAxis,
    yAxis: valueAxis,
    legend: { top: 0, right: 0, icon: 'circle', itemWidth: 8, itemHeight: 8 },
    series:
      trend === 'operations'
        ? ['human', 'agent', 'mcp_client', 'system'].map((actorType) => ({
            name: t(actorLabels[actorType]),
            type: 'bar',
            barMaxWidth: 28,
            stack: 'operations',
            data: operations.map((row) => row[actorType] ?? 0),
            emphasis: { focus: 'series' },
            itemStyle: {
              borderRadius: actorType === 'system' ? [3, 3, 0, 0] : 0,
            },
          }))
        : [
            {
              name: t('登录次数'),
              type: 'line',
              smooth: true,
              showSymbol: loginTrend.length < 2,
              symbolSize: 7,
              lineStyle: { width: 3 },
              areaStyle: { color: '#2563eb', opacity: 0.07 },
              data: loginTrend.map((row) => row.count ?? row.total ?? 0),
            },
          ],
  };
  return (
    <PageContainer
      title={t('工作台')}
      subTitle={t('欢迎回来，{name}', {
        name: identity?.user.nickName || identity?.user.userName || 'Intent',
      })}
      className="intent-dashboard"
      extra={
        <Space wrap>
          <Segmented
            value={days}
            onChange={(value) => setDays(Number(value))}
            options={[
              { label: t('近 7 天'), value: 7 },
              { label: t('近 30 天'), value: 30 },
            ]}
          />
          <Button
            icon={<ReloadOutlined />}
            loading={busy}
            onClick={() => void load(true).catch(() => {})}
          >
            {t('刷新统计')}
          </Button>
        </Space>
      }
    >
      {data.degraded && (
        <Alert
          type="warning"
          showIcon
          title={t('部分依赖暂时不可用，显示已取得的数据。')}
          className="intent-dashboard-alert"
        />
      )}
      <GridRow gutter={[20, 20]}>
        {primary.map((metric) => (
          <Col xs={24} sm={12} xl={6} key={metric.title}>
            <Card className="intent-kpi-card" loading={busy && !loaded}>
              <div className="intent-kpi-top">
                <span>{metric.title}</span>
                <span
                  className={`intent-kpi-icon intent-kpi-icon-${metric.tone}`}
                >
                  {metric.icon}
                </span>
              </div>
              <Statistic value={metric.value ?? '—'} />
              <div className="intent-kpi-caption">{metric.detail}</div>
            </Card>
          </Col>
        ))}
        <Col span={24}>
          <Card className="intent-operational-card">
            <GridRow gutter={[0, 20]}>
              {secondary.map(([title, value]) => (
                <Col xs={12} sm={6} xl={3} key={String(title)}>
                  <div className="intent-operational-stat">
                    <span>{title}</span>
                    <strong>
                      {typeof value === 'number'
                        ? number(value)
                        : (value ?? '—')}
                    </strong>
                  </div>
                </Col>
              ))}
            </GridRow>
          </Card>
        </Col>
        <Col xs={24} xl={16}>
          <Card
            className="intent-chart-card"
            title={t('活动趋势')}
            extra={
              <Typography.Text type="secondary">
                {t('近 {days} 天', { days })}
              </Typography.Text>
            }
          >
            <Tabs
              activeKey={trend}
              onChange={setTrend}
              items={[
                { key: 'operations', label: t('管理操作') },
                { key: 'logins', label: t('账号登录') },
              ]}
            />
            {points.length ? (
              <ReactECharts
                option={trendOption}
                notMerge
                style={{ height: 290 }}
              />
            ) : (
              <div className="intent-chart-empty">
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description={t('所选时间段暂无活动')}
                />
              </div>
            )}
          </Card>
        </Col>
        <Col xs={24} xl={8}>
          <Card className="intent-chart-card" title={t('操作主体分布')}>
            {actors.some((row) => Number(row.count) > 0) ? (
              <ReactECharts
                style={{ height: 218 }}
                option={{
                  color: palette,
                  tooltip: { trigger: 'item', formatter: '{b}: {c} ({d}%)' },
                  title: {
                    text: number(
                      actors.reduce((sum, row) => sum + Number(row.count), 0),
                    ),
                    subtext: t('操作总量'),
                    left: 'center',
                    top: '36%',
                    textStyle: {
                      fontSize: 26,
                      fontWeight: 650,
                      color: '#172b4d',
                    },
                    subtextStyle: { fontSize: 12, color: '#8390a2' },
                  },
                  series: [
                    {
                      type: 'pie',
                      radius: ['64%', '82%'],
                      center: ['50%', '49%'],
                      label: { show: false },
                      itemStyle: {
                        borderColor: '#fff',
                        borderWidth: 4,
                        borderRadius: 5,
                      },
                      data: actors.map((row) => ({
                        name: t(actorLabels[row.actorType] ?? row.actorType),
                        value: row.count,
                      })),
                    },
                  ],
                }}
              />
            ) : (
              <div className="intent-actor-empty">
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description={t('暂无操作数据')}
                />
              </div>
            )}
            <div className="intent-actor-legend">
              {actors.map((row, index) => (
                <div key={row.actorType}>
                  <span>
                    <i
                      style={{ background: palette[index % palette.length] }}
                    />
                    {t(actorLabels[row.actorType] ?? row.actorType)}
                  </span>
                  <strong>{number(row.count)}</strong>
                  <Typography.Text type="secondary">
                    {t('失败率')}{' '}
                    {(Number(row.failureRate ?? 0) * 100).toFixed(1)}%
                  </Typography.Text>
                </div>
              ))}
            </div>
          </Card>
        </Col>
        <Col span={24}>
          <Card
            title={t('最近操作')}
            className="intent-recent-card"
            extra={
              data.cachedAt ? (
                <Typography.Text type="secondary">
                  {t('更新于 {time}', { time: time(data.cachedAt) })}
                </Typography.Text>
              ) : undefined
            }
          >
            <Table<Row>
              rowKey={(row) =>
                row.operId ?? `${row.capability}:${row.operTime}`
              }
              loading={busy && !loaded}
              scroll={{ x: 820 }}
              dataSource={data.recentOperations ?? []}
              pagination={false}
              locale={{
                emptyText: (
                  <Empty
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                    description={t('暂无操作记录')}
                  />
                ),
              }}
              columns={[
                {
                  title: t('操作名称'),
                  dataIndex: 'title',
                  width: 180,
                  render: (value) => (
                    <Typography.Text strong>
                      {value ? t(value) : '—'}
                    </Typography.Text>
                  ),
                },
                {
                  title: t('能力'),
                  dataIndex: 'capability',
                  ellipsis: true,
                  render: (value) => (
                    <Typography.Text type="secondary">
                      {value || '—'}
                    </Typography.Text>
                  ),
                },
                {
                  title: t('主体'),
                  dataIndex: 'actorType',
                  width: 110,
                  render: (value) => (
                    <Tag
                      bordered={false}
                      color={
                        value === 'human'
                          ? 'blue'
                          : value === 'agent'
                            ? 'purple'
                            : 'default'
                      }
                    >
                      {t(actorLabels[value] ?? value)}
                    </Tag>
                  ),
                },
                { title: t('账号'), dataIndex: 'operName', width: 120 },
                {
                  title: t('结果'),
                  dataIndex: 'status',
                  width: 120,
                  render: (value) => (
                    <Badge
                      status={String(value) === '0' ? 'success' : 'error'}
                      text={String(value) === '0' ? t('成功') : t('失败')}
                    />
                  ),
                },
                {
                  title: t('时间'),
                  dataIndex: 'operTime',
                  width: 190,
                  render: (value) => (
                    <Typography.Text type="secondary">
                      {time(value)}
                    </Typography.Text>
                  ),
                },
              ]}
            />
          </Card>
        </Col>
      </GridRow>
    </PageContainer>
  );
}
