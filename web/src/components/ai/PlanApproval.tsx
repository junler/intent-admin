import { TeamOutlined } from '@ant-design/icons';
import { history } from '@umijs/max';
import {
  Alert,
  App,
  Button,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Typography,
} from 'antd';
import { useState } from 'react';
import { t } from '@/i18n';
import { ApiError, api, Code } from '@/utils/request';
import { approvalLabels } from './approval';
import type { Plan, PlanApproval } from './state';

export function PlanApprovalControls({
  plan,
  onUpdated,
  disabled,
}: {
  plan: Plan;
  onUpdated: (plan: Plan) => void;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [reviewers, setReviewers] = useState<
    { userId: number; userName: string }[]
  >([]);
  const [form] = Form.useForm<{ reviewerId: number; comment?: string }>();
  const { message, modal } = App.useApp();
  const approval = plan.approval;
  const canRequest =
    plan.status === 'previewed' &&
    !plan.changesetId &&
    !plan.steps.some((s) => ['pending', 'applied'].includes(s.status));
  const expired =
    approval &&
    ['pending', 'approved'].includes(approval.status) &&
    Date.parse(approval.expiresAt) <= Date.now();
  const active =
    approval && ['pending', 'approved'].includes(approval.status) && !expired;

  async function selectReviewer() {
    setLoading(true);
    try {
      const result = await api.get<{ userId: number; userName: string }[]>(
        '/ai/approval-reviewers',
        { planId: plan.id },
        { dedupe: false },
      );
      setReviewers(result);
      form.resetFields();
      setOpen(true);
    } catch {
      /* The request layer reports API errors. */
    } finally {
      setLoading(false);
    }
  }
  async function submit() {
    try {
      const values = await form.validateFields();
      setLoading(true);
      await api.post<PlanApproval>(
        `/ai/plans/${encodeURIComponent(plan.id)}/approval`,
        values,
        { dedupe: false },
      );
      onUpdated(
        await api.get<Plan>(
          `/ai/plans/${encodeURIComponent(plan.id)}`,
          undefined,
          { dedupe: false },
        ),
      );
      setOpen(false);
      message.success(t('已发送审批申请，对方批准后由你执行'));
    } catch {
      /* Field validation and the request layer report errors. */
    } finally {
      setLoading(false);
    }
  }
  async function apply() {
    setLoading(true);
    try {
      const result = await api.post<Plan>(
        `/ai/approvals/${encodeURIComponent(approval!.id)}/apply`,
        {},
        { dedupe: false },
      );
      onUpdated(result);
      message.success(t('已按他人审批执行计划'));
    } catch (error) {
      if (error instanceof ApiError && error.code === Code.StaleData) {
        onUpdated(
          await api.get<Plan>(
            `/ai/plans/${encodeURIComponent(plan.id)}`,
            undefined,
            { dedupe: false },
          ),
        );
      }
    } finally {
      setLoading(false);
    }
  }
  function revoke() {
    modal.confirm({
      title: t('撤销审批并改为本人确认？'),
      content: t('撤销后必须重新预演，才能本人确认或再次申请他人审批。'),
      okText: t('撤销审批'),
      cancelText: t('取消'),
      onOk: async () => {
        await api.post(
          `/ai/approvals/${encodeURIComponent(approval!.id)}/revoke`,
          {},
          { dedupe: false },
        );
        onUpdated(
          await api.get<Plan>(
            `/ai/plans/${encodeURIComponent(plan.id)}`,
            undefined,
            { dedupe: false },
          ),
        );
      },
    });
  }
  return (
    <div className="mt-3">
      {approval && (
        <Alert
          showIcon
          type={
            approval.status === 'approved' && !expired
              ? 'success'
              : ['rejected', 'stale', 'expired'].includes(approval.status) ||
                  expired
                ? 'warning'
                : 'info'
          }
          title={
            approvalLabels[expired ? 'expired' : approval.status] ??
            approval.status
          }
          description={
            <Space orientation="vertical" size={4}>
              <span>
                {t('指定审批人：{name}', { name: approval.reviewerName })}
              </span>
              <span>
                {t('审批绑定当前差异，数据或权限变化后必须重新申请。')}
              </span>
              {approval.decisionComment && (
                <Typography.Text>{approval.decisionComment}</Typography.Text>
              )}
            </Space>
          }
          action={
            <Button
              type="link"
              onClick={() =>
                history.push(
                  `/ai/approvals?id=${encodeURIComponent(approval.id)}`,
                )
              }
            >
              {t('审批详情')}
            </Button>
          }
        />
      )}
      <Space wrap className="mt-2">
        {!active && canRequest && (
          <Button
            icon={<TeamOutlined />}
            loading={loading}
            disabled={disabled}
            onClick={() => void selectReviewer()}
          >
            {t(approval ? '重新申请他人审批' : '请他人审批')}
          </Button>
        )}
        {approval?.status === 'approved' && !expired && (
          <Button
            type="primary"
            loading={loading}
            disabled={disabled}
            onClick={() => void apply()}
          >
            {t('按已批准差异执行')}
          </Button>
        )}
        {approval && !['applied', 'revoked'].includes(approval.status) && (
          <Button disabled={disabled || loading} onClick={revoke}>
            {t('撤销审批，改为本人确认')}
          </Button>
        )}
      </Space>
      <Modal
        open={open}
        title={t('申请另一位成员审批')}
        onCancel={() => setOpen(false)}
        onOk={() => void submit()}
        confirmLoading={loading}
        okButtonProps={{ disabled: reviewers.length === 0 }}
        okText={t('发送审批申请')}
        cancelText={t('取消')}
      >
        <Alert
          className="mb-4"
          showIcon
          type="info"
          title={t(
            '对方需要审批权限、每一步操作权限及足够的 AI 风险上限。批准不会自动执行。',
          )}
        />
        {!reviewers.length && (
          <Alert
            className="mb-3"
            type="warning"
            title={t('没有符合条件的其他审批人，请联系管理员分配审批权限。')}
          />
        )}
        <Form form={form} layout="vertical">
          <Form.Item
            name="reviewerId"
            label={t('指定审批人')}
            rules={[{ required: true, message: t('请选择审批人') }]}
          >
            <Select
              showSearch
              optionFilterProp="label"
              options={reviewers.map((r) => ({
                value: r.userId,
                label: r.userName,
              }))}
              placeholder={t('请选择审批人')}
            />
          </Form.Item>
          <Form.Item name="comment" label={t('审批说明')}>
            <Input.TextArea
              rows={3}
              maxLength={1000}
              showCount
              placeholder={t('说明操作目的和需要核对的内容')}
            />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
