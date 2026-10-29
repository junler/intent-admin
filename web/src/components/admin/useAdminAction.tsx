import { useState } from "react";
import { usePageActive } from "@/contexts/ActivePageContext";
import { Alert, App, Modal, Button, Space } from "antd";
import { history } from "@umijs/max";
import { usePerm } from "@/hooks/usePerm";
import { DiffTable } from "@/components/diff/DiffTable";
import type { Preview } from "@/api/types";
import { api, request, ApiError, Code } from "@/utils/request";
import { invalidateDicts } from "@/hooks/useDict";
import type { Row } from "./types";
interface Command {
	title: string;
	url: string;
	method: "POST" | "PUT" | "DELETE";
	args?: Row;
	previewUrl?: string;
	execute?: () => Promise<unknown>;
	result?: (value: any) => void;
	after?: () => void;
}
export function useAdminAction(after?: () => void) {
	const active = usePageActive();
	const canViewChanges = usePerm("system:changeset:list");
	const { message, modal: dialogs } = App.useApp();
	const [command, setCommand] = useState<Command>();
	const [preview, setPreview] = useState<Preview>();
	const [loading, setLoading] = useState(false);
	const [executing, setExecuting] = useState(false);
	async function run(next: Command) {
		setCommand(next);
		setPreview(undefined);
		setLoading(true);
		try {
			if (next.previewUrl)
				setPreview(await api.post<Preview>(next.previewUrl, next.args));
			else
				setPreview({
					summary: next.title,
					affected: 1,
					skipped: 0,
					reversible: false,
					warnings: ["此操作执行后可能无法撤销，请确认。"],
				});
		} catch {
			setCommand(undefined);
		} finally {
			setLoading(false);
		}
	}
	async function confirm() {
		if (!command) return;
		setExecuting(true);
		try {
			const value = await (command.execute
				? command.execute()
				: request({
						url: command.url,
						method: command.method,
						data: command.args,
					}));
			if (command.url.startsWith("/system/dict")) invalidateDicts();
			if (
				/^\/system\/(?:menu|role)(?:\/|$)|^\/system\/user\/(?:authRole|authPerm|aiRisk)$/.test(
					command.url,
				)
			)
				window.dispatchEvent(new Event("intent-admin:permissions-changed"));
			command.result?.(value);
			const changesetId = (value as Row | undefined)?.changesetId;
			message.success({
				duration: changesetId ? 8 : 3,
				content: (
					<Space>
						操作成功
						{changesetId && canViewChanges && (
							<Button
								type="link"
								size="small"
								onClick={() =>
									history.push(
										`/changesets?changesetId=${encodeURIComponent(changesetId)}`,
									)
								}
							>
								查看变更
							</Button>
						)}
					</Space>
				),
			});
			setCommand(undefined);
			command.after?.();
			after?.();
		} catch (error) {
			if (error instanceof ApiError && error.code === Code.StaleData) {
				dialogs.warning({
					title: "数据已变化",
					content: "请重新预览并确认最新的变更范围。",
					onOk: () => run(command),
				});
			} else if (
				error instanceof ApiError &&
				error.code === Code.RiskExceeded
			) {
				dialogs.info({ title: "风险上限限制", content: error.message });
			}
		} finally {
			setExecuting(false);
		}
	}
	const modal = (
		<Modal
			title={command?.title}
			open={active && !!command}
			onCancel={() => !executing && setCommand(undefined)}
			onOk={() => void confirm()}
			confirmLoading={executing}
			okButtonProps={{ disabled: loading || !preview }}
			cancelButtonProps={{ disabled: executing }}
			width={850}
			destroyOnHidden
		>
			{loading ? (
				<Alert type="info" title="正在生成变更预览…" />
			) : (
				preview && <DiffTable preview={preview} />
			)}
		</Modal>
	);
	return { run, modal };
}
