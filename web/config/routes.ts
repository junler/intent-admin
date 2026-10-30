// Ant Design Pro / Umi Max owns routing; DynamicPage resolves authorized menus.
export default [
  { path: '/oauth/consent', component: './oauth/consent', layout: false },
  { path: '/login', component: './login', layout: false },
  { path: '/', component: './dynamic' },
  { path: '/*', component: './dynamic' },
];
