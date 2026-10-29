import { LogPage } from "@/components/admin/LogPage";
export default function JobLog() {
	return (
		<LogPage
			title="任务执行日志"
			base="/monitor/jobLog"
			idKey="jobLogId"
			perm="monitor:job"
			fields={[
				{ name: "jobId", label: "任务编号", type: "number", search: true },
				{ name: "jobName", label: "任务", search: true },
				{ name: "jobGroup", label: "队列", search: true },
				{ name: "invokeTarget", label: "处理器" },
				{
					name: "status",
					label: "结果",
					type: "select",
					search: true,
					options: [
						{ label: "成功", value: "0" },
						{ label: "失败", value: "1" },
					],
				},
				{ name: "costTime", label: "耗时 ms", type: "number" },
				{ name: "jobMessage", label: "说明" },
				{ name: "createTime", label: "时间" },
			]}
		/>
	);
}
