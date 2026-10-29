-- +goose Up
-- Serialise explicit seed reruns as well as initial migrations.
SELECT pg_advisory_xact_lock(745186203);

-- Inserts never replace a password, an operator's configuration, or edited menus.
INSERT INTO sys_user (user_name, nick_name, password, create_by)
VALUES ('admin', '管理员', '$2a$10$RnxrYwnRrVQYfhHXwPLbUuJPEeQFbCVfEyK6mDW1P7qrF1CcOeNoO', 'system')
ON CONFLICT (user_name) WHERE del_flag = '0' DO NOTHING;

INSERT INTO sys_role (role_name, role_key, role_sort, create_by)
VALUES ('超级管理员', 'admin', 1, 'system')
ON CONFLICT (role_key) WHERE del_flag = '0' DO NOTHING;

INSERT INTO sys_user_role (user_id, role_id)
SELECT u.user_id, r.role_id FROM sys_user u CROSS JOIN sys_role r
WHERE u.user_name = 'admin' AND u.del_flag = '0'
  AND r.role_key = 'admin' AND r.del_flag = '0'
ON CONFLICT DO NOTHING;

-- Only shipped pages are navigable. Later phases add their menus with their pages.
INSERT INTO sys_menu (menu_name, path, component, menu_type, order_num, perms, icon, create_by)
SELECT s.menu_name, s.path, s.component, 'C', s.order_num, s.perms, s.icon, 'system'
FROM (VALUES
  ('示例条目', '/demo/item', 'demo/item/index', 1, 'demo:item:list', 'AppstoreOutlined'),
  ('能力浏览器', '/capabilities', 'capabilities/index', 2, 'ai:capability:list', 'ApiOutlined')
) AS s(menu_name, path, component, order_num, perms, icon)
WHERE NOT EXISTS (
  SELECT 1 FROM sys_menu m WHERE m.path = s.path AND m.menu_type = 'C' AND m.del_flag = '0'
);

INSERT INTO sys_menu (menu_name, parent_id, menu_type, order_num, perms, create_by)
SELECT s.menu_name, m.menu_id, 'F', s.order_num, s.perms, 'system'
FROM sys_menu m CROSS JOIN (VALUES
  ('修改示例条目', 1, 'demo:item:edit'),
  ('删除示例条目', 2, 'demo:item:remove'),
  ('导出示例条目', 3, 'demo:item:export')
) AS s(menu_name, order_num, perms)
WHERE m.path = '/demo/item' AND m.menu_type = 'C' AND m.del_flag = '0'
  AND NOT EXISTS (SELECT 1 FROM sys_menu e WHERE e.perms = s.perms AND e.del_flag = '0');

INSERT INTO sys_role_menu (role_id, menu_id)
SELECT r.role_id, m.menu_id FROM sys_role r CROSS JOIN sys_menu m
WHERE r.role_key = 'admin' AND r.del_flag = '0' AND m.del_flag = '0'
ON CONFLICT DO NOTHING;

INSERT INTO sys_config (config_name, config_key, config_value, config_type, create_by)
VALUES
  ('登录验证码开关', 'sys.account.captchaEnabled', 'true', 'Y', 'system'),
  ('用户注册开关', 'sys.account.registerUser', 'false', 'Y', 'system'),
  ('用户初始密码', 'sys.user.initPassword', '123456', 'Y', 'system'),
  ('侧边栏主题', 'sys.index.sideTheme', 'light', 'Y', 'system'),
  ('变更历史保留天数', 'sys.changeset.retainDays', '90', 'Y', 'system')
ON CONFLICT (config_key) DO NOTHING;

INSERT INTO sys_dict_type (dict_name, dict_type, create_by)
VALUES
  ('用户性别', 'sys_user_sex', 'system'),
  ('菜单状态', 'sys_show_hide', 'system'),
  ('系统开关', 'sys_yes_no', 'system'),
  ('任务分组', 'sys_job_group', 'system'),
  ('任务状态', 'sys_job_status', 'system'),
  ('通知类型', 'sys_notice_type', 'system'),
  ('通知状态', 'sys_notice_status', 'system'),
  ('操作类型', 'sys_oper_type', 'system'),
  ('系统状态', 'sys_normal_disable', 'system'),
  ('操作状态', 'sys_common_status', 'system'),
  ('调用来源', 'sys_call_source', 'system'),
  ('主体类型', 'sys_actor_type', 'system'),
  ('风险等级', 'sys_risk_level', 'system'),
  ('变更集状态', 'sys_changeset_status', 'system'),
  ('审批方式', 'sys_approval_mode', 'system'),
  ('模型厂商', 'sys_ai_vendor', 'system'),
  ('存储驱动', 'sys_storage_driver', 'system'),
  ('URL模式', 'sys_storage_url_mode', 'system')
ON CONFLICT (dict_type) DO NOTHING;

