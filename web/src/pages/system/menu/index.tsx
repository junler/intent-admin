import { useEffect, useState } from 'react';
import { CrudPage } from '@/components/admin/CrudPage';
import { allRows } from '@/components/admin/data';
import { type Row, statusOptions } from '@/components/admin/types';
import { t } from '@/i18n';
import { isSafeExternalUrl } from '@/router/menu';
export default function Menus() {
  const [parents, setParents] = useState<Row[]>([]);
  useEffect(() => {
    void allRows('/system/menu').then(setParents);
  }, []);
  return (
    <CrudPage
      title={t('菜单与权限点')}
      base="/system/menu"
      idKey="menuId"
      perm="system:menu"
      tree
      fields={[
        { name: 'menuName', label: t('名称'), required: true, search: true },
        {
          name: 'parentId',
          label: t('上级'),
          type: 'select',
          defaultValue: 0,
          options: [
            { label: t('顶级'), value: 0 },
            ...parents
              .filter((m) => m.menuType !== 'F')
              .map((m) => ({ label: m.menuName, value: m.menuId })),
          ],
          column: false,
        },
        {
          name: 'menuType',
          label: t('类型'),
          type: 'select',
          required: true,
          defaultValue: 'C',
          options: [
            { label: t('目录'), value: 'M' },
            { label: t('菜单'), value: 'C' },
            { label: t('按钮'), value: 'F' },
          ],
        },
        { name: 'orderNum', label: t('排序'), type: 'number', defaultValue: 0 },
        {
          name: 'path',
          label: t('路由路径 / 外链 URL'),
          rules: [
            ({ getFieldValue }: any) => ({
              validator: (_: unknown, value: string) =>
                getFieldValue('isFrame') !== '0' ||
                isSafeExternalUrl(value ?? '')
                  ? Promise.resolve()
                  : Promise.reject(
                      new Error(t('外链必须是无账号密码的完整 HTTP(S) 地址')),
                    ),
            }),
          ],
        },
        {
          name: 'component',
          label: t('页面组件'),
          help: t('例如 system/user/index；目录使用 Layout。'),
        },
        {
          name: 'perms',
          label: t('权限字符'),
          help: t('菜单或按钮请填写，例如 system:user:list。'),
        },
        { name: 'icon', label: t('图标') },
        {
          name: 'query',
          label: t('默认参数 JSON'),
          column: false,
          rules: [
            {
              validator: (_: unknown, value: string) => {
                if (!value) return Promise.resolve();
                try {
                  const parsed = JSON.parse(value);
                  if (
                    parsed &&
                    typeof parsed === 'object' &&
                    !Array.isArray(parsed)
                  )
                    return Promise.resolve();
                } catch {}
                return Promise.reject(new Error(t('默认参数必须是 JSON 对象')));
              },
            },
          ],
        },
        {
          name: 'visible',
          label: t('侧栏显示'),
          type: 'select',
          defaultValue: '0',
          options: [
            { label: t('显示'), value: '0' },
            { label: t('隐藏但可访问'), value: '1' },
          ],
        },
        {
          name: 'isFrame',
          label: t('链接类型'),
          type: 'select',
          defaultValue: '1',
          options: [
            { label: t('站内页面'), value: '1' },
            { label: t('外部链接'), value: '0' },
          ],
          column: false,
        },
        {
          name: 'isCache',
          label: t('缓存'),
          type: 'select',
          defaultValue: '0',
          options: [
            { label: t('缓存'), value: '0' },
            { label: t('不缓存'), value: '1' },
          ],
          column: false,
        },
        {
          name: 'status',
          label: t('状态'),
          type: 'select',
          options: statusOptions,
          defaultValue: '0',
          search: true,
        },
      ]}
    />
  );
}
