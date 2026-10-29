import { usePageActive } from "@/contexts/ActivePageContext";
import { useEffect, useRef, useState } from "react";
import { history, useLocation } from "@umijs/max";
import {
	App,
	Button,
	Drawer,
	Empty,
	Input,
	Modal,
	Popconfirm,
	Space,
	Spin,
	Tabs,
} from "antd";
import {
	PageContainer,
	ProTable,
	type ActionType,
} from "@ant-design/pro-components";
import { api } from "@/utils/request";
import { useAiPanel } from "@/components/ai/AiPanel";
import { PlanCard, type Plan } from "@/components/ai/PlanCard";
import { RiskColor, RiskLabel } from "@/api/types";
interface Session {
	id: string;
	title: string;
	model: string;
	updatedAt: string;
}

export default function SessionsPage() {
	const active = usePageActive();
	const location = useLocation();
	const sessionsRef = useRef<ActionType | undefined>(undefined);
	const plansRef = useRef<ActionType | undefined>(undefined);
	const [plan, setPlan] = useState<Plan>();
	const [loadingPlan, setLoadingPlan] = useState(false);
	const [renaming, setRenaming] = useState<Session>();
	const [title, setTitle] = useState("");
	const [saving, setSaving] = useState(false);
	const show = useAiPanel((state) => state.show);
	const { message } = App.useApp();
	const planId = new URLSearchParams(location.search).get("planId");
	useEffect(() => {
		if (!planId) {
			setPlan(undefined);
			return;
		}
		let active = true;
		setLoadingPlan(true);
		void api
			.get<Plan>(`/ai/plans/${encodeURIComponent(planId)}`, undefined, {
				dedupe: false,
			})
			.then((result) => {
				if (active) setPlan(result);
			})
			.catch(() => {
				if (active) setPlan(undefined);
			})
			.finally(() => {
				if (active) setLoadingPlan(false);
			});
		return () => {
			active = false;
		};
	}, [planId]);
	function viewPlan(id?: string) {
		const params = new URLSearchParams(location.search);
		if (id) params.set("planId", id);
		else params.delete("planId");
		history.push(`${location.pathname}${params.size ? `?${params}` : ""}`);
	}
	return (
		<PageContainer
			title="AI 会话与计划"
			content="会话保存在 Redis；计划与实际变更独立保留，过期会话不会抹去审批和变更记录。"
		>
			<Tabs
				defaultActiveKey={planId ? "plans" : "sessions"}
				items={[
					{
						key: "sessions",
						label: "会话",
						children: (
							<ProTable<Session>
								rowKey="id"
								search={false}
								actionRef={sessionsRef}
								request={async () => ({
									data: await api.get<Session[]>("/ai/sessions", undefined, {
										dedupe: false,
									}),
									success: true,
								})}
								columns={[
									{ title: "标题", dataIndex: "title" },
									{ title: "模型", dataIndex: "model" },
									{
										title: "更新时间",
										dataIndex: "updatedAt",
										valueType: "dateTime",
									},
									{
										title: "操作",
										valueType: "option",
										render: (_, row) => (
											<Space>
												<Button
													type="link"
													onClick={() => show(undefined, row.id)}
												>
													继续会话
												</Button>
												<Button
													type="link"
													onClick={() => {
														setRenaming(row);
														setTitle(row.title);
													}}
												>
													重命名
												</Button>
												<Popconfirm
													title="确认删除会话？计划与变更记录会保留。"
													onConfirm={async () => {
														await api.del(`/ai/sessions/${row.id}`);
														sessionsRef.current?.reload();
													}}
												>
													<Button type="link" danger>
														删除
													</Button>
												</Popconfirm>
											</Space>
										),
									},
								]}
								toolBarRender={() => [
									<Button key="new" type="primary" onClick={() => show()}>
										新会话
									</Button>,
								]}
							/>
						),
					},
					{
						key: "plans",
						label: "计划记录",
						children: (
							<ProTable<Plan>
								rowKey="id"
								actionRef={plansRef}
								search={false}
								request={async () => ({
									data: await api.get<Plan[]>("/ai/plans", undefined, {
										dedupe: false,
									}),
									success: true,
								})}
								columns={[
									{ title: "原始意图", dataIndex: "intent", ellipsis: true },
									{
										title: "风险",
										dataIndex: "maxRisk",
										valueEnum: Object.fromEntries(
											Object.entries(RiskLabel).map(([key, text]) => [
												key,
												{
													text,
													color: RiskColor[key as keyof typeof RiskColor],
												},
											]),
										),
									},
									{
										title: "状态",
										dataIndex: "status",
										valueEnum: {
											draft: "草稿",
											previewed: "待确认",
											partial: "部分完成",
											done: "已完成",
											aborted: "已放弃",
										},
									},
									{ title: "影响数", dataIndex: "affected" },
									{
										title: "有效期",
										dataIndex: "expiresAt",
										valueType: "dateTime",
									},
									{
										title: "操作",
										valueType: "option",
										render: (_, row) => (
											<Space>
												<Button type="link" onClick={() => viewPlan(row.id)}>
													预览 / 确认
												</Button>
												{row.changesetId && (
													<Button
														type="link"
														onClick={() =>
															history.push(
																`/changesets?id=${encodeURIComponent(row.changesetId ?? "")}`,
															)
														}
													>
														变更与回滚
													</Button>
												)}
											</Space>
										),
									},
								]}
							/>
						),
					},
				]}
			/>
			<Drawer
				title="计划详情"
				open={active && !!planId}
				width={1000}
				onClose={() => viewPlan()}
			>
				{loadingPlan ? (
					<Spin />
				) : plan ? (
					<PlanCard
						initialPlan={plan}
						onUpdated={(result) => {
							setPlan(result);
							plansRef.current?.reload();
						}}
					/>
				) : (
					<Empty description="计划不存在或当前账号无法访问" />
				)}
			</Drawer>
			<Modal
				title="重命名会话"
				open={active && !!renaming}
				onCancel={() => setRenaming(undefined)}
				confirmLoading={saving}
				onOk={async () => {
					if (!renaming || !title.trim()) return;
					setSaving(true);
					try {
						await api.put(`/ai/sessions/${renaming.id}`, {
							title: title.trim(),
						});
						setRenaming(undefined);
						sessionsRef.current?.reload();
						message.success("会话名称已更新");
					} finally {
						setSaving(false);
					}
				}}
				okButtonProps={{ disabled: !title.trim() }}
			>
				<Input
					value={title}
					onChange={(event) => setTitle(event.target.value)}
					maxLength={60}
					showCount
				/>
			</Modal>
		</PageContainer>
	);
}
