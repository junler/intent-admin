// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { Form, type FormInstance } from 'antd';
import { expect, it, vi } from 'vitest';
import { Fields } from './Fields';

const input = vi.hoisted(() => ({
  onChange: (_value: string) => {},
  onBlur: () => {},
}));
vi.mock('./CronInput', () => ({
  CronInput: (props: typeof input) => {
    Object.assign(input, props);
    return null;
  },
}));
vi.mock('@/components/ai/AiFormContext', () => ({
  AiFormScope: ({ children }: { children: React.ReactNode }) => children,
}));

const validator = vi.fn().mockResolvedValue(undefined);
let form!: FormInstance;
function Harness() {
  [form] = Form.useForm();
  return (
    <Form form={form}>
      <Fields
        fields={[
          {
            name: 'cronExpression',
            label: 'Schedule',
            type: 'cron',
            rules: [{ validator }],
          },
        ]}
      />
    </Form>
  );
}

it('validates cron on blur and submit, not every keystroke', async () => {
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
  window.matchMedia = vi.fn().mockReturnValue({
    matches: false,
    addListener: vi.fn(),
    removeListener: vi.fn(),
  });
  const container = document.createElement('div');
  const root = createRoot(container);
  try {
    await act(async () => root.render(<Harness />));
    await act(async () => {
      input.onChange('0');
      input.onChange('0 9 * * *');
    });
    expect(validator).not.toHaveBeenCalled();
    await act(async () => {
      input.onBlur();
    });
    expect(validator).toHaveBeenCalledTimes(1);
    await act(async () => {
      await form.validateFields();
    });
    expect(validator).toHaveBeenCalledTimes(2);
  } finally {
    await act(async () => root.unmount());
  }
});
