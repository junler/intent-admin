import type { Row } from "./types";
export function deletionArgs(idKey: string, ids: unknown[]): Row {
	return {
		ids,
		[`${idKey}s`]: ids,
		...(ids.length === 1 ? { [idKey]: ids[0] } : {}),
	};
}
export function cleanForm(values: Row): Row {
	return Object.fromEntries(
		Object.entries(values).filter(
			([key, value]) => !key.startsWith("__") && value !== undefined,
		),
	);
}
export function rowsToTree(
	rows: Row[],
	idKey: string,
	parentKey = "parentId",
): Row[] {
	const byId = new Map<string, Row>(
		rows.map((row) => [String(row[idKey]), { ...row, children: [] as Row[] }]),
	);
	const roots: Row[] = [];
	for (const row of byId.values()) {
		const parent = byId.get(String(row[parentKey]));
		if (parent && parent !== row) parent.children.push(row);
		else roots.push(row);
	}
	const prune = (items: Row[], seen = new Set<string>()): Row[] =>
		items
			.filter((row) => !seen.has(String(row[idKey])))
			.map((row) => {
				const next = new Set(seen).add(String(row[idKey]));
				const children = prune(row.children, next);
				return { ...row, children: children.length ? children : undefined };
			});
	return prune(roots);
}

export function menuTreeOptions(rows: Row[]): Row[] {
	return rowsToTree(rows, "menuId").map(function convert(row): Row {
		return {
			title: row.menuName,
			value: row.menuId,
			children: row.children?.map(convert),
		};
	});
}

/** Credential masks displayed by list APIs never enter write forms. */
export function editFormValues(
	fields: import("./types").Field[],
	record: Row,
): Row {
	const values = { ...record };
	for (const field of fields) {
		if (field.type === "password") values[field.name] = "";
		if (field.type === "json" && typeof values[field.name] !== "string")
			values[field.name] = values[field.name]
				? JSON.stringify(values[field.name], null, 2)
				: "";
	}
	return values;
}
export function writeFormValues(
	fields: import("./types").Field[],
	input: Row,
): Row {
	const values = cleanForm(input);
	for (const field of fields) {
		if (field.type === "date" && values[field.name]?.toISOString)
			values[field.name] = values[field.name].toISOString();
		if (field.type === "password" && !values[field.name])
			delete values[field.name];
		if (field.type === "json" && values[field.name])
			values[field.name] = JSON.parse(values[field.name]);
	}
	return values;
}
