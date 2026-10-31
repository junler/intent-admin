import { usePageRefresh } from '@/hooks/usePageRefresh';
import { DetailDialog } from '@/components/common/DetailDialog';
import {
  type ActionType,
  PageContainer,
  ProTable,
} from '@ant-design/pro-components';
import { history, useLocation } from '@umijs/max';
import { App, Button, Empty, Input, Modal, Space, Tabs } from 'antd';
import { useEffect, useRef, useState } from 'react';
import { RiskColor, RiskLabel } from '@/api/types';
import { AiFormHeading } from '@/components/ai/AiFormContext';
import { useAiPanel } from '@/components/ai/AiPanel';
import { type Plan, PlanCard } from '@/components/ai/PlanCard';
import { Loading } from '@/components/common/Loading';
import { usePageActive } from '@/contexts/ActivePageContext';
import { t } from '@/i18n';
import { useAiForm } from '@/hooks/useAiForm';
import { api } from '@/utils/request';

interface Session {
  id: string;
  title: string;
  model: string;
  updatedAt: string;
}

export default function SessionsPage() {
  const active = usePageActive();
  const location = useLocation();
  const sessionsRef = useRef<ActionType | undefined>(undefined);
  const plansRef = useRef<ActionType | undefined>(undefined);
  usePageRefresh(() => Promise.all([sessionsRef.current?.reload(), plansRef.current?.reload()]));
  const [plan, setPlan] = useState<Plan>();
  const [loadingPlan, setLoadingPlan] = useState(false);
  const [renaming, setRenaming] = useState<Session>();
  const [title, setTitle] = useState('');
  const [saving, setSaving] = useState(false);
  useAiForm({
    id: `session.rename.${renaming?.id ?? ''}`,
    title: t('重命名会话'),
    fields: ['title'],
    permission: 'ai:agent:use',
    enabled: !!renaming,
    fill: (values) => {
      if (typeof values.title === 'string') setTitle(values.title.slice(0, 60));
    },
  });
  const show = useAiPanel((state) => state.show);
  const { message } = App.useApp();
  const planId = new URLSearchParams(location.search).get('planId');
  useEffect(() => {
    if (!planId) {
      setPlan(undefined);
      return;
    }
    let active = true;
    setLoadingPlan(true);
    void api
      .get<Plan>(`/ai/plans/${encodeURIComponent(planId)}`, undefined, {
        dedupe: false,
      })
      .then((result) => {
        if (active) setPlan(result);
      })
      .catch(() => {
        if (active) setPlan(undefined);
      })
      .finally(() => {
        if (active) setLoadingPlan(false);
      });
    return () => {
      active = false;
    };
  }, [planId]);
  function viewPlan(id?: string) {
    const params = new URLSearchParams(location.search);
    if (id) params.set('planId', id);
    else params.delete('planId');
    history.push(`${location.pathname}${params.size ? `?${params}` : ''}`);
  }
  return (
    <PageContainer
      title={t('AI 会话与计划')}
      content={t(
        '会话保存在 Redis；操作计划独立保留，AI 发起的操作由本人确认后执行。',
      )}
    >
      <Tabs
        defaultActiveKey={planId ? 'plans' : 'sessions'}
        items={[
          {
            key: 'sessions',
            label: t('会话'),
            children: (
              <ProTable<Session>
                rowKey="id"
                pagination={{ hideOnSinglePage: true }}
                search={false}
                actionRef={sessionsRef}
                request={async () => ({
                  data: await api.get<Session[]>('/ai/sessions', undefined, {
                    dedupe: false,
                  }),
                  success: true,
                })}
                columns={[
                  { title: t('标题'), dataIndex: 'title' },
                  { title: t('模型'), dataIndex: 'model' },
                  {
                    title: t('更新时间'),
                    dataIndex: 'updatedAt',
                    valueType: 'dateTime',
                  },
                  {
                    title: t('操作'),
                    valueType: 'option',
                    render: (_, row) => (
                      <Space>
                        <Button
                          type="link"
                          onClick={() => show(undefined, row.id)}
                        >
                          {t('继续会话')}
                        </Button>
                        <Button
                          type="link"
                          onClick={() => {
                            setRenaming(row);
                            setTitle(row.title);
                          }}
                        >
                          {t('重命名')}
                        </Button>
                        <Button
                          type="link"
                          danger
                          onClick={async () => {
                            await api.del(`/ai/sessions/${row.id}`);
                            sessionsRef.current?.reload();
                          }}
                        >
                          {t('删除')}
                        </Button>
                      </Space>
                    ),
                  },
                ]}
                toolBarRender={() => [
                  <Button key="new" type="primary" onClick={() => show()}>
                    {t('新会话')}
                  </Button>,
                ]}
              />
            ),
          },
          {
            key: 'plans',
            label: t('计划记录'),
            children: (
              <ProTable<Plan>
                rowKey="id"
                pagination={{ hideOnSinglePage: true }}
                actionRef={plansRef}
                search={false}
                request={async () => ({
                  data: await api.get<Plan[]>('/ai/plans', undefined, {
                    dedupe: false,
                  }),
                  success: true,
                })}
                columns={[
                  { title: t('原始意图'), dataIndex: 'intent', ellipsis: true },
                  {
                    title: t('风险'),
                    dataIndex: 'maxRisk',
                    valueEnum: Object.fromEntries(
                      Object.entries(RiskLabel).map(([key, text]) => [
                        key,
                        {
                          text,
                          color: RiskColor[key as keyof typeof RiskColor],
                        },
                      ]),
                    ),
                  },
                  {
                    title: t('状态'),
                    dataIndex: 'status',
                    valueEnum: {
                      draft: t('草稿'),
                      previewed: t('待确认'),
                      partial: t('部分完成'),
                      done: t('已完成'),
                      aborted: t('已放弃'),
                    },
                  },
                  { title: t('影响数'), dataIndex: 'affected' },
                  {
                    title: t('有效期'),
                    dataIndex: 'expiresAt',
                    valueType: 'dateTime',
                  },
                  {
                    title: t('操作'),
                    valueType: 'option',
                    render: (_, row) => (
                      <Space>
                        <Button type="link" onClick={() => viewPlan(row.id)}>
                          {t('预览 / 确认')}
                        </Button>
                      </Space>
                    ),
                  },
                ]}
              />
            ),
          },
        ]}
      />
      <DetailDialog
        title={t('计划详情')}
        open={active && !!planId}
        width={1000}
        onClose={() => viewPlan()}
      >
        {loadingPlan ? (
          <Loading panel />
        ) : plan ? (
          <PlanCard
            initialPlan={plan}
            onUpdated={(result) => {
              setPlan(result);
              plansRef.current?.reload();
            }}
          />
        ) : (
          <Empty description={t('计划不存在或当前账号无法访问')} />
        )}
      </DetailDialog>
      <Modal
        title={
          <AiFormHeading
            title={t('重命名会话')}
            form={{
              title: t('重命名会话'),
              mode: 'action',
              formId: `ui.session.rename.${renaming?.id ?? ''}`,
            }}
          />
        }
        open={active && !!renaming}
        onCancel={() => setRenaming(undefined)}
        confirmLoading={saving}
        onOk={async () => {
          if (!renaming || !title.trim()) return;
          setSaving(true);
          try {
            await api.put(`/ai/sessions/${renaming.id}`, {
              title: title.trim(),
            });
            setRenaming(undefined);
            sessionsRef.current?.reload();
            message.success(t('会话名称已更新'));
          } finally {
            setSaving(false);
          }
        }}
        okButtonProps={{ disabled: !title.trim() }}
      >
        <Input
          value={title}
          onChange={(event) => setTitle(event.target.value)}
          maxLength={60}
          showCount
        />
      </Modal>
    </PageContainer>
  );
}
