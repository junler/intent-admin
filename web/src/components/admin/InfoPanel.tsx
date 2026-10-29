import { Card, Descriptions, Empty, Space, Tag, Typography } from 'antd';
import { getLocale, t } from '@/i18n';

const labels: Record<string, string> = {
  cpu: 'CPU',
  memory: t('内存'),
  system: t('操作系统'),
  disk: t('磁盘'),
  go: t('Go 运行时'),
  sampledAt: t('采样时间'),
  cores: t('核心数'),
  percent: t('使用率'),
  info: t('硬件详情'),
  total: t('总容量'),
  available: t('可用容量'),
  used: t('已用容量'),
  free: t('空闲容量'),
  usedPercent: t('使用率'),
  hostname: t('主机名'),
  os: t('操作系统'),
  platform: t('系统平台'),
  platformVersion: t('平台版本'),
  kernelVersion: t('内核版本'),
  kernelArch: t('内核架构'),
  uptime: t('运行时长（秒）'),
  bootTime: t('启动时间'),
  version: t('版本'),
  goroutines: t('协程数'),
  heapAlloc: t('堆内存分配'),
  heapSys: t('堆内存保留'),
  gcCount: t('GC 次数'),
  path: t('路径'),
  fstype: t('文件系统'),
  redisVersion: t('Redis 版本'),
  usedMemory: t('已用内存'),
  connectedClients: t('客户端连接'),
  totalCommandsProcessed: t('处理命令数'),
  uptimeInSeconds: t('运行时长（秒）'),
};
const byteFields = new Set([
  'total',
  'available',
  'used',
  'free',
  'heapAlloc',
  'heapSys',
]);
function display(value: unknown, key = ''): React.ReactNode {
  if (value === undefined || value === null || value === '')
    return <Typography.Text type="secondary">—</Typography.Text>;
  if (typeof value === 'boolean')
    return (
      <Tag bordered={false} color={value ? 'success' : 'default'}>
        {value ? t('是') : t('否')}
      </Tag>
    );
  if (typeof value === 'object')
    return (
      <details className="intent-info-details">
        <summary>{t('查看详情')}</summary>
        <pre className="intent-detail-value intent-detail-json">
          {JSON.stringify(value, null, 2)}
        </pre>
      </details>
    );
  if (typeof value === 'number') {
    if (byteFields.has(key)) {
      const index =
        value > 0
          ? Math.min(4, Math.floor(Math.log(value) / Math.log(1024)))
          : 0;
      return `${(value / 1024 ** index).toLocaleString(getLocale(), { maximumFractionDigits: 2 })} ${['B', 'KB', 'MB', 'GB', 'TB'][Math.max(0, index)]}`;
    }
    if (/percent/i.test(key)) return `${value.toFixed(1)}%`;
    return value.toLocaleString(getLocale());
  }
  if (
    /(?:At|Time)$/.test(key) &&
    typeof value === 'string' &&
    !Number.isNaN(Date.parse(value))
  )
    return new Date(value).toLocaleString(getLocale());
  return <span className="break-all">{String(value)}</span>;
}
export function InfoPanel({ title, value }: { title: string; value: unknown }) {
  return (
    <Card className="intent-info-panel" title={t(labels[title] ?? title)}>
      {value === null || value === undefined ? (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={t('暂无监控数据')}
        />
      ) : Array.isArray(value) ? (
        <Space wrap>
          {value.map((item, index) => (
            <div key={String(index)}>{display(item)}</div>
          ))}
        </Space>
      ) : typeof value === 'object' ? (
        <Descriptions
          layout="vertical"
          colon={false}
          column={{ xs: 1, sm: 2, lg: 3 }}
          items={Object.entries(value).map(([key, item]) => ({
            key,
            label: t(labels[key] ?? key),
            children: display(item, key),
          }))}
        />
      ) : (
        display(value, title)
      )}
    </Card>
  );
}
