import { useEffect, useState } from "react";
import {
	Badge,
	Button,
	Empty,
	List,
	Popover,
	Space,
	Typography,
	App,
} from "antd";
import { BellOutlined } from "@ant-design/icons";
import { useUserStore } from "@/stores/user";
import { api, ApiError, type PageData } from "@/utils/request";
import {
	exportUploadId,
	safeDownloadURL,
	readNoticeIds,
	type Notice,
} from "./notifications";
export function NotificationBell() {
	const identity = useUserStore((s) => s.identity);
	const userId = identity?.user.userId;
	const [notices, setNotices] = useState<Notice[]>([]);
	const [read, setRead] = useState<number[]>([]);
	const [open, setOpen] = useState(false);
	const { modal, message } = App.useApp();
	const marker = `intent-admin:notices:read:${identity?.user.userId ?? "anonymous"}`;
	useEffect(() => {
		setRead(readNoticeIds(localStorage.getItem(marker)));
		setNotices([]);
		setOpen(false);
		if (!userId) return;
		let active = true;
		async function load() {
			try {
				const result = await api.get<PageData<Notice>>(
					"/notifications",
					{ pageNum: 1, pageSize: 20, status: "0" },
					{ silent: true },
				);
				if (active) setNotices(result.rows);
			} catch {
				/* The authenticated request layer handles expired sessions. */
			}
		}
		void load();
		const visible = () => {
			if (!document.hidden) void load();
		};
		const timer = window.setInterval(visible, 30_000);
		window.addEventListener("focus", visible);
		return () => {
			active = false;
			window.clearInterval(timer);
			window.removeEventListener("focus", visible);
		};
	}, [userId, marker]);
	function mark(ids: number[]) {
		const next = [...new Set([...ids, ...read])].slice(0, 200);
		setRead(next);
		localStorage.setItem(marker, JSON.stringify(next));
	}
	async function show(notice: Notice) {
		mark([notice.noticeId]);
		setOpen(false);
		const uploadId = exportUploadId(notice);
		if (uploadId) {
			try {
				const result = await api.get<{ url: string }>(
					`/system/upload/${encodeURIComponent(uploadId)}/url`,
					undefined,
					{ dedupe: false },
				);
				const url = safeDownloadURL(result.url, window.location.origin);
				if (!url) throw new Error("下载地址无效，请联系管理员");
				window.open(url, "_blank", "noopener,noreferrer");
			} catch (error) {
				if (!(error instanceof ApiError))
					message.error(error instanceof Error ? error.message : "下载失败");
			}
			return;
		}
		modal.info({
			title: notice.noticeTitle,
			width: 700,
			content: (
				<Typography.Paragraph className="whitespace-pre-wrap break-words">
					{notice.noticeContent}
				</Typography.Paragraph>
			),
		});
	}
	if (!userId) return null;
	const unread = notices.filter((notice) => !read.includes(notice.noticeId));
	return (
		<Popover
			open={open}
			onOpenChange={setOpen}
			trigger="click"
			placement="bottomRight"
			title={
				<Space className="w-full justify-between">
					<span>通知</span>
					<Button
						type="link"
						size="small"
						onClick={() => mark(notices.map((notice) => notice.noticeId))}
					>
						全部已读
					</Button>
				</Space>
			}
			content={
				<div className="max-h-96 w-80 overflow-auto">
					{notices.length ? (
						<List
							size="small"
							dataSource={notices}
							renderItem={(notice) => (
								<List.Item
									className="cursor-pointer"
									onClick={() => void show(notice)}
								>
									<List.Item.Meta
										title={
											<Space>
												<Badge
													status={
														read.includes(notice.noticeId)
															? "default"
															: "processing"
													}
												/>
												{notice.noticeTitle}
												{exportUploadId(notice) && " · 下载"}
											</Space>
										}
										description={
											<Typography.Text type="secondary">
												{notice.createTime
													? new Date(notice.createTime).toLocaleString()
													: ""}
											</Typography.Text>
										}
									/>
								</List.Item>
							)}
						/>
					) : (
						<Empty
							image={Empty.PRESENTED_IMAGE_SIMPLE}
							description="暂无通知"
						/>
					)}
				</div>
			}
		>
			<Button type="text" aria-label={`通知，${unread.length} 条未读`}>
				<Badge count={unread.length} size="small">
					<BellOutlined className="text-lg" />
				</Badge>
			</Button>
		</Popover>
	);
}
