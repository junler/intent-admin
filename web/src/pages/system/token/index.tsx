import { Alert, App, Typography } from "antd";
import { CrudPage } from "@/components/admin/CrudPage";
import { riskOptions } from "@/components/admin/types";
export default function Tokens() {
	const { modal } = App.useApp();
	return (
		<CrudPage
			title="个人访问令牌"
			base="/system/token"
			removePreviewUrl="/system/token/revoke/preview"
			idKey="tokenId"
			perm=""
			canEdit={false}
			onCreateResult={(data) =>
				modal.info({
					title: "请立即保存令牌",
					width: 650,
					content: (
						<>
							<Alert
								type="warning"
								title="明文只显示这一次，关闭后无法再次读取。"
								className="mb-3"
							/>
							<Typography.Paragraph copyable code>
								{data.token}
							</Typography.Paragraph>
						</>
					),
				})
			}
			fields={[
				{ name: "name", label: "名称", required: true, search: true },
				{ name: "prefix", label: "令牌前缀", form: false },
				{
					name: "scopes",
					label: "权限白名单",
					type: "multi",
					column: false,
					help: "填写完整权限字符；留空表示不额外收窄当前用户权限。",
				},
				{
					name: "maxRisk",
					label: "风险上限",
					type: "select",
					options: riskOptions,
					defaultValue: "low",
				},
				{ name: "expiresAt", label: "过期时间", type: "date" },
				{ name: "lastUsedAt", label: "最近使用", form: false },
				{ name: "createTime", label: "创建时间", form: false },
			]}
		/>
	);
}
