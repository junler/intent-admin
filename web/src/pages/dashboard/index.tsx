import { useEffect, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  Col,
  Radio,
  Row as GridRow,
  Space,
  Statistic,
  Table,
} from 'antd';
import ReactECharts from 'echarts-for-react';
import { api } from '@/utils/request';
import type { Row } from '@/components/admin/types';
const actorLabels: Record<string, string> = {
  human: '人',
  agent: 'AI',
  mcp_client: 'MCP',
  system: '系统任务',
};
export default function Dashboard() {
  const [days, setDays] = useState(7);
  const [data, setData] = useState<Row>({});
  const [busy, setBusy] = useState(false);
  async function load(refresh = false) {
    setBusy(true);
    try {
      setData(await api.get<Row>('/dashboard/overview', { days, refresh }));
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    void load();
  }, [days]);
  const metrics = [
    ['用户', data.userCount],
    ['角色', data.roleCount],
    ['正常用户', data.activeUsers],
    ['停用用户', data.disabledUsers],
    ['任务成功', data.jobSuccess],
    ['任务失败', data.jobFailed],
    ['在线', data.onlineCount ?? data.onlineUsers],
    ['今日登录', data.todayLogins],
    ['今日操作', data.todayOperations],
    ['变更集', data.changesetCount],
    [
      '回滚率',
      data.rollbackRate === undefined
        ? '—'
        : `${(Number(data.rollbackRate) * 100).toFixed(1)}%`,
    ],
    [
      '队列积压',
      typeof data.queueBacklog === 'object'
        ? Object.values(data.queueBacklog ?? {}).reduce<number>(
            (sum, value) => sum + Number(value),
            0,
          )
        : data.queueBacklog,
    ],
  ];
  const loginTrend = data.loginTrend ?? [];
  const operations = data.operationTrend ?? [];
  return (
    <Space direction="vertical" className="w-full">
      <Card
        title="运行概览"
        extra={
          <Space>
            <Radio.Group
              value={days}
              onChange={(event) => setDays(event.target.value)}
              options={[
                { label: '7 天', value: 7 },
                { label: '30 天', value: 30 },
              ]}
              optionType="button"
            />
            <Button loading={busy} onClick={() => void load(true)}>
              刷新统计
            </Button>
          </Space>
        }
      >
        {data.degraded && (
          <Alert
            type="warning"
            title="部分依赖暂时不可用，显示已取得的数据。"
            className="mb-4"
          />
        )}
        <GridRow gutter={[16, 16]}>
          {metrics.map(([title, value]) => (
            <Col xs={12} md={6} key={title}>
              <Card size="small">
                <Statistic title={title} value={value ?? '—'} />
              </Card>
            </Col>
          ))}
        </GridRow>
      </Card>
      <GridRow gutter={[16, 16]}>
        <Col xs={24} lg={12}>
          <Card title="登录趋势">
            <ReactECharts
              option={{
                tooltip: { trigger: 'axis' },
                xAxis: {
                  type: 'category',
                  data: loginTrend.map((r: Row) => r.date ?? r.day),
                },
                yAxis: { type: 'value' },
                series: [
                  {
                    type: 'line',
                    smooth: true,
                    data: loginTrend.map((r: Row) => r.count ?? r.total ?? 0),
                  },
                ],
              }}
            />
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card title="操作主体分布">
            <ReactECharts
              option={{
                tooltip: { trigger: 'item' },
                legend: { bottom: 0 },
                series: [
                  {
                    type: 'pie',
                    radius: ['40%', '70%'],
                    data: (data.byActor ?? []).map((r: Row) => ({
                      name: actorLabels[r.actorType] ?? r.actorType,
                      value: r.count,
                    })),
                  },
                ],
              }}
            />
          </Card>
        </Col>
      </GridRow>
      {operations.length > 0 && (
        <Card title="近期操作趋势">
          <ReactECharts
            option={{
              tooltip: { trigger: 'axis' },
              legend: {},
              xAxis: {
                type: 'category',
                data: operations.map((r: Row) => r.date ?? r.day),
              },
              yAxis: { type: 'value' },
              series: ['human', 'agent', 'mcp_client', 'system'].map(
                (actorType) => ({
                  name: actorLabels[actorType] ?? actorType,
                  type: 'bar',
                  stack: 'operations',
                  data: operations.map((r: Row) => r[actorType] ?? 0),
                }),
              ),
            }}
          />
        </Card>
      )}
      <Card title="主体操作与失败率">
        <Table<Row>
          size="small"
          rowKey="actorType"
          pagination={false}
          dataSource={data.byActor ?? []}
          columns={[
            {
              title: '主体',
              dataIndex: 'actorType',
              render: (value) => actorLabels[value] ?? value,
            },
            { title: '操作量', dataIndex: 'count' },
            { title: '失败次数', dataIndex: 'failed' },
            {
              title: '失败率',
              dataIndex: 'failureRate',
              render: (value) => `${(Number(value ?? 0) * 100).toFixed(1)}%`,
            },
          ]}
        />
      </Card>
      <Card title="最近操作">
        <Table<Row>
          rowKey={(r) => r.operId ?? `${r.capability}:${r.operTime}`}
          size="small"
          dataSource={data.recentOperations ?? []}
          columns={[
            { title: '功能', dataIndex: 'title' },
            { title: '能力', dataIndex: 'capability' },
            {
              title: '主体',
              dataIndex: 'actorType',
              render: (value) => actorLabels[value] ?? value,
            },
            { title: '账号', dataIndex: 'operName' },
            {
              title: '结果',
              dataIndex: 'status',
              render: (value) => (value === '0' ? '成功' : '失败'),
            },
            {
              title: '时间',
              dataIndex: 'operTime',
              render: (value) =>
                value ? new Date(value).toLocaleString() : '—',
            },
          ]}
          pagination={false}
        />
      </Card>
    </Space>
  );
}
