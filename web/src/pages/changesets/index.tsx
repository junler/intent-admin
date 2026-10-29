import { usePageActive } from "@/contexts/ActivePageContext";
import { useEffect, useRef, useState } from "react";
import { history, useLocation } from "@umijs/max";
import {
	PageContainer,
	ProTable,
	type ActionType,
	type ProColumns,
} from "@ant-design/pro-components";
import {
	Alert,
	App,
	Button,
	Descriptions,
	Drawer,
	Empty,
	Modal,
	Space,
	Spin,
	Table,
	Tag,
	Typography,
} from "antd";
import { DiffTable } from "@/components/diff/DiffTable";
import { Auth } from "@/components/auth/Auth";
import { DictTag } from "@/components/dict/DictTag";
import { usePerm } from "@/hooks/usePerm";
import { api, ApiError, Code, type PageData } from "@/utils/request";
import type { Preview } from "@/api/types";
import { changesetPreview, type Changeset, type ChangesetItem } from "./data";

const setStatus = {
	applied: { text: "已应用" },
	partial: { text: "部分应用" },
	rolled_back: { text: "已回滚" },
	rollback_failed: { text: "回滚有失败项" },
};
const actorTypes = {
	human: "人",
	agent: "AI",
	mcp_client: "MCP",
	system: "系统任务",
};
const sources = { http: "页面", ai: "AI", mcp: "MCP", task: "任务" };
const risks = {
	low: "低风险",
	medium: "中风险",
	high: "高风险",
	critical: "极高风险",
};