INSERT INTO sys_dict_data (dict_sort, dict_label, dict_value, dict_type, list_class, create_by)
VALUES
  (1, '男', '0', 'sys_user_sex', 'blue', 'system'),
  (2, '女', '1', 'sys_user_sex', 'magenta', 'system'),
  (3, '未知', '2', 'sys_user_sex', 'default', 'system'),
  (1, '显示', '0', 'sys_show_hide', 'green', 'system'),
  (2, '隐藏', '1', 'sys_show_hide', 'default', 'system'),
  (1, '是', 'Y', 'sys_yes_no', 'green', 'system'),
  (2, '否', 'N', 'sys_yes_no', 'default', 'system'),
  (1, '默认', 'DEFAULT', 'sys_job_group', 'blue', 'system'),
  (2, '系统', 'SYSTEM', 'sys_job_group', 'purple', 'system'),
  (1, '正常', '0', 'sys_job_status', 'green', 'system'),
  (2, '暂停', '1', 'sys_job_status', 'orange', 'system'),
  (1, '通知', '1', 'sys_notice_type', 'blue', 'system'),
  (2, '公告', '2', 'sys_notice_type', 'orange', 'system'),
  (1, '正常', '0', 'sys_notice_status', 'green', 'system'),
  (2, '关闭', '1', 'sys_notice_status', 'default', 'system'),
  (1, '其他', '0', 'sys_oper_type', 'default', 'system'),
  (2, '新增', '1', 'sys_oper_type', 'green', 'system'),
  (3, '修改', '2', 'sys_oper_type', 'blue', 'system'),
  (4, '删除', '3', 'sys_oper_type', 'red', 'system'),
  (5, '授权', '4', 'sys_oper_type', 'purple', 'system'),
  (6, '导出', '5', 'sys_oper_type', 'cyan', 'system'),
  (7, '导入', '6', 'sys_oper_type', 'cyan', 'system'),
  (8, '强退', '7', 'sys_oper_type', 'orange', 'system'),
  (9, '清空', '8', 'sys_oper_type', 'red', 'system'),
  (1, '正常', '0', 'sys_normal_disable', 'green', 'system'),
  (2, '停用', '1', 'sys_normal_disable', 'red', 'system'),
  (1, '成功', '0', 'sys_common_status', 'green', 'system'),
  (2, '失败', '1', 'sys_common_status', 'red', 'system'),
  (1, '页面', 'http', 'sys_call_source', 'blue', 'system'),
  (2, 'AI', 'ai', 'sys_call_source', 'purple', 'system'),
  (3, 'MCP', 'mcp', 'sys_call_source', 'cyan', 'system'),
  (4, '定时任务', 'task', 'sys_call_source', 'orange', 'system'),
  (1, '用户', 'human', 'sys_actor_type', 'blue', 'system'),
  (2, '智能体', 'agent', 'sys_actor_type', 'purple', 'system'),
  (3, 'MCP客户端', 'mcp_client', 'sys_actor_type', 'cyan', 'system'),
  (4, '系统', 'system', 'sys_actor_type', 'orange', 'system'),
  (1, '低风险', 'low', 'sys_risk_level', 'green', 'system'),
  (2, '中风险', 'medium', 'sys_risk_level', 'orange', 'system'),
  (3, '高风险', 'high', 'sys_risk_level', 'red', 'system'),
  (4, '极高风险', 'critical', 'sys_risk_level', 'magenta', 'system'),
  (1, '已应用', 'applied', 'sys_changeset_status', 'green', 'system'),
  (2, '部分完成', 'partial', 'sys_changeset_status', 'orange', 'system'),
  (3, '已回滚', 'rolled_back', 'sys_changeset_status', 'blue', 'system'),
  (4, '回滚失败', 'rollback_failed', 'sys_changeset_status', 'red', 'system'),
  (1, '自动', 'auto', 'sys_approval_mode', 'default', 'system'),
  (2, '已确认', 'confirmed', 'sys_approval_mode', 'green', 'system'),
  (3, '差异确认', 'diff_confirmed', 'sys_approval_mode', 'blue', 'system'),
  (4, '手动', 'manual', 'sys_approval_mode', 'purple', 'system'),
  (1, 'DeepSeek', 'deepseek', 'sys_ai_vendor', 'blue', 'system'),
  (2, '火山方舟', 'ark', 'sys_ai_vendor', 'blue', 'system'),
  (3, '阿里云百炼', 'dashscope', 'sys_ai_vendor', 'blue', 'system'),
  (4, 'Moonshot', 'moonshot', 'sys_ai_vendor', 'blue', 'system'),
  (5, '智谱', 'zhipu', 'sys_ai_vendor', 'blue', 'system'),
  (6, '硅基流动', 'siliconflow', 'sys_ai_vendor', 'blue', 'system'),
  (7, 'OpenAI', 'openai', 'sys_ai_vendor', 'blue', 'system'),
  (8, '自定义', 'custom', 'sys_ai_vendor', 'default', 'system'),
  (1, '本地', 'local', 'sys_storage_driver', 'blue', 'system'),
  (2, 'S3兼容', 's3', 'sys_storage_driver', 'cyan', 'system'),
  (1, '静态地址', 'static', 'sys_storage_url_mode', 'blue', 'system'),
  (2, '预签名', 'presigned', 'sys_storage_url_mode', 'orange', 'system')
ON CONFLICT (dict_type, dict_value) DO NOTHING;

-- +goose Down
-- Seed data may have been edited or referenced by real users. Keep it on a
-- seed rollback; rolling back 00002 removes the schema and its data explicitly.
SELECT 1;
