-- +goose Up
CREATE TABLE IF NOT EXISTS sys_notification (
  notification_id UUID PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES sys_user(user_id),
  title VARCHAR(200) NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  link VARCHAR(500) NOT NULL,
  read_at TIMESTAMPTZ,
  create_time TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_notification_recipient ON sys_notification(user_id, create_time DESC);

SELECT pg_advisory_xact_lock(745186203);
INSERT INTO sys_menu (menu_name,path,component,perms,parent_id,order_num,visible,icon,menu_type,create_by)
SELECT s.name,s.path,s.component,s.perm,COALESCE(g.menu_id,0),s.sort,CASE WHEN s.path='/system/mcp-grants' THEN '1' ELSE '0' END,s.icon,'C','system'
FROM (VALUES
  ('审批中心','/ai/approvals','ai/approvals/index','ai:agent:use','/ai',3,'AuditOutlined'),
  ('队列管理','/monitor/queue','monitor/queue/index','monitor:queue:list','/monitor',8,'BarsOutlined'),
  ('MCP 授权','/system/mcp-grants','oauth/grants/index','','/system',13,'SafetyCertificateOutlined')
) AS s(name,path,component,perm,parent,sort,icon)
LEFT JOIN sys_menu g ON g.path=s.parent AND g.menu_type='M' AND g.del_flag='0'
WHERE NOT EXISTS (SELECT 1 FROM sys_menu m WHERE m.path=s.path AND m.menu_type='C' AND m.del_flag='0');

INSERT INTO sys_menu (menu_name,parent_id,menu_type,order_num,perms,create_by)
SELECT s.name,m.menu_id,'F',s.sort,s.perm,'system'
FROM (VALUES
  ('/ai/approvals','ai:plan:approve','审批计划',1),
  ('/monitor/queue','monitor:queue:query','查看队列任务',1),
  ('/monitor/queue','monitor:queue:retry','重试队列任务',2),
  ('/monitor/queue','monitor:queue:archive','归档队列任务',3),
  ('/monitor/queue','monitor:queue:cancel','取消队列任务',4),
  ('/monitor/queue','monitor:queue:remove','删除队列任务',5),
  ('/monitor/queue','monitor:queue:pause','暂停和恢复队列',6)
) AS s(path,perm,name,sort)
JOIN sys_menu m ON m.path=s.path AND m.menu_type='C' AND m.del_flag='0'
WHERE NOT EXISTS (SELECT 1 FROM sys_menu x WHERE x.perms=s.perm AND x.del_flag='0');

-- +goose Down
DELETE FROM sys_menu WHERE create_by='system' AND perms IN ('ai:plan:approve','monitor:queue:query','monitor:queue:retry','monitor:queue:archive','monitor:queue:cancel','monitor:queue:remove','monitor:queue:pause');
DELETE FROM sys_menu WHERE create_by='system' AND path IN ('/ai/approvals','/monitor/queue','/system/mcp-grants');
DROP TABLE IF EXISTS sys_notification;
