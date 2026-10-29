-- +goose Up
-- Menu rows are inserted by natural route/permission keys. Rerunning seed never
-- overwrites administrator edits; the one Phase 0 alias upgrade is narrowly scoped.
SELECT pg_advisory_xact_lock(745186203);

INSERT INTO sys_menu (menu_name,path,component,menu_type,order_num,icon,create_by)
SELECT s.name,s.path,'Layout','M',s.sort,s.icon,'system'
FROM (VALUES
  ('系统管理', '/system', 10, 'SettingOutlined'),
  ('系统监控', '/monitor', 20, 'MonitorOutlined'),
  ('变更与审计', '/changes', 30, 'HistoryOutlined'),
  ('AI', '/ai', 40, 'RobotOutlined')
) AS s(name,path,sort,icon)
WHERE NOT EXISTS (SELECT 1 FROM sys_menu m WHERE m.path=s.path AND m.menu_type='M' AND m.del_flag='0');

UPDATE sys_menu m SET path='/ai/capabilities',parent_id=g.menu_id
FROM sys_menu g
WHERE m.path='/capabilities' AND m.menu_name='能力浏览器' AND m.component='capabilities/index'
  AND m.perms='ai:capability:list' AND m.parent_id=0 AND m.del_flag='0'
  AND g.path='/ai' AND g.menu_type='M' AND g.del_flag='0'
  AND NOT EXISTS (SELECT 1 FROM sys_menu x WHERE x.path='/ai/capabilities' AND x.del_flag='0');

INSERT INTO sys_menu (menu_name,path,component,perms,parent_id,order_num,visible,icon,menu_type,create_by)
SELECT s.name,s.path,s.component,s.perm,COALESCE(g.menu_id,0),s.sort,s.visible,s.icon,'C','system'
FROM (VALUES
  ('首页', '/', 'dashboard/index', 'dashboard:view', '', 0, '0', 'DashboardOutlined'),
  ('用户管理', '/system/user', 'system/user/index', 'system:user:list', '/system', 1, '0', 'UserOutlined'),
  ('角色管理', '/system/role', 'system/role/index', 'system:role:list', '/system', 2, '0', 'TeamOutlined'),
  ('菜单管理', '/system/menu', 'system/menu/index', 'system:menu:list', '/system', 3, '0', 'MenuOutlined'),
  ('字典管理', '/system/dict/type', 'system/dict/type/index', 'system:dict:list', '/system', 4, '0', 'BookOutlined'),
  ('字典数据', '/system/dict/data', 'system/dict/data/index', 'system:dict:list', '/system', 5, '1', 'BookOutlined'),
  ('参数设置', '/system/config', 'system/config/index', 'system:config:list', '/system', 6, '0', 'SettingOutlined'),
  ('通知公告', '/system/notice', 'system/notice/index', 'system:notice:list', '/system', 7, '0', 'NotificationOutlined'),
  ('文件管理', '/system/upload', 'system/upload/index', 'system:upload:list', '/system', 8, '0', 'FolderOutlined'),
  ('模型服务', '/system/ai-provider', 'system/ai-provider/index', 'system:aiprovider:list', '/system', 9, '0', 'ApiOutlined'),
  ('存储配置', '/system/storage', 'system/storage/index', 'system:storage:list', '/system', 10, '0', 'CloudOutlined'),
  ('个人中心', '/system/profile', 'system/profile/index', '', '/system', 11, '1', 'UserOutlined'),
  ('访问令牌', '/system/token', 'system/token/index', '', '/system', 12, '1', 'KeyOutlined'),
  ('在线用户', '/monitor/online', 'monitor/online/index', 'monitor:online:list', '/monitor', 1, '0', 'TeamOutlined'),
  ('定时任务', '/monitor/job', 'monitor/job/index', 'monitor:job:list', '/monitor', 2, '0', 'ScheduleOutlined'),
  ('任务日志', '/monitor/jobLog', 'monitor/jobLog/index', 'monitor:job:list', '/monitor', 3, '0', 'FileTextOutlined'),
  ('操作日志', '/monitor/operlog', 'monitor/operlog/index', 'monitor:operlog:list', '/monitor', 4, '0', 'FileTextOutlined'),
  ('登录日志', '/monitor/loginlog', 'monitor/loginlog/index', 'monitor:loginlog:list', '/monitor', 5, '0', 'LoginOutlined'),
  ('服务监控', '/monitor/server', 'monitor/server/index', 'monitor:server:list', '/monitor', 6, '0', 'MonitorOutlined'),
  ('缓存监控', '/monitor/cache', 'monitor/cache/index', 'monitor:cache:list', '/monitor', 7, '0', 'DatabaseOutlined'),
  ('变更历史', '/changesets', 'changesets/index', 'system:changeset:list', '/changes', 1, '0', 'HistoryOutlined'),
  ('行为溯源', '/audit', 'audit/index', 'monitor:operlog:list', '/changes', 2, '0', 'AuditOutlined'),
  ('AI 会话', '/ai/sessions', 'ai/sessions/index', 'ai:agent:use', '/ai', 1, '0', 'RobotOutlined'),
  ('能力浏览器', '/ai/capabilities', 'capabilities/index', 'ai:capability:list', '/ai', 2, '0', 'ApiOutlined')
) AS s(name,path,component,perm,parent,sort,visible,icon)
LEFT JOIN sys_menu g ON g.path=s.parent AND g.menu_type='M' AND g.del_flag='0'
WHERE NOT EXISTS (SELECT 1 FROM sys_menu m WHERE m.path=s.path AND m.menu_type='C' AND m.del_flag='0');

