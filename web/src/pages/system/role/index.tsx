import { useEffect, useState } from "react";
import { CrudPage } from "@/components/admin/CrudPage";
import { menuTreeOptions } from "@/components/admin/actions";
import { statusOptions, type Row } from "@/components/admin/types";
import { api } from "@/utils/request";
import { allRows } from "@/components/admin/data";
export default function Roles() {
	const [menus, setMenus] = useState<Row[]>([]);
	useEffect(() => {
		void allRows("/system/menu").then(setMenus);
	}, []);
	return (
		<CrudPage
			title="角色管理"
			base="/system/role"
			idKey="roleId"
			perm="system:role"
			export
			fields={[
				{ name: "roleName", label: "角色名称", required: true, search: true },
				{ name: "roleKey", label: "角色标识", required: true, search: true },
				{ name: "roleSort", label: "排序", type: "number", defaultValue: 0 },
				{
					name: "menuCheckStrictly",
					label: "父子独立选择",
					type: "switch",
					defaultValue: false,
				},
				{
					name: "status",
					label: "状态",
					type: "select",
					options: statusOptions,
					defaultValue: "0",
					search: true,
				},
				{ name: "remark", label: "备注", type: "textarea" },
			]}
			actions={[
				{
					label: "菜单授权",
					perm: "system:role:edit",
					url: "/system/role/authMenu",
					previewUrl: "/system/role/authMenu/preview",
					fields: [
						{
							name: "menuIds",
							label: "授权菜单与权限点",
							type: "tree",
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
