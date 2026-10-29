import { App, Button, Descriptions, Space } from "antd";
import { CrudPage } from "./CrudPage";
import { Auth } from "@/components/auth/Auth";
import { useAdminAction } from "./useAdminAction";
import type { Field, RowAction } from "./types";
export function LogPage({
	title,
	base,
	idKey,
	perm,
	fields,
	actions = [],
}: {
	title: string;
	base: string;
	idKey: string;
	perm: string;
	fields: Field[];
	actions?: RowAction[];
}) {
	const { modal } = App.useApp();
	const mutation = useAdminAction();
	return (
		<>
			<CrudPage
				title={title}
				base={base}
				idKey={idKey}
				perm={perm}
				canCreate={false}
				canEdit={false}
				export
				fields={fields}
				extra={
					<Space>
						<Auth perm={`${perm}:remove`}>
							<Button
								danger
								onClick={() =>
									void mutation.run({
										title: `清空${title}`,
										url: `${base}/clean`,
										method: "DELETE",
										previewUrl: `${base}/clean/preview`,
									})
								}
							>
								清空日志
							</Button>
						</Auth>
					</Space>
				}
				actions={[
					{
						label: "详情",
						perm: `${perm}:list`,
						readOnly: true,
						execute: async (row) => row,
						result: (row) =>
							modal.info({
								title: "日志详情",
								width: 850,
								content: (
									<Descriptions
										column={1}
										items={Object.entries(row).map(([key, value]) => ({
											key,
											label: key,
											children: (
												<pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all">
													{typeof value === "object"
														? JSON.stringify(value, null, 2)
														: String(value ?? "")}
												</pre>
											),
										}))}
									/>
								),
							}),
					},
					...actions,
				]}
			/>
			{mutation.modal}
		</>
	);
}
