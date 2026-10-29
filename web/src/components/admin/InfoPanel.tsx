import { Card, Descriptions, Tag } from "antd";
export function InfoPanel({ title, value }: { title: string; value: unknown }) {
	if (value === null || value === undefined) return null;
	if (Array.isArray(value))
		return (
			<Card size="small" title={title}>
				<pre className="overflow-auto whitespace-pre-wrap">
					{JSON.stringify(value, null, 2)}
				</pre>
			</Card>
		);
	if (typeof value === "object")
		return (
			<Card size="small" title={title}>
				<Descriptions
					column={{ xs: 1, sm: 2, lg: 3 }}
					items={Object.entries(value).map(([key, item]) => ({
						key,
						label: key,
						children:
							typeof item === "object" ? (
								<pre className="max-h-64 overflow-auto whitespace-pre-wrap">
									{JSON.stringify(item, null, 2)}
								</pre>
							) : typeof item === "boolean" ? (
								<Tag color={item ? "green" : "orange"}>{String(item)}</Tag>
							) : (
								String(item ?? "—")
							),
					}))}
				/>
			</Card>
		);
	return (
		<Card size="small" title={title}>
			{String(value)}
		</Card>
	);
}
