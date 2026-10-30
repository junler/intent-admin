import { PageContainer } from '@ant-design/pro-components';
import {
  Button,
  Card,
  Descriptions,
  Input,
  Select,
  Space,
  Table,
  Typography,
} from 'antd';
import { useEffect, useState } from 'react';
import { InfoPanel } from '@/components/admin/InfoPanel';
import type { Row } from '@/components/admin/types';
import { useAdminAction } from '@/components/admin/useAdminAction';
import { Auth } from '@/components/auth/Auth';
import { t } from '@/i18n';
import { api } from '@/utils/request';
export default function Cache() {
  const [info, setInfo] = useState<Row>({});
  const [names, setNames] = useState<Row[]>([]);
  const [name, setName] = useState('');
  const [keys, setKeys] = useState<string[]>([]);
  const [filter, setFilter] = useState('');
  const [value, setValue] = useState<Row>();
  const action = useAdminAction(() => void loadKeys());
  async function load() {
    const [a, b] = await Promise.all([
      api.get<Row>('/monitor/cache/info'),
      api.get<(Row | string)[]>('/monitor/cache/names'),
    ]);
    setInfo(a);
    const normalized = b.map((item) =>
      typeof item === 'string' ? { cacheName: item } : item,
    );
    setNames(normalized);
    if (!name && normalized[0])
      setName(normalized[0].cacheName ?? normalized[0].name);
  }
  async function loadKeys() {
    if (!name) return;
    const result = await api.get<(string | Row)[]>('/monitor/cache/keys', {
      cacheName: name,
    });
    setKeys(
      result.map((item) =>
        typeof item === 'string' ? item : (item.cacheKey ?? item.key),
      ),
    );
  }
  useEffect(() => {
    void load();
  }, []);
  useEffect(() => {
    void loadKeys();
    setValue(undefined);
  }, [name]);
  async function show(key: string) {
    const result = await api.get<Row>('/monitor/cache/value', {
      cacheName: name,
      cacheKey: key,
    });
    setValue(
      typeof result === 'object' ? result : { cacheKey: key, value: result },
    );
  }
  function clear(kind: 'clearName' | 'clearKey' | 'clearAll', key?: string) {
    return action.run({
      title:
        kind === 'clearAll'
          ? t('清理全部受管理缓存')
          : kind === 'clearName'
            ? t('清理 {value0} 缓存', { value0: name })
            : t('删除缓存键'),
      url: `/monitor/cache/${kind}`,
      method: 'DELETE',
      args: { cacheName: name, cacheKey: key },
      previewUrl: `/monitor/cache/${kind}/preview`,
    });
  }
  return (
    <PageContainer title={t('缓存监控')}>
      <Space direction="vertical" className="w-full" size={20}>
        <InfoPanel title={t('Redis 概览')} value={info} />
        <Card
          title={t('缓存浏览')}
          extra={
            <Space>
              <Button onClick={() => void load()}>{t('刷新')}</Button>
              <Auth perm="monitor:cache:remove">
                <Button danger onClick={() => void clear('clearAll')}>
                  {t('清理全部受管理缓存')}
                </Button>
              </Auth>
            </Space>
          }
        >
          <Space wrap className="mb-4">
            <Select
              className="w-64"
              value={name || undefined}
              options={names.map((item) => ({
                label: item.remark
                  ? `${item.cacheName} · ${item.remark}`
                  : (item.cacheName ?? item.name),
                value: item.cacheName ?? item.name,
              }))}
              onChange={setName}
            />
            <Input.Search
              allowClear
              placeholder={t('筛选当前键')}
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
              onSearch={() => void loadKeys()}
            />
            <Auth perm="monitor:cache:remove">
              <Button
                danger
                disabled={!name}
                onClick={() => void clear('clearName')}
              >
                {t('清理当前前缀')}
              </Button>
            </Auth>
          </Space>
          <Typography.Paragraph type="secondary">
            {t('只显示服务端白名单内的缓存前缀，最多加载 500 个键。')}
          </Typography.Paragraph>
          <Table
            rowKey="key"
            size="small"
            dataSource={keys
              .filter((key) => key.includes(filter))
              .map((key) => ({ key }))}
            columns={[
              {
                title: t('缓存键'),
                dataIndex: 'key',
                render: (key: string) => (
                  <Button type="link" onClick={() => void show(key)}>
                    {key}
                  </Button>
                ),
              },
              {
                title: t('操作'),
                width: 100,
                render: (_: unknown, row: { key: string }) => (
                  <Auth perm="monitor:cache:remove">
                    <Button
                      danger
                      type="link"
                      onClick={() => void clear('clearKey', row.key)}
                    >
                      {t('删除')}
                    </Button>
                  </Auth>
                ),
              },
            ]}
            pagination={{ pageSize: 15 }}
          />
          {value && (
            <Descriptions
              title={t('缓存详情')}
              column={1}
              items={Object.entries(value).map(([key, item]) => ({
                key,
                label: key,
                children: (
                  <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all">
                    {typeof item === 'object'
                      ? JSON.stringify(item, null, 2)
                      : String(item ?? '')}
                  </pre>
                ),
              }))}
            />
          )}
        </Card>
        {action.modal}
      </Space>
    </PageContainer>
  );
}
