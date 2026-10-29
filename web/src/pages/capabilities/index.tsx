import { usePageActive } from "@/contexts/ActivePageContext";
import { useRef, useState } from "react";
import { history } from "@umijs/max";
import {
	PageContainer,
	ProTable,
	type ActionType,
	type ProColumns,
} from "@ant-design/pro-components";
import {
	Alert,
	Button,
	Collapse,
	Descriptions,
	Drawer,
	Empty,
	Input,
	Space,
	Spin,
	Table,
	Tabs,
	Tag,
	Typography,
} from "antd";
import { RiskColor, RiskLabel, type Risk } from "@/api/types";
import { api } from "@/utils/request";
import { managementURL } from "@/components/ai/state";

interface CapabilityRow {
	name: string;
	summary: string;
	perm: string;
	risk: Risk;
	mutating: boolean;
	reversible: boolean;
	aiEnabled: boolean;
	mcpEnabled: boolean;
	tags: string[];
	examples?: { utterance: string; args: string }[];
	inputSchema?: unknown;
	http?: { method?: string; path?: string; Method?: string; Path?: string };
}
interface EnumDef {
	dictType: string;
	summary: string;
	values: Record<string, string>;
	usedBy?: string[];
}
interface EntityDef {
	table: string;
	summary: string;
	keyField: string;
	labelField: string;
	relations?: { kind: string; target: string; via?: string; summary: string }[];
	notes?: string[];
}
interface RuleDef {
	scope: string;
	rule: string;
	enforced: boolean;
}
interface Knowledge {
	enums: EnumDef[];
	entities: EntityDef[];
	rules: RuleDef[];
}
function pretty(value: unknown) {
	if (typeof value === "string") {
		try {
			return JSON.stringify(JSON.parse(value), null, 2);
		} catch {
			return value;
		}
	}
	return JSON.stringify(value, null, 2);
}

