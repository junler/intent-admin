import {
	DatePicker,
	Form,
	Input,
	InputNumber,
	Select,
	Switch,
	TreeSelect,
} from "antd";
import type { Field } from "./types";
import { CronInput } from "./CronInput";
export function Fields({
	fields,
	editing = false,
}: {
	fields: Field[];
	editing?: boolean;
}) {
	return (
		<>
			{fields
				.filter(
					(f) => !f.hidden && f.form !== false && !(editing && f.createOnly),
				)
				.map((f) => (
					<Form.Item
						key={f.name}
						name={f.name}
						label={f.label}
						help={f.help}
						rules={[
							...(f.required || (!editing && f.createRequired)
								? [{ required: true, message: `请填写${f.label}` }]
								: []),
							...(f.rules ?? []),
						]}
						valuePropName={f.type === "switch" ? "checked" : "value"}
					>
						{f.type === "cron" ? (
							<CronInput />
						) : f.type === "tree" ? (
							<TreeSelect
								treeCheckable
								treeDefaultExpandAll
								showSearch
								treeNodeFilterProp="title"
								treeData={f.treeData}
								showCheckedStrategy={TreeSelect.SHOW_ALL}
							/>
						) : f.type === "number" ? (
							<InputNumber className="w-full" />
						) : f.type === "select" || f.type === "multi" ? (
							<Select
								allowClear
								showSearch
								optionFilterProp="label"
								mode={
									f.type === "multi"
										? f.options
											? "multiple"
											: "tags"
										: undefined
								}
								options={f.options}
							/>
						) : f.type === "password" ? (
							<Input.Password
								autoComplete="new-password"
								placeholder={editing ? "留空保留现有值" : undefined}
							/>
						) : f.type === "textarea" || f.type === "json" ? (
							<Input.TextArea rows={f.type === "json" ? 6 : 3} />
						) : f.type === "switch" ? (
							<Switch />
						) : f.type === "date" ? (
							<DatePicker showTime className="w-full" />
						) : (
							<Input allowClear />
						)}
					</Form.Item>
				))}
		</>
	);
}
