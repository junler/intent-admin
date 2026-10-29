import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { App } from "antd";
vi.mock("@umijs/max", () => ({ history: { push: vi.fn() } }));
vi.mock("@/utils/request", () => ({
	api: { get: vi.fn() },
	request: vi.fn(),
	download: vi.fn(),
	auth: { get: () => "" },
	ApiError: class extends Error {},
	Code: {},
}));
vi.mock("@ant-design/pro-components", () => ({
	ProForm: () => null,
	ProTable: ({ columns }: { columns: any[] }) => (
		<div>
			{columns.slice(0, -1).map((column) => (
				<span key={String(column.dataIndex)}>
					{column.render(<strong>formatted node</strong>, {
						userName: "alice",
						status: "0",
					})}
				</span>
			))}
		</div>
	),
}));
import { CrudPage } from "./CrudPage";
describe("ProTable raw cell values", () => {
	it("renders raw API fields instead of stringifying ProTable formatted React nodes", () => {
		const html = renderToStaticMarkup(
			<App>
				<CrudPage
					title="Users"
					base="/system/user"
					idKey="userId"
					perm="system:user"
					canCreate={false}
					canEdit={false}
					canDelete={false}
					fields={[
						{
							name: "userName",
							label: "Account",
							search: true,
							required: true,
						},
						{
							name: "status",
							label: "Status",
							render: (value) => (value === "0" ? "正常" : "停用"),
						},
					]}
				/>
			</App>,
		);
		expect(html).toContain("alice");
		expect(html).toContain("正常");
		expect(html).not.toContain("[object Object]");
		expect(html).not.toContain("formatted node");
		expect(html).not.toContain("ant-form-item-required");
	});
});