export default function CapabilitiesPage() {
	const active = usePageActive();
	const actionRef = useRef<ActionType | undefined>(undefined);
	const [keyword, setKeyword] = useState("");
	const [selected, setSelected] = useState<CapabilityRow>();
	const [knowledgeOpen, setKnowledgeOpen] = useState(false);
	const [knowledge, setKnowledge] = useState<Knowledge>();
	const [loadingKnowledge, setLoadingKnowledge] = useState(false);
	const selectedRoute = selected
		? managementURL({
				seq: 0,
				capability: selected.name,
				args: {},
				note: "",
				status: "",
			})
		: undefined;
	async function loadKnowledge() {
		setKnowledgeOpen(true);
		setLoadingKnowledge(true);
		try {
			setKnowledge(
				await api.get<Knowledge>("/ai/knowledge", undefined, { dedupe: false }),
			);
		} catch {
			setKnowledge(undefined);
		} finally {
			setLoadingKnowledge(false);
		}
	}
	const columns: ProColumns<CapabilityRow>[] = [
		{
			title: "功能",
			dataIndex: "name",
			width: 250,
			ellipsis: true,
			search: false,
			render: (_, row) => (
				<>
					<Typography.Text>{row.summary}</Typography.Text>
					<br />
					<Typography.Text type="secondary" className="text-xs" copyable>
						{row.name}
					</Typography.Text>
				</>
			),
		},
		{
			title: "接口",
			dataIndex: "http",
			search: false,
			width: 280,
			render: (_, row) =>
				row.http ? (
					<Typography.Text code>
						{row.http.method ?? row.http.Method}{" "}
						{row.http.path ?? row.http.Path}
					</Typography.Text>
				) : (
					"无 HTTP 入口"
				),
		},
		{
			title: "权限",
			dataIndex: "perm",
			search: false,
			renderText: (value: string) => value || "登录即可",
		},
		{
			title: "风险",
			dataIndex: "risk",
			valueType: "select",
			valueEnum: Object.fromEntries(
				Object.entries(RiskLabel).map(([key, text]) => [key, { text }]),
			),
			render: (_, row) => (
				<Tag color={RiskColor[row.risk]}>{RiskLabel[row.risk]}</Tag>
			),
		},
		{
			title: "行为",
			dataIndex: "mutating",
			valueType: "select",
			valueEnum: { true: "修改数据", false: "只读" },
			render: (_, row) => (
				<Space>
					<Tag color={row.mutating ? "orange" : "green"}>
						{row.mutating ? "修改" : "只读"}
					</Tag>
					{row.mutating && (
						<Tag color={row.reversible ? "green" : "red"}>
							{row.reversible ? "可回滚" : "不可回滚"}
						</Tag>
					)}
				</Space>
			),
		},
		{
			title: "AI 可用",
			dataIndex: "aiEnabled",
			valueType: "select",
			valueEnum: { true: "开放", false: "关闭" },
			render: (_, row) => (row.aiEnabled ? <Tag color="purple">AI</Tag> : "—"),
		},
		{
			title: "MCP 可用",
			dataIndex: "mcpEnabled",
			valueType: "select",
			valueEnum: { true: "开放", false: "关闭" },
			render: (_, row) => (row.mcpEnabled ? <Tag color="cyan">MCP</Tag> : "—"),
		},
		{
			title: "操作",
			valueType: "option",
			render: (_, row) => (
				<Button type="link" onClick={() => setSelected(row)}>
					Schema / 示例
				</Button>
			),
		},
	];
	return (
		<PageContainer
			title="能力浏览器"
			content="注册表是接口、AI 工具和 MCP 工具的共同来源。这里仅展示当前账号有权限了解的能力。"
		>
			<Alert
				type="info"
				showIcon
				title="权限与风险分别控制可访问功能和 AI 可代执行的范围"
				description="模型服务、存储配置、令牌及个人敏感操作始终与 AI/MCP 写入口隔离。AI 的所有修改先进入计划预演，等待用户确认。"
				className="mb-4"
			/>
			<ProTable<CapabilityRow>
				rowKey="name"
				actionRef={actionRef}
				params={{ keyword }}
				columns={columns}
				scroll={{ x: 1450 }}
				search={{ labelWidth: "auto" }}
				pagination={{ pageSize: 20, showSizeChanger: true }}
				toolBarRender={() => [
					<Input.Search
						key="keyword"
						placeholder="搜索名称或说明"
						allowClear
						onSearch={setKeyword}
						className="w-64"
					/>,
					<Button key="knowledge" onClick={() => void loadKnowledge()}>
						系统知识库
					</Button>,
				]}
				request={async (params) => {
					let rows = await api.get<CapabilityRow[]>("/ai/capabilities", {
						keyword: params.keyword,
					});
					for (const key of [
						"risk",
						"mutating",
						"aiEnabled",
						"mcpEnabled",
					] as const) {
						const value = params[key];
						if (value !== undefined && value !== "")
							rows = rows.filter((row) => String(row[key]) === String(value));
					}
					const size = params.pageSize ?? 20,
						start = ((params.current ?? 1) - 1) * size;
					return {
						data: rows.slice(start, start + size),
						total: rows.length,
						success: true,
					};
				}}
			/>
			<Drawer
				title={selected?.summary ?? "功能详情"}
				open={active && !!selected}
				onClose={() => setSelected(undefined)}
				width={900}
			>
				{selected && (
					<>
						<Descriptions
							bordered
							column={2}
							size="small"
							items={[
								{
									key: "name",
									label: "名称",
									children: (
										<Typography.Text copyable>{selected.name}</Typography.Text>
									),
									span: 2,
								},
								{
									key: "permission",
									label: "权限",
									children: selected.perm || "登录即可",
								},
								{
									key: "risk",
									label: "风险",
									children: (
										<Tag color={RiskColor[selected.risk]}>
											{RiskLabel[selected.risk]}
										</Tag>
									),
								},
								{
									key: "entry",
									label: "开放入口",
									children: (
										<Space>
											{selected.aiEnabled && <Tag color="purple">AI</Tag>}
											{selected.mcpEnabled && <Tag color="cyan">MCP</Tag>}
											{!selected.aiEnabled && !selected.mcpEnabled && "仅页面"}
										</Space>
									),
								},
								{
									key: "undo",
									label: "可逆性",
									children: selected.mutating
										? selected.reversible
											? "可回滚"
											: "不可回滚"
										: "只读",
								},
							]}
						/>
						<Typography.Title level={5}>输入 JSON Schema</Typography.Title>
						<pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all rounded bg-slate-50 p-3">
							{pretty(selected.inputSchema)}
						</pre>
						<Typography.Title level={5}>调用示例</Typography.Title>
						{selected.examples?.length ? (
							<Collapse
								items={selected.examples.map((example, index) => ({
									key: index,
									label: example.utterance,
									children: (
										<pre className="overflow-auto whitespace-pre-wrap">
											{pretty(example.args)}
										</pre>
									),
								}))}
							/>
						) : (
							<Empty
								image={Empty.PRESENTED_IMAGE_SIMPLE}
								description="此功能未声明自然语言示例"
							/>
						)}
						{selectedRoute && (
							<Button
								className="mt-4"
								onClick={() => history.push(selectedRoute)}
							>
								前往管理页面
							</Button>
						)}
					</>
				)}
			</Drawer>
			<Drawer
				title="系统自省知识库"
				open={active && knowledgeOpen}
				onClose={() => setKnowledgeOpen(false)}
				width={1000}
				extra={<Button onClick={() => void loadKnowledge()}>刷新</Button>}
			>
				{loadingKnowledge ? (
					<Spin />
				) : knowledge ? (
					<Tabs
						items={[
							{
								key: "enums",
								label: `字典语义 (${knowledge.enums.length})`,
								children: (
									<Table<EnumDef>
										rowKey="dictType"
										dataSource={knowledge.enums}
										pagination={{ pageSize: 10 }}
										columns={[
											{ title: "字典", dataIndex: "dictType" },
											{ title: "说明", dataIndex: "summary" },
											{
												title: "取值",
												render: (_, item) => (
													<Space wrap>
														{Object.entries(item.values).map(
															([value, label]) => (
																<Tag key={value}>
																	{value}：{label}
																</Tag>
															),
														)}
													</Space>
												),
											},
											{
												title: "应用字段",
												render: (_, item) => item.usedBy?.join("、") || "—",
											},
										]}
									/>
								),
							},
							{
								key: "entities",
								label: `实体与关系 (${knowledge.entities.length})`,
								children: (
									<Collapse
										items={knowledge.entities.map((entity) => ({
											key: entity.table,
											label: `${entity.summary} (${entity.table})`,
											children: (
												<>
													<Typography.Paragraph>
														主键：{entity.keyField} · 名称字段：
														{entity.labelField}
													</Typography.Paragraph>
													{entity.relations?.map((relation) => (
														<Typography.Paragraph
															key={`${relation.kind}:${relation.target}:${relation.via ?? ""}`}
														>
															{relation.summary}：{relation.kind} →{" "}
															{relation.target}
															{relation.via ? `，通过 ${relation.via}` : ""}
														</Typography.Paragraph>
													))}
													{entity.notes?.map((note) => (
														<Alert
															className="mb-2"
															key={note}
															type="info"
															title={note}
														/>
													))}
												</>
											),
										}))}
									/>
								),
							},
							{
								key: "rules",
								label: `业务规则 (${knowledge.rules.length})`,
								children: (
									<Table<RuleDef>
										rowKey={(row) => `${row.scope}:${row.rule}`}
										dataSource={knowledge.rules}
										pagination={false}
										columns={[
											{ title: "范围", dataIndex: "scope" },
											{ title: "规则", dataIndex: "rule" },
											{
												title: "代码约束",
												render: (_, row) => (
													<Tag color={row.enforced ? "green" : "default"}>
														{row.enforced ? "已强制执行" : "约定"}
													</Tag>
												),
											},
										]}
									/>
								),
							},
						]}
					/>
				) : (
					<Empty description="无法加载知识库" />
				)}
			</Drawer>
		</PageContainer>
	);
}
