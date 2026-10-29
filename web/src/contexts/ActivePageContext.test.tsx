import { describe, it, expect } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter, useLocation, useSearchParams } from "react-router-dom";
import { FrozenPageBoundary, usePageActive } from "./ActivePageContext";
function Reader() {
	const location = useLocation();
	const [params] = useSearchParams();
	return (
		<span>{`${usePageActive()}:${location.pathname}:${params.get("status")}`}</span>
	);
}
describe("cached page isolation", () => {
	it("keeps an inactive page on its own URL instead of the foreground page URL", () => {
		const html = renderToStaticMarkup(
			<MemoryRouter initialEntries={["/foreground?status=1"]}>
				<FrozenPageBoundary
					active={false}
					location={{
						pathname: "/cached",
						search: "?status=0",
						hash: "",
						key: "old",
						state: null,
					}}
				>
					<Reader />
				</FrozenPageBoundary>
				<Reader />
			</MemoryRouter>,
		);
		expect(html).toContain("false:/cached:0");
		expect(html).toContain("true:/foreground:1");
		expect(html).toContain('hidden=""');
	});
});
