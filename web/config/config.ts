import { defineConfig } from '@umijs/max';
import defaultSettings from './defaultSettings';
import routes from './routes';

// Adapted from the official Ant Design Pro v6 scaffold. Backend APIs replace
// the starter mock service; analytics and external demo assets are omitted.
export default defineConfig({
  title: 'Intent',
  hash: true,
  publicPath: '/',
  routes,
  npmClient: 'npm',
  model: {},
  initialState: {},
  access: {},
  request: {},
  layout: { locale: false, ...defaultSettings },
  antd: {
    appConfig: {},
    configProvider: {
      theme: { token: { colorPrimary: '#1677ff', borderRadius: 6 } },
    },
  },
  locale: { default: 'zh-CN', antd: true, baseNavigator: false },
  ignoreMomentLocale: true,
  tailwindcss: {},
  proxy: {
    '/api': {
      target: process.env.API_PROXY ?? 'http://127.0.0.1:8080',
      changeOrigin: true,
    },
    '/openapi.json': {
      target: process.env.API_PROXY ?? 'http://127.0.0.1:8080',
      changeOrigin: true,
    },
    '/healthz': {
      target: process.env.API_PROXY ?? 'http://127.0.0.1:8080',
      changeOrigin: true,
    },
  },
  define: { __API_BASE__: process.env.API_BASE ?? '/api' },
  mfsu: false,
  esbuildMinifyIIFE: true,
});
