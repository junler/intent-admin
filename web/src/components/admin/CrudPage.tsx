import { ProForm, ProTable } from '@ant-design/pro-components';
import { history } from '@umijs/max';
import { usePageActive, usePageLocation } from '@/contexts/ActivePageContext';
import { usePageContext } from '@/stores/pageContext';
import { readCrudQuery, writeCrudQuery, listCapability } from './query';
import { useCallback, useEffect, useState, useId } from 'react';
import { Button, Card, Form, Modal, Space, Tag } from 'antd';
import {
  PlusOutlined,
  ReloadOutlined,
  DownloadOutlined,
  DeleteOutlined,
} from '@ant-design/icons';
import { Auth } from '@/components/auth/Auth';
import { api, download, type PageData } from '@/utils/request';
import { Fields } from './Fields';
import { allRows } from './data';
import {
  editFormValues,
  writeFormValues,
  deletionArgs,
  rowsToTree,
} from './actions';
import { useAdminAction } from './useAdminAction';
import type { CrudConfig, Field, Row, RowAction } from './types';

export function CrudPage(cfg: CrudConfig) {
  const active = usePageActive();
  const pageLocation = usePageLocation();
  const urlSearch =
    pageLocation?.search ??
    (typeof location === 'undefined' ? '' : location.search);
  const [syncedSearch, setSyncedSearch] = useState(urlSearch);
  const initialQuery = readCrudQuery(cfg.fields, urlSearch);
  const [form] = Form.useForm();
  const [search] = Form.useForm();
  const [rows, setRows] = useState<Row[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(initialQuery.page);
  const [filters, setFilters] = useState<Row>(initialQuery.filters);
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState<React.Key[]>([]);
  const [editing, setEditing] = useState<Row | null>();
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result = cfg.tree
        ? await allRows(cfg.base, { ...filters, ...cfg.params })
        : await api.get<PageData<Row> | Row[]>(`${cfg.base}/list`, {
            ...page,
            ...filters,
            ...cfg.params,
          });
      setRows(Array.isArray(result) ? result : result.rows);
      setTotal(Array.isArray(result) ? result.length : result.total);
    } finally {
      setLoading(false);
    }
  }, [cfg.base, page, filters, JSON.stringify(cfg.params)]);
  const action = useAdminAction(() => {
    setSelected([]);
    void load().catch(() => {});
  });
  useEffect(() => {
    if (!active || syncedSearch === urlSearch) return;
    const query = readCrudQuery(cfg.fields, urlSearch);
    setFilters(query.filters);
    setPage(query.page);
    search.setFieldsValue(
      Object.fromEntries(
        cfg.fields
          .filter((field) => field.search)
          .map((field) => [field.name, query.filters[field.name]]),
      ),
    );
    setSyncedSearch(urlSearch);
  }, [active, urlSearch, syncedSearch]);
  useEffect(() => {
    if (!active || syncedSearch !== urlSearch) return;
    void load().catch(() => {});
  }, [active, load, syncedSearch, urlSearch]);
  usePageContext({
    capability: listCapability(cfg.base),
    filters: { ...filters, ...cfg.params },
    selectedIds: selected.map((id) =>
      typeof id === 'number' ? id : String(id),
    ),
    visibleIds: rows.map((row) => row[cfg.idKey]),
    selectedLabels: selected.map((id) => {
      const row = rows.find((item) => item[cfg.idKey] === id);
      return row ? String(row[cfg.fields[0]?.name] ?? id) : String(id);
    }),
  });
  function changeQuery(
    nextFilters: Row,
    nextPage: { pageNum: number; pageSize: number },
  ) {
    setFilters(nextFilters);
    setPage(nextPage);
    const nextSearch = writeCrudQuery(
      cfg.fields,
      urlSearch,
      nextFilters,
      nextPage,
    );
    setSyncedSearch(nextSearch);
    search.setFieldsValue(
      Object.fromEntries(
        cfg.fields
          .filter((field) => field.search)
          .map((field) => [field.name, nextFilters[field.name]]),
      ),
    );
    if (active)
      history.replace({
        pathname: pageLocation?.pathname ?? location.pathname,
        search: nextSearch,
        hash: pageLocation?.hash ?? location.hash,
      });
  }
  async function edit(row?: Row) {
    let values = row
      ? { ...row }
      : Object.fromEntries(
          cfg.fields
            .filter((f) => f.defaultValue !== undefined)
            .map((f) => [f.name, f.defaultValue]),
        );
    if (row) {
      try {
        values = {
          ...values,
          ...(await api.get<Row>(`${cfg.base}/${row[cfg.idKey]}`)),
        };
      } catch {
        return;
      }
    }
    values = editFormValues(cfg.fields, values);
    form.resetFields();
    form.setFieldsValue(values);
    setEditing(row ?? null);
  }
  async function save() {
    const raw = await form.validateFields();
    let values: Row;
    try {
      values = writeFormValues(cfg.fields, raw);
    } catch {
      const invalid = cfg.fields.find(
        (field) => field.type === 'json' && raw[field.name],
      );
      if (invalid)
        form.setFields([{ name: invalid.name, errors: ['请输入有效 JSON'] }]);
      return;
    }
    if (cfg.prepare) values = cfg.prepare(values, !!editing);
    if (editing) values[cfg.idKey] = editing[cfg.idKey];
    const updating = !!editing;
    await action.run({
      title: updating ? `修改${cfg.title}` : `新增${cfg.title}`,
      url: updating ? (cfg.updateUrl ?? cfg.base) : (cfg.createUrl ?? cfg.base),
      method: updating ? 'PUT' : 'POST',
      args: values,
      previewUrl: `${cfg.base}/${updating ? 'update' : 'create'}/preview`,
      result: updating ? undefined : cfg.onCreateResult,
      after: () => setEditing(undefined),
    });
  }
  const remove = (ids: unknown[]) =>
    action.run({
      title: `删除 ${ids.length} 条${cfg.title}`,
      url: `${cfg.base}/${ids.join(',')}`,
      method: 'DELETE',
      args: deletionArgs(cfg.idKey, ids),
      previewUrl: cfg.removePreviewUrl ?? `${cfg.base}/remove/preview`,
    });
  const columns = [
    ...cfg.fields
      .filter(
        (f) =>
          f.column !== false &&
          !f.hidden &&
          f.type !== 'password' &&
          f.type !== 'json' &&
          f.type !== 'textarea',
      )
      .map((f) => ({
        title: f.label,
        dataIndex: f.name,
        key: f.name,
        width: f.type === 'number' ? 100 : 150,
        ellipsis: true,
        render: (_value: any, row: Row) =>
          f.render ? f.render(row[f.name], row) : renderValue(f, row[f.name]),
      })),
    {
      title: '操作',
      key: 'actions',
      width: Math.max(
        190,
        Math.min(540, ((cfg.actions?.length ?? 0) + 2) * 70),
      ),
      fixed: 'right' as const,
      render: (_: unknown, row: Row) => (
        <Space wrap size={2}>
          {cfg.canEdit !== false && (
            <Auth perm={cfg.perm ? `${cfg.perm}:edit` : ''}>
              <Button size="small" type="link" onClick={() => void edit(row)}>
                编辑
              </Button>
            </Auth>
          )}
          {cfg.actions?.map((spec) => (
            <ActionButton key={spec.label} spec={spec} row={row} reload={load} />
          ))}
          {cfg.canDelete !== false && (
            <Auth perm={cfg.perm ? `${cfg.perm}:remove` : ''}>
              <Button
                type="link"
                danger
                size="small"
                onClick={() => void remove([row[cfg.idKey]])}
              >
                删除
              </Button>
            </Auth>
          )}
        </Space>
      ),
    },
  ];
  return (
    <>
      <Card
        title={cfg.title}
        extra={
          <Space wrap>
            {cfg.extra}
            {cfg.export && (
              <Auth perm={cfg.perm ? `${cfg.perm}:export` : ''}>
                <Button
                  icon={<DownloadOutlined />}
                  onClick={() =>
                    void download(`${cfg.base}/list/export`, filters)
                  }
                >
                  导出
                </Button>
              </Auth>
            )}
            <Button
              icon={<ReloadOutlined />}
              onClick={() => void load().catch(() => {})}
            >
              刷新
            </Button>
            {cfg.canCreate !== false && (
              <Auth perm={cfg.perm ? `${cfg.perm}:add` : ''}>
                <Button
                  type="primary"
                  icon={<PlusOutlined />}
                  onClick={() => void edit()}
                >
                  新增
                </Button>
              </Auth>
            )}
          </Space>
        }
      >
        <Form
          name={`search-${cfg.idKey}`}
          initialValues={initialQuery.filters}
          form={search}
          layout="inline"
          className="mb-4"
          onFinish={(values) => {
            changeQuery(values, { ...page, pageNum: 1 });
          }}
        >
          <Fields
            fields={cfg.fields
              .filter((f) => f.search)
              .map((field) => ({
                ...field,
                form: true,
                required: false,
                createRequired: false,
                rules: [],
              }))}
          />
          <Form.Item>
            <Space>
              <Button type="primary" htmlType="submit">
                查询
              </Button>
              <Button
                onClick={() => {
                  search.resetFields();
                  changeQuery({}, { ...page, pageNum: 1 });
                }}
              >
                重置
              </Button>
            </Space>
          </Form.Item>
        </Form>
        {cfg.canDelete !== false &&
          cfg.batchDelete !== false &&
          selected.length > 0 && (
            <Auth perm={cfg.perm ? `${cfg.perm}:remove` : ''}>
              <Button
                danger
                className="mb-3"
                icon={<DeleteOutlined />}
                onClick={() => void remove(selected)}
              >
                删除选中 ({selected.length})
              </Button>
            </Auth>
          )}
        <ProTable<Row>
          search={false}
          options={{ density: true, fullScreen: true, setting: true }}
          toolBarRender={false}
          rowKey={cfg.idKey}
          loading={loading}
          columns={columns}
          dataSource={cfg.tree ? rowsToTree(rows, cfg.idKey) : rows}
          scroll={{ x: 'max-content' }}
          rowSelection={
            cfg.canDelete !== false && cfg.batchDelete !== false
              ? {
                  selectedRowKeys: selected,
                  onChange: (keys) => setSelected(keys),
                }
              : undefined
          }
          pagination={
            cfg.tree
              ? false
              : {
                  current: page.pageNum,
                  pageSize: page.pageSize,
                  total,
                  showSizeChanger: true,
                  showTotal: (t) => `共 ${t} 条`,
                  onChange: (pageNum, pageSize) =>
                    changeQuery(filters, { pageNum, pageSize }),
                }
          }
        />
      </Card>
      <Modal
        title={editing ? '编辑' : '新增'}
        open={active && editing !== undefined}
        onCancel={() => setEditing(undefined)}
        onOk={() => void save().catch(() => {})}
        width={700}
        destroyOnHidden
      >
        <ProForm
          name={`editor-${cfg.idKey}`}
          submitter={false}
          form={form}
          layout="vertical"
          onValuesChange={(changed, values) => {
            if (cfg.onValuesChange)
              form.setFieldsValue(cfg.onValuesChange(changed, values));
          }}
        >
          <Fields fields={cfg.fields} editing={!!editing} />
          {cfg.editorExtra?.(form, editing ?? null)}
        </ProForm>
      </Modal>
      {action.modal}
    </>
  );
}
function renderValue(field: Field, value: any) {
  if (value === undefined || value === null || value === '') return '—';
  if (
    typeof value === 'string' &&
    /(?:Time|Date|At)$/.test(field.name) &&
    !Number.isNaN(Date.parse(value))
  )
    return new Date(value).toLocaleString();
  if (field.type === 'switch')
    return <Tag color={value ? 'green' : 'default'}>{value ? '是' : '否'}</Tag>;
  const option = field.options?.find((o) => o.value === value);
  return option?.label ?? String(value);
}
function ActionButton({
  spec,
  row,
  reload,
}: {
  spec: RowAction;
  row: Row;
  reload: () => Promise<void>;
}) {
  const active = usePageActive();
  const formName = useId().replace(/:/g, '');
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [form] = Form.useForm();
  const action = useAdminAction(() => void reload().catch(() => {}));
  async function execute(values: Row = {}) {
    setOpen(false);
    const url =
      typeof spec.url === 'function' ? spec.url(row) : (spec.url ?? '');
    await action.run({
      title: spec.label,
      url,
      method: spec.method ?? 'PUT',
      args: spec.args ? spec.args(row, values) : { ...row, ...values },
      previewUrl:
        typeof spec.previewUrl === 'function'
          ? spec.previewUrl(row)
          : spec.previewUrl,
      execute: spec.execute ? () => spec.execute!(row, values) : undefined,
      result: spec.result,
    });
  }
  async function click() {
    if (spec.readOnly) {
      setBusy(true);
      try {
        const url =
          typeof spec.url === 'function' ? spec.url(row) : (spec.url ?? '');
        const args = spec.args ? spec.args(row, {}) : row;
        const value = await (spec.execute
          ? spec.execute(row, {})
          : api.post(url, args));
        spec.result?.(value);
        await reload();
      } finally {
        setBusy(false);
      }
      return;
    }
    if (!spec.fields) {
      await execute();
      return;
    }
    setBusy(true);
    try {
      form.resetFields();
      form.setFieldsValue(spec.defaults ? await spec.defaults(row) : row);
      setOpen(true);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Auth perm={spec.perm ?? ''}>
      <Button
        type="link"
        size="small"
        danger={spec.danger}
        loading={busy}
        onClick={() => void click().catch(() => {})}
      >
        {spec.label}
      </Button>
      <Modal
        title={spec.label}
        open={active && open}
        onCancel={() => setOpen(false)}
        onOk={() =>
          void form
            .validateFields()
            .then(execute)
            .catch(() => {})
        }
        destroyOnHidden
      >
        <ProForm
          name={`action-${formName}`}
          form={form}
          layout="vertical"
          submitter={false}
        >
          <Fields fields={spec.fields ?? []} />
        </ProForm>
      </Modal>
      {action.modal}
    </Auth>
  );
}
