import { LogPage } from '@/components/admin/LogPage';
import { t } from '@/i18n';
export default function OperLog() {
  return (
    <LogPage
      title={t('操作日志')}
      base="/monitor/operlog"
      idKey="operId"
      perm="monitor:operlog"
      fields={[
        { name: 'title', label: t('模块'), search: true },
        { name: 'capability', label: t('能力'), search: true },
        { name: 'operName', label: t('操作者'), search: true },
        {
          name: 'actorType',
          label: t('主体'),
          type: 'select',
          search: true,
          options: [
            { label: t('用户'), value: 'human' },
            { label: 'AI', value: 'agent' },
            { label: 'MCP', value: 'mcp_client' },
            { label: t('任务'), value: 'system' },
          ],
        },
        { name: 'source', label: t('来源'), search: true },
        { name: 'operIp', label: 'IP' },
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
        { name: 'operTime', label: t('时间') },
      ]}
    />
  );
}