export default function ChangesetsPage() {
	const active = usePageActive();
	const location = useLocation();
	const actionRef = useRef<ActionType | undefined>(undefined);
	const [detail, setDetail] = useState<Changeset>();
	const [detailLoading, setDetailLoading] = useState(false);
	const [preview, setPreview] = useState<Preview>();
	const [rollbackTarget, setRollbackTarget] = useState<string>();
	const [rollbackBusy, setRollbackBusy] = useState(false);
	const canQuery = usePerm("system:changeset:query");
	const { message } = App.useApp();
	const query = Object.fromEntries(
		[...new URLSearchParams(location.search)].filter(([key]) =>
			["actorType", "source", "status", "intent", "risk"].includes(key),
		),
	);
	const selectedId =
		new URLSearchParams(location.search).get("id") ??
		new URLSearchParams(location.search).get("changesetId");

	useEffect(() => {
		if (!selectedId || !canQuery) {
			setDetail(undefined);
			return;
		}
		let active = true;
		setDetailLoading(true);
		void api
			.get<Changeset>(
				`/system/changeset/${encodeURIComponent(selectedId)}`,
				undefined,
				{ dedupe: false },
			)
			.then((value) => {
				if (active) setDetail(value);
			})
			.catch(() => {
				if (active) setDetail(undefined);
			})
			.finally(() => {
				if (active) setDetailLoading(false);
			});
		return () => {
			active = false;
		};
	}, [selectedId, canQuery]);

	function view(id: string | undefined) {
		if (!id) return;
		const params = new URLSearchParams(location.search);
		params.set("id", id);
		params.delete("changesetId");
		history.push(`${location.pathname}?${params}`);
	}
	function closeDetail() {
		const params = new URLSearchParams(location.search);
		params.delete("id");
		params.delete("changesetId");
		history.replace(`${location.pathname}${params.size ? `?${params}` : ""}`);
		setDetail(undefined);
	}
	async function requestRollback(id: string) {
		setRollbackTarget(id);
		setPreview(undefined);
		setRollbackBusy(true);
		try {
			setPreview(
				await api.post<Preview>(
					"/system/changeset/rollback/preview",
					{ id },
					{ dedupe: false },
				),
			);
		} catch {
			setRollbackTarget(undefined);
		} finally {
			setRollbackBusy(false);
		}
	}
	async function rollback() {
		if (!rollbackTarget || !preview || rollbackBusy) return;
		setRollbackBusy(true);
		try {
			const result = await api.post<Changeset>(
				`/system/changeset/${encodeURIComponent(rollbackTarget)}/rollback`,
				{ id: rollbackTarget },
				{ dedupe: false },
			);
			setRollbackTarget(undefined);
			setPreview(undefined);
			actionRef.current?.reload();
			if (result.status === "rollback_failed")
				message.warning("部分项目回滚失败，请查看详情中的失败原因");
			else message.success("回滚已完成，回滚本身已记录为新的变更集");
			if (selectedId === rollbackTarget)
				setDetail(
					await api.get<Changeset>(
						`/system/changeset/${rollbackTarget}`,
						undefined,
						{ dedupe: false },
					),
				);
			if (result.rollbackChangesetId)
				message.info({
					content: (
						<Button
							type="link"
							onClick={() => view(result.rollbackChangesetId)}
						>
							查看此次回滚产生的变更集
						</Button>
					),
					duration: 8,
				});
		} catch (error) {
			if (error instanceof ApiError && error.code === Code.StaleData) {
				message.warning("数据已变化，请检查最新的回滚预览");
				setPreview(undefined);
				setPreview(
					await api.post<Preview>(
						"/system/changeset/rollback/preview",
						{ id: rollbackTarget },
						{ dedupe: false },
					),
				);
			}
		} finally {
			setRollbackBusy(false);
		}
	}

	const columns: ProColumns<Changeset>[] = [
		{
			title: "变更摘要",
			dataIndex: "summary",
			search: false,
			width: 260,
			render: (_, row) => (
				<>
					<Typography.Text>{row.summary || "数据变更"}</Typography.Text>
					<br />
					<Typography.Text type="secondary" className="text-xs" copyable>
						{row.changesetId}
					</Typography.Text>
				</>
			),
		},
		{ title: "原始意图", dataIndex: "intent", ellipsis: true },
		{
			title: "主体",
			dataIndex: "actorType",
			valueType: "select",
			valueEnum: actorTypes,
			render: (_, row) => (
				<Space>
					<DictTag type="sys_actor_type" value={row.actorType} />
					{row.actorName}
				</Space>
			),
		},
		{
			title: "来源",
			dataIndex: "source",
			valueType: "select",
			valueEnum: sources,
			render: (_, row) => <DictTag type="sys_call_source" value={row.source} />,
		},
		{
			title: "风险",
			dataIndex: "risk",
			valueType: "select",
			valueEnum: risks,
			render: (_, row) => <DictTag type="sys_risk_level" value={row.risk} />,
		},
		{
			title: "状态",
			dataIndex: "status",
			valueType: "select",
			valueEnum: setStatus,
		},
		{ title: "影响数", dataIndex: "affectedCount", search: false },
		{
			title: "时间",
			dataIndex: "createTime",
			valueType: "dateTime",
			search: false,
			width: 170,
		},
		{
			title: "操作",
			valueType: "option",
			width: 180,
			render: (_, row) => (
				<Space wrap>
					{canQuery && (
						<Button type="link" onClick={() => view(row.changesetId)}>
							详情
						</Button>
					)}
					<Auth perm="system:changeset:rollback">
						{row.status !== "rolled_back" && (
							<Button
								type="link"
								onClick={() => void requestRollback(row.changesetId)}
							>
								预览回滚
							</Button>
						)}
					</Auth>
				</Space>
			),
		},
	];

	return (
		<PageContainer
			title="变更历史"
			content="每次修改都有记录。回滚仅还原本次涉及的字段；被后续操作改动的记录会明确提示冲突。"
		>
			<ProTable<Changeset>
				key={JSON.stringify(query)}
				params={query}
				form={{ initialValues: query }}
				onSubmit={(values) => {
					const params = new URLSearchParams();
					for (const [key, value] of Object.entries(values)) {
						if (value !== undefined && value !== null && value !== "")
							params.set(key, String(value));
					}
					history.replace(
						`${location.pathname}${params.size ? `?${params}` : ""}`,
					);
				}}
				onReset={() => history.replace(location.pathname)}
				rowKey="changesetId"
				actionRef={actionRef}
				columns={columns}
				scroll={{ x: 1400 }}
				search={{ labelWidth: "auto" }}
				pagination={{ pageSize: 20, showSizeChanger: true }}
				request={async (params) => {
					const result = await api.get<PageData<Changeset>>(
						"/system/changeset/list",
						{ ...params, pageNum: params.current, pageSize: params.pageSize },
					);
					return { data: result.rows, total: result.total, success: true };
				}}
			/>
			<Drawer
				title="变更集详情"
				open={active && !!selectedId && canQuery}
				onClose={closeDetail}
				width={980}
				extra={
					detail && (
						<Auth perm="system:changeset:rollback">
							<Button
								disabled={detail.status === "rolled_back"}
								onClick={() => void requestRollback(detail.changesetId)}
							>
								预览回滚
							</Button>
						</Auth>
					)
				}
			>
				{detailLoading ? (
					<Spin />
				) : detail ? (
					<>
						<Descriptions
							bordered
							size="small"
							column={2}
							items={[
								{
									key: "id",
									label: "变更集",
									children: (
										<Typography.Text copyable>
											{detail.changesetId}
										</Typography.Text>
									),
									span: 2,
								},
								{
									key: "actor",
									label: "主体",
									children: `${actorTypes[detail.actorType as keyof typeof actorTypes] ?? detail.actorType} / ${detail.actorName}`,
								},
								{
									key: "source",
									label: "来源",
									children: (
										<DictTag type="sys_call_source" value={detail.source} />
									),
								},
								{
									key: "intent",
									label: "原始意图",
									children: detail.intent || "—",
									span: 2,
								},
								{
									key: "status",
									label: "状态",
									children:
										setStatus[detail.status as keyof typeof setStatus]?.text ??
										detail.status,
								},
								{
									key: "risk",
									label: "风险",
									children: (
										<DictTag type="sys_risk_level" value={detail.risk} />
									),
								},
								{
									key: "approval",
									label: "审批方式",
									children: (
										<DictTag
											type="sys_approval_mode"
											value={detail.approvalMode}
										/>
									),
								},
								{
									key: "time",
									label: "时间",
									children: new Date(detail.createTime).toLocaleString(),
								},
							]}
						/>
						<Space wrap className="my-4">
							{detail.planId && (
								<Auth perm="ai:agent:use">
									<Button
										onClick={() =>
											history.push(
												`/ai/sessions?planId=${encodeURIComponent(detail.planId ?? "")}`,
											)
										}
									>
										查看来源计划
									</Button>
								</Auth>
							)}
							{detail.rollbackOf && (
								<Button onClick={() => view(detail.rollbackOf)}>
									查看被回滚的变更集
								</Button>
							)}
							{detail.rollbackChangesetId && (
								<Button onClick={() => view(detail.rollbackChangesetId)}>
									查看回滚变更集
								</Button>
							)}
							<Auth perm="monitor:operlog:list">
								<Button
									onClick={() =>
										history.push(
											`/audit?changesetId=${encodeURIComponent(detail.changesetId)}`,
										)
									}
								>
									查看行为溯源
								</Button>
							</Auth>
						</Space>
						{detail.rolledBackAt && (
							<Alert
								type="info"
								title={`已于 ${new Date(detail.rolledBackAt).toLocaleString()} 由 ${detail.rolledBackBy} 回滚`}
								className="mb-4"
							/>
						)}
						<DiffTable preview={changesetPreview(detail)} historical />
						<Typography.Title level={5}>逐项执行记录</Typography.Title>
						<Table<ChangesetItem>
							size="small"
							rowKey="itemId"
							dataSource={detail.items ?? []}
							pagination={false}
							columns={[
								{ title: "序号", dataIndex: "seq" },
								{ title: "功能", dataIndex: "capability" },
								{
									title: "记录",
									render: (_, item) =>
										`${item.entityLabel || item.entity} #${item.entityId}`,
								},
								{
									title: "状态",
									dataIndex: "status",
									render: (value: string) => (
										<Tag
											color={
												value === "failed"
													? "red"
													: value === "rolled_back"
														? "green"
														: "default"
											}
										>
											{value}
										</Tag>
									),
								},
								{
									title: "说明 / 失败原因",
									dataIndex: "errorMsg",
									render: (value?: string) => value || "—",
								},
							]}
						/>
					</>
				) : (
					<Empty description="无法加载变更集" />
				)}
			</Drawer>
			<Modal
				title="确认回滚"
				open={active && !!rollbackTarget}
				onCancel={() => {
					if (!rollbackBusy) setRollbackTarget(undefined);
				}}
				width={1000}
				onOk={() => void rollback()}
				confirmLoading={rollbackBusy}
				okText="确认按差异回滚"
				okButtonProps={{
					disabled:
						!preview || (preview.affected === 0 && preview.skipped === 0),
				}}
			>
				{rollbackBusy && !preview ? (
					<Spin />
				) : (
					preview && <DiffTable preview={preview} />
				)}
				<Alert
					type="info"
					title="回滚会产生一条新的变更集，不会删除原始记录。不可逆项、冲突项和失败项会保留原因。"
					className="mt-4"
				/>
			</Modal>
		</PageContainer>
	);
}
