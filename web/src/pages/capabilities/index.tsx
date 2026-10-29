import {
  type ActionType,
  PageContainer,
  type ProColumns,
  ProTable,
} from '@ant-design/pro-components';
import { history } from '@umijs/max';
import {
  Alert,
  Button,
  Collapse,
  Descriptions,
  Drawer,
  Empty,
  Input,
  Space,
  Spin,
  Table,
  Tabs,
  Tag,
  Typography,
} from 'antd';
import { useRef, useState } from 'react';
import { type Risk, RiskColor, RiskLabel } from '@/api/types';
import { managementURL } from '@/components/ai/state';
import { usePageActive } from '@/contexts/ActivePageContext';
import { t } from '@/i18n';
import { api } from '@/utils/request';

interface CapabilityRow {
  name: string;
  summary: string;
  perm: string;
  risk: Risk;
  mutating: boolean;
  reversible: boolean;
  aiEnabled: boolean;
  mcpEnabled: boolean;
  tags: string[];
  examples?: { utterance: string; args: string }[];
  inputSchema?: unknown;
  http?: { method?: string; path?: string; Method?: string; Path?: string };
}
interface EnumDef {
  dictType: string;
  summary: string;
  values: Record<string, string>;
  usedBy?: string[];
}
interface EntityDef {
  table: string;
  summary: string;
  keyField: string;
  labelField: string;
  relations?: { kind: string; target: string; via?: string; summary: string }[];
  notes?: string[];
}
interface RuleDef {
  scope: string;
  rule: string;
  enforced: boolean;
}
interface Knowledge {
  enums: EnumDef[];
  entities: EntityDef[];
  rules: RuleDef[];
}
function pretty(value: unknown) {
  if (typeof value === 'string') {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      return value;
    }
  }
  return JSON.stringify(value, null, 2);
}

