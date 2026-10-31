import {
  Alert,
  Empty,
  Pagination,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { useEffect, useState } from 'react';
import {
  type Change,
  type FieldDiff,
  OpLabel,
  type Preview,
} from '@/api/types';
import { DictTag } from '@/components/dict/DictTag';
import { t } from '@/i18n';
import { annotateDiffField } from './fieldMetadata';

const opColor: Record<Change['op'], string> = {
  create: 'green',
  update: 'blue',
  delete: 'red',
  skip: 'default',
};

/** Renders a single value, distinguishing empty from absent. */
function Value({
  raw,
  text,
  formatted = false,
}: {
  raw: unknown;
  text?: string;
  formatted?: boolean;
}) {
  const dictionary = text ? /^([a-z][a-z\d_]*):(.*)$/i.exec(text) : null;
  if (dictionary) return <DictTag type={dictionary[1]} value={dictionary[2]} />;
  if (raw === null || raw === undefined || raw === '') {
    return <Typography.Text type="secondary">{t('（空）')}</Typography.Text>;
  }
  if (formatted && !text) {
    let structured = raw;
    if (typeof raw === 'string' && /^[\[{]/.test(raw.trim())) {
      try {
        structured = JSON.parse(raw);
      } catch {
        // Preserve non-JSON business text exactly as returned.
      }
    }
    if (structured !== null && typeof structured === 'object') {
      return (
        <pre className="intent-diff-json">
          {JSON.stringify(structured, null, 2)}
        </pre>
      );
    }
  }
  return (
    <Typography.Text>
      {text ?? (typeof raw === 'object' ? JSON.stringify(raw) : String(raw))}
    </Typography.Text>
  );
}

function ChangeCards({ changes }: { changes: Change[] }) {
  const [page, setPage] = useState(1);
  useEffect(() => setPage(1), [changes]);
  return (
    <div className="intent-diff-cards">
      {changes.slice((page - 1) * 10, page * 10).map((change, index) => (
        <section
          className="intent-diff-record"
          key={`${page}-${index}-${change.entity}-${change.id}`}
        >
          <header className="intent-diff-record-header">
            <Tag color={opColor[change.op]}>{OpLabel[change.op]}</Tag>
            <Typography.Text strong>{change.label || '—'}</Typography.Text>
            {change.id && change.id !== '0' && (
              <Typography.Text
                type="secondary"
                className="intent-diff-record-id"
              >
                #{change.id}
              </Typography.Text>
            )}
          </header>
          {change.op === 'skip' ? (
            <Typography.Text type="secondary">
              {change.reason || t('无变化')}
            </Typography.Text>
          ) : (
            !!change.fields?.length && (
              <details className="intent-diff-details">
                <summary>{t('变更内容')}</summary>
                <dl className="intent-diff-fields">
                  {change.fields
                    .map((field) => annotateDiffField(change.entity, field))
                    .map((field) => (
                      <div className="intent-diff-field" key={field.field}>
                        <dt>{field.label || field.field}</dt>
                        <dd>
                          {change.op === 'update' ? (
                            <div className="intent-diff-comparison">
                              <div className="intent-diff-value intent-diff-value-before">
                                <span className="intent-diff-value-label">
                                  {t('原值')}
                                </span>
                                <Value
                                  raw={field.before}
                                  text={field.beforeText}
                                  formatted
                                />
                              </div>
                              <div className="intent-diff-value intent-diff-value-after">
                                <span className="intent-diff-value-label">
                                  {t('新值')}
                                </span>
                                <Value
                                  raw={field.after}
                                  text={field.afterText}
                                  formatted
                                />
                              </div>
                            </div>
                          ) : (
                            <div className="intent-diff-value">
                              <Value
                                raw={
                                  change.op === 'delete'
                                    ? field.before
                                    : field.after
                                }
                                text={
                                  change.op === 'delete'
                                    ? field.beforeText
                                    : field.afterText
                                }
                                formatted
                              />
                            </div>
                          )}
                        </dd>
                      </div>
                    ))}
                </dl>
              </details>
            )
          )}
        </section>
      ))}
      {changes.length > 10 && (
        <Pagination
          hideOnSinglePage
          size="small"
          current={page}
          pageSize={10}
          total={changes.length}
          showSizeChanger={false}
          onChange={setPage}
        />
      )}
    </div>
  );
}

function Fields({ fields, entity }: { fields: FieldDiff[]; entity: string }) {
  return (
    <div className="flex flex-col gap-1">
      {fields
        .map((field) => annotateDiffField(entity, field))
        .map((f) => (
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
  layout?: 'table' | 'cards';
}

/**
 * DiffTable renders a structured diff.
 *
 * This component is deliberately shared between the import preview
 * and the AI plan card. "Are you sure?" carries no information; showing which
 * records change, which fields, and from what to what is the difference
 * between a user approving blindly and approving knowingly.
 */
export function DiffTable({ preview, layout = 'table' }: DiffTableProps) {
  const rowIndices = new Map(
    (preview.changes ?? []).map((change, index) => [change, index]),
  );
  const columns: ColumnsType<Change> = [
    {
      title: t('操作'),
      dataIndex: 'op',
      width: 88,
      render: (op: Change['op']) => (
        <Tag color={opColor[op]}>{OpLabel[op]}</Tag>
      ),
    },
    {
      title: t('记录'),
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
      title: t('变更内容'),
      key: 'fields',
      render: (_, row) => {
        if (row.op === 'skip') {
          return (
            <Typography.Text type="secondary">
              {row.reason || t('无变化')}
            </Typography.Text>
          );
        }
        if (!row.fields?.length) {
          return <Typography.Text type="secondary">—</Typography.Text>;
        }
        return <Fields fields={row.fields} entity={row.entity} />;
      },
    },
  ];

  return (
    <div className="intent-diff flex flex-col gap-3">
      <Space size={8} wrap>
        <Typography.Text strong>{preview.summary}</Typography.Text>
        <Tag color="blue">
          {t('影响 ')}
          {preview.affected} {t(' 条')}
        </Tag>
        {preview.skipped > 0 && (
          <Tag>
            {t('跳过 ')}
            {preview.skipped} {t(' 条')}
          </Tag>
        )}
      </Space>

      {preview.warnings?.map((w) => (
        <Alert key={w} type="warning" showIcon message={w} />
      ))}

      {preview.changes?.length ? (
        layout === 'cards' ? (
          <ChangeCards changes={preview.changes} />
        ) : (
          <Table<Change>
            rowKey={(row) =>
              `${rowIndices.get(row)}#${row.entity}#${row.id}#${row.op}`
            }
            size="small"
            columns={columns}
            dataSource={preview.changes}
            pagination={
              preview.changes.length > 10
                ? { pageSize: 10, size: 'small', hideOnSinglePage: true }
                : false
            }
            scroll={{ y: 360 }}
          />
        )
      ) : (
        <Empty
          description={t('没有需要变更的记录')}
          image={Empty.PRESENTED_IMAGE_SIMPLE}
        />
      )}

      {preview.truncated && (
        <Typography.Text type="secondary" className="text-xs">
          {t('变更条目过多，仅列出前 200 条；上方的影响条数是完整统计。')}
        </Typography.Text>
      )}
    </div>
  );
}
