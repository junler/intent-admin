import { Select, type SelectProps } from "antd";
import { useDict } from "@/hooks/useDict";
export function DictSelect({
	dictType,
	...props
}: SelectProps & { dictType: string }) {
	const { items, loading } = useDict(dictType);
	return (
		<Select
			allowClear
			showSearch
			optionFilterProp="label"
			loading={loading}
			options={items.map((item) => ({
				label: item.dictLabel,
				value: item.dictValue,
			}))}
			{...props}
		/>
	);
}
