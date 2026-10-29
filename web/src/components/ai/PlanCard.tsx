import { useEffect, useState } from "react";
import {
	Alert,
	App,
	Button,
	Card,
	Collapse,
	Radio,
	Space,
	Tag,
	Typography,
} from "antd";
import { history } from "@umijs/max";
import type { Risk } from "@/api/types";
import { RiskColor, RiskLabel } from "@/api/types";
import { DiffTable } from "@/components/diff/DiffTable";
import { api, ApiError, Code } from "@/utils/request";
import { useUserStore } from "@/stores/user";
import { managementURL, planState, type Plan } from "./state";
export type { Plan, PlanStep } from "./state";

const statusLabels: Record<string, string> = {
	draft: "待预演",
	previewed: "待确认",
	partial: "部分执行",
	done: "已完成",
	aborted: "已放弃",
	pending: "等待依赖结果",
	applied: "已执行",
	failed: "执行失败",
	skipped: "已跳过",
};

export function PlanCard({
	initialPlan,
	onUpdated,
}: {
	initialPlan: Plan;
	onUpdated?: (plan: Plan) => void;
}) {
	const [plan, setPlan] = useState(initialPlan);
	const [mode, setMode] = useState(
		initialPlan.steps.some((step) => step.status === "pending")
			? "stepwise"
			: "all",
	);
	const [busy, setBusy] = useState(false);
	const [stale, setStale] = useState(false);
	const [riskBlocked, setRiskBlocked] = useState(false);
	const [now, setNow] = useState(Date.now());
	const { message } = App.useApp();
	const maxRisk = useUserStore((s) => s.identity?.aiMaxRisk) as
		| Risk
		| undefined;
	const state = planState(plan, maxRisk, now);
	useEffect(() => {
		setPlan(initialPlan);
		setStale(false);
		setRiskBlocked(false);
	}, [initialPlan]);
	useEffect(() => {
		const timer = setInterval(() => setNow(Date.now()), 30_000);
		return () => clearInterval(timer);
	}, []);
	useEffect(() => {
		if (state.requiresStepwise) setMode("stepwise");
	}, [state.requiresStepwise]);

	async function action(kind: "apply" | "preview" | "abort") {
		if (busy) return;
		setBusy(true);
		try {
			const result = await api.post<Plan>(
				`/ai/plans/${encodeURIComponent(plan.id)}/${kind}`,
				kind === "apply"
					? {
							mode,
							...(mode === "stepwise" && state.step
								? { stepSeq: state.step.seq }
								: {}),
						}
					: {},
				{ dedupe: false },
			);
			setPlan(result);
			onUpdated?.(result);
			setStale(false);
			setRiskBlocked(false);
			setNow(Date.now());
			if (kind === "apply")
				message.success(
					result.status === "done"
						? "计划已执行"
						: "此步骤已执行，请检查下一步的实际预览再确认",
				);
			else if (kind === "preview")
				message.success("已获取最新预览，请检查差异后确认");
		} catch (error) {
			if (error instanceof ApiError && error.code === Code.StaleData) {
				setStale(true);
				message.warning("数据或有效期已变化，请重新预演并确认");
			} else if (
				error instanceof ApiError &&
				error.code === Code.RiskExceeded
			) {
				setRiskBlocked(true);
				message.warning(error.message);
			}
		} finally {
			setBusy(false);
		}
	}

	const needsPreview = stale || state.expired;
	const manual = state.exceeded || riskBlocked;
	return (
		<Card
			size="small"
			title="操作计划"
			extra={
				<Tag color={RiskColor[plan.maxRisk]}>{RiskLabel[plan.maxRisk]}</Tag>
			}
		>
			<Typography.Paragraph>{plan.intent}</Typography.Paragraph>
			<Space wrap className="mb-3">
				<Tag>{statusLabels[plan.status] ?? plan.status}</Tag>
				<Typography.Text>预计影响 {plan.affected} 条</Typography.Text>
				{plan.expiresAt && (
					<Typography.Text type="secondary">
						有效期至 {new Date(plan.expiresAt).toLocaleString()}
					</Typography.Text>
				)}
			</Space>
			{manual && (
				<Alert
					type="warning"
					showIcon
					title="需要在管理页面执行"
					description="此计划超出 AI 的执行上限。请检查差异，并使用每步下方的入口前往管理页面。"
					className="mb-3"
				/>
			)}
			{needsPreview && (
				<Alert
					type="warning"
					showIcon
					title="需要重新预演"
					description="当前数据或计划有效期已变化，重新预演并确认新差异后才可继续。"
					className="mb-3"
				/>
			)}
			{state.requiresStepwise && (
				<Alert
					type="info"
					title="此计划依赖前一步的执行结果，需要逐步检查与确认。"
					className="mb-3"
				/>
			)}
			<Collapse
				defaultActiveKey={plan.steps.slice(0, 1).map((step) => step.seq)}
				items={plan.steps.map((step) => ({
					key: step.seq,
					label: (
						<Space wrap>
							<Tag>{statusLabels[step.status] ?? step.status}</Tag>
							{step.seq + 1}. {step.note || step.capability}
						</Space>
					),
					children: (
						<>
							<Typography.Paragraph code>
								{step.capability}
							</Typography.Paragraph>
							{step.error && (
								<Alert type="error" title={step.error} className="my-2" />
							)}
							{step.preview ? (
								<DiffTable preview={step.preview} />
							) : (
								<Typography.Paragraph type="secondary">
									只读查询步骤。
								</Typography.Paragraph>
							)}
							{step.result !== undefined && (
								<Collapse
									size="small"
									items={[
										{
											key: "result",
											label: "查询 / 执行结果",
											children: (
												<pre className="max-h-80 overflow-auto whitespace-pre-wrap break-all">
													{JSON.stringify(step.result, null, 2)}
												</pre>
											),
										},
									]}
								/>
							)}
							{managementURL(step) && (
								<Button
									type="link"
									onClick={() => {
										const route = managementURL(step);
										if (route) history.push(route);
									}}
								>
									前往管理页面核对
								</Button>
							)}
						</>
					),
				}))}
			/>
			<Space wrap className="mt-4">
				{state.executable && !stale && !riskBlocked && (
					<>
						<Radio.Group
							value={mode}
							disabled={busy}
							onChange={(e) => setMode(e.target.value)}
							options={[
								{
									value: "all",
									label: "确认整个计划",
									disabled: state.requiresStepwise,
								},
								{ value: "stepwise", label: "逐步确认" },
							]}
						/>
						<Button
							type="primary"
							loading={busy}
							onClick={() => void action("apply")}
						>
							{mode === "stepwise"
								? `确认执行第 ${(state.step?.seq ?? 0) + 1} 步`
								: "确认执行计划"}
						</Button>
					</>
				)}
				{needsPreview && !["done", "aborted"].includes(plan.status) && (
					<Button loading={busy} onClick={() => void action("preview")}>
						重新预演
					</Button>
				)}
				{!["done", "aborted"].includes(plan.status) && (
					<Button disabled={busy} onClick={() => void action("abort")}>
						放弃计划
					</Button>
				)}
				<Button
					disabled={busy}
					onClick={async () => {
						const result = await api.get<Plan>(
							`/ai/plans/${plan.id}`,
							undefined,
							{ dedupe: false },
						);
						setPlan(result);
						onUpdated?.(result);
						setNow(Date.now());
					}}
				>
					刷新状态
				</Button>
				{plan.changesetId && (
					<Button
						onClick={() =>
							history.push(
								`/changesets?id=${encodeURIComponent(plan.changesetId ?? "")}`,
							)
						}
					>
						查看变更与撤销
					</Button>
				)}
			</Space>
		</Card>
	);
}
