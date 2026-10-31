import { history } from '@umijs/max';
import { Alert, App, Button, Card, Space, Tag, Typography } from 'antd';
import { useEffect, useState } from 'react';
import type { Risk } from '@/api/types';
import { RiskColor, RiskLabel } from '@/api/types';
import { t } from '@/i18n';
import { useUserStore } from '@/stores/user';
import { ApiError, api, Code } from '@/utils/request';
import { refreshCurrentPage } from '@/utils/pageRefresh';
import { managementURL, type Plan, planState } from './state';

export type { Plan, PlanStep } from './state';

const statusLabels: Record<string, string> = {
  draft: t('待预演'),
  previewed: t('待确认'),
  partial: t('部分执行'),
  done: t('已完成'),
  aborted: t('已放弃'),
  pending: t('等待依赖结果'),
  applied: t('已执行'),
  failed: t('执行失败'),
  skipped: t('已跳过'),
};

export function PlanCard({
  initialPlan,
  onUpdated,
  onNavigate,
  readOnly = false,
}: {
  initialPlan: Plan;
  onUpdated?: (plan: Plan) => void;
  onNavigate?: () => void;
  readOnly?: boolean;
}) {
  const [plan, setPlan] = useState(initialPlan);
  const [mode, setMode] = useState(
    initialPlan.steps.some((step) => step.status === 'pending')
      ? 'stepwise'
      : 'all',
  );
  const [busy, setBusy] = useState(false);
  const [stale, setStale] = useState(false);
  const [riskBlocked, setRiskBlocked] = useState(false);
  const [now, setNow] = useState(Date.now());
  const { message } = App.useApp();
  const maxRisk = useUserStore((s) => s.identity?.aiMaxRisk) as
    | Risk
    | undefined;
  const state = planState(plan, maxRisk, now);
  useEffect(() => {
    setPlan(initialPlan);
    setStale(false);
    setRiskBlocked(false);
  }, [initialPlan]);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    if (state.requiresStepwise) setMode('stepwise');
  }, [state.requiresStepwise]);

  async function action(kind: 'apply' | 'preview' | 'abort') {
    if (busy) return;
    setBusy(true);
    try {
      const result = await api.post<Plan>(
        `/ai/plans/${encodeURIComponent(plan.id)}/${kind}`,
        kind === 'apply'
          ? {
              mode,
              ...(mode === 'stepwise' && state.step
                ? { stepSeq: state.step.seq }
                : {}),
            }
          : {},
        { dedupe: false },
      );
      setPlan(result);
      onUpdated?.(result);
      setStale(false);
      setRiskBlocked(false);
      setNow(Date.now());
      if (kind === 'apply') {
        void refreshCurrentPage();
        message.success(
          result.status === 'done'
            ? t('计划已执行')
            : t('此步骤已执行，请检查下一步的实际预览再确认'),
        );
      } else if (kind === 'preview')
        message.success(t('已获取最新预览，请检查差异后确认'));
    } catch (error) {
      if (error instanceof ApiError && error.code === Code.StaleData) {
        setStale(true);
        message.warning(t('数据或有效期已变化，请重新预演并确认'));
      } else if (
        error instanceof ApiError &&
        error.code === Code.RiskExceeded
      ) {
        setRiskBlocked(true);
        message.warning(error.message);
      }
    } finally {
      setBusy(false);
    }
  }

  const finished = ['done', 'aborted'].includes(plan.status);
  const needsPreview =
    !finished && (stale || state.expired || plan.status === 'draft');
  const manual = !finished && (state.exceeded || riskBlocked);
  const warnings = [
    ...new Set(plan.steps.flatMap((step) => step.preview?.warnings ?? [])),
  ];

  return (
    <Card
      className="intent-plan-card"
      size="small"
      title={
        finished ? (statusLabels[plan.status] ?? plan.status) : t('操作确认')
      }
      extra={
        <Tag color={RiskColor[plan.maxRisk]}>{RiskLabel[plan.maxRisk]}</Tag>
      }
    >
      <Typography.Paragraph>
        {mode === 'stepwise' && state.step
          ? state.step.note || state.step.preview?.summary || plan.intent
          : plan.intent}
      </Typography.Paragraph>
      {warnings.map((warning) => (
        <Alert
          key={warning}
          type="warning"
          showIcon
          title={warning}
          className="mb-3"
        />
      ))}
      {manual && (
        <Alert
          type="warning"
          showIcon
          title={t('需要在管理页面执行')}
          description={t('此操作超出 AI 的执行上限，请前往管理页面执行。')}
          className="mb-3"
        />
      )}
      {needsPreview && (
        <Alert
          type="warning"
          showIcon
          title={t('需要重新预演')}
          description={t(
            '当前数据或计划有效期已变化，重新预演并确认新差异后才可继续。',
          )}
          className="mb-3"
        />
      )}
      {state.requiresStepwise && (
        <Alert
          type="info"
          title={t('此计划依赖前一步的执行结果，需要逐步检查与确认。')}
          className="mb-3"
        />
      )}
      {!readOnly && !finished && (
        <Space wrap className="mt-4">
          {state.executable && !stale && !riskBlocked && (
            <Button
              type="primary"
              loading={busy}
              onClick={() => void action('apply')}
            >
              {mode === 'stepwise'
                ? t('确认执行第 {value0} 步', {
                    value0: (state.step?.seq ?? 0) + 1,
                  })
                : t('确认执行')}
            </Button>
          )}
          {needsPreview && !['done', 'aborted'].includes(plan.status) && (
            <Button loading={busy} onClick={() => void action('preview')}>
              {t('重新预演')}
            </Button>
          )}
          {!['done', 'aborted'].includes(plan.status) && (
            <Button disabled={busy} onClick={() => void action('abort')}>
              {t('取消')}
            </Button>
          )}
          {manual &&
            [
              ...new Set(
                plan.steps
                  .map(managementURL)
                  .filter((route): route is string => !!route),
              ),
            ].map((route) => (
              <Button
                key={route}
                onClick={() => {
                  history.push(route);
                  onNavigate?.();
                }}
              >
                {t('前往管理页面核对')}
              </Button>
            ))}
        </Space>
      )}
    </Card>
  );
}
