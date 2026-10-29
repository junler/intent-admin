import { describe, it, expect } from "vitest";
import { withDefaultQuery, reconcilePageCache } from "./pageCache";
import type { MenuRoute } from "./menu";
import type { PageLocation } from "@/contexts/ActivePageContext";
const location = (path: string, key = path, search = ""): PageLocation => ({
	pathname: path,
	key,
	search,
	hash: "",
	state: null,
});
const routes: MenuRoute[] = ["a", "b", "c"].map((path) => ({
	path: `/${path}`,
	component: `${path}/index`,
}));
describe("default route query", () => {
	it("fills only missing keys and preserves explicit empty or repeated URL values", () => {
		expect(
			withDefaultQuery(
				"?status=&name=alice&tag=user&tag=ops",
				'{"status":"0","pageSize":20,"tag":["default"],"enabled":true}',
			),
		).toBe("?status=&name=alice&tag=user&tag=ops&pageSize=20&enabled=true");
	});
	it("supports scalar arrays without injecting nested objects or prototype keys", () => {
		expect(
			withDefaultQuery(
				"",
				'{"ids":[1,2],"nested":{"x":1},"__proto__":"bad","missing":null}',
			),
		).toBe("?ids=1&ids=2");
	});
	it.each(["broken", "[]", "null", '"status"'])(
		"ignores malformed defaults %s",
		(query) => {
			expect(withDefaultQuery("?status=1", query)).toBe("?status=1");
		},
	);
});
describe("authorized page cache", () => {
	it("evicts the least recently visited page while retaining recently touched instances", () => {
		let cache = reconcilePageCache([], routes, routes[0], location("/a"), 2);
		cache = reconcilePageCache(cache, routes, routes[1], location("/b"), 2);
		cache = reconcilePageCache(
			cache,
			routes,
			routes[0],
			location("/a", "a-again", "?status=1"),
			2,
		);
		cache = reconcilePageCache(cache, routes, routes[2], location("/c"), 2);
		expect(cache.map((page) => page.path)).toEqual(["/a", "/c"]);
		expect(cache[0].location.search).toBe("?status=1");
	});
	it("removes revoked, reconfigured, and noCache pages before they can stay mounted", () => {
		let cache = reconcilePageCache([], routes, routes[0], location("/a"));
		cache = reconcilePageCache(cache, routes, routes[1], location("/b"));
		expect(
			reconcilePageCache(cache, [routes[1]], undefined, location("/b")).map(
				(page) => page.path,
			),
		).toEqual(["/b"]);
		expect(
			reconcilePageCache(
				cache,
				[
					{ ...routes[0], component: "changed/index" },
					{ ...routes[1], meta: { noCache: true } },
				],
				undefined,
				location("/b"),
			),
		).toEqual([]);
	});
	it("never caches external links or pages explicitly marked noCache", () => {
		const noCache = { ...routes[0], meta: { noCache: true } };
		expect(reconcilePageCache([], [noCache], noCache, location("/a"))).toEqual(
			[],
		);
		const external = {
			path: "https://example.com",
			external: true,
			component: "InnerLink",
		};
		expect(
			reconcilePageCache([], [external], external, location("/a")),
		).toEqual([]);
	});
});
