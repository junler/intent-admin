import { useEffect, useState } from 'react';
import { menuTreeOptions } from '@/components/admin/actions';
import { CrudPage } from '@/components/admin/CrudPage';
import { allRows } from '@/components/admin/data';
import { type Row, statusOptions } from '@/components/admin/types';
import { t } from '@/i18n';
import { api } from '@/utils/request';
export default function Roles() {
  const [menus, setMenus] = useState<Row[]>([]);
  useEffect(() => {
    void allRows('/system/menu').then(setMenus);
  }, []);
  return (
    <CrudPage
      title={t('角色管理')}
      base="/system/role"
      idKey="roleId"
      perm="system:role"
      export
      import
      fields={[
        {
          name: 'roleName',
          label: t('角色名称'),
          required: true,
          search: true,
        },
        { name: 'roleKey', label: t('角色标识'), required: true, search: true },
        { name: 'roleSort', label: t('排序'), type: 'number', defaultValue: 0 },
        {
          name: 'menuCheckStrictly',
          label: t('父子独立选择'),
          type: 'switch',
          defaultValue: false,
        },
        {
          name: 'status',
          label: t('状态'),
          type: 'select',
          options: statusOptions,
          defaultValue: '0',
          search: true,
        },
        { name: 'remark', label: t('备注'), type: 'textarea' },
      ]}
      actions={[
        {
          label: t('菜单授权'),
          perm: 'system:role:edit',
          url: '/system/role/authMenu',
          previewUrl: '/system/role/authMenu/preview',
          fields: [
            {
              name: 'menuIds',
              label: t('授权菜单与权限点'),
              type: 'tree',
              treeData: menuTreeOptions(menus),
            },
          ],
          defaults: (r) => api.get<Row>(`/system/role/${r.roleId}`),
          args: (r, v) => ({ roleId: r.roleId, menuIds: v.menuIds ?? [] }),
        },
      ]}
    />
  );
}
