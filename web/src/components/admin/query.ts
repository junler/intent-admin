import type { Field, Row } from "./types";
export function readCrudQuery(fields: Field[], search: string) {
	const params = new URLSearchParams(search);
	const filters: Row = {};
	for (const field of fields.filter((item) => item.search)) {
		if (!params.has(field.name)) continue;
		const raw = params.get(field.name) ?? "";
		if (field.type === "number") {
			const value = Number(raw);
			if (raw !== "" && Number.isFinite(value)) filters[field.name] = value;
		} else if (field.type === "multi")
			filters[field.name] = params.getAll(field.name);
		else filters[field.name] = raw;
	}
	const integer = (key: string, fallback: number, max: number) => {
		const value = Number(params.get(key));
		return Number.isInteger(value) && value > 0
			? Math.min(value, max)
			: fallback;
	};
	return {
		filters,
		page: {
			pageNum: integer("pageNum", 1, 1_000_000),
			pageSize: integer("pageSize", 10, 100),
		},
	};
}
export function writeCrudQuery(
	fields: Field[],
	search: string,
	filters: Row,
	page: { pageNum: number; pageSize: number },
): string {
	const params = new URLSearchParams(search);
	for (const field of fields.filter((item) => item.search)) {
		params.delete(field.name);
		const value = filters[field.name];
		if (Array.isArray(value)) {
			if (value.length)
				for (const item of value) params.append(field.name, String(item));
			else params.set(field.name, "");
		} else
			params.set(
				field.name,
				value === undefined || value === null ? "" : String(value),
			);
	}
	params.set("pageNum", String(page.pageNum));
	params.set("pageSize", String(page.pageSize));
	return `?${params.toString()}`;
}
export function listCapability(base: string): string {
	return (
		(
			{
				"/system/dict/type": "system.dictType.list",
				"/system/dict/data": "system.dictData.list",
				"/system/ai-provider": "system.aiProvider.list",
			} as Record<string, string>
		)[base] ?? `${base.replace(/^\//, "").replace(/\//g, ".")}.list`
	);
}
