import { usePageRefresh } from '@/hooks/usePageRefresh';
import {
  DeleteOutlined,
  DownloadOutlined,
  DownOutlined,
  PlusOutlined,
  ReloadOutlined,
  SearchOutlined,
} from '@ant-design/icons';
import { PageContainer, ProForm, ProTable } from '@ant-design/pro-components';
import { history } from '@umijs/max';
import {
  App,
  Button,
  Card,
  Empty,
  Form,
  Modal,
  Popover,
  Space,
  Tag,
  Typography,
} from 'antd';
import { useCallback, useEffect, useId, useRef, useState } from 'react';
import { Auth } from '@/components/auth/Auth';
import { AiFormMarker, AiFormHeading } from '@/components/ai/AiFormContext';
import ImportDialog from '@/components/import/ImportDialog';
import { usePageActive, usePageLocation } from '@/contexts/ActivePageContext';
import { getLocale, t } from '@/i18n';
import { useAiForm } from '@/hooks/useAiForm';
import { formDefaults, fillWithDefaults } from '@/components/ai/formDefaults';
import { useFormDraftStore } from '@/stores/formDraft';
import { useUserStore } from '@/stores/user';
import { hasPermission } from '@/utils/permission';
import { usePageContext } from '@/stores/pageContext';
import { api, download, type PageData } from '@/utils/request';
import {
  deletionArgs,
  editFormValues,
  fillableValues,
  matchesFormPath,
  rowsToTree,
  writeFormValues,
} from './actions';
import { allRows } from './data';
import { Fields } from './Fields';
import { listCapability, readCrudQuery, writeCrudQuery } from './query';
import type { CrudConfig, Field, Row, RowAction } from './types';
import { useAdminAction } from './useAdminAction';

