import { useEffect, useState } from "react";
import { Button } from "antd";
import { useNavigate } from "@umijs/max";
import { CrudPage } from "@/components/admin/CrudPage";
import { validCron } from "@/components/admin/cron";
import type { Row } from "@/components/admin/types";
import { api } from "@/utils/request";
export default function Jobs() {
	const [handlers, setHandlers] = useState<Row[]>([]);
	const navigate = useNavigate();
	useEffect(() => {
		void api.get<Row[]>("/monitor/job/handlers").then(setHandlers);
	}, []);
	return (
		<CrudPage
			title="定时任务"
			base="/monitor/job"
			createUrl="/monitor/job/create"
			updateUrl="/monitor/job/update"
			idKey="jobId"
			perm="monitor:job"
			extra={
				<Button onClick={() => navigate("/monitor/jobLog")}>执行日志</Button>
			}
			fields={[
				{ name: "jobName", label: "任务名称", required: true, search: true },
				{
					name: "jobGroup",
					label: "队列",
					defaultValue: "DEFAULT",
					search: true,
				},
				{
					name: "invokeTarget",
					label: "处理器",
					required: true,
					type: "select",
					options: handlers.map((h) => ({
						label: `${h.name} · ${h.summary}`,
						value: h.name,
					})),
				},
				{
					name: "jobParams",
					label: "参数 JSON",
					type: "textarea",
					defaultValue: "{}",
					rules: [
						{
							validator: (_: unknown, value: string) => {
								try {
									JSON.parse(value || "{}");
									return Promise.resolve();
								} catch {
									return Promise.reject(new Error("请输入有效 JSON"));
								}
							},
						},
					],
				},
				{
					name: "cronExpression",
					label: "执行计划",
					type: "cron",
					required: true,
					defaultValue: "0 * * * *",
					help: "标准 5 字段：分钟 小时 日 月 周；或 @every 5m。不支持秒字段。",
					rules: [
						{
							validator: (_: unknown, value: string) =>
								validCron(value ?? "")
									? Promise.resolve()
									: Promise.reject(
											new Error("请输入 5 字段 cron 或 @every 时间间隔"),
										),
						},
					],
				},
				{
					name: "misfirePolicy",
					label: "补偿策略",
					type: "select",
					defaultValue: "3",
					options: [
						{ label: "立即补一次", value: "1" },
						{ label: "全部补偿", value: "2" },
						{ label: "忽略错过的执行", value: "3" },
					],
					column: false,
				},
				{
					name: "concurrent",
					label: "并发",
					type: "select",
					defaultValue: "1",
					options: [
						{ label: "禁止并发", value: "1" },
						{ label: "允许并发", value: "0" },
					],
					column: false,
				},
				{
					name: "status",
					label: "状态",
					type: "select",
					defaultValue: "1",
					search: true,
					options: [
						{ label: "运行", value: "0" },
						{ label: "暂停", value: "1" },
					],
				},
				{ name: "nextExecution", label: "下次执行", form: false },
				{ name: "remark", label: "备注", type: "textarea" },
			]}
			actions={[
				{
					label: "立即执行",
					perm: "monitor:job:run",
					url: "/monitor/job/run",
					previewUrl: "/monitor/job/run/preview",
					args: (r) => ({ jobId: r.jobId }),
				},
				{
					label: "启停",
					perm: "monitor:job:changeStatus",
					url: "/monitor/job/changeStatus",
					previewUrl: "/monitor/job/changeStatus/preview",
					args: (r) => ({
						jobId: r.jobId,
						status: r.status === "0" ? "1" : "0",
					}),
				},
			]}
		/>
	);
}
