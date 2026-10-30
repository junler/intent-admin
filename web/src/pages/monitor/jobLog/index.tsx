import { LogPage } from '@/components/admin/LogPage';
import { t } from '@/i18n';
export default function JobLog() {
  return (
    <LogPage
      title={t('任务执行日志')}
      base="/monitor/jobLog"
      idKey="jobLogId"
      perm="monitor:job"
      fields={[
        { name: 'jobId', label: t('任务编号'), type: 'number', search: true },
        { name: 'jobName', label: t('任务'), search: true },
        { name: 'jobGroup', label: t('队列'), search: true },
        { name: 'invokeTarget', label: t('处理器') },
        {
          name: 'status',
          label: t('结果'),
          type: 'select',
          search: true,
          options: [
            { label: t('成功'), value: '0' },
            { label: t('失败'), value: '1' },
          ],
        },
        { name: 'costTime', label: t('耗时 ms'), type: 'number' },
        { name: 'jobMessage', label: t('说明') },
        { name: 'createTime', label: t('时间') },
      ]}
    />
  );
}
