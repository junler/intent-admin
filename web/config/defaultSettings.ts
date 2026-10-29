import type { ProLayoutProps } from '@ant-design/pro-components';

// Derived from ant-design/ant-design-pro/config/defaultSettings.ts (MIT).
const settings: ProLayoutProps = {
  title: 'Intent',
  logo: '/logo.svg',
  navTheme: 'light',
  colorPrimary: '#2563eb',
  layout: 'mix',
  contentWidth: 'Fluid',
  fixedHeader: true,
  fixSiderbar: true,
  colorWeak: false,
  siderWidth: 236,
  token: {
    bgLayout: '#f4f7fb',
    header: {
      colorBgHeader: '#ffffff',
      colorHeaderTitle: '#172b4d',
      heightLayoutHeader: 60,
    },
    sider: {
      colorMenuBackground: '#ffffff',
      colorBgMenuItemSelected: '#edf3ff',
      colorTextMenuSelected: '#2563eb',
      colorTextMenu: '#56657b',
      colorTextMenuActive: '#2563eb',
    },
    pageContainer: {
      paddingInlinePageContainerContent: 28,
      paddingBlockPageContainerContent: 20,
    },
  },
};
export default settings;
