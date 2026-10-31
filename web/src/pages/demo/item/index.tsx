import { usePageRefresh } from '@/hooks/usePageRefresh';
import {
  DeleteOutlined,
  DownloadOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import {
  Button,
  Card,
  Form,
  Input,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { useCallback, useEffect, useState } from 'react';
import { Auth } from '@/components/auth/Auth';
import { AiFormScope } from '@/components/ai/AiFormContext';

import { useAdminAction } from '@/components/admin/useAdminAction';
import ImportDialog from '@/components/import/ImportDialog';
import { usePerm } from '@/hooks/usePerm';
import { useAiForm } from '@/hooks/useAiForm';
import { t } from '@/i18n';
import { usePageContext } from '@/stores/pageContext';
import { api, download, type PageData } from '@/utils/request';

interface DemoItem {
  itemId: number;
  name: string;
  category: string;
  amount: number;
  status: string;
  remark: string;
  createTime: string;
  updateTime: string;
}

interface Filters {
  name?: string;
  category?: string;
  status?: string;
}

const statusLabel: Record<string, { text: string; color: string }> = {
  '0': { text: t('正常'), color: 'green' },
  '1': { text: t('停用'), color: 'red' },
};

/**
 * Demo page: the end-to-end proof that the Capability layer works.
 *
 * It exercises everything Phase 0 delivers — a generated list endpoint, the
 * direct page mutations through HTTP capabilities, a generated /export
 * endpoint, and page-context reporting that makes the rows
 * AI-addressable. Real modules look exactly like this.
 */
export default function DemoItemPage() {
  const canRemove = usePerm('demo:item:remove');
  const [form] = Form.useForm<Filters>();
  const [filters, setFilters] = useState<Filters>({});
  useAiForm({
    id: 'demo.item.search',
    title: t('搜索'),
    fields: ['name', 'category', 'status'],
    fill: (values) => form.setFieldsValue(values),
  });
  const [page, setPage] = useState({ pageNum: 1, pageSize: 10 });
  const [data, setData] = useState<PageData<DemoItem>>({
    rows: [],
    total: 0,
    pageNum: 1,
    pageSize: 10,
  });
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState<DemoItem[]>([]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.get<PageData<DemoItem>>('/demo/item/list', {
        ...filters,
        ...page,
      });
      setData(res);
    } finally {
      setLoading(false);
    }
  }, [filters, page]);

  usePageRefresh(() => { setSelected([]); return load(); });
  useEffect(() => {
    void load();
  }, [load]);

  // One hook call makes this page's rows addressable by the Omnibox:
  // "停用选中的这几个" now resolves to actual ids.
  usePageContext({
    capability: 'demo.item.list',
    filters: { ...filters },
    selectedIds: selected.map((r) => r.itemId),
    selectedLabels: selected.map((r) => r.name),
    visibleIds: data.rows.map((r) => r.itemId),
  });

  const removeAction = useAdminAction(() => {
    setSelected([]);
    void load();
  });
  const remove = (itemIds: number[]) =>
    removeAction.run({
      title: t('删除'),
      url: `/demo/item/remove/${itemIds.join(',')}`,
      method: 'DELETE',
    });
  const toggleStatus = useAdminAction(() => void load());

  const columns: ColumnsType<DemoItem> = [
    { title: t('编号'), dataIndex: 'itemId', width: 80 },
    { title: t('名称'), dataIndex: 'name', width: 180 },
    { title: t('分类'), dataIndex: 'category', width: 120 },
    {
      title: t('金额'),
      dataIndex: 'amount',
      width: 120,
      align: 'right',
      render: (cents: number) => (cents / 100).toFixed(2),
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      width: 100,
      render: (s: string) => {
        const meta = statusLabel[s] ?? { text: s, color: 'default' };
        return <Tag color={meta.color}>{meta.text}</Tag>;
      },
    },
    { title: t('备注'), dataIndex: 'remark', ellipsis: true },
    {
      title: t('操作'),
      key: 'action',
      width: 160,
      fixed: 'right',
      render: (_, row) => (
        <Space size={4}>
          <Auth perm="demo:item:edit">
            <Button
              type="link"
              size="small"
              onClick={() =>
                void toggleStatus.run({
                  title: row.status === '0' ? t('停用') : t('启用'),
                  url: `/demo/item/update/${row.itemId}`,
                  method: 'PUT',
                  args: {
                    itemId: row.itemId,
                    status: row.status === '0' ? '1' : '0',
                  },
                })
              }
            >
              {row.status === '0' ? t('停用') : t('启用')}
            </Button>
          </Auth>
          <Auth perm="demo:item:remove">
            <Button
              type="link"
              size="small"
              danger
              onClick={() => void remove([row.itemId])}
            >
              {t('删除')}
            </Button>
          </Auth>
        </Space>
      ),
    },
  ];

  return (
    <Card
      title={t('示例条目')}
      extra={
        <Space>
          <ImportDialog
            base="/demo/item"
            permission="demo:item:edit"
            onSuccess={() => {
              setSelected([]);
              void load().catch(() => {});
            }}
          />
          <Auth perm="demo:item:export">
            <Button
              icon={<DownloadOutlined />}
              onClick={() => void download('/demo/item/list/export', filters)}
            >
              {t('导出')}
            </Button>
          </Auth>
          <Button icon={<ReloadOutlined />} onClick={() => void load()}>
            {t('刷新')}
          </Button>
        </Space>
      }
    >
      <AiFormScope
        value={{
          title: t('搜索'),
          mode: 'action',
          formId: 'ui.demo.item.search',
        }}
      >
        <Form
          form={form}
          layout="inline"
          className="mb-4"
          onFinish={(values) => {
            setFilters(values);
            setPage((p) => ({ ...p, pageNum: 1 }));
          }}
        >
          <Form.Item name="name" label={t('名称')}>
            <Input placeholder={t('模糊匹配')} allowClear className="w-40" />
          </Form.Item>
          <Form.Item name="category" label={t('分类')}>
            <Input placeholder={t('精确匹配')} allowClear className="w-32" />
          </Form.Item>
          <Form.Item name="status" label={t('状态')}>
            <Select
              allowClear
              placeholder={t('全部')}
              className="w-28"
              options={[
                { value: '0', label: t('正常') },
                { value: '1', label: t('停用') },
              ]}
            />
          </Form.Item>
          <Form.Item>
            <Space>
              <Button type="primary" htmlType="submit">
                {t('查询')}
              </Button>
              <Button
                onClick={() => {
                  form.resetFields();
                  setFilters({});
                  setPage((p) => ({ ...p, pageNum: 1 }));
                }}
              >
                {t('重置')}
              </Button>
            </Space>
          </Form.Item>
        </Form>
      </AiFormScope>

      {canRemove && selected.length > 0 && (
        <Space className="mb-3">
          <Typography.Text type="secondary">
            {t('已选 ')}
            {selected.length} {t(' 项')}
          </Typography.Text>
          <Button
            danger
            size="small"
            icon={<DeleteOutlined />}
            onClick={() => void remove(selected.map((r) => r.itemId))}
          >
            {t('批量删除')}
          </Button>
        </Space>
      )}

      <Table<DemoItem>
        rowKey="itemId"
        loading={loading}
        columns={columns}
        dataSource={data.rows}
        scroll={{ x: 1000 }}
        rowSelection={
          canRemove
            ? {
                selectedRowKeys: selected.map((r) => r.itemId),
                onChange: (_, rows) => setSelected(rows),
              }
            : undefined
        }
        pagination={{
          hideOnSinglePage: true,
          current: data.pageNum || page.pageNum,
          pageSize: data.pageSize || page.pageSize,
          total: data.total,
          showSizeChanger: true,
          showTotal: (total) => t('共 {value0} 条', { value0: total }),
          onChange: (pageNum, pageSize) => setPage({ pageNum, pageSize }),
        }}
      />
    </Card>
  );
}
