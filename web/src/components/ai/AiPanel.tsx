import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "zustand";
import {
	Alert,
	App,
	Button,
	Drawer,
	Empty,
	Input,
	Select,
	Space,
	Spin,
	Tag,
	Typography,
} from "antd";
import { history } from "@umijs/max";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { PlanCard, type Plan } from "./PlanCard";
import { evidenceURL } from "./state";
import type { Evidence } from "@/api/types";
import { api } from "@/utils/request";
import { streamChat, type StreamEvent } from "@/utils/sse";
import { usePageContextStore } from "@/stores/pageContext";
import { usePerm } from "@/hooks/usePerm";

interface ChatMessage {
	id: string;
	role: string;
	content: string;
	planId?: string;
	plan?: Plan;
	planUnavailable?: boolean;
	evidence?: Evidence[];
}
interface Session {
	id: string;
	title: string;
	model: string;
	messages: ChatMessage[];
}
interface Models {
	providerId: number;
	name: string;
	defaultModel: string;
	models: string[];
	isDefault: boolean;
}
interface PanelState {
	open: boolean;
	request?: { id: number; text?: string; sessionId?: string };
	show: (text?: string, sessionId?: string) => void;
	close: () => void;
}
interface ActiveRun {
	id: number;
	controller: AbortController;
	sessionId?: string;
}
let commandId = 0;
export const useAiPanel = create<PanelState>((set) => ({
	open: false,
	show: (text, sessionId) =>
		set({ open: true, request: { id: ++commandId, text, sessionId } }),
	close: () => set({ open: false }),
}));

function evidence(value: unknown): Evidence[] {
	return Array.isArray(value)
		? value.filter(
				(entry): entry is Evidence =>
					!!entry &&
					typeof entry === "object" &&
					typeof entry.route === "string",
			)
		: [];
}