INSERT INTO sys_menu (menu_name,parent_id,menu_type,order_num,perms,create_by)
SELECT s.name,m.menu_id,'F',s.sort,s.perm,'system'
FROM (VALUES
  ('/system/user', 'system:user:query', '查看', 1),
  ('/system/user', 'system:user:add', '新增', 2),
  ('/system/user', 'system:user:edit', '修改', 3),
  ('/system/user', 'system:user:remove', '删除', 4),
  ('/system/user', 'system:user:export', '导出', 5),
  ('/system/user', 'system:user:resetPwd', '重置密码', 6),
  ('/system/user', 'system:user:authPerm', '额外授权和风险限制', 7),
  ('/system/role', 'system:role:query', '查看', 1),
  ('/system/role', 'system:role:add', '新增', 2),
  ('/system/role', 'system:role:edit', '修改', 3),
  ('/system/role', 'system:role:remove', '删除', 4),
  ('/system/role', 'system:role:export', '导出', 5),
  ('/system/menu', 'system:menu:query', '查看', 1),
  ('/system/menu', 'system:menu:add', '新增', 2),
  ('/system/menu', 'system:menu:edit', '修改', 3),
  ('/system/menu', 'system:menu:remove', '删除', 4),
  ('/system/dict/type', 'system:dict:query', '查看', 1),
  ('/system/dict/type', 'system:dict:add', '新增', 2),
  ('/system/dict/type', 'system:dict:edit', '修改', 3),
  ('/system/dict/type', 'system:dict:remove', '删除', 4),
  ('/system/dict/type', 'system:dict:export', '导出', 5),
  ('/system/config', 'system:config:query', '查看', 1),
  ('/system/config', 'system:config:add', '新增', 2),
  ('/system/config', 'system:config:edit', '修改', 3),
  ('/system/config', 'system:config:remove', '删除', 4),
  ('/system/config', 'system:config:export', '导出', 5),
  ('/system/notice', 'system:notice:query', '查看', 1),
  ('/system/notice', 'system:notice:add', '新增', 2),
  ('/system/notice', 'system:notice:edit', '修改', 3),
  ('/system/notice', 'system:notice:remove', '删除', 4),
  ('/system/upload', 'system:upload:query', '查看', 1),
  ('/system/upload', 'system:upload:add', '新增', 2),
  ('/system/upload', 'system:upload:remove', '删除', 3),
  ('/system/ai-provider', 'system:aiprovider:query', '查看', 1),
  ('/system/ai-provider', 'system:aiprovider:add', '新增', 2),
  ('/system/ai-provider', 'system:aiprovider:edit', '修改', 3),
  ('/system/ai-provider', 'system:aiprovider:remove', '删除', 4),
  ('/system/storage', 'system:storage:query', '查看', 1),
  ('/system/storage', 'system:storage:add', '新增', 2),
  ('/system/storage', 'system:storage:edit', '修改', 3),
  ('/system/storage', 'system:storage:remove', '删除', 4),
  ('/monitor/online', 'monitor:online:forceLogout', '强制退出', 1),
  ('/monitor/job', 'monitor:job:query', '查看', 1),
  ('/monitor/job', 'monitor:job:add', '新增', 2),
  ('/monitor/job', 'monitor:job:edit', '修改', 3),
  ('/monitor/job', 'monitor:job:changeStatus', '启停', 4),
  ('/monitor/job', 'monitor:job:run', '立即执行', 5),
  ('/monitor/job', 'monitor:job:remove', '删除', 6),
  ('/monitor/job', 'monitor:job:export', '导出', 7),
  ('/monitor/operlog', 'monitor:operlog:remove', '删除', 1),
  ('/monitor/operlog', 'monitor:operlog:export', '导出', 2),
  ('/monitor/loginlog', 'monitor:loginlog:remove', '删除', 1),
  ('/monitor/loginlog', 'monitor:loginlog:export', '导出', 2),
  ('/monitor/loginlog', 'monitor:loginlog:unlock', '解除锁定', 3),
  ('/monitor/cache', 'monitor:cache:remove', '删除', 1),
  ('/changesets', 'system:changeset:query', '查看', 1),
  ('/changesets', 'system:changeset:rollback', '回滚', 2)
) AS s(parent,perm,name,sort)
JOIN sys_menu m ON m.path=s.parent AND m.menu_type='C' AND m.del_flag='0'
WHERE NOT EXISTS (SELECT 1 FROM sys_menu x WHERE x.perms=s.perm AND x.menu_type='F' AND x.del_flag='0');

