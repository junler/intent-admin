import { useEffect, useState } from "react";
import { Button, Card, Space, Spin } from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import { InfoPanel } from "@/components/admin/InfoPanel";
import { api } from "@/utils/request";
import type { Row } from "@/components/admin/types";
export default function Server() {
	const [info, setInfo] = useState<Row>();
	const [busy, setBusy] = useState(false);
	async function load() {
		setBusy(true);
		try {
			setInfo(await api.get<Row>("/monitor/server"));
		} finally {
			setBusy(false);
		}
	}
	useEffect(() => {
		void load();
	}, []);
	return (
		<Card
			title="服务监控"
			extra={
				<Button
					icon={<ReloadOutlined />}
					onClick={() => void load()}
					loading={busy}
				>
					刷新
				</Button>
			}
		>
			<Spin spinning={busy}>
				<Space direction="vertical" className="w-full">
					{Object.entries(info ?? {}).map(([key, value]) => (
						<InfoPanel key={key} title={key} value={value} />
					))}
				</Space>
			</Spin>
		</Card>
	);
}
