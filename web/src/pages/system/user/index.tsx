import { useEffect, useState } from 'react';
import { CrudPage } from '@/components/admin/CrudPage';
import { allRows } from '@/components/admin/data';
import {
  type Field,
  type Row,
  riskOptions,
  statusOptions,
} from '@/components/admin/types';
import { t } from '@/i18n';
import { api } from '@/utils/request';
export default function Users() {
  const [roles, setRoles] = useState<Row[]>([]);
  const [menus, setMenus] = useState<Row[]>([]);
  useEffect(() => {
    void allRows('/system/role').then(setRoles);
    void allRows('/system/menu').then(setMenus);
  }, []);
  const fields: Field[] = [
    { name: 'userName', label: t('账号'), required: true, search: true },
    { name: 'nickName', label: t('昵称'), required: true, search: true },
    {
      name: 'password',
      label: t('初始密码'),
      type: 'password',
      required: true,
      createOnly: true,
    },
    { name: 'email', label: t('邮箱'), rules: [{ type: 'email' }] },
    { name: 'phonenumber', label: t('手机') },
    {
      name: 'sex',
      label: t('性别'),
      type: 'select',
      defaultValue: '2',
      options: [
        { label: t('男'), value: '0' },
        { label: t('女'), value: '1' },
        { label: t('未知'), value: '2' },
      ],
    },
    {
      name: 'status',
      label: t('状态'),
      type: 'select',
      options: statusOptions,
      defaultValue: '0',
      search: true,
    },
    {
      name: 'roleIds',
      label: t('角色'),
      type: 'multi',
      column: false,
      options: roles.map((r) => ({ label: r.roleName, value: r.roleId })),
    },
    { name: 'remark', label: t('备注'), type: 'textarea' },
  ];
  const detail = (row: Row) => api.get<Row>(`/system/user/${row.userId}`);
  return (
    <CrudPage
      title={t('用户管理')}
      base="/system/user"
      idKey="userId"
      perm="system:user"
      fields={fields}
      export
      import
      actions={[
        {
          label: t('角色'),
          perm: 'system:user:edit',
          url: '/system/user/authRole',
          previewUrl: '/system/user/authRole/preview',
          fields: [fields.find((f) => f.name === 'roleIds')!],
          defaults: detail,
          args: (r, v) => ({ userId: r.userId, roleIds: v.roleIds ?? [] }),
        },
        {
          label: t('额外授权'),
          perm: 'system:user:authPerm',
          url: '/system/user/authPerm',
          previewUrl: '/system/user/authPerm/preview',
          fields: [
            {
              name: 'permissions',
              label: t('额外权限（仅叠加角色已有权限）'),
              type: 'multi',
              options: menus
                .filter((m) => m.perms)
                .map((m) => ({
                  label: `${m.menuName} · ${m.perms}`,
                  value: m.perms,
                })),
            },
          ],
          defaults: detail,
          args: (r, v) => ({
            userId: r.userId,
            permissions: v.permissions ?? [],
          }),
        },
        {
          label: t('AI 上限'),
          perm: 'system:user:authPerm',
          url: '/system/user/aiRisk',
          previewUrl: '/system/user/aiRisk/setAiRisk/preview',
          fields: [
            {
              name: 'aiMaxRisk',
              label: t('AI 风险上限'),
              type: 'select',
              options: [{ label: t('跟随全局'), value: '' }, ...riskOptions],
              help: t('只能收窄，不能提高全局上限。'),
            },
          ],
          defaults: detail,
          args: (r, v) => ({
            userId: r.userId,
            aiMaxRisk: v.aiMaxRisk || null,
          }),
        },
        {
          label: t('改密'),
          perm: 'system:user:resetPwd',
          url: '/system/user/resetPwd',
          previewUrl: '/system/user/resetPwd/preview',
          fields: [
            {
              name: 'password',
              label: t('新密码'),
              type: 'password',
              required: true,
              rules: [{ min: 6 }],
            },
          ],
          defaults: () => ({}),
          args: (r, v) => ({ userId: r.userId, password: v.password }),
        },
        {
          label: t('启停'),
          perm: 'system:user:edit',
          url: '/system/user/changeStatus',
          previewUrl: '/system/user/changeStatus/preview',
          args: (r) => ({
            userId: r.userId,
            status: r.status === '0' ? '1' : '0',
          }),
        },
      ]}
    />
  );
}
