import { CheckOutlined, GlobalOutlined } from '@ant-design/icons';
import { setLocale } from '@umijs/max';
import { Button, Dropdown, Space } from 'antd';
import { getLocale, type Locale, t } from './index';

export default function LocaleSwitcher() {
  const locale = getLocale();
  return (
    <Dropdown
      trigger={['click']}
      menu={{
        selectedKeys: [locale],
        items: [
          {
            key: 'zh-CN',
            label: (
              <Space>简体中文{locale === 'zh-CN' && <CheckOutlined />}</Space>
            ),
          },
          {
            key: 'en-US',
            label: (
              <Space>English{locale === 'en-US' && <CheckOutlined />}</Space>
            ),
          },
        ],
        onClick: ({ key }) => {
          if (key !== locale) setLocale(key as Locale, true);
        },
      }}
    >
      <Button
        type="text"
        icon={<GlobalOutlined />}
        aria-label={t('切换语言')}
        title={t('切换语言')}
      />
    </Dropdown>
  );
}
