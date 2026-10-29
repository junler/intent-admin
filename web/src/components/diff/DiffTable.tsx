import { Alert, Empty, Space, Table, Tag, Tooltip, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';

import { OpLabel, type Change, type FieldDiff, type Preview } from '@/api/types';

const opColor: Record<Change['op'], string> = {
  create: 'green',
  update: 'blue',
  delete: 'red',
  skip: 'default',
};

/** Renders a single value, distinguishing empty from absent. */
function Value({ raw, text }: { raw: unknown; text?: string }) {
  // beforeText/afterText arrive as "dictType:rawValue"; the frontend owns the
  // dictionary cache, so only the raw part is meaningful until that cache is
  // wired up in Phase 3. Showing the raw value is honest in the meantime.
  const display = text ? text.split(':').slice(1).join(':') || text : undefined;
  if (raw === null || raw === undefined || raw === '') {
    return <Typography.Text type="secondary">（空）</Typography.Text>;
  }
  return <Typography.Text>{display ?? String(raw)}</Typography.Text>;
}

function Fields({ fields }: { fields: FieldDiff[] }) {
  return (
    <div className="flex flex-col gap-1">
      {fields.map((f) => (
        <div key={f.field} className="flex items-center gap-2 text-sm">
          <span className="min-w-20 text-gray-500">{f.label || f.field}</span>
          <Value raw={f.before} text={f.beforeText} />
          <span className="text-gray-400">→</span>
          <Typography.Text strong>
            <Value raw={f.after} text={f.afterText} />
          </Typography.Text>
        </div>
      ))}
    </div>
  );
}

export interface DiffTableProps {
  preview: Preview;
}

/**
 * DiffTable renders a structured diff.
 *
 * This component is deliberately shared between the page confirmation modal
 * and the AI plan card. "Are you sure?" carries no information; showing which
 * records change, which fields, and from what to what is the difference
 * between a user approving blindly and approving knowingly.
 */
export function DiffTable({ preview }: DiffTableProps) {
  const columns: ColumnsType<Change> = [
    {
      title: '操作',
      dataIndex: 'op',
      width: 88,
      render: (op: Change['op']) => <Tag color={opColor[op]}>{OpLabel[op]}</Tag>,
    },
    {
      title: '记录',
      dataIndex: 'label',
      width: 220,
      render: (label: string | undefined, row) => (
        <Space size={4}>
          <Typography.Text>{label || '—'}</Typography.Text>
          <Tooltip title={`${row.entity}#${row.id}`}>
            <Typography.Text type="secondary" className="text-xs">
              #{row.id}
            </Typography.Text>
          </Tooltip>
        </Space>
      ),
    },
    {
      title: '变更内容',
      key: 'fields',
      render: (_, row) => {
        if (row.op === 'skip') {
          return <Typography.Text type="secondary">{row.reason || '无变化'}</Typography.Text>;
        }
        if (!row.fields?.length) {
          return <Typography.Text type="secondary">—</Typography.Text>;
        }
        return <Fields fields={row.fields} />;
      },
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <Space size={8} wrap>
        <Typography.Text strong>{preview.summary}</Typography.Text>
        <Tag color="blue">影响 {preview.affected} 条</Tag>
        {preview.skipped > 0 && <Tag>跳过 {preview.skipped} 条</Tag>}
        {!preview.reversible && <Tag color="red">不可回滚</Tag>}
      </Space>

      {/* An irreversible operation is the one case where emphasis is
          warranted: there is no undo to fall back on. */}
      {!preview.reversible && (
        <Alert
          type="error"
          showIcon
          message="此操作不可回滚"
          description="执行后无法通过变更历史撤销，请确认影响范围后再继续。"
        />
      )}

      {preview.warnings?.map((w) => (
        <Alert key={w} type="warning" showIcon message={w} />
      ))}

      {preview.changes?.length ? (
        <Table<Change>
          rowKey={(row) => `${row.entity}#${row.id}#${row.op}`}
          size="small"
          columns={columns}
          dataSource={preview.changes}
          pagination={preview.changes.length > 10 ? { pageSize: 10, size: 'small' } : false}
          scroll={{ y: 360 }}
        />
      ) : (
        <Empty description="没有需要变更的记录" image={Empty.PRESENTED_IMAGE_SIMPLE} />
      )}

      {preview.truncated && (
        <Typography.Text type="secondary" className="text-xs">
          变更条目过多，仅列出前 200 条；上方的影响条数是完整统计。
        </Typography.Text>
      )}
    </div>
  );
}
