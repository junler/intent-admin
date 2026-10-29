import {
  type ActionType,
  PageContainer,
  type ProColumns,
  ProTable,
} from '@ant-design/pro-components';
import { history, useLocation } from '@umijs/max';
import {
  Alert,
  App,
  Button,
  Descriptions,
  Drawer,
  Empty,
  Modal,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
} from 'antd';
import { useEffect, useRef, useState } from 'react';
import { Auth } from '@/components/auth/Auth';
import { DictTag } from '@/components/dict/DictTag';
import { DiffTable } from '@/components/diff/DiffTable';
import { usePageActive } from '@/contexts/ActivePageContext';
import { usePerm } from '@/hooks/usePerm';
import { getLocale, t } from '@/i18n';
import { ApiError, api, Code, type PageData } from '@/utils/request';
import {
  type Changeset,
  type ChangesetItem,
  changesetPreview,
  type RollbackPreview,
  selectableItem,
  updateRollbackSelection,
} from './data';

const setStatus = {
  applied: { text: t('已应用') },
  partial: { text: t('部分应用') },
  partially_rolled_back: { text: t('部分回滚') },
  rolled_back: { text: t('已回滚') },
  rollback_failed: { text: t('回滚有失败项') },
};
const itemStatuses: Record<string, string> = {
  applied: t('已应用'),
  failed: t('回滚失败'),
  rolled_back: t('已回滚'),
  skipped: t('已跳过'),
  ready: t('可回滚'),
  conflict: t('数据冲突'),
  blocked: t('同组受阻'),
  already_rolled_back: t('已回滚'),
};
const actorTypes = {
  human: t('人'),
  agent: 'AI',
  mcp_client: 'MCP',
  system: t('系统任务'),
};
const sources = { http: t('页面'), ai: 'AI', mcp: 'MCP', task: t('任务') };
const risks = {
  low: t('低风险'),
  medium: t('中风险'),
  high: t('高风险'),
  critical: t('极高风险'),
};