/** Row actions shown inline before the rest collapse behind 更多. */
const INLINE_ACTIONS = 2;

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
  const editorId = `editor.${cfg.base}.${editing ? String(editing[cfg.idKey]) : 'create'}`;
  const draft = useFormDraftStore((state) => state.draft);
  const owner = useUserStore((state) => state.identity?.user.userId);
  const { message } = App.useApp();
  const [aiAction, setAiAction] = useState<{
    spec: RowAction;
    row: Row;
    values?: Row;
  }>();
  useEffect(() => {
    if (
      !active ||
      !draft ||
      draft.target.formId ||
      draft.target.route !== cfg.base
    )
      return;
    const pending = useFormDraftStore.getState().take(owner ?? 0, cfg.base);
    if (!pending) return;
    const permissions = useUserStore.getState().identity?.permissions ?? [];
    const { mode, recordId, path } = pending.target;
    if (editing !== undefined) {
      const sameForm =
        (mode === 'create' && editing === null) ||
        (mode === 'update' &&
          editing &&
          String(editing[cfg.idKey]) === recordId);
      const allowed = hasPermission(
        permissions,
        cfg.perm ? `${cfg.perm}:${mode === 'create' ? 'add' : 'edit'}` : '',
      );
      if (sameForm && allowed) {
        const values = fillableValues(cfg.fields, pending.values, !!editing);
        const combined = { ...form.getFieldsValue(), ...values };
        form.setFieldsValue({
          ...cfg.onValuesChange?.(values, combined),
          ...values,
        });
      } else message.info(t('请先关闭当前编辑表单，再让 AI 填写'));
      return;
    }
    void (async () => {
      if (mode === 'create') {
        if (
          cfg.canCreate === false ||
          !hasPermission(permissions, cfg.perm ? `${cfg.perm}:add` : '')
        )
          return;
        await edit(undefined, pending.values);
      } else if (mode === 'update') {
        if (
          cfg.canEdit === false ||
          !recordId ||
          !hasPermission(permissions, cfg.perm ? `${cfg.perm}:edit` : '')
        )
          return;
        const row = await api.get<Row>(
          `${cfg.base}/${encodeURIComponent(recordId)}`,
        );
        if (owner !== useUserStore.getState().identity?.user.userId) return;
        await edit(row, pending.values);
      } else {
        if (!recordId || !path) return;
        const row = await api.get<Row>(
          `${cfg.base}/${encodeURIComponent(recordId)}`,
        );
        if (owner !== useUserStore.getState().identity?.user.userId) return;
        const spec = cfg.actions?.find(
          (action) =>
            action.fields &&
            !action.readOnly &&
            hasPermission(permissions, action.perm ?? '') &&
            matchesFormPath(
              path,
              typeof action.url === 'function'
                ? action.url(row)
                : (action.url ?? ''),
            ),
        );
        if (spec) setAiAction({ spec, row, values: pending.values });
        else message.info(t('此操作没有可填写的表单'));
      }
    })().catch(() => {});
  }, [active, cfg.base, draft, owner, editing]);
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
  useAiForm({
    id: `search.${cfg.base}`,
    title: t('{title}：搜索表单', { title: cfg.title }),
    fields: cfg.fields
      .filter((field) => field.search)
      .map((field) => field.name),
    fill: (values) => search.setFieldsValue(values),
  });
  useAiForm({
    id: editorId,
    title: cfg.title,
    mode: editing ? 'update' : 'create',
    enabled: editing !== undefined,
    permission: cfg.perm ? `${cfg.perm}:${editing ? 'edit' : 'add'}` : '',
    fields: cfg.fields.filter((field) => !field.hidden && field.form !== false && !(editing && field.createOnly)).map((field) => field.name),
    defaults: formDefaults(cfg.fields, !!editing, cfg.title),
    required: cfg.fields.filter((field) => !field.hidden && field.form !== false && !(editing && field.createOnly) && (field.required || (!editing && field.createRequired))).map((field) => field.name),
    descriptions: Object.fromEntries(cfg.fields.filter((field) => field.help).map((field) => [field.name, field.help as string])),
    fill: (values, defaults) => {
      const patch = fillWithDefaults(form.getFieldsValue(), fillableValues(cfg.fields, defaults, !!editing), fillableValues(cfg.fields, values, !!editing));
      const combined = { ...form.getFieldsValue(), ...patch };
      form.setFieldsValue({ ...cfg.onValuesChange?.(patch, combined), ...patch });
    },
  });
  usePageRefresh(() => { setSelected([]); return load(); });
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
    form:
      editing === undefined
        ? undefined
        : {
            route: cfg.base,
            mode: editing ? 'update' : 'create',
            recordId: editing ? String(editing[cfg.idKey]) : undefined,
          },
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
  async function edit(row?: Row, prefill?: Row) {
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
    if (prefill) {
      const draftValues = fillableValues(cfg.fields, prefill, !!row);
      const combined = { ...values, ...draftValues };
      values = {
        ...combined,
        ...cfg.onValuesChange?.(draftValues, combined),
        ...draftValues,
      };
    }
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
        form.setFields([
          { name: invalid.name, errors: [t('请输入有效 JSON')] },
        ]);
      return;
    }
    if (cfg.prepare) values = cfg.prepare(values, !!editing);
    if (editing) values[cfg.idKey] = editing[cfg.idKey];
    const updating = !!editing;
    await action.run({
      title: updating
        ? t('修改{value0}', { value0: cfg.title })
        : t('新增{value0}', { value0: cfg.title }),
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
      title: t('删除 {value0} 条{value1}', {
        value0: ids.length,
        value1: cfg.title,
      }),
      url: `${cfg.base}/${ids.join(',')}`,
      method: 'DELETE',
      args: deletionArgs(cfg.idKey, ids),
      previewUrl: cfg.removePreviewUrl ?? `${cfg.base}/remove/preview`,
    });
  // Two row actions read fine inline; more than that becomes a wall of links.
  const collapseActions = (cfg.actions?.length ?? 0) > INLINE_ACTIONS;
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
          f.render
            ? f.render(row[f.name], row)
            : renderValue(
                f,
                cfg.base === '/monitor/operlog' &&
                  f.name === 'title' &&
                  typeof row[f.name] === 'string'
                  ? t(row[f.name])
                  : row[f.name],
              ),
      })),
    {
      title: t('操作'),
      key: 'actions',
      // Edit and delete stay inline; anything beyond INLINE_ACTIONS collapses
      // behind 更多, so the column keeps a predictable width instead of
      // growing a wall of blue links.
      width: collapseActions
        ? 190
        : Math.max(190, ((cfg.actions?.length ?? 0) + 2) * 70),
      fixed: 'right' as const,
      render: (_: unknown, row: Row) => (
        <Space wrap size={2}>
          {cfg.canEdit !== false && (
            <Auth perm={cfg.perm ? `${cfg.perm}:edit` : ''}>
              <Button size="small" type="link" onClick={() => void edit(row)}>
                {t('编辑')}
              </Button>
            </Auth>
          )}
          {collapseActions ? (
            <Popover
              trigger="click"
              placement="bottomRight"
              content={
                <div className="intent-row-actions">
                  {cfg.actions?.map((spec) => (
                    <ActionButton
                      key={spec.label}
                      spec={spec}
                      row={row}
                      reload={load}
                    />
                  ))}
                </div>
              }
            >
              <Button size="small" type="link">
                {t('更多')} <DownOutlined className="intent-row-more-caret" />
              </Button>
            </Popover>
          ) : (
            cfg.actions?.map((spec) => (
              <ActionButton
                key={spec.label}
                spec={spec}
                row={row}
                reload={load}
              />
            ))
          )}
          {cfg.canDelete !== false && (
            <Auth perm={cfg.perm ? `${cfg.perm}:remove` : ''}>
              <Button
                type="link"
                danger
                size="small"
                onClick={() => void remove([row[cfg.idKey]])}
              >
                {t('删除')}
              </Button>
            </Auth>
          )}
        </Space>
      ),
    },
  ];
  const toolbar = [
    <Space wrap key="actions" className="intent-table-actions">
      {cfg.extra}
      {cfg.import && (
        <ImportDialog
          base={cfg.base}
          permission={
            (typeof cfg.import === 'object' && cfg.import.permission) ||
            `${cfg.perm}:add`
          }
          onSuccess={() => {
            setSelected([]);
            void load().catch(() => {});
          }}
        />
      )}
      {cfg.export && (
        <Auth perm={cfg.perm ? `${cfg.perm}:export` : ''}>
          <Button
            icon={<DownloadOutlined />}
            onClick={() => void download(`${cfg.base}/list/export`, filters)}
          >
            {t('导出')}
          </Button>
        </Auth>
      )}
      {cfg.canCreate !== false && (
        <Auth perm={cfg.perm ? `${cfg.perm}:add` : ''}>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => void edit()}
          >
            {t('新增')}
          </Button>
        </Auth>
      )}
    </Space>,
  ];
  const searchFields = cfg.fields.filter((field) => field.search);
  return (
    <PageContainer title={cfg.title} className="intent-crud-page">
      {searchFields.length > 0 && (
        <Card className="intent-search-card">
          <Form
            name={`search-${cfg.idKey}`}
            initialValues={initialQuery.filters}
            form={search}
            layout="vertical"
            className="intent-search-form"
            onFinish={(values) => {
              changeQuery(values, { ...page, pageNum: 1 });
            }}
          >
            <Fields
              aiForm={{
                title: cfg.title,
                mode: 'query',
                formId: `ui.search.${cfg.base}`,
              }}
              fields={searchFields.map((field) => ({
                ...field,
                form: true,
                required: false,
                createRequired: false,
                rules: [],
              }))}
            />
            <Form.Item className="intent-search-buttons">
              <Space>
                <AiFormMarker
                  form={{
                    title: cfg.title,
                    mode: 'query',
                    formId: `ui.search.${cfg.base}`,
                  }}
                />
                <Button
                  type="primary"
                  htmlType="submit"
                  icon={<SearchOutlined />}
                >
                  {t('查询')}
                </Button>
                <Button
                  icon={<ReloadOutlined />}
                  onClick={() => {
                    search.resetFields();
                    changeQuery({}, { ...page, pageNum: 1 });
                  }}
                >
                  {t('重置')}
                </Button>
              </Space>
            </Form.Item>
          </Form>
        </Card>
      )}
      <div className="intent-table-card">
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
                {t('删除选中 (')}
                {selected.length})
              </Button>
            </Auth>
          )}
        <ProTable<Row>
          className="intent-data-table"
          search={false}
          headerTitle={
            <Space size={10}>
              <span>{t('数据列表')}</span>
              <Typography.Text type="secondary" className="intent-table-count">
                {t('共 {count} 条', { count: total })}
              </Typography.Text>
            </Space>
          }
          options={{
            reload: () => void load().catch(() => {}),
            density: true,
            fullScreen: true,
            setting: true,
          }}
          toolBarRender={() => toolbar}
          rowKey={cfg.idKey}
          loading={loading}
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={t('暂无数据，试试调整筛选条件')}
              />
            ),
          }}
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
                  hideOnSinglePage: true,
                  current: page.pageNum,
                  pageSize: page.pageSize,
                  total,
                  showSizeChanger: true,
                  showTotal: (total) => t('共 {value0} 条', { value0: total }),
                  onChange: (pageNum, pageSize) =>
                    changeQuery(filters, { pageNum, pageSize }),
                }
          }
        />
      </div>
      <Modal
        title={
          <AiFormHeading
            title={
              editing
                ? t('编辑{title}', { title: cfg.title })
                : t('新增{title}', { title: cfg.title })
            }
            form={{
              title: cfg.title,
              mode: editing ? 'update' : 'create',
              route: cfg.base,
              formId: `ui.${editorId}`,
              recordId: editing ? String(editing[cfg.idKey]) : undefined,
            }}
          />
        }
        className="intent-editor-modal"
        afterClose={() => form.resetFields()}
        open={active && editing !== undefined}
        onCancel={() => {
          if (!action.executing) setEditing(undefined);
        }}
        onOk={() => void save().catch(() => {})}
        width={760}
        okText={t('保存')}
        confirmLoading={action.executing}
        cancelButtonProps={{ disabled: action.executing }}
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
          <div className="intent-form-grid">
            <Fields
              fields={cfg.fields}
              editing={!!editing}
              aiForm={{
                title: cfg.title,
                mode: editing ? 'update' : 'create',
                route: cfg.base,
                formId: `ui.${editorId}`,
                recordId: editing ? String(editing[cfg.idKey]) : undefined,
              }}
            />
          </div>
          {cfg.editorExtra?.(form, editing ?? null)}
        </ProForm>
      </Modal>
      {aiAction && (
        <ActionButton
          spec={aiAction.spec}
          row={aiAction.row}
          reload={load}
          prefill={aiAction.values}
          onConsumed={() =>
            setAiAction((current) =>
              current ? { ...current, values: undefined } : current,
            )
          }
          onClosed={() => setAiAction(undefined)}
        />
      )}
      {action.modal}
    </PageContainer>
  );
}
function renderValue(field: Field, value: any) {
  if (value === undefined || value === null || value === '') return '—';
  if (
    typeof value === 'string' &&
    /(?:Time|Date|At)$/.test(field.name) &&
    !Number.isNaN(Date.parse(value))
  )
    return new Date(value).toLocaleString(getLocale());
  if (field.type === 'switch')
    return (
      <Tag color={value ? 'green' : 'default'}>{value ? t('是') : t('否')}</Tag>
    );
  const option = field.options?.find((o) => o.value === value);
  if (option && field.name === 'status')
    return (
      <Tag
        bordered={false}
        color={String(value) === '0' ? 'success' : 'default'}
      >
        {option.label}
      </Tag>
    );
  return option?.label ?? String(value);
}
function ActionButton({
  spec,
  row,
  reload,
  prefill,
  onConsumed,
  onClosed,
}: {
  spec: RowAction;
  row: Row;
  reload: () => Promise<void>;
  prefill?: Row;
  onConsumed?: () => void;
  onClosed?: () => void;
}) {
  const active = usePageActive();
  const formName = useId().replace(/:/g, '');
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [form] = Form.useForm();
  useAiForm({
    id: `action.${formName}`,
    title: spec.label,
    fields: (spec.fields ?? []).map((field) => field.name),
    permission: spec.perm,
    enabled: open && !spec.readOnly,
    defaults: formDefaults(spec.fields ?? [], false, spec.label),
    required: (spec.fields ?? []).filter((field) => field.required || field.createRequired).map((field) => field.name),
    descriptions: Object.fromEntries((spec.fields ?? []).filter((field) => field.help).map((field) => [field.name, field.help as string])),
    fill: (values, defaults) =>
      form.setFieldsValue(fillWithDefaults(form.getFieldsValue(), fillableValues(spec.fields ?? [], defaults, false), fillableValues(spec.fields ?? [], values, false))),
  });
  const previewing = useRef(false);
  const action = useAdminAction(
    () => void reload().catch(() => {}),
    () => {
      previewing.current = false;
      onClosed?.();
    },
  );
  async function execute(values: Row = {}) {
    previewing.current = true;
    setOpen(false);
    const executeAction = spec.execute;
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
      execute: executeAction ? () => executeAction(row, values) : undefined,
      result: spec.result,
    });
  }
  useEffect(() => {
    if (prefill) void click(prefill).catch(() => {});
  }, [prefill]);
  async function click(draftValues?: Row) {
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
      const defaults = spec.defaults ? await spec.defaults(row) : row;
      form.setFieldsValue({
        ...editFormValues(spec.fields ?? [], defaults),
        ...fillableValues(spec.fields ?? [], draftValues ?? {}, false),
      });
      setOpen(true);
      onConsumed?.();
    } finally {
      setBusy(false);
    }
  }
  return (
    <Auth perm={spec.perm ?? ''}>
      <Button
        style={onClosed ? { display: 'none' } : undefined}
        type="link"
        size="small"
        danger={spec.danger}
        loading={busy || action.executing}
        onClick={() => void click().catch(() => {})}
      >
        {spec.label}
      </Button>
      <Modal
        className="intent-editor-modal"
        title={
          <AiFormHeading
            title={spec.label}
            form={{
              title: spec.label,
              mode: 'action',
              formId: `ui.action.${formName}`,
            }}
          />
        }
        afterClose={() => {
          form.resetFields();
          if (!previewing.current) onClosed?.();
        }}
        okText={t('确定')}
        confirmLoading={action.executing}
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
          <Fields
            fields={spec.fields ?? []}
            aiForm={{
              title: spec.label,
              mode: 'action',
              formId: `ui.action.${formName}`,
            }}
          />
        </ProForm>
      </Modal>
      {action.modal}
    </Auth>
  );
}
