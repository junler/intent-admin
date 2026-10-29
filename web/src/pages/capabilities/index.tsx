import { useEffect, useMemo, useState } from 'react';
import { Alert, Card, Input, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';

import { RiskColor, RiskLabel, type Risk } from '@/api/types';

interface OpenAPIOperation {
  operationId?: string;
  summary?: string;
  description?: string;
  tags?: string[];
}

interface CapabilityRow {
  name: string;
  summary: string;
  method: string;
  path: string;
  perm: string;
  risk: Risk;
  reversible?: boolean;
  aiEnabled: boolean;
  mcpEnabled: boolean;
  tags: string[];
}

/**
 * Capability browser.
 *
 * "What can the AI actually do to my system?" must be answerable from the UI,
 * not by reading code. Until the dedicated endpoint lands in Phase 6 this page
 * derives the catalogue from the generated OpenAPI document, which already
 * carries permission, risk and reversibility in each operation description.
 */
export default function CapabilitiesPage() {
  const [rows, setRows] = useState<CapabilityRow[]>([]);
  const [keyword, setKeyword] = useState('');
  const [error, setError] = useState<string>();

  useEffect(() => {
    fetch('/openapi.json')
      .then((r) => r.json())
      .then((doc: { paths: Record<string, Record<string, OpenAPIOperation>> }) => {
        const out: CapabilityRow[] = [];
        for (const [path, methods] of Object.entries(doc.paths ?? {})) {
          for (const [method, op] of Object.entries(methods)) {
            if (!op.operationId || /\.(preview|export|template)$/.test(op.operationId)) continue;
            out.push({
              name: op.operationId,
              summary: op.summary ?? '',
              method: method.toUpperCase(),
              path,
              perm: pick(op.description, /所需权限：`?([^`\n]+)`?/),
              risk: (pick(op.description, /风险等级：.*?`(\w+)`/) || 'critical') as Risk,
              reversible: /可回滚：/.test(op.description ?? '')
                ? /可回滚：是/.test(op.description ?? '')
                : undefined,
              aiEnabled: /AI 可见：是/.test(op.description ?? ''),
              mcpEnabled: /MCP 可见：是/.test(op.description ?? ''),
              tags: op.tags ?? [],
            });
          }
        }
        out.sort((a, b) => a.name.localeCompare(b.name));
        setRows(out);
      })
      .catch((e: unknown) => setError(String(e)));
  }, []);

  const filtered = useMemo(() => {
    const kw = keyword.trim().toLowerCase();
    if (!kw) return rows;
    return rows.filter((r) =>
      `${r.name} ${r.summary} ${r.path} ${r.tags.join(' ')}`.toLowerCase().includes(kw),
    );
  }, [rows, keyword]);

  const columns: ColumnsType<CapabilityRow> = [
    {
      title: '功能',
      dataIndex: 'summary',
      width: 200,
      render: (summary: string, row) => (
        <div>
          <div>{summary}</div>
          <Typography.Text type="secondary" className="text-xs">
            {row.name}
          </Typography.Text>
        </div>
      ),
    },
    {
      title: '接口',
      key: 'http',
      width: 260,
      render: (_, row) => (
        <Typography.Text code className="text-xs">
          {row.method} {row.path}
        </Typography.Text>
      ),
    },
    {
      title: '权限',
      dataIndex: 'perm',
      width: 180,
      render: (p: string) => (p ? <Typography.Text code>{p}</Typography.Text> : '登录即可'),
    },
    {
      title: '风险',
      dataIndex: 'risk',
      width: 100,
      filters: (['low', 'medium', 'high', 'critical'] as Risk[]).map((r) => ({
        text: RiskLabel[r],
        value: r,
      })),
      onFilter: (value, row) => row.risk === value,
      render: (r: Risk) => <Tag color={RiskColor[r]}>{RiskLabel[r]}</Tag>,
    },
    {
      title: '可回滚',
      dataIndex: 'reversible',
      width: 90,
      render: (v: boolean | undefined) =>
        v === undefined ? '—' : v ? <Tag color="green">是</Tag> : <Tag color="red">否</Tag>,
    },
    {
      title: '开放入口',
      key: 'entry',
      width: 140,
      render: (_, row) => (
        <Space size={4}>
          {row.aiEnabled && <Tag color="purple">AI</Tag>}
          {row.mcpEnabled && <Tag color="cyan">MCP</Tag>}
          {!row.aiEnabled && !row.mcpEnabled && (
            <Typography.Text type="secondary">仅页面</Typography.Text>
          )}
        </Space>
      ),
    },
  ];

  return (
    <Card
      title="能力浏览器"
      extra={
        <Input.Search
          placeholder="搜索功能、路径、标签"
          allowClear
          className="w-72"
          onChange={(e) => setKeyword(e.target.value)}
        />
      }
    >
      <Alert
        type="info"
        showIcon
        className="mb-4"
        message="这里列出系统注册的全部 Capability"
        description="包含所需权限、风险等级、是否可回滚，以及是否对 AI 与 MCP 开放。配置类写操作（模型服务、存储配置、访问令牌）在启动时会被强制校验为不对 AI/MCP 开放。"
      />
      {error && <Alert type="error" showIcon message="加载能力目录失败" description={error} />}
      <Table<CapabilityRow>
        rowKey="name"
        size="small"
        columns={columns}
        dataSource={filtered}
        scroll={{ x: 1100 }}
        pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 项` }}
      />
    </Card>
  );
}

function pick(text: string | undefined, re: RegExp): string {
  return re.exec(text ?? '')?.[1]?.trim() ?? '';
}