export default function ChangesetsPage() {
  const active = usePageActive();
  const location = useLocation();
  const actionRef = useRef<ActionType | undefined>(undefined);
  const [detail, setDetail] = useState<Changeset>();
  const [detailLoading, setDetailLoading] = useState(false);
  const [preview, setPreview] = useState<RollbackPreview>();
  const [rollbackTarget, setRollbackTarget] = useState<string>();
  const [rollbackBusy, setRollbackBusy] = useState(false);
  const [selectedItemIds, setSelectedItemIds] = useState<number[]>([]);
  const [rollbackItemIds, setRollbackItemIds] = useState<
    number[] | undefined
  >();
  const canQuery = usePerm('system:changeset:query');
  const canRollback = usePerm('system:changeset:rollback');
  const canView = canQuery || canRollback;
  const { message } = App.useApp();
  const query = Object.fromEntries(
    [...new URLSearchParams(location.search)].filter(([key]) =>
      ['actorType', 'source', 'status', 'intent', 'risk'].includes(key),
    ),
  );
  const selectedId =
    new URLSearchParams(location.search).get('id') ??
    new URLSearchParams(location.search).get('changesetId');

  useEffect(() => {
    if (!selectedId || !canView) {
      setDetail(undefined);
      return;
    }
    let active = true;
    setDetailLoading(true);
    void api
      .get<Changeset>(
        `/system/changeset/${encodeURIComponent(selectedId)}`,
        undefined,
        { dedupe: false },
      )
      .then((value) => {
        if (active) {
          setDetail(value);
          setSelectedItemIds([]);
        }
      })
      .catch(() => {
        if (active) setDetail(undefined);
      })
      .finally(() => {
        if (active) setDetailLoading(false);
      });
    return () => {
      active = false;
    };
  }, [selectedId, canView]);

  function view(id: string | undefined) {
    if (!id) return;
    const params = new URLSearchParams(location.search);
    params.set('id', id);
    params.delete('changesetId');
    history.push(`${location.pathname}?${params}`);
  }
  function closeDetail() {
    const params = new URLSearchParams(location.search);
    params.delete('id');
    params.delete('changesetId');
    history.replace(`${location.pathname}${params.size ? `?${params}` : ''}`);
    setDetail(undefined);
  }
  async function requestRollback(id: string, itemIds?: number[]) {
    setRollbackItemIds(itemIds);
    setRollbackTarget(id);
    setPreview(undefined);
    setRollbackBusy(true);
    try {
      setPreview(
        await api.post<RollbackPreview>(
          '/system/changeset/rollback/preview',
          { id, ...(itemIds ? { itemIds } : {}) },
          { dedupe: false },
        ),
      );
    } catch {
      setRollbackTarget(undefined);
    } finally {
      setRollbackBusy(false);
    }
  }
  async function rollback() {
    if (!rollbackTarget || !preview || rollbackBusy) return;
    setRollbackBusy(true);
    try {
      const result = await api.post<Changeset>(
        `/system/changeset/${encodeURIComponent(rollbackTarget)}/rollback`,
        {
          id: rollbackTarget,
          ...(rollbackItemIds ? { itemIds: rollbackItemIds } : {}),
          previewHash: preview.confirmationHash,
        },
        { dedupe: false },
      );
      setRollbackTarget(undefined);
      setPreview(undefined);
      setSelectedItemIds([]);
      actionRef.current?.reload();
      if (result.status === 'rollback_failed')
        message.warning(t('部分项目回滚失败，请查看详情中的失败原因'));
      else if (result.status === 'partially_rolled_back')
        message.success(t('所选条目已回滚，其余条目仍可继续回滚'));
      else message.success(t('回滚已完成，回滚本身已记录为新的变更集'));
      if (selectedId === rollbackTarget)
        setDetail(
          await api.get<Changeset>(
            `/system/changeset/${rollbackTarget}`,
            undefined,
            { dedupe: false },
          ),
        );
      if (result.rollbackChangesetId)
        message.info({
          content: (
            <Button
              type="link"
              onClick={() => view(result.rollbackChangesetId)}
            >
              {t('查看此次回滚产生的变更集')}
            </Button>
          ),
          duration: 8,
        });
    } catch (error) {
      if (error instanceof ApiError && error.code === Code.StaleData) {
        message.warning(t('数据已变化，请检查最新的回滚预览'));
        setPreview(undefined);
        setPreview(
          await api.post<RollbackPreview>(
            '/system/changeset/rollback/preview',
            {
              id: rollbackTarget,
              ...(rollbackItemIds ? { itemIds: rollbackItemIds } : {}),
            },
            { dedupe: false },
          ),
        );
      }
    } finally {
      setRollbackBusy(false);
    }
  }

  const columns: ProColumns<Changeset>[] = [
    {
      title: t('变更摘要'),
      dataIndex: 'summary',
      search: false,
      width: 260,
      render: (_, row) => (
        <>
          <Typography.Text>
            {row.summary
              ? row.planId
                ? row.summary
                : t(row.summary)
              : t('数据变更')}
          </Typography.Text>
          <br />
          <Typography.Text type="secondary" className="text-xs" copyable>
            {row.changesetId}
          </Typography.Text>
        </>
      ),
    },
    { title: t('原始意图'), dataIndex: 'intent', ellipsis: true },
    {
      title: t('主体'),
      dataIndex: 'actorType',
      valueType: 'select',
      valueEnum: actorTypes,
      render: (_, row) => (
        <Space>
          <DictTag type="sys_actor_type" value={row.actorType} />
          {row.actorName}
        </Space>
      ),
    },
    {
      title: t('来源'),
      dataIndex: 'source',
      valueType: 'select',
      valueEnum: sources,
      render: (_, row) => <DictTag type="sys_call_source" value={row.source} />,
    },
    {
      title: t('风险'),
      dataIndex: 'risk',
      valueType: 'select',
      valueEnum: risks,
      render: (_, row) => <DictTag type="sys_risk_level" value={row.risk} />,
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      valueType: 'select',
      valueEnum: setStatus,
    },
    { title: t('影响数'), dataIndex: 'affectedCount', search: false },
    {
      title: t('时间'),
      dataIndex: 'createTime',
      valueType: 'dateTime',
      search: false,
      width: 170,
    },
    {
      title: t('操作'),
      valueType: 'option',
      width: 180,
      render: (_, row) => (
        <Space wrap>
          {canView && (
            <Button type="link" onClick={() => view(row.changesetId)}>
              {t('详情')}
            </Button>
          )}
          <Auth perm="system:changeset:rollback">
            {row.status !== 'rolled_back' && (
              <Button
                type="link"
                onClick={() => void requestRollback(row.changesetId)}
              >
                {t('回滚全部剩余条目')}
              </Button>
            )}
          </Auth>
        </Space>
      ),
    },
  ];

  return (
    <PageContainer
      title={t('变更历史')}
      content={t(
        '每次修改都有记录。回滚仅还原本次涉及的字段；被后续操作改动的记录会明确提示冲突。',
      )}
    >
      <ProTable<Changeset>
        key={JSON.stringify(query)}
        params={query}
        form={{ initialValues: query }}
        onSubmit={(values) => {
          const params = new URLSearchParams();
          for (const [key, value] of Object.entries(values)) {
            if (value !== undefined && value !== null && value !== '')
              params.set(key, String(value));
          }
          history.replace(
            `${location.pathname}${params.size ? `?${params}` : ''}`,
          );
        }}
        onReset={() => history.replace(location.pathname)}
        rowKey="changesetId"
        actionRef={actionRef}
        columns={columns}
        scroll={{ x: 1400 }}
        search={{ labelWidth: 'auto' }}
        pagination={{ pageSize: 20, showSizeChanger: true }}
        request={async (params) => {
          const result = await api.get<PageData<Changeset>>(
            '/system/changeset/list',
            { ...params, pageNum: params.current, pageSize: params.pageSize },
          );
          return { data: result.rows, total: result.total, success: true };
        }}
      />
      <Drawer
        title={t('变更集详情')}
        open={active && !!selectedId && canView}
        onClose={closeDetail}
        width={980}
        extra={
          detail && (
            <Auth perm="system:changeset:rollback">
              <Button
                disabled={detail.status === 'rolled_back'}
                onClick={() => void requestRollback(detail.changesetId)}
              >
                {t('回滚全部剩余条目')}
              </Button>
            </Auth>
          )
        }
      >
        {detailLoading ? (
          <Spin />
        ) : detail ? (
          <>
            <Descriptions
              bordered
              size="small"
              column={2}
              items={[
                {
                  key: 'id',
                  label: t('变更集'),
                  children: (
                    <Typography.Text copyable>
                      {detail.changesetId}
                    </Typography.Text>
                  ),
                  span: 2,
                },
                {
                  key: 'actor',
                  label: t('主体'),
                  children: `${actorTypes[detail.actorType as keyof typeof actorTypes] ?? detail.actorType} / ${detail.actorName}`,
                },
                {
                  key: 'source',
                  label: t('来源'),
                  children: (
                    <DictTag type="sys_call_source" value={detail.source} />
                  ),
                },
                {
                  key: 'intent',
                  label: t('原始意图'),
                  children: detail.intent || '—',
                  span: 2,
                },
                {
                  key: 'status',
                  label: t('状态'),
                  children: t(
                    setStatus[detail.status as keyof typeof setStatus]?.text ??
                      detail.status,
                  ),
                },
                {
                  key: 'risk',
                  label: t('风险'),
                  children: (
                    <DictTag type="sys_risk_level" value={detail.risk} />
                  ),
                },
                {
                  key: 'approval',
                  label: t('审批方式'),
                  children: (
                    <DictTag
                      type="sys_approval_mode"
                      value={detail.approvalMode}
                    />
                  ),
                },
                {
                  key: 'time',
                  label: t('时间'),
                  children: new Date(detail.createTime).toLocaleString(
                    getLocale(),
                  ),
                },
              ]}
            />
            <Space wrap className="my-4">
              {detail.planId && (
                <Auth perm="ai:agent:use">
                  <Button
                    onClick={() =>
                      history.push(
                        `/ai/sessions?planId=${encodeURIComponent(detail.planId ?? '')}`,
                      )
                    }
                  >
                    {t('查看来源计划')}
                  </Button>
                </Auth>
              )}
              {detail.rollbackOf && (
                <Button onClick={() => view(detail.rollbackOf)}>
                  {t('查看被回滚的变更集')}
                </Button>
              )}
              {(
                detail.rollbackChangesetIds ??
                (detail.rollbackChangesetId ? [detail.rollbackChangesetId] : [])
              ).map((id, index) => (
                <Button key={id} onClick={() => view(id)}>
                  {t('查看第 {count} 次回滚', { count: index + 1 })}
                </Button>
              ))}
              <Auth perm="monitor:operlog:list">
                <Button
                  onClick={() =>
                    history.push(
                      `/audit?changesetId=${encodeURIComponent(detail.changesetId)}`,
                    )
                  }
                >
                  {t('查看行为溯源')}
                </Button>
              </Auth>
            </Space>
            {detail.rolledBackAt && (
              <Alert
                type="info"
                title={t('最近回滚：{time}，操作人：{name}', {
                  time: new Date(detail.rolledBackAt).toLocaleString(
                    getLocale(),
                  ),
                  name: detail.rolledBackBy ?? '—',
                })}
                className="mb-4"
              />
            )}
            <DiffTable preview={changesetPreview(detail)} historical />
            <Space className="my-4" wrap>
              <Typography.Title level={5} style={{ margin: 0 }}>
                {t('逐项执行记录')}
              </Typography.Title>
              {canRollback && (
                <>
                  <Button
                    type="primary"
                    disabled={!selectedItemIds.length}
                    onClick={() =>
                      void requestRollback(detail.changesetId, selectedItemIds)
                    }
                  >
                    {t('预览所选 {count} 项', {
                      count: selectedItemIds.length,
                    })}
                  </Button>
                  <Button
                    disabled={!selectedItemIds.length}
                    onClick={() => setSelectedItemIds([])}
                  >
                    {t('清空选择')}
                  </Button>
                  <Typography.Text type="secondary">
                    {t('共享补偿数据的条目会自动整组选中。')}
                  </Typography.Text>
                </>
              )}
            </Space>
            <Table<ChangesetItem>
              size="small"
              rowKey="itemId"
              dataSource={detail.items ?? []}
              rowSelection={
                canRollback
                  ? {
                      selectedRowKeys: selectedItemIds,
                      onChange: (keys) =>
                        setSelectedItemIds(
                          updateRollbackSelection(
                            detail.items ?? [],
                            selectedItemIds,
                            keys.map(Number),
                          ),
                        ),
                      getCheckboxProps: (item) => ({
                        disabled: !selectableItem(item),
                      }),
                    }
                  : undefined
              }
              scroll={{ x: 1050 }}
              pagination={false}
              columns={[
                { title: t('序号'), dataIndex: 'seq' },
                { title: t('功能'), dataIndex: 'capability' },
                {
                  title: t('记录'),
                  render: (_, item) =>
                    `${item.entityLabel || item.entity} #${item.entityId}`,
                },
                {
                  title: t('状态'),
                  dataIndex: 'status',
                  render: (value: string) => (
                    <Tag
                      color={
                        value === 'failed'
                          ? 'red'
                          : value === 'rolled_back'
                            ? 'green'
                            : 'default'
                      }
                    >
                      {t(itemStatuses[value] ?? value)}
                    </Tag>
                  ),
                },
                {
                  title: t('说明 / 失败原因'),
                  dataIndex: 'errorMsg',
                  render: (value: string | undefined, item) => (
                    <Space orientation="vertical" size={2}>
                      <span>{value || '—'}</span>
                      {item.conflictFields?.length ? (
                        <Typography.Text type="danger">
                          {t('冲突字段：{fields}', {
                            fields: item.conflictFields.join(', '),
                          })}
                        </Typography.Text>
                      ) : null}
                      {item.undoGroup && (
                        <Tag>
                          {t('原子补偿组 {group}', {
                            group: item.undoGroup.slice(0, 8),
                          })}
                        </Tag>
                      )}
                    </Space>
                  ),
                },
                {
                  title: t('回滚记录'),
                  key: 'rollback',
                  render: (_, item) =>
                    item.rollbackChangesetId ? (
                      <Space orientation="vertical" size={2}>
                        <Button
                          type="link"
                          size="small"
                          onClick={() => view(item.rollbackChangesetId)}
                        >
                          {t('查看回滚变更集')}
                        </Button>
                        <Typography.Text type="secondary">
                          {item.rolledBackAt
                            ? new Date(item.rolledBackAt).toLocaleString(
                                getLocale(),
                              )
                            : '—'}{' '}
                          / {item.rolledBackBy}
                        </Typography.Text>
                      </Space>
                    ) : (
                      '—'
                    ),
                },
              ]}
            />
          </>
        ) : (
          <Empty description={t('无法加载变更集')} />
        )}
      </Drawer>
      <Modal
        title={t('确认回滚')}
        open={active && !!rollbackTarget}
        onCancel={() => {
          if (!rollbackBusy) setRollbackTarget(undefined);
        }}
        width={1000}
        onOk={() => void rollback()}
        confirmLoading={rollbackBusy}
        okText={t('确认按差异回滚')}
        okButtonProps={{
          disabled:
            !preview || preview.affected === 0 || !preview.confirmationHash,
        }}
      >
        {rollbackBusy && !preview ? (
          <Spin />
        ) : (
          preview && (
            <>
              <Space wrap className="mb-4">
                <Tag color="blue">
                  {t('本次选择 {count} 项', {
                    count: preview.selection?.itemIds.length ?? 0,
                  })}
                </Tag>
                <Tag color="green">
                  {t('可回滚 {count} 项', { count: preview.affected })}
                </Tag>
                <Tag color="orange">
                  {t('跳过 {count} 项', { count: preview.skipped })}
                </Tag>
                <Tag>
                  {t('成功后剩余 {count} 项', {
                    count: preview.selection?.remaining ?? 0,
                  })}
                </Tag>
              </Space>
              <DiffTable preview={preview} />
              {preview.changes?.some(
                (change) => change.conflictFields?.length,
              ) && (
                <Table
                  size="small"
                  pagination={false}
                  rowKey="itemId"
                  dataSource={preview.changes.filter(
                    (change) => change.conflictFields?.length,
                  )}
                  columns={[
                    { title: t('条目'), dataIndex: 'itemId' },
                    {
                      title: t('状态'),
                      render: (_, change) =>
                        t(
                          itemStatuses[change.status ?? ''] ??
                            change.status ??
                            '—',
                        ),
                    },
                    {
                      title: t('冲突字段'),
                      render: (_, change) => change.conflictFields?.join(', '),
                    },
                    { title: t('原因'), dataIndex: 'reason' },
                  ]}
                />
              )}
            </>
          )
        )}
        <Alert
          type="info"
          title={t(
            '仅回滚本次预览中可执行的所选条目；冲突项保留原因并可重试。每次回滚都会生成独立的变更集，支持继续恢复。',
          )}
          className="mt-4"
        />
      </Modal>
    </PageContainer>
  );
}
