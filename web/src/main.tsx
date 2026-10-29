import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { StyleProvider } from '@ant-design/cssinjs';
import { App as AntApp, ConfigProvider, theme } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import { RouterProvider } from 'react-router';

import { router } from '@/router';
import '@/styles/global.css';

const root = document.getElementById('root');
if (!root) throw new Error('#root not found');

createRoot(root).render(
  <StrictMode>
    {/* `layer` wraps antd's generated CSS in @layer antd so Tailwind
        utilities win. ConfigProvider must be inside it for icon styles to
        pick up the same layer. */}
    <StyleProvider layer>
      <ConfigProvider
        locale={zhCN}
        theme={{
          algorithm: theme.defaultAlgorithm,
          token: { colorPrimary: '#1677ff', borderRadius: 6 },
        }}
      >
        {/* AntApp provides the message/notification/modal instances used by
            the request interceptor, which has no React context of its own. */}
        <AntApp>
          <RouterProvider router={router} />
        </AntApp>
      </ConfigProvider>
    </StyleProvider>
  </StrictMode>,
);