export default function CapabilitiesPage() {
  const active = usePageActive();
  const actionRef = useRef<ActionType | undefined>(undefined);
  const [keyword, setKeyword] = useState('');
  const [selected, setSelected] = useState<CapabilityRow>();
  const [knowledgeOpen, setKnowledgeOpen] = useState(false);
  const [knowledge, setKnowledge] = useState<Knowledge>();
  const [loadingKnowledge, setLoadingKnowledge] = useState(false);
  const selectedRoute = selected
    ? managementURL({
        seq: 0,
        capability: selected.name,
        args: {},
        note: '',
        status: '',
      })
    : undefined;
  async function loadKnowledge() {
    setKnowledgeOpen(true);
    setLoadingKnowledge(true);
    try {
      setKnowledge(
        await api.get<Knowledge>('/ai/knowledge', undefined, { dedupe: false }),
      );
    } catch {
      setKnowledge(undefined);
    } finally {
      setLoadingKnowledge(false);
    }
  }
  const columns: ProColumns<CapabilityRow>[] = [
    {
      title: t('功能'),
      dataIndex: 'name',
      width: 250,
      ellipsis: true,
      search: false,
      render: (_, row) => (
        <>
          <Typography.Text>{row.summary}</Typography.Text>
          <br />
          <Typography.Text type="secondary" className="text-xs" copyable>
            {row.name}
          </Typography.Text>
        </>
      ),
    },
    {
      title: t('接口'),
      dataIndex: 'http',
      search: false,
      width: 280,
      render: (_, row) =>
        row.http ? (
          <Typography.Text code>
            {row.http.method ?? row.http.Method}{' '}
            {row.http.path ?? row.http.Path}
          </Typography.Text>
        ) : (
          t('无 HTTP 入口')
        ),
    },
    {
      title: t('权限'),
      dataIndex: 'perm',
      search: false,
      renderText: (value: string) => value || t('登录即可'),
    },
    {
      title: t('风险'),
      dataIndex: 'risk',
      valueType: 'select',
      valueEnum: Object.fromEntries(
        Object.entries(RiskLabel).map(([key, text]) => [key, { text }]),
      ),
      render: (_, row) => (
        <Tag color={RiskColor[row.risk]}>{RiskLabel[row.risk]}</Tag>
      ),
    },
    {
      title: t('行为'),
      dataIndex: 'mutating',
      valueType: 'select',
      valueEnum: { true: t('修改数据'), false: t('只读') },
      render: (_, row) => (
        <Space>
          <Tag color={row.mutating ? 'orange' : 'green'}>
            {row.mutating ? t('修改') : t('只读')}
          </Tag>
          {row.mutating && (
            <Tag color={row.reversible ? 'green' : 'red'}>
              {row.reversible ? t('可回滚') : t('不可回滚')}
            </Tag>
          )}
        </Space>
      ),
    },
    {
      title: t('AI 可用'),
      dataIndex: 'aiEnabled',
      valueType: 'select',
      valueEnum: { true: t('开放'), false: t('关闭') },
      render: (_, row) => (row.aiEnabled ? <Tag color="purple">AI</Tag> : '—'),
    },
    {
      title: t('MCP 可用'),
      dataIndex: 'mcpEnabled',
      valueType: 'select',
      valueEnum: { true: t('开放'), false: t('关闭') },
      render: (_, row) => (row.mcpEnabled ? <Tag color="cyan">MCP</Tag> : '—'),
    },
    {
      title: t('操作'),
      valueType: 'option',
      render: (_, row) => (
        <Button type="link" onClick={() => setSelected(row)}>
          {t('Schema / 示例')}
        </Button>
      ),
    },
  ];
  return (
    <PageContainer
      title={t('能力浏览器')}
      content={t(
        '注册表是接口、AI 工具和 MCP 工具的共同来源。这里仅展示当前账号有权限了解的能力。',
      )}
    >
      <Alert
        type="info"
        showIcon
        title={t('权限与风险分别控制可访问功能和 AI 可代执行的范围')}
        description={t(
          '模型服务、存储配置、令牌及个人敏感操作始终与 AI/MCP 写入口隔离。AI 的所有修改先进入计划预演，等待用户确认。',
        )}
        className="mb-4"
      />
      <ProTable<CapabilityRow>
        rowKey="name"
        actionRef={actionRef}
        params={{ keyword }}
        columns={columns}
        scroll={{ x: 1450 }}
        search={{ labelWidth: 'auto' }}
        pagination={{ pageSize: 20, showSizeChanger: true }}
        toolBarRender={() => [
          <Input.Search
            key="keyword"
            placeholder={t('搜索名称或说明')}
            allowClear
            onSearch={setKeyword}
            className="w-64"
          />,
          <Button key="knowledge" onClick={() => void loadKnowledge()}>
            {t('系统知识库')}
          </Button>,
        ]}
        request={async (params) => {
          let rows = await api.get<CapabilityRow[]>('/ai/capabilities', {
            keyword: params.keyword,
          });
          for (const key of [
            'risk',
            'mutating',
            'aiEnabled',
            'mcpEnabled',
          ] as const) {
            const value = params[key];
            if (value !== undefined && value !== '')
              rows = rows.filter((row) => String(row[key]) === String(value));
          }
          const size = params.pageSize ?? 20,
            start = ((params.current ?? 1) - 1) * size;
          return {
            data: rows.slice(start, start + size),
            total: rows.length,
            success: true,
          };
        }}
      />
      <Drawer
        title={selected?.summary ?? t('功能详情')}
        open={active && !!selected}
        onClose={() => setSelected(undefined)}
        width={900}
      >
        {selected && (
          <>
            <Descriptions
              bordered
              column={2}
              size="small"
              items={[
                {
                  key: 'name',
                  label: t('名称'),
                  children: (
                    <Typography.Text copyable>{selected.name}</Typography.Text>
                  ),
                  span: 2,
                },
                {
                  key: 'permission',
                  label: t('权限'),
                  children: selected.perm || t('登录即可'),
                },
                {
                  key: 'risk',
                  label: t('风险'),
                  children: (
                    <Tag color={RiskColor[selected.risk]}>
                      {RiskLabel[selected.risk]}
                    </Tag>
                  ),
                },
                {
                  key: 'entry',
                  label: t('开放入口'),
                  children: (
                    <Space>
                      {selected.aiEnabled && <Tag color="purple">AI</Tag>}
                      {selected.mcpEnabled && <Tag color="cyan">MCP</Tag>}
                      {!selected.aiEnabled &&
                        !selected.mcpEnabled &&
                        t('仅页面')}
                    </Space>
                  ),
                },
                {
                  key: 'undo',
                  label: t('可逆性'),
                  children: selected.mutating
                    ? selected.reversible
                      ? t('可回滚')
                      : t('不可回滚')
                    : t('只读'),
                },
              ]}
            />
            <Typography.Title level={5}>
              {t('输入 JSON Schema')}
            </Typography.Title>
            <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all rounded bg-slate-50 p-3">
              {pretty(selected.inputSchema)}
            </pre>
            <Typography.Title level={5}>{t('调用示例')}</Typography.Title>
            {selected.examples?.length ? (
              <Collapse
                items={selected.examples.map((example, index) => ({
                  key: index,
                  label: example.utterance,
                  children: (
                    <pre className="overflow-auto whitespace-pre-wrap">
                      {pretty(example.args)}
                    </pre>
                  ),
                }))}
              />
            ) : (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={t('此功能未声明自然语言示例')}
              />
            )}
            {selectedRoute && (
              <Button
                className="mt-4"
                onClick={() => history.push(selectedRoute)}
              >
                {t('前往管理页面')}
              </Button>
            )}
          </>
        )}
      </Drawer>
      <Drawer
        title={t('系统自省知识库')}
        open={active && knowledgeOpen}
        onClose={() => setKnowledgeOpen(false)}
        width={1000}
        extra={
          <Button onClick={() => void loadKnowledge()}>{t('刷新')}</Button>
        }
      >
        {loadingKnowledge ? (
          <Spin />
        ) : knowledge ? (
          <Tabs
            items={[
              {
                key: 'enums',
                label: t('字典语义 ({value0})', {
                  value0: knowledge.enums.length,
                }),
                children: (
                  <Table<EnumDef>
                    rowKey="dictType"
                    dataSource={knowledge.enums}
                    pagination={{ pageSize: 10 }}
                    columns={[
                      { title: t('字典'), dataIndex: 'dictType' },
                      { title: t('说明'), dataIndex: 'summary' },
                      {
                        title: t('取值'),
                        render: (_, item) => (
                          <Space wrap>
                            {Object.entries(item.values).map(
                              ([value, label]) => (
                                <Tag key={value}>
                                  {value}：{label}
                                </Tag>
                              ),
                            )}
                          </Space>
                        ),
                      },
                      {
                        title: t('应用字段'),
                        render: (_, item) => item.usedBy?.join('、') || '—',
                      },
                    ]}
                  />
                ),
              },
              {
                key: 'entities',
                label: t('实体与关系 ({value0})', {
                  value0: knowledge.entities.length,
                }),
                children: (
                  <Collapse
                    items={knowledge.entities.map((entity) => ({
                      key: entity.table,
                      label: `${entity.summary} (${entity.table})`,
                      children: (
                        <>
                          <Typography.Paragraph>
                            {t('主键：')}
                            {entity.keyField} {t(' · 名称字段：')}
                            {entity.labelField}
                          </Typography.Paragraph>
                          {entity.relations?.map((relation) => (
                            <Typography.Paragraph
                              key={`${relation.kind}:${relation.target}:${relation.via ?? ''}`}
                            >
                              {relation.summary}：{relation.kind} →{' '}
                              {relation.target}
                              {relation.via
                                ? t('，通过 {value0}', { value0: relation.via })
                                : ''}
                            </Typography.Paragraph>
                          ))}
                          {entity.notes?.map((note) => (
                            <Alert
                              className="mb-2"
                              key={note}
                              type="info"
                              title={note}
                            />
                          ))}
                        </>
                      ),
                    }))}
                  />
                ),
              },
              {
                key: 'rules',
                label: t('业务规则 ({value0})', {
                  value0: knowledge.rules.length,
                }),
                children: (
                  <Table<RuleDef>
                    rowKey={(row) => `${row.scope}:${row.rule}`}
                    dataSource={knowledge.rules}
                    pagination={false}
                    columns={[
                      { title: t('范围'), dataIndex: 'scope' },
                      { title: t('规则'), dataIndex: 'rule' },
                      {
                        title: t('代码约束'),
                        render: (_, row) => (
                          <Tag color={row.enforced ? 'green' : 'default'}>
                            {row.enforced ? t('已强制执行') : t('约定')}
                          </Tag>
                        ),
                      },
                    ]}
                  />
                ),
              },
            ]}
          />
        ) : (
          <Empty description={t('无法加载知识库')} />
        )}
      </Drawer>
    </PageContainer>
  );
}
