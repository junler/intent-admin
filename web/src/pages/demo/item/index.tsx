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
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { useCallback, useEffect, useState } from 'react';
import { Auth } from '@/components/auth/Auth';

import { DiffTable } from '@/components/diff/DiffTable';
import ImportDialog from '@/components/import/ImportDialog';
import { usePerm } from '@/hooks/usePerm';
import { usePreviewAction } from '@/hooks/usePreviewAction';
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
                  itemId: row.itemId,
                  status: row.status === '0' ? '1' : '0',
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
              onClick={() => void remove.run({ itemIds: [row.itemId] })}
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
            onClick={() =>
              void remove.run({ itemIds: selected.map((r) => r.itemId) })
            }
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
          current: data.pageNum || page.pageNum,
          pageSize: data.pageSize || page.pageSize,
          total: data.total,
          showSizeChanger: true,
          showTotal: (total) => t('共 {value0} 条', { value0: total }),
          onChange: (pageNum, pageSize) => setPage({ pageNum, pageSize }),
        }}
      />

      <PreviewModal
        title={t('删除确认')}
        action={remove}
        okText={t('确认删除')}
        danger
      />
      <PreviewModal
        title={t('状态变更确认')}
        action={toggleStatus}
        okText={t('确认执行')}
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
      cancelText={t('取消')}
      okButtonProps={{ danger, loading: state.executing }}
      confirmLoading={state.executing}
      width={760}
      destroyOnHidden
    >
      {state.loading && (
        <Typography.Text type="secondary">{t('正在预演…')}</Typography.Text>
      )}
      {state.preview && <DiffTable preview={state.preview} />}
    </Modal>
  );
}
