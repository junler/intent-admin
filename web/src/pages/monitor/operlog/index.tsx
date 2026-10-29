import { LogPage } from "@/components/admin/LogPage";
export default function OperLog() {
	return (
		<LogPage
			title="操作日志"
			base="/monitor/operlog"
			idKey="operId"
			perm="monitor:operlog"
			fields={[
				{ name: "title", label: "模块", search: true },
				{ name: "capability", label: "能力", search: true },
				{ name: "operName", label: "操作者", search: true },
				{
					name: "actorType",
					label: "主体",
					type: "select",
					search: true,
					options: [
						{ label: "用户", value: "human" },
						{ label: "AI", value: "agent" },
						{ label: "MCP", value: "mcp_client" },
						{ label: "任务", value: "system" },
					],
				},
				{ name: "source", label: "来源", search: true },
				{ name: "operIp", label: "IP" },
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
				{ name: "operTime", label: "时间" },
			]}
		/>
	);
}
