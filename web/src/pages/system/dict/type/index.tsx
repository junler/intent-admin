import { Button } from "antd";
import { useNavigate } from "@umijs/max";
import { CrudPage } from "@/components/admin/CrudPage";
import { statusOptions } from "@/components/admin/types";
import { useAdminAction } from "@/components/admin/useAdminAction";
import { Auth } from "@/components/auth/Auth";
export default function DictTypes() {
	const navigate = useNavigate();
	const action = useAdminAction();
	return (
		<>
			<CrudPage
				title="字典类型"
				base="/system/dict/type"
				idKey="dictId"
				perm="system:dict"
				extra={
					<Auth perm="system:dict:edit">
						<Button
							onClick={() =>
								void action.run({
									title: "刷新字典缓存",
									url: "/system/dict/refreshCache",
									method: "DELETE",
									previewUrl: "/system/dict/refreshCache/preview",
								})
							}
						>
							刷新缓存
						</Button>
					</Auth>
				}
				fields={[
					{ name: "dictName", label: "名称", required: true, search: true },
					{
						name: "dictType",
						label: "类型",
						required: true,
						search: true,
						render: (value) => (
							<Button
								type="link"
								onClick={() =>
									navigate(
										`/system/dict/data?dictType=${encodeURIComponent(value)}`,
									)
								}
							>
								{value}
							</Button>
						),
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
			{action.modal}
		</>
	);
}
