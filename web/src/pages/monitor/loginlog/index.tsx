import { LogPage } from "@/components/admin/LogPage";
export default function LoginLog() {
	return (
		<LogPage
			title="登录日志"
			base="/monitor/loginlog"
			idKey="infoId"
			perm="monitor:loginlog"
			fields={[
				{ name: "userName", label: "账号", search: true },
				{ name: "ipaddr", label: "IP", search: true },
				{ name: "browser", label: "浏览器" },
				{ name: "os", label: "系统" },
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
				{ name: "msg", label: "说明" },
				{ name: "loginTime", label: "时间" },
			]}
			actions={[
				{
					label: "解锁账号",
					perm: "monitor:loginlog:unlock",
					url: "/monitor/loginlog/unlock",
					previewUrl: "/monitor/loginlog/unlock/preview",
					args: (row) => ({ userName: row.userName }),
				},
			]}
		/>
	);
}
