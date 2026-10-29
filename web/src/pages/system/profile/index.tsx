import { useEffect, useState } from "react";
import { history } from "@umijs/max";
import {
	App,
	Avatar,
	Button,
	Card,
	Form,
	Input,
	Select,
	Space,
	Tabs,
	Upload,
} from "antd";
import { UserOutlined, UploadOutlined } from "@ant-design/icons";
import { api } from "@/utils/request";
import { useUserStore } from "@/stores/user";
import { useAdminAction } from "@/components/admin/useAdminAction";
import type { Row } from "@/components/admin/types";
export default function Profile() {
	const [form] = Form.useForm();
	const [password] = Form.useForm();
	const [user, setUser] = useState<Row>({});
	const { message } = App.useApp();
	async function load() {
		const data = await api.get<Row>("/system/user/profile");
		const value = data.user ?? data;
		setUser(value);
		form.setFieldsValue(value);
	}
	useEffect(() => {
		void load().catch(() => {});
	}, []);
	const action = useAdminAction(() => {
		void load().catch(() => {});
		useUserStore.getState().reset();
		void useUserStore
			.getState()
			.load()
			.catch(() => {});
	});
	async function saveProfile() {
		const values = await form.validateFields();
		await action.run({
			title: "更新个人资料",
			url: "/system/user/profile",
			method: "PUT",
			args: values,
			previewUrl: "/system/user/profile/update/preview",
		});
	}
	async function savePassword() {
		const values = await password.validateFields();
		await action.run({
			title: "修改登录密码",
			url: "/system/user/profile/updatePwd",
			method: "PUT",
			args: {
				oldPassword: values.oldPassword,
				newPassword: values.newPassword,
			},
			previewUrl: "/system/user/profile/updatePwd/preview",
			after: () => password.resetFields(),
		});
	}
	return (
		<Card title="个人中心">
			<div className="mb-6 flex items-center gap-4">
				<Avatar
					size={72}
					src={user.avatar || undefined}
					icon={<UserOutlined />}
				/>
				<div>
					<h2 className="text-xl font-semibold">
						{user.nickName || user.userName}
					</h2>
					<Upload
						accept="image/png,image/jpeg,image/webp"
						showUploadList={false}
						customRequest={async (option) => {
							try {
								const data = new FormData();
								data.append("avatarfile", option.file);
								await api.post("/system/user/profile/avatar", data);
								option.onSuccess?.({});
								message.success("头像已更新");
								await load();
							} catch (error) {
								option.onError?.(error as Error);
							}
						}}
					>
						<Button icon={<UploadOutlined />}>上传头像</Button>
					</Upload>
					<Button
						className="ms-2"
						onClick={() => history.push("/system/token")}
					>
						访问令牌
					</Button>
				</div>
			</div>
			<Tabs
				items={[
					{
						key: "profile",
						label: "基本资料",
						children: (
							<Form form={form} layout="vertical" className="max-w-lg">
								<Form.Item
									name="nickName"
									label="昵称"
									rules={[{ required: true }]}
								>
									<Input />
								</Form.Item>
								<Form.Item
									name="email"
									label="邮箱"
									rules={[{ type: "email" }]}
								>
									<Input />
								</Form.Item>
								<Form.Item name="phonenumber" label="手机">
									<Input />
								</Form.Item>
								<Form.Item name="sex" label="性别">
									<Select
										options={[
											{ label: "男", value: "0" },
											{ label: "女", value: "1" },
											{ label: "未知", value: "2" },
										]}
									/>
								</Form.Item>
								<Button
									type="primary"
									onClick={() => void saveProfile().catch(() => {})}
								>
									保存资料
								</Button>
							</Form>
						),
					},
					{
						key: "password",
						label: "修改密码",
						children: (
							<Form form={password} layout="vertical" className="max-w-lg">
								<Form.Item
									name="oldPassword"
									label="当前密码"
									rules={[{ required: true }]}
								>
									<Input.Password autoComplete="current-password" />
								</Form.Item>
								<Form.Item
									name="newPassword"
									label="新密码"
									rules={[{ required: true }, { min: 6 }]}
								>
									<Input.Password autoComplete="new-password" />
								</Form.Item>
								<Form.Item
									name="confirmPassword"
									label="确认密码"
									dependencies={["newPassword"]}
									rules={[
										{ required: true },
										({ getFieldValue }) => ({
											validator: (_, value) =>
												value === getFieldValue("newPassword")
													? Promise.resolve()
													: Promise.reject(new Error("两次密码不一致")),
										}),
									]}
								>
									<Input.Password autoComplete="new-password" />
								</Form.Item>
								<Space>
									<Button
										type="primary"
										onClick={() => void savePassword().catch(() => {})}
									>
										修改密码
									</Button>
								</Space>
							</Form>
						),
					},
				]}
			/>
			{action.modal}
		</Card>
	);
}