export function AiPanel() {
	const { open, request: incoming, close } = useAiPanel();
	const [messages, setMessages] = useState<ChatMessage[]>([]);
	const [sessions, setSessions] = useState<Session[]>([]);
	const [sessionId, setSessionId] = useState<string>();
	const [models, setModels] = useState<Models[]>([]);
	const [model, setModel] = useState<string>();
	const [text, setText] = useState("");
	const [busy, setBusy] = useState(false);
	const [restoring, setRestoring] = useState(false);
	const [tool, setTool] = useState("");
	const [error, setError] = useState("");
	const active = useRef<ActiveRun | undefined>(undefined);
	const sequence = useRef(0);
	const loadSequence = useRef(0);
	const handled = useRef(0);
	const bottom = useRef<HTMLDivElement>(null);
	const allowed = usePerm("ai:agent:use");
	const { message } = App.useApp();
	const refreshSessions = useCallback(
		() =>
			api
				.get<Session[]>("/ai/sessions", undefined, {
					silent: true,
					dedupe: false,
				})
				.then(setSessions)
				.catch(() => {}),
		[],
	);

	const stopActive = useCallback((reset = true) => {
		const run = active.current;
		active.current = undefined;
		if (run) {
			run.controller.abort();
			if (run.sessionId)
				void api
					.post(
						"/ai/stop",
						{ sessionId: run.sessionId },
						{ silent: true, dedupe: false },
					)
					.catch(() => {});
		}
		if (reset) {
			setBusy(false);
			setTool("");
		}
	}, []);

	useEffect(() => {
		if (!open || !allowed) return;
		let mounted = true;
		void refreshSessions();
		void api
			.get<Models[]>("/ai/models", undefined, { silent: true, dedupe: false })
			.then((items) => {
				if (!mounted) return;
				setModels(items);
				const provider = items.find((item) => item.isDefault) ?? items[0];
				if (provider)
					setModel(
						(old) => old ?? `${provider.providerId}:${provider.defaultModel}`,
					);
			})
			.catch((reason: unknown) => {
				if (mounted)
					setError(
						reason instanceof Error ? reason.message : "模型列表加载失败",
					);
			});
		return () => {
			mounted = false;
		};
	}, [open, allowed, refreshSessions]);
	const lastMessage = messages.at(-1);
	const scrollAnchor = `${lastMessage?.id}:${lastMessage?.content}:${lastMessage?.plan?.status}:${tool}`;
	useEffect(() => {
		if (scrollAnchor) bottom.current?.scrollIntoView({ behavior: "smooth" });
	}, [scrollAnchor]);
	useEffect(() => {
		if (!open || !allowed) stopActive();
	}, [open, allowed, stopActive]);
	useEffect(
		() => () => {
			loadSequence.current++;
			stopActive(false);
		},
		[stopActive],
	);

	const newConversation = useCallback(() => {
		stopActive();
		loadSequence.current++;
		setRestoring(false);
		setSessionId(undefined);
		setMessages([]);
		setText("");
		setError("");
	}, [stopActive]);
	const loadSession = useCallback(
		async (id: string) => {
			stopActive();
			const load = ++loadSequence.current;
			setRestoring(true);
			setError("");
			try {
				const session = await api.get<Session>(
					`/ai/sessions/${encodeURIComponent(id)}`,
					undefined,
					{ dedupe: false, silent: true },
				);
				const ids = [
					...new Set(
						(session.messages ?? [])
							.map((item) => item.planId)
							.filter((value): value is string => !!value),
					),
				];
				const plans = new Map<string, Plan>();
				await Promise.all(
					ids.map(async (planId) => {
						try {
							plans.set(
								planId,
								await api.get<Plan>(
									`/ai/plans/${encodeURIComponent(planId)}`,
									undefined,
									{ silent: true, dedupe: false },
								),
							);
						} catch {
							/* The message remains visible even if its plan is unavailable. */
						}
					}),
				);
				if (load !== loadSequence.current) return;
				setSessionId(session.id);
				setMessages(
					(session.messages ?? []).map((item) => ({
						...item,
						plan: item.planId ? plans.get(item.planId) : undefined,
						planUnavailable: !!item.planId && !plans.has(item.planId),
					})),
				);
				setModel(session.model || undefined);
			} catch (reason) {
				if (load === loadSequence.current)
					setError(reason instanceof Error ? reason.message : "会话加载失败");
			} finally {
				if (load === loadSequence.current) setRestoring(false);
			}
		},
		[stopActive],
	);

	const send = useCallback(
		async (input: string) => {
			if (!input.trim() || !allowed) return;
			if (restoring) {
				setText(input);
				message.info("正在恢复会话，完成后可以发送此消息");
				return;
			}
			if (active.current) {
				message.info("当前会话正在生成，可先停止再发送新消息");
				return;
			}
			const assistantId = crypto.randomUUID();
			const run: ActiveRun = {
				id: ++sequence.current,
				controller: new AbortController(),
				sessionId,
			};
			active.current = run;
			setText("");
			setError("");
			setBusy(true);
			setMessages((old) => [
				...old,
				{ id: crypto.randomUUID(), role: "user", content: input },
				{ id: assistantId, role: "assistant", content: "" },
			]);
			const current = () => active.current?.id === run.id;
			const update = (fn: (item: ChatMessage) => ChatMessage) => {
				if (current())
					setMessages((old) =>
						old.map((item) => (item.id === assistantId ? fn(item) : item)),
					);
			};
			function onEvent(event: StreamEvent) {
				if (!current()) return;
				if (event.type === "start") {
					run.sessionId = String(event.sessionId);
					setSessionId(run.sessionId);
				} else if (event.type === "delta")
					update((item) => ({
						...item,
						content: item.content + String(event.text ?? ""),
					}));
				else if (event.type === "answer")
					update((item) => ({
						...item,
						content: String(event.text ?? item.content),
						evidence: evidence(event.evidence),
					}));
				else if (event.type === "tool_call")
					setTool(String(event.summary ?? event.name ?? "正在查询"));
				else if (event.type === "tool_result") setTool("");
				else if (event.type === "plan")
					update((item) => ({
						...item,
						planId: String(event.id),
						plan: event as unknown as Plan,
					}));
				else if (event.type === "navigate") {
					const url = evidenceURL({
						route: String(event.route ?? ""),
						filters:
							event.filters && typeof event.filters === "object"
								? (event.filters as Record<string, unknown>)
								: undefined,
					});
					if (url) {
						history.push(url);
						update((item) => ({
							...item,
							content: String(event.note ?? "已打开筛选后的页面"),
							evidence: [{ route: url }],
						}));
					}
				} else if (event.type === "error")
					setError(String(event.message ?? "生成失败"));
				else if (event.type === "done") void refreshSessions();
			}
			try {
				await streamChat(
					{
						sessionId: run.sessionId,
						model,
						message: input,
						pageContext: usePageContextStore.getState().context,
					},
					onEvent,
					run.controller.signal,
				);
			} catch (reason) {
				if (current() && !run.controller.signal.aborted)
					setError(reason instanceof Error ? reason.message : "连接失败");
			} finally {
				if (current()) {
					active.current = undefined;
					setBusy(false);
					setTool("");
				}
			}
		},
		[allowed, restoring, message, sessionId, model, refreshSessions],
	);

	useEffect(() => {
		if (!incoming || incoming.id === handled.current || !allowed) return;
		handled.current = incoming.id;
		if (incoming.sessionId) void loadSession(incoming.sessionId);
		else if (incoming.text) void send(incoming.text);
		else newConversation();
	}, [incoming, allowed, loadSession, send, newConversation]);

	return (
		<Drawer
			title="Intent 助手"
			open={open}
			onClose={() => {
				stopActive();
				loadSequence.current++;
				close();
			}}
			width={760}
			extra={<Button onClick={newConversation}>新会话</Button>}
		>
			<Alert
				type="info"
				showIcon
				title="数据发送提示"
				description="会话内容和查询到的业务数据会发送给你配置的模型服务商。写操作会先展示计划与差异，确认后执行。"
				className="mb-4"
			/>
			{!allowed ? (
				<Empty description="当前账号没有使用 AI 的权限" />
			) : (
				<>
					<Space wrap className="mb-4 w-full">
						<Select
							placeholder="历史会话"
							value={sessionId}
							allowClear
							className="w-52"
							options={sessions.map((session) => ({
								value: session.id,
								label: session.title,
							}))}
							onChange={(id) => (id ? void loadSession(id) : newConversation())}
						/>
						<Select
							placeholder="选择模型"
							value={model}
							showSearch
							optionFilterProp="label"
							className="w-72"
							options={models.flatMap((provider) =>
								[
									...new Set(
										provider.models?.length
											? provider.models
											: [provider.defaultModel],
									),
								].map((name) => ({
									value: `${provider.providerId}:${name}`,
									label: `${provider.name} / ${name}`,
								})),
							)}
							onChange={setModel}
							disabled={busy || restoring}
						/>
					</Space>
					{restoring ? (
						<Spin tip="正在恢复会话与计划" />
					) : (
						<div className="min-h-40 space-y-5">
							{messages
								.filter(
									(item) =>
										item.role === "user" || item.content || item.plan || busy,
								)
								.map((item) => (
									<section
										key={item.id}
										className={
											item.role === "user" ? "rounded-lg bg-blue-50 p-3" : "p-2"
										}
									>
										<Tag color={item.role === "user" ? "blue" : "purple"}>
											{item.role === "user" ? "你" : "Intent"}
										</Tag>
										<div className="prose max-w-none text-sm">
											<ReactMarkdown remarkPlugins={[remarkGfm]}>
												{item.content}
											</ReactMarkdown>
										</div>
										{item.plan && (
											<PlanCard
												key={item.plan.id}
												initialPlan={item.plan}
												onUpdated={(plan) =>
													setMessages((old) =>
														old.map((entry) =>
															entry.id === item.id ? { ...entry, plan } : entry,
														),
													)
												}
											/>
										)}
										{item.planUnavailable && (
											<Alert
												type="warning"
												title="关联计划暂时无法加载"
												action={
													<Button
														size="small"
														onClick={() =>
															history.push(
																`/ai/sessions?planId=${encodeURIComponent(item.planId ?? "")}`,
															)
														}
													>
														查看计划
													</Button>
												}
											/>
										)}
										{!!item.evidence?.length && (
											<Space wrap className="mt-2">
												{[
													...new Set(
														item.evidence
															.map(evidenceURL)
															.filter((url): url is string => !!url),
													),
												].map((url) => {
													return (
														url && (
															<Button
																key={url}
																size="small"
																type="link"
																onClick={() => history.push(url)}
															>
																核对数据：{url.split("?")[0]}
															</Button>
														)
													);
												})}
											</Space>
										)}
									</section>
								))}
							{busy && (
								<Space>
									<Spin size="small" />
									<Typography.Text type="secondary">
										{tool || "正在生成…"}
									</Typography.Text>
								</Space>
							)}
							<div ref={bottom} />
						</div>
					)}
					{error && (
						<Alert type="error" showIcon title={error} className="my-3" />
					)}
					<Input.TextArea
						value={text}
						disabled={restoring}
						onChange={(event) => setText(event.target.value)}
						placeholder="输入意图，例如：把选中的用户停用（Shift+Enter 换行）"
						autoSize={{ minRows: 2, maxRows: 6 }}
						className="mt-4"
						onPressEnter={(event) => {
							if (!event.shiftKey && !event.nativeEvent.isComposing) {
								event.preventDefault();
								void send(text);
							}
						}}
					/>
					<Space wrap className="mt-3">
						<Button
							type="primary"
							disabled={!text.trim() || !models.length || restoring}
							loading={busy}
							onClick={() => void send(text)}
						>
							发送
						</Button>
						{busy && <Button onClick={() => stopActive()}>停止</Button>}
						{sessionId && (
							<Button
								danger
								disabled={busy || restoring}
								onClick={async () => {
									await api.del(`/ai/sessions/${sessionId}`);
									newConversation();
									await refreshSessions();
									message.success("会话已删除");
								}}
							>
								删除会话
							</Button>
						)}
					</Space>
					{!models.length && (
						<Alert
							type="warning"
							className="mt-4"
							title="尚无可用模型"
							description="请在模型服务中配置并测试支持工具调用的模型后使用 AI。"
							action={
								<Button onClick={() => history.push("/system/ai-provider")}>
									模型服务
								</Button>
							}
						/>
					)}
				</>
			)}
		</Drawer>
	);
}
