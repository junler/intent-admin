import {
  DatePicker,
  Form,
  Input,
  InputNumber,
  Select,
  Switch,
  TreeSelect,
} from 'antd';
import { t } from '@/i18n';
import { AiFormScope } from '@/components/ai/AiFormContext';
import { RemoteSelect } from './RemoteSelect';
import { FileField } from './FileField';
import { LineItems } from './LineItems';
import { CronInput } from './CronInput';
import type { Field } from './types';
import type { FormFocus } from '@/components/ai/panelState';
export function Fields({
  fields,
  editing = false,
  aiForm,
}: {
  fields: Field[];
  editing?: boolean;
  aiForm?: FormFocus;
}) {
  return (
    <AiFormScope value={aiForm}>
      {fields
        .filter(
          (f) => !f.hidden && f.form !== false && !(editing && f.createOnly),
        )
        .map((f) =>
          f.type === 'items' ? (
            <div key={f.name} className="col-span-full">
              <span>{f.label}</span>
              <LineItems
                name={f.name}
                fields={f.fields ?? []}
                maxCount={f.maxCount}
              />
            </div>
          ) : (
            <Form.Item
              className={`intent-field intent-field-${f.type ?? 'text'}`}
              key={f.name}
              name={f.namePath ?? f.name}
              label={f.label}
              extra={f.help}
              rules={[
                ...(f.required || (!editing && f.createRequired)
                  ? [
                      {
                        required: true,
                        message: t('请填写{value0}', { value0: f.label }),
                      },
                    ]
                  : []),
                ...(f.rules ?? []),
              ]}
              valuePropName={f.type === 'switch' ? 'checked' : 'value'}
            >
              {f.type === 'remote' && f.remote ? (
                <RemoteSelect source={f.remote} />
              ) : f.type === 'dateRange' ? (
                <DatePicker.RangePicker showTime className="w-full" />
              ) : f.type === 'file' || f.type === 'image' ? (
                <FileField image={f.type === 'image'} maxCount={f.maxCount} />
              ) : f.type === 'cron' ? (
                <CronInput />
              ) : f.type === 'tree' ? (
                <TreeSelect
                  treeCheckable
                  treeDefaultExpandAll
                  showSearch
                  treeNodeFilterProp="title"
                  treeData={f.treeData}
                  showCheckedStrategy={TreeSelect.SHOW_ALL}
                />
              ) : f.type === 'number' ? (
                <InputNumber className="w-full" />
              ) : f.type === 'select' || f.type === 'multi' ? (
                <Select
                  allowClear
                  showSearch
                  optionFilterProp="label"
                  mode={
                    f.type === 'multi'
                      ? f.options
                        ? 'multiple'
                        : 'tags'
                      : undefined
                  }
                  options={f.options}
                />
              ) : f.type === 'password' ? (
                <Input.Password
                  autoComplete="new-password"
                  placeholder={editing ? t('留空保留现有值') : undefined}
                />
              ) : f.type === 'textarea' || f.type === 'json' ? (
                <Input.TextArea rows={f.type === 'json' ? 6 : 3} />
              ) : f.type === 'switch' ? (
                <Switch />
              ) : f.type === 'date' ? (
                <DatePicker showTime className="w-full" />
              ) : (
                <Input allowClear />
              )}
            </Form.Item>
          ),
        )}
    </AiFormScope>
  );
}
