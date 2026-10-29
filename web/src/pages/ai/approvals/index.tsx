import {
  AuditOutlined,
  CheckOutlined,
  ClockCircleOutlined,
  CloseOutlined,
} from '@ant-design/icons';
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
  Input,
  Modal,
  Segmented,
  Space,
  Spin,
  Tag,
  Typography,
} from 'antd';
import { useEffect, useRef, useState } from 'react';
import { approvalActions, approvalLabels } from '@/components/ai/approval';
import { PlanCard } from '@/components/ai/PlanCard';
import type { PlanApproval } from '@/components/ai/state';
import { usePageActive } from '@/contexts/ActivePageContext';
import { usePerm } from '@/hooks/usePerm';
import { getLocale, t } from '@/i18n';
import { useUserStore } from '@/stores/user';
import { ApiError, api, Code, type PageData } from '@/utils/request';

export default function ApprovalsPage() {
  const active = usePageActive();
  const location = useLocation();
  const canApprove = usePerm('ai:plan:approve');
  const userId = useUserStore((s) => s.identity?.user.userId);
  const [box, setBox] = useState(canApprove ? 'inbox' : 'sent');
  const [detail, setDetail] = useState<PlanApproval>();
  const [detailLoading, setDetailLoading] = useState(false);
  const [decision, setDecision] = useState<'approve' | 'reject' | 'revoke'>();
  const [comment, setComment] = useState('');
  const [busy, setBusy] = useState(false);
  const [now, setNow] = useState(Date.now());
  const actionRef = useRef<ActionType | undefined>(undefined);
  const { message, modal } = App.useApp();
  const selected = new URLSearchParams(location.search).get('id');
  const actions = detail && approvalActions(detail, userId, canApprove, now);

  async function load(id: string) {
    const result = await api.get<PlanApproval>(
      `/ai/approvals/${encodeURIComponent(id)}`,
      undefined,
      { dedupe: false },
    );
    setDetail(result);
    setNow(Date.now());
  }
  useEffect(() => {
    if (!active || !selected) {
      setDetail(undefined);
      return;
    }
    let keep = true;
    setDetailLoading(true);
    void api
      .get<PlanApproval>(
        `/ai/approvals/${encodeURIComponent(selected)}`,
        undefined,
        { dedupe: false },
      )
      .then((value) => {
        if (keep) setDetail(value);
      })
      .catch(() => {
        if (keep) setDetail(undefined);
      })
      .finally(() => {
        if (keep) setDetailLoading(false);
      });
    return () => {
      keep = false;
    };
  }, [selected, active]);
  useEffect(() => {
    if (!active) return;
    const timer = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(timer);
  }, [active]);

  function choose(next: 'approve' | 'reject' | 'revoke') {
    setComment('');
    setDecision(next);
  }
  async function decide() {
    if (!detail || !decision || busy) return;
    if (decision === 'reject' && !comment.trim()) {
      message.warning(t('请填写驳回原因'));
      return;
    }
    setBusy(true);
    try {
      await api.post(
        `/ai/approvals/${encodeURIComponent(detail.id)}/${decision}`,
        { comment },
        { dedupe: false },
      );
      setDecision(undefined);
      await load(detail.id);
      actionRef.current?.reload();
      message.success(t('审批状态已更新'));
    } catch (error) {
      if (error instanceof ApiError && error.code === Code.StaleData) {
        setDecision(undefined);
        await load(detail.id);
        actionRef.current?.reload();
      }
    } finally {
      setBusy(false);
    }
  }
  function execute() {
    if (!detail) return;
    modal.confirm({
      title: t('按他人批准的差异执行？'),
      content: t(
        '执行前会再次校验双方权限、风险上限和当前数据。任何变化都会要求重新审批。',
      ),
      okText: t('执行计划'),
      cancelText: t('取消'),
      onOk: async () => {
        try {
          await api.post(
            `/ai/approvals/${encodeURIComponent(detail.id)}/apply`,
            {},
            { dedupe: false },
          );
          await load(detail.id);
          actionRef.current?.reload();
          message.success(t('已按他人审批执行计划'));
        } catch (error) {
          if (error instanceof ApiError && error.code === Code.StaleData)
            await load(detail.id);
          throw error;
        }
      },
    });
  }
  const columns: ProColumns<PlanApproval>[] = [
    {
      title: t('审批记录'),
      dataIndex: 'id',
      search: false,
      ellipsis: true,
      width: 180,
      render: (_, row) => (
        <Button
          type="link"
          className="px-0"
          onClick={() =>
            history.push(`/ai/approvals?id=${encodeURIComponent(row.id)}`)
          }
        >
          {row.id.slice(0, 8)}
        </Button>
      ),
    },
    { title: t('发起人'), dataIndex: 'proposerName', search: false },
    { title: t('审批人'), dataIndex: 'reviewerName', search: false },
    {
      title: t('状态'),
      dataIndex: 'status',
      valueType: 'select',
      valueEnum: Object.fromEntries(
        Object.entries(approvalLabels).map(([key, text]) => [key, { text }]),
      ),
      render: (_, row) => (
        <Tag
          color={
            row.status === 'approved'
              ? 'success'
              : row.status === 'pending'
                ? 'processing'
                : undefined
          }
        >
          {approvalLabels[row.status] ?? row.status}
        </Tag>
      ),
    },
    {
      title: t('申请时间'),
      dataIndex: 'createTime',
      valueType: 'dateTime',
      search: false,
    },
    {
      title: t('有效期至'),
      dataIndex: 'expiresAt',
      valueType: 'dateTime',
      search: false,
    },
    {
      title: t('操作'),
      valueType: 'option',
      width: 100,
      render: (_, row) => (
        <Button
          type="link"
          onClick={() =>
            history.push(`/ai/approvals?id=${encodeURIComponent(row.id)}`)
          }
        >
          {t('核对差异')}
        </Button>
      ),
    },
  ];
  return (
    <PageContainer
      title={t('审批中心')}
      subTitle={t('另一位成员核对差异，发起人按批准的版本执行')}
    >
      <Alert
        className="mb-5"
        showIcon
        icon={<AuditOutlined />}
        type="info"
        title={t(
          '本人确认与跨人审批可按需选择。跨人审批不会提升任何人的操作权限或 AI 风险上限。',
        )}
      />
      <ProTable<PlanApproval>
        actionRef={actionRef}
        rowKey="id"
        columns={columns}
        headerTitle={
          <Segmented
            value={box}
            onChange={(value) => setBox(String(value))}
            options={[
              ...(canApprove
                ? [
                    {
                      label: t('待我处理'),
                      value: 'inbox',
                      icon: <ClockCircleOutlined />,
                    },
                  ]
                : []),
              { label: t('我发起的'), value: 'sent' },
            ]}
          />
        }
        params={{ box }}
        request={async (params) => {
          const result = await api.get<PageData<PlanApproval>>(
            '/ai/approvals',
            {
              box,
              status: params.status,
              pageNum: params.current,
              pageSize: params.pageSize,
            },
            { dedupe: false },
          );
          return { data: result.rows, total: result.total, success: true };
        }}
        pagination={{ defaultPageSize: 10 }}
        scroll={{ x: 950 }}
      />
      <Drawer
        width={960}
        open={!!selected}
        onClose={() => history.push('/ai/approvals')}
        title={t('审批详情')}
        extra={
          detail && (
            <Button onClick={() => void load(detail.id).catch(() => {})}>
              {t('刷新状态')}
            </Button>
          )
        }
      >
        {detailLoading ? (
          <Spin />
        ) : detail ? (
          <Space orientation="vertical" size="large" className="w-full">
            <Descriptions bordered size="small" column={2}>
              <Descriptions.Item label={t('状态')}>
                <Tag>
                  {approvalLabels[
                    actions?.expired ? 'expired' : detail.status
                  ] ?? detail.status}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label={t('审批记录')}>
                <Typography.Text copyable>{detail.id}</Typography.Text>
              </Descriptions.Item>
              <Descriptions.Item label={t('发起人')}>
                {detail.proposerName}
              </Descriptions.Item>
              <Descriptions.Item label={t('指定审批人')}>
                {detail.reviewerName}
              </Descriptions.Item>
              <Descriptions.Item label={t('申请时间')}>
                {new Date(detail.createTime).toLocaleString(getLocale())}
              </Descriptions.Item>
              <Descriptions.Item label={t('有效期至')}>
                {new Date(detail.expiresAt).toLocaleString(getLocale())}
              </Descriptions.Item>
              <Descriptions.Item label={t('审批说明')} span={2}>
                {detail.requestComment || '—'}
              </Descriptions.Item>
              {detail.decisionComment && (
                <Descriptions.Item label={t('审批意见')} span={2}>
                  {detail.decisionComment}
                </Descriptions.Item>
              )}
              <Descriptions.Item label={t('预演指纹')} span={2}>
                <Typography.Text code copyable>
                  {detail.fingerprint}
                </Typography.Text>
              </Descriptions.Item>
            </Descriptions>
            {(detail.status === 'stale' ||
              detail.status === 'expired' ||
              actions?.expired) && (
              <Alert
                showIcon
                type="warning"
                title={t('原审批已失效，不能继续执行')}
                description={t(
                  '发起人需要重新预演并申请审批；旧差异仅供追溯。',
                )}
              />
            )}
            {detail.plan && <PlanCard initialPlan={detail.plan} readOnly />}
            <Space wrap>
              {actions?.approve && (
                <Button
                  type="primary"
                  icon={<CheckOutlined />}
                  onClick={() => choose('approve')}
                >
                  {t('核对差异并批准')}
                </Button>
              )}
              {actions?.reject && (
                <Button
                  danger
                  icon={<CloseOutlined />}
                  onClick={() => choose('reject')}
                >
                  {t('驳回申请')}
                </Button>
              )}
              {actions?.revoke && (
                <Button onClick={() => choose('revoke')}>
                  {t('撤销审批')}
                </Button>
              )}
              {actions?.apply && (
                <Button type="primary" onClick={execute}>
                  {t('按已批准差异执行')}
                </Button>
              )}
              {detail.changesetId && (
                <Button
                  onClick={() =>
                    history.push(
                      `/changesets?id=${encodeURIComponent(detail.changesetId!)}`,
                    )
                  }
                >
                  {t('查看变更与撤销')}
                </Button>
              )}
            </Space>
          </Space>
        ) : (
          <Empty description={t('审批不存在或当前账号无法访问')} />
        )}
      </Drawer>
      <Modal
        open={!!decision}
        title={t(
          decision === 'approve'
            ? '确认已核对全部差异？'
            : decision === 'reject'
              ? '驳回申请'
              : '撤销审批',
        )}
        confirmLoading={busy}
        onCancel={() => setDecision(undefined)}
        onOk={() => void decide()}
        okText={t(
          decision === 'approve'
            ? '批准计划'
            : decision === 'reject'
              ? '确认驳回'
              : '撤销审批',
        )}
        cancelText={t('取消')}
      >
        <Alert
          className="mb-4"
          showIcon
          type="info"
          title={t(
            decision === 'approve'
              ? '批准仅对当前预演版本有效，发起人仍需确认执行。'
              : decision === 'revoke'
                ? '撤销后计划回到待预演状态，不能使用原批准继续执行。'
                : '请说明需要调整的内容，发起人可以重新申请审批。',
          )}
        />
        <Input.TextArea
          value={comment}
          onChange={(event) => setComment(event.target.value)}
          rows={3}
          maxLength={1000}
          showCount
          placeholder={t(
            decision === 'reject' ? '驳回原因（必填）' : '审批意见（选填）',
          )}
        />
      </Modal>
    </PageContainer>
  );
}
