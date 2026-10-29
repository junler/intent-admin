import { Button, Form, Input, Modal, Select, Space, Typography } from 'antd';
import { useState } from 'react';
import { usePageActive } from '@/contexts/ActivePageContext';
import { t } from '@/i18n';
import { validCron } from './cron';
export const cronPresets = [
  { label: t('每 5 分钟'), value: '*/5 * * * *' },
  { label: t('每小时整点'), value: '0 * * * *' },
  { label: t('每天 09:00'), value: '0 9 * * *' },
  { label: t('工作日 09:00'), value: '0 9 * * MON-FRI' },
  { label: t('每周一 09:00'), value: '0 9 * * MON' },
  { label: t('每月 1 日 00:00'), value: '0 0 1 * *' },
  { label: t('每隔 5 分钟'), value: '@every 5m' },
];
export function cronFields(value: string): string[] {
  const fields = value.trim().split(/\s+/);
  return fields.length === 5 && !value.includes('?')
    ? fields
    : ['*', '*', '*', '*', '*'];
}
export function CronInput({
  value = '',
  onChange,
}: {
  value?: string;
  onChange?: (value: string) => void;
}) {
  const active = usePageActive();
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm();
  const watched = Form.useWatch([], form) ?? {};
  const names = ['minute', 'hour', 'day', 'month', 'week'];
  const titles = [
    t('分钟（0–59）'),
    t('小时（0–23）'),
    t('日（1–31）'),
    t('月（1–12）'),
    t('周（0–6 / MON–SUN）'),
  ];
  const generated = names.map((name) => watched[name] || '*').join(' ');
  function show() {
    form.setFieldsValue(
      Object.fromEntries(
        names.map((name, index) => [name, cronFields(value)[index]]),
      ),
    );
    setOpen(true);
  }
  function apply() {
    onChange?.(generated);
    setOpen(false);
  }
  return (
    <>
      <Space.Compact className="w-full">
        <Input
          value={value}
          onChange={(event) => onChange?.(event.target.value)}
          placeholder={t('分 时 日 月 周')}
        />
        <Button onClick={show}>{t('生成器')}</Button>
      </Space.Compact>
      <Select<string>
        className="mt-2 w-full"
        placeholder={t('选择常用执行计划')}
        options={cronPresets}
        value={undefined}
        onChange={(next) => onChange?.(next)}
      />
      <Modal
        title={t('生成标准五字段 cron')}
        open={active && open}
        onCancel={() => setOpen(false)}
        onOk={apply}
        okButtonProps={{ disabled: !validCron(generated) }}
      >
        <Typography.Paragraph type="secondary">
          {t(
            '* 表示每个值；支持逗号列表、范围和步长，例如 1,15、MON-FRI、*/5。执行时区由服务配置决定。',
          )}
        </Typography.Paragraph>
        <Form form={form} layout="vertical">
          {names.map((name, index) => (
            <Form.Item key={name} label={titles[index]} name={name}>
              <Input placeholder="*" />
            </Form.Item>
          ))}
        </Form>
        <Typography.Paragraph copyable code>
          {generated}
        </Typography.Paragraph>
      </Modal>
    </>
  );
}
