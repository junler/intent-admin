// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { Form, type FormInstance } from 'antd';
import { expect, it } from 'vitest';
import { Fields } from './Fields';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
Object.defineProperty(window, 'matchMedia', {
  value: () => ({
    matches: false,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
  }),
});
let form: FormInstance;
function Harness() {
  [form] = Form.useForm();
  return (
    <Form form={form} initialValues={{ lines: [{ name: 'A', quantity: 2 }] }}>
      <Fields
        fields={[
          {
            name: 'lines',
            label: 'Lines',
            type: 'items',
            fields: [
              { name: 'name', label: 'Name', required: true },
              { name: 'quantity', label: 'Quantity', type: 'number' },
            ],
          },
        ]}
      />
    </Form>
  );
}

it('keeps detail rows under the parent array when adding and removing rows', async () => {
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  try {
    await act(async () => root.render(<Harness />));
    let values: any;
    await act(async () => {
      values = await form.validateFields();
    });
    expect(values.lines).toEqual([{ name: 'A', quantity: 2 }]);
    await act(async () =>
      (
        Array.from(container.querySelectorAll('button')).find((button) =>
          button.textContent?.includes('添加明细'),
        ) as HTMLButtonElement
      ).click(),
    );
    await act(async () => form.setFieldValue(['lines', 1, 'name'], 'B'));
    await act(async () =>
      (
        container.querySelector('[aria-label="删除明细"]') as HTMLButtonElement
      ).click(),
    );
    expect(form.getFieldValue('lines')).toEqual([{ name: 'B' }]);
    expect(form.getFieldValue(['lines', 'lines'])).toBeUndefined();
  } finally {
    await act(async () => root.unmount());
    container.remove();
  }
});
