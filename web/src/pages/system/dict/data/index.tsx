import { useSearchParams } from "@umijs/max";
import { CrudPage } from "@/components/admin/CrudPage";
import { statusOptions } from "@/components/admin/types";
export default function DictData() {
	const [query] = useSearchParams();
	const dictType = query.get("dictType") ?? "";
	return (
		<CrudPage
			title={`字典数据${dictType ? " · " + dictType : ""}`}
			base="/system/dict/data"
			idKey="dictCode"
			perm="system:dict"
			export
			params={dictType ? { dictType } : undefined}
			fields={[
				{
					name: "dictType",
					label: "字典类型",
					required: true,
					search: true,
					defaultValue: dictType,
				},
				{ name: "dictLabel", label: "标签", required: true, search: true },
				{ name: "dictValue", label: "值", required: true },
				{ name: "dictSort", label: "排序", type: "number", defaultValue: 0 },
				{
					name: "listClass",
					label: "标签颜色",
					type: "select",
					options: [
						"default",
						"success",
						"warning",
						"error",
						"processing",
						"blue",
						"green",
						"red",
						"orange",
						"purple",
						"cyan",
					].map((value) => ({ label: value, value })),
					defaultValue: "default",
				},
				{ name: "cssClass", label: "CSS 类名", column: false },
				{
					name: "isDefault",
					label: "默认项",
					type: "select",
					defaultValue: "N",
					options: [
						{ label: "是", value: "Y" },
						{ label: "否", value: "N" },
					],
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
		/>
	);
}
