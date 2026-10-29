import { CrudPage } from "@/components/admin/CrudPage";
import { statusOptions } from "@/components/admin/types";
export default function Notices() {
	return (
		<CrudPage
			title="通知公告"
			base="/system/notice"
			idKey="noticeId"
			perm="system:notice"
			fields={[
				{ name: "noticeTitle", label: "标题", required: true, search: true },
				{
					name: "noticeType",
					label: "类型",
					type: "select",
					required: true,
					defaultValue: "1",
					options: [
						{ label: "通知", value: "1" },
						{ label: "公告", value: "2" },
					],
					search: true,
				},
				{
					name: "noticeContent",
					label: "正文",
					type: "textarea",
					required: true,
				},
				{
					name: "status",
					label: "状态",
					type: "select",
					defaultValue: "0",
					options: statusOptions,
					search: true,
				},
				{ name: "remark", label: "备注", type: "textarea" },
			]}
		/>
	);
}
