import { useEffect, useState } from 'react';
import { Typography } from 'antd';
import { usePageActive } from '@/contexts/ActivePageContext';
import { t } from '@/i18n';
import { api } from '@/utils/request';

export interface SchedulePreviewData {
  cronExpression: string;
  timeZone: string;
  baseTime: string;
  nextExecutions: string[];
  estimated: boolean;
}

export function previewSchedule(cronExpression: string) {
  return api.get<SchedulePreviewData>(
    '/monitor/job/schedulePreview',
    { cronExpression },
    { silent: true, dedupe: false },
  );
}

// Preserve the server's numeric offset rather than converting to browser time.
export function scheduleTime(value: string) {
  return value.replace('T', ' ');
}

export function SchedulePreview({
  expression,
  paused,
}: {
  expression: string;
  paused: boolean;
}) {
  const active = usePageActive();
  const [state, setState] = useState<{
    expression: string;
    data?: SchedulePreviewData;
    error?: string;
  }>();
  useEffect(() => {
    let current = true;
    setState(undefined);
    if (!active || !expression.trim()) return;
    const timer = setTimeout(() => {
      void previewSchedule(expression).then(
        (data) => {
          if (current) setState({ expression, data });
        },
        (error: unknown) => {
          if (current)
            setState({
              expression,
              error: error instanceof Error ? error.message : t('计划预览失败'),
            });
        },
      );
    }, 400);
    return () => {
      current = false;
      clearTimeout(timer);
    };
  }, [expression, active]);
  if (!active || !expression.trim()) return null;
  const current = state?.expression === expression ? state : undefined;
  if (current?.error)
    return (
      <div role="alert">
        <Typography.Text type="danger">{current.error}</Typography.Text>
      </div>
    );
  if (!current?.data) return <div role="status">{t('正在校验执行计划…')}</div>;
  const data = current.data;
  return (
    <div className="mt-2" aria-live="polite">
      <div>{t('执行时区：{timeZone}', { timeZone: data.timeZone })}</div>
      <div>
        {t('规范表达式：{expression}', { expression: data.cronExpression })}
      </div>
      <div>{t('计算基准：{time}', { time: scheduleTime(data.baseTime) })}</div>
      {paused && (
        <Typography.Text type="warning">
          {t('按表达式估算，当前不会执行')}
        </Typography.Text>
      )}
      {data.estimated && (
        <div>{t('@every 按当前时刻估算，不代表队列的实际下次触发时间')}</div>
      )}
      <div>{t('未来 5 次预计执行时间')}</div>
      {data.nextExecutions.length ? (
        <ol>
          {data.nextExecutions.map((time) => (
            <li key={time}>{scheduleTime(time)}</li>
          ))}
        </ol>
      ) : (
        <div>{t('该表达式没有可计算的未来执行时间')}</div>
      )}
    </div>
  );
}
