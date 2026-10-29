import { describe, it, expect } from "vitest";
import {
	exportUploadId,
	readNoticeIds,
	safeDownloadURL,
} from "./notifications";
describe("owned export delivery", () => {
	it("uses the server-supplied owned file ID without routing through global file management", () => {
		const id = "bd858f64-0cb6-4cc6-979c-3cfb55e92466";
		expect(
			exportUploadId({
				noticeId: 1,
				noticeTitle: "导出完成：登录日志",
				uploadId: id,
			}),
		).toBe(id);
		expect(
			exportUploadId({ noticeId: 2, noticeTitle: "普通公告" }),
		).toBeUndefined();
		expect(
			exportUploadId({
				noticeId: 3,
				noticeTitle: "导出完成",
				uploadId: "../../private",
			}),
		).toBeUndefined();
	});
	it("resolves API-relative signed URLs against the current application origin", () => {
		expect(
			safeDownloadURL(
				"/api/public/files/file?token=signed",
				"http://localhost:18081",
			),
		).toBe("http://localhost:18081/api/public/files/file?token=signed");
		expect(
			safeDownloadURL(
				"https://storage.example/file?signature=x",
				"http://localhost:18081",
			),
		).toBe("https://storage.example/file?signature=x");
	});
	it("rejects script and data links", () => {
		expect(
			safeDownloadURL("javascript:alert(1)", "http://localhost"),
		).toBeUndefined();
		expect(
			safeDownloadURL("data:text/html,invalid", "http://localhost"),
		).toBeUndefined();
	});
	it("treats invalid read markers as unread and retains only positive numeric IDs", () => {
		expect(readNoticeIds("invalid")).toEqual([]);
		expect(readNoticeIds('[1,"2",null,-1,0,3]')).toEqual([1, 3]);
	});
});