-- The older seed also knows the legacy route. Merge only its untouched
-- duplicate when explicit seed reruns recreate it after the canonical route
-- already exists, preserving every role's effective capability permission.
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT rm.role_id,canonical.menu_id
FROM sys_menu legacy JOIN sys_role_menu rm ON rm.menu_id=legacy.menu_id
JOIN sys_menu canonical ON canonical.path='/ai/capabilities' AND canonical.menu_type='C' AND canonical.del_flag='0'
WHERE legacy.path='/capabilities' AND legacy.menu_name='能力浏览器'
  AND legacy.component='capabilities/index' AND legacy.perms='ai:capability:list'
  AND legacy.create_by='system' AND legacy.parent_id=0 AND legacy.del_flag='0'
ON CONFLICT DO NOTHING;

DELETE FROM sys_menu legacy
WHERE legacy.path='/capabilities' AND legacy.menu_name='能力浏览器'
  AND legacy.component='capabilities/index' AND legacy.perms='ai:capability:list'
  AND legacy.create_by='system' AND legacy.parent_id=0 AND legacy.del_flag='0'
  AND EXISTS (SELECT 1 FROM sys_menu canonical WHERE canonical.path='/ai/capabilities' AND canonical.menu_type='C' AND canonical.del_flag='0');

INSERT INTO sys_role_menu (role_id,menu_id)
SELECT r.role_id,m.menu_id FROM sys_role r CROSS JOIN sys_menu m
WHERE r.role_key='admin' AND r.del_flag='0' AND m.del_flag='0'
ON CONFLICT DO NOTHING;

-- +goose Down
-- Menus can be edited and referenced by live roles. Keep them on rollback;
-- the system-schema migration owns their eventual removal.
SELECT 1;
