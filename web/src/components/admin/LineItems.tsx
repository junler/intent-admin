import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons';
import { Button, Form, Space } from 'antd';
import { t } from '@/i18n';
import { Fields } from './Fields';
import type { Field } from './types';

// Form.List keeps validation and value ownership in the parent business form.
export function LineItems({
  name,
  fields,
  maxCount = 100,
}: {
  name: string;
  fields: Field[];
  maxCount?: number;
}) {
  return (
    <Form.List name={name}>
      {(rows, { add, remove }) => (
        <>
          {rows.map((row) => (
            <Space key={row.key} align="start" className="flex flex-wrap">
              <LineItemFields prefix={[row.name]} fields={fields} />

              <Button
                aria-label={t('删除明细')}
                icon={<MinusCircleOutlined />}
                onClick={() => remove(row.name)}
              />
            </Space>
          ))}
          <Button
            icon={<PlusOutlined />}
            disabled={rows.length >= maxCount}
            onClick={() => add({})}
          >
            {t('添加明细')}
          </Button>
        </>
      )}
    </Form.List>
  );
}
function LineItemFields({
  prefix,
  fields,
}: {
  prefix: (string | number)[];
  fields: Field[];
}) {
  return (
    <Fields
      fields={fields.map((field) => ({
        ...field,
        namePath: [...prefix, field.name],
      }))}
    />
  );
}
