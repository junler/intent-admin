import { useEffect, useState } from "react";
import { CrudPage } from "@/components/admin/CrudPage";
import { isSafeExternalUrl } from "@/router/menu";
import { statusOptions, type Row } from "@/components/admin/types";
import { allRows } from "@/components/admin/data";
export default function Menus() {
	const [parents, setParents] = useState<Row[]>([]);
	useEffect(() => {
		void allRows("/system/menu").then(setParents);
	}, []);
	return (
		<CrudPage
			title="菜单与权限点"
			base="/system/menu"
			idKey="menuId"
			perm="system:menu"
			tree
			fields={[
				{ name: "menuName", label: "名称", required: true, search: true },
				{
					name: "parentId",
					label: "上级",
					type: "select",
					defaultValue: 0,
					options: [
						{ label: "顶级", value: 0 },
						...parents
							.filter((m) => m.menuType !== "F")
							.map((m) => ({ label: m.menuName, value: m.menuId })),
					],
					column: false,
				},
				{
					name: "menuType",
					label: "类型",
					type: "select",
					required: true,
					defaultValue: "C",
					options: [
						{ label: "目录", value: "M" },
						{ label: "菜单", value: "C" },
						{ label: "按钮", value: "F" },
					],
				},
				{ name: "orderNum", label: "排序", type: "number", defaultValue: 0 },
				{
					name: "path",
					label: "路由路径 / 外链 URL",
					rules: [
						({ getFieldValue }: any) => ({
							validator: (_: unknown, value: string) =>
								getFieldValue("isFrame") !== "0" ||
								isSafeExternalUrl(value ?? "")
									? Promise.resolve()
									: Promise.reject(
											new Error("外链必须是无账号密码的完整 HTTP(S) 地址"),
										),
						}),
					],
				},
				{
					name: "component",
					label: "页面组件",
					help: "例如 system/user/index；目录使用 Layout。",
				},
				{
					name: "perms",
					label: "权限字符",
					help: "菜单或按钮请填写，例如 system:user:list。",
				},
				{ name: "icon", label: "图标" },
				{
					name: "query",
					label: "默认参数 JSON",
					column: false,
					rules: [
						{
							validator: (_: unknown, value: string) => {
								if (!value) return Promise.resolve();
								try {
									const parsed = JSON.parse(value);
									if (
										parsed &&
										typeof parsed === "object" &&
										!Array.isArray(parsed)
									)
										return Promise.resolve();
								} catch {}
								return Promise.reject(new Error("默认参数必须是 JSON 对象"));
							},
						},
					],
				},
				{
					name: "visible",
					label: "侧栏显示",
					type: "select",
					defaultValue: "0",
					options: [
						{ label: "显示", value: "0" },
						{ label: "隐藏但可访问", value: "1" },
					],
				},
				{
					name: "isFrame",
					label: "链接类型",
					type: "select",
					defaultValue: "1",
					options: [
						{ label: "站内页面", value: "1" },
						{ label: "外部链接", value: "0" },
					],
					column: false,
				},
				{
					name: "isCache",
					label: "缓存",
					type: "select",
					defaultValue: "0",
					options: [
						{ label: "缓存", value: "0" },
						{ label: "不缓存", value: "1" },
					],
					column: false,
				},
				{
					name: "status",
					label: "状态",
					type: "select",
					options: statusOptions,
					defaultValue: "0",
					search: true,
				},
			]}
		/>
	);
}
