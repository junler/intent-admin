import { useCallback, useEffect, useState } from 'react';
import {
  Button,
  Card,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import {
  DeleteOutlined,
  DownloadOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';

import { DiffTable } from '@/components/diff/DiffTable';
import { Auth } from '@/components/auth/Auth';
import { usePerm } from '@/hooks/usePerm';
import { usePreviewAction } from '@/hooks/usePreviewAction';
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
  '0': { text: '正常', color: 'green' },
  '1': { text: '停用', color: 'red' },
};

/**
 * Demo page: the end-to-end proof that the Capability layer works.
 *
 * It exercises everything Phase 0 delivers — a generated list endpoint, the
 * generated /preview endpoint feeding the shared DiffTable, a generated
 * /export endpoint, and page-context reporting that makes the rows
 * AI-addressable. Real modules look exactly like this.
 */
export default function DemoItemPage() {
  const canRemove = usePerm('demo:item:remove');
  const [form] = Form.useForm<Filters>();
  const [filters, setFilters] = useState<Filters>({});
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

  const remove = usePreviewAction({
    capability: 'demo.item.remove',
    previewUrl: '/demo/item/remove/preview',
    execute: (args) => {
      const { itemIds } = args as { itemIds: number[] };
      return api.del(`/demo/item/remove/${itemIds.join(',')}`);
    },
    onDone: () => {
      setSelected([]);
      void load();
    },
  });

  const toggleStatus = usePreviewAction({
    capability: 'demo.item.update',
    previewUrl: '/demo/item/update/preview',
    execute: (args) => {
      const { itemId, status } = args as { itemId: number; status: string };
      return api.put(`/demo/item/update/${itemId}`, { itemId, status });
    },
    onDone: () => void load(),
  });

  const columns: ColumnsType<DemoItem> = [
    { title: '编号', dataIndex: 'itemId', width: 80 },
    { title: '名称', dataIndex: 'name', width: 180 },
    { title: '分类', dataIndex: 'category', width: 120 },
    {
      title: '金额',
      dataIndex: 'amount',
      width: 120,
      align: 'right',
      render: (cents: number) => (cents / 100).toFixed(2),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (s: string) => {
        const meta = statusLabel[s] ?? { text: s, color: 'default' };
        return <Tag color={meta.color}>{meta.text}</Tag>;
      },
    },
    { title: '备注', dataIndex: 'remark', ellipsis: true },
    {
      title: '操作',
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
                  itemId: row.itemId,
                  status: row.status === '0' ? '1' : '0',
                })
              }
            >
              {row.status === '0' ? '停用' : '启用'}
            </Button>
          </Auth>
          <Auth perm="demo:item:remove">
            <Button
              type="link"
              size="small"
              danger
              onClick={() => void remove.run({ itemIds: [row.itemId] })}
            >
              删除
            </Button>
          </Auth>
        </Space>
      ),
    },
  ];

  return (
    <Card
      title="示例条目"
      extra={
        <Space>
          <Auth perm="demo:item:export">
            <Button
              icon={<DownloadOutlined />}
              onClick={() => void download('/demo/item/list/export', filters)}
            >
              导出
            </Button>
          </Auth>
          <Button icon={<ReloadOutlined />} onClick={() => void load()}>
            刷新
          </Button>
        </Space>
      }
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
        <Form.Item name="name" label="名称">
          <Input placeholder="模糊匹配" allowClear className="w-40" />
        </Form.Item>
        <Form.Item name="category" label="分类">
          <Input placeholder="精确匹配" allowClear className="w-32" />
        </Form.Item>
        <Form.Item name="status" label="状态">
          <Select
            allowClear
            placeholder="全部"
            className="w-28"
            options={[
              { value: '0', label: '正常' },
              { value: '1', label: '停用' },
            ]}
          />
        </Form.Item>
        <Form.Item>
          <Space>
            <Button type="primary" htmlType="submit">
              查询
            </Button>
            <Button
              onClick={() => {
                form.resetFields();
                setFilters({});
                setPage((p) => ({ ...p, pageNum: 1 }));
              }}
            >
              重置
            </Button>
          </Space>
        </Form.Item>
      </Form>

      {canRemove && selected.length > 0 && (
        <Space className="mb-3">
          <Typography.Text type="secondary">
            已选 {selected.length} 项
          </Typography.Text>
          <Button
            danger
            size="small"
            icon={<DeleteOutlined />}
            onClick={() =>
              void remove.run({ itemIds: selected.map((r) => r.itemId) })
            }
          >
            批量删除
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
          current: data.pageNum || page.pageNum,
          pageSize: data.pageSize || page.pageSize,
          total: data.total,
          showSizeChanger: true,
          showTotal: (t) => `共 ${t} 条`,
          onChange: (pageNum, pageSize) => setPage({ pageNum, pageSize }),
        }}
      />

      <PreviewModal title="删除确认" action={remove} okText="确认删除" danger />
      <PreviewModal
        title="状态变更确认"
        action={toggleStatus}
        okText="确认执行"
      />
    </Card>
  );
}

function PreviewModal({
  title,
  action,
  okText,
  danger,
}: {
  title: string;
  action: ReturnType<typeof usePreviewAction>;
  okText: string;
  danger?: boolean;
}) {
  const { state, confirm, close } = action;
  return (
    <Modal
      title={title}
      open={state.open}
      onCancel={close}
      onOk={() => void confirm()}
      okText={okText}
      cancelText="取消"
      okButtonProps={{ danger, loading: state.executing }}
      confirmLoading={state.executing}
      width={760}
      destroyOnHidden
    >
      {state.loading && (
        <Typography.Text type="secondary">正在预演…</Typography.Text>
      )}
      {state.preview && <DiffTable preview={state.preview} />}
    </Modal>
  );
}
