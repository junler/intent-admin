// Ant Design Pro / Umi Max owns routing; DynamicPage resolves authorized menus.
export default [
  { path: '/login', component: './login', layout: false },
  { path: '/', component: './dynamic' },
  { path: '/*', component: './dynamic' },
];
