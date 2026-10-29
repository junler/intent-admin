import type { MenuRoute } from "./menu";
import type { PageLocation } from "@/contexts/ActivePageContext";
export interface CachedPage {
	path: string;
	component: string;
	query: string;
	location: PageLocation;
}
const blockedKeys = new Set(["__proto__", "prototype", "constructor"]);
export function withDefaultQuery(search: string, query?: string): string {
	if (!query) return search;
	let defaults: unknown;
	try {
		defaults = JSON.parse(query);
	} catch {
		return search;
	}
	if (!defaults || typeof defaults !== "object" || Array.isArray(defaults))
		return search;
	const result = new URLSearchParams(search);
	let changed = false;
	for (const [key, value] of Object.entries(defaults)) {
		if (blockedKeys.has(key) || result.has(key)) continue;
		const values = Array.isArray(value) ? value : [value];
		const safe = values.filter((item) =>
			["string", "number", "boolean"].includes(typeof item),
		);
		if (!safe.length) continue;
		for (const item of safe) result.append(key, String(item));
		changed = true;
	}
	return changed ? `?${result.toString()}` : search;
}
/** LRU order is oldest first. A route change or revoked grant discards its old instance. */
export function reconcilePageCache(
	previous: CachedPage[],
	routes: MenuRoute[],
	current: MenuRoute | undefined,
	location: PageLocation,
	limit = 10,
): CachedPage[] {
	const allowed = new Map(
		routes
			.filter(
				(route) => !route.external && route.component && !route.meta?.noCache,
			)
			.map((route) => [route.path, route]),
	);
	const retained = previous.filter((entry) => {
		const route = allowed.get(entry.path);
		return (
			route?.component === entry.component &&
			(route.query ?? "") === entry.query
		);
	});
	if (current && allowed.has(current.path)) {
		const existing = retained.find((entry) => entry.path === current.path);
		const same =
			existing &&
			existing.location.key === location.key &&
			existing.location.search === location.search &&
			existing.location.hash === location.hash;
		const next = same
			? existing
			: {
					path: current.path,
					component: current.component!,
					query: current.query ?? "",
					location,
				};
		return [
			...retained.filter((entry) => entry.path !== current.path),
			next,
		].slice(-Math.max(1, limit));
	}
	return retained.slice(-Math.max(1, limit));
}
export function cacheSignature(entries: CachedPage[]): string {
	return JSON.stringify(
		entries.map((entry) => [
			entry.path,
			entry.component,
			entry.query,
			entry.location.key,
			entry.location.search,
			entry.location.hash,
		]),
	);
}
