// Package knowledge supplies factual dictionary, entity and rule definitions to
// the agent without filling every conversation with the whole schema.
package knowledge

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"gorm.io/gorm"
)

type Knowledge struct {
	Enums    []EnumDef   `json:"enums"`
	Entities []EntityDef `json:"entities"`
	Rules    []RuleDef   `json:"rules"`
}
type EnumDef struct {
	DictType string            `json:"dictType"`
	Summary  string            `json:"summary"`
	Values   map[string]string `json:"values"`
	UsedBy   []string          `json:"usedBy,omitempty"`
}
type Relation struct {
	Kind    string `json:"kind"`
	Target  string `json:"target"`
	Via     string `json:"via,omitempty"`
	Summary string `json:"summary"`
}
type EntityDef struct {
	Table      string     `json:"table"`
	Summary    string     `json:"summary"`
	KeyField   string     `json:"keyField"`
	LabelField string     `json:"labelField"`
	Relations  []Relation `json:"relations,omitempty"`
	Notes      []string   `json:"notes,omitempty"`
}
type RuleDef struct {
	Scope    string `json:"scope"`
	Rule     string `json:"rule"`
	Enforced bool   `json:"enforced"`
}
type Service struct {
	db      *gorm.DB
	mu      sync.Mutex
	expires time.Time
	enums   []EnumDef
}

func New(db *gorm.DB) *Service { return &Service{db: db} }

var registry = struct {
	sync.RWMutex
	entities []EntityDef
	rules    []RuleDef
}{}

func RegisterEntity(entity EntityDef) {
	registry.Lock()
	defer registry.Unlock()
	for i, e := range registry.entities {
		if e.Table == entity.Table {
			registry.entities[i] = entity
			return
		}
	}
	registry.entities = append(registry.entities, entity)
}
func RegisterRule(rule RuleDef) {
	registry.Lock()
	defer registry.Unlock()
	for _, r := range registry.rules {
		if r.Scope == rule.Scope && r.Rule == rule.Rule {
			return
		}
	}
	registry.rules = append(registry.rules, rule)
}
func init() {
	defs := [][4]string{{"sys_user", "系统用户", "user_id", "user_name"}, {"sys_role", "角色", "role_id", "role_name"}, {"sys_menu", "菜单和权限点", "menu_id", "menu_name"}, {"sys_config", "系统参数", "config_id", "config_name"}, {"sys_notice", "通知公告", "notice_id", "notice_title"}, {"sys_dict_type", "字典类型", "dict_id", "dict_name"}, {"sys_dict_data", "字典项", "dict_code", "dict_label"}, {"sys_upload", "文件", "upload_id", "file_name"}, {"sys_job", "定时任务", "job_id", "job_name"}, {"sys_job_log", "任务执行日志", "job_log_id", "job_name"}, {"sys_oper_log", "行为溯源", "oper_id", "capability"}, {"sys_login_log", "登录日志", "info_id", "user_name"}, {"sys_ai_provider", "模型服务配置", "provider_id", "name"}, {"sys_storage_config", "存储配置", "storage_id", "name"}, {"sys_access_token", "个人访问令牌", "token_id", "name"}, {"sys_ai_plan", "操作计划", "plan_id", "intent"}, {"sys_changeset", "变更集", "changeset_id", "summary"}, {"sys_changeset_item", "变更明细", "item_id", "entity_label"}, {"sys_ai_plan_approval", "跨人计划审批", "approval_id", "plan_id"}, {"sys_notification", "个人通知", "notification_id", "title"}, {"sys_oauth_grant", "MCP 授权", "grant_id", "client_id"}, {"demo_item", "示例条目", "item_id", "name"}}
	for _, d := range defs {
		RegisterEntity(EntityDef{Table: d[0], Summary: d[1], KeyField: d[2], LabelField: d[3], Notes: []string{"status='0' 正常，'1' 停用；del_flag='0' 存在，'2' 软删除（带这些字段的表）"}})
	}
	RegisterEntity(EntityDef{Table: "sys_user", Summary: "系统用户", KeyField: "user_id", LabelField: "user_name", Relations: []Relation{{Kind: "many2many", Target: "sys_role", Via: "sys_user_role", Summary: "用户所属角色"}, {Kind: "one2many", Target: "sys_user_perm", Summary: "用户额外授权，仅叠加不撤回角色权限"}}, Notes: []string{"ai_max_risk为空时用全局值；用户风险上限不能提高全局上限", "password是bcrypt哈希，不得向模型返回"}})
	RegisterEntity(EntityDef{Table: "sys_role", Summary: "角色", KeyField: "role_id", LabelField: "role_name", Relations: []Relation{{Kind: "many2many", Target: "sys_menu", Via: "sys_role_menu", Summary: "角色的菜单和按钮授权"}}, Notes: []string{"role_key='admin' 的有效角色获得 *:*:* 权限"}})
	RegisterRule(RuleDef{Scope: "*", Rule: "写操作必须先预演并确认；计划执行前重新检查当前数据，变化时返回412", Enforced: true})
	RegisterRule(RuleDef{Scope: "*", Rule: "AI和MCP调用同时受权限、入口可见性和风险上限约束", Enforced: true})
	RegisterRule(RuleDef{Scope: "sys_user", Rule: "超级管理员不可删除、不可停用，非超管不能修改其角色或密码", Enforced: true})
	RegisterRule(RuleDef{Scope: "sys_menu", Rule: "M是目录、C是页面、F是按钮权限；隐藏菜单仍保留路由，停用菜单不贡献权限", Enforced: true})
	RegisterRule(RuleDef{Scope: "sys_ai_provider", Rule: "凭证加密保存；模型服务及存储配置写操作不向AI和MCP开放", Enforced: true})
	RegisterRule(RuleDef{Scope: "sys_changeset", Rule: "回滚只恢复变更涉及的字段，当前值偏离记录时跳过并报告冲突", Enforced: true})
	RegisterRule(RuleDef{Scope: "sys_job", Rule: "执行计划保存为标准五字段 cron（分钟 小时 日 月 周）；每分钟为 * * * * *，也支持 @every 1m。新任务默认暂停，需要运行时显式设置 status='0'", Enforced: true})
	RegisterRule(RuleDef{Scope: "sys_job", Rule: "定时个人通知使用 system:notifyUser，jobParams 为包含 title、content 的 JSON 字符串；接收人由服务端绑定为创建者，后续编辑不得更换接收人；通知显示在接收人的铃铛中，不是公开公告", Enforced: true})
	RegisterRule(RuleDef{Scope: "sys_job", Rule: "定时创建公开通知公告使用 system:createNotice，参数 noticeTitle、noticeContent、noticeType（1通知或2公告）、status（0正常或1停用），每次执行新增一条；创建、修改、启用和手动执行此任务还需要 system:notice:add 权限", Enforced: true})
	RegisterRule(RuleDef{Scope: "sys_job", Rule: "jobGroup 是执行队列，只能使用 DEFAULT、critical、low；普通任务和公共公告任务使用 DEFAULT。历史字典 SYSTEM 是 DEFAULT 的兼容别名，不是独立队列；公告公开范围不决定队列，不要使用 PUBLIC 或任意业务分组", Enforced: true})
}
func (s *Service) Invalidate() { s.mu.Lock(); s.expires = time.Time{}; s.mu.Unlock() }
func (s *Service) loadEnums(ctx context.Context) ([]EnumDef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, transactional := dbx.DBFrom(ctx, s.db).Statement.ConnPool.(gorm.TxCommitter)
	if !transactional && time.Now().Before(s.expires) {
		return s.enums, nil
	}
	var rows []struct {
		DictType  string
		DictName  string
		DictValue string
		DictLabel string
	}
	if err := dbx.DBFrom(ctx, s.db).Table("sys_dict_data d").Select("d.dict_type,t.dict_name,d.dict_value,d.dict_label").Joins("JOIN sys_dict_type t ON t.dict_type=d.dict_type").Where("d.status='0' AND d.del_flag='0' AND t.status='0' AND t.del_flag='0'").Order("d.dict_type,d.dict_sort,d.dict_code").Scan(&rows).Error; err != nil {
		return nil, err
	}
	enums := []EnumDef{}
	indices := map[string]int{}
	for _, r := range rows {
		index, exists := indices[r.DictType]
		if !exists {
			index = len(enums)
			indices[r.DictType] = index
			enums = append(enums, EnumDef{DictType: r.DictType, Summary: r.DictName, Values: map[string]string{}})
		}
		enums[index].Values[r.DictValue] = r.DictLabel
	}
	for i := range enums {
		switch enums[i].DictType {
		case "sys_normal_disable":
			enums[i].UsedBy = []string{"sys_user.status", "sys_role.status", "sys_menu.status", "demo_item.status"}
		case "sys_user_sex":
			enums[i].UsedBy = []string{"sys_user.sex"}
		case "sys_risk_level":
			enums[i].UsedBy = []string{"sys_user.ai_max_risk", "sys_changeset.risk", "sys_ai_plan.max_risk"}
		case "sys_changeset_status":
			enums[i].UsedBy = []string{"sys_changeset.status"}
		}
	}
	if !transactional {
		s.enums = enums
		s.expires = time.Now().Add(30 * time.Minute)
	}
	return enums, nil
}
func (s *Service) Get(ctx context.Context, topic string) (Knowledge, error) {
	a, ok := capability.ActorFrom(ctx)
	if !ok {
		return Knowledge{}, errcode.Unauthorizedf("请先登录")
	}
	if !a.Can("ai:agent:use") && !a.Can("ai:capability:list") && !(a.Source == capability.SourceMCP && len(a.Perms) > 0) {
		return Knowledge{}, errcode.Forbiddenf("没有读取系统知识的权限")
	}
	enums, err := s.loadEnums(ctx)
	if err != nil {
		return Knowledge{}, err
	}
	topic = strings.TrimSpace(topic)
	out := Knowledge{Enums: []EnumDef{}, Entities: []EntityDef{}, Rules: []RuleDef{}}
	registry.RLock()
	defer registry.RUnlock()
	if topic == "overview" {
		common := map[string]bool{"sys_normal_disable": true, "sys_user_sex": true, "sys_common_status": true, "sys_risk_level": true, "sys_call_source": true}
		for _, e := range enums {
			if common[e.DictType] {
				out.Enums = append(out.Enums, e)
			}
		}
		for _, e := range registry.entities {
			if e.Table == "sys_user" || e.Table == "sys_role" {
				out.Entities = append(out.Entities, EntityDef{Table: e.Table, Summary: e.Summary, KeyField: e.KeyField, LabelField: e.LabelField})
			}
		}
		for _, r := range registry.rules {
			if r.Scope == "*" || r.Scope == "sys_user" {
				out.Rules = append(out.Rules, r)
			}
		}
		return out, nil
	}
	for _, e := range enums {
		if topic == "" || topic == "all" || topic == e.DictType {
			out.Enums = append(out.Enums, e)
		}
	}
	for _, e := range registry.entities {
		if topic == "" || topic == "all" || topic == e.Table {
			out.Entities = append(out.Entities, e)
		}
	}
	for _, r := range registry.rules {
		if topic == "" || topic == "all" || topic == "rules" || r.Scope == topic || r.Scope == "*" {
			out.Rules = append(out.Rules, r)
		}
	}
	if topic != "" && topic != "all" && topic != "rules" && len(out.Enums) == 0 && len(out.Entities) == 0 {
		return out, errcode.NotFoundf("知识主题不存在")
	}
	return out, nil
}

// CapabilityEntities maps a capability namespace to the table it operates on,
// for the namespaces that predate the naming convention. It is the single
// source for that mapping; the MCP resource filter and CheckCoverage both read
// it instead of keeping their own copies.
// An empty value means the namespace is deliberately not backed by a table.
var CapabilityEntities = map[string]string{
	"system.user": "sys_user", "system.role": "sys_role", "system.menu": "sys_menu",
	"system.config": "sys_config", "system.notice": "sys_notice",
	"system.dictType": "sys_dict_type", "system.dictData": "sys_dict_data",
	"system.dict": "sys_dict_data", "system.upload": "sys_upload",
	"system.aiProvider": "sys_ai_provider", "system.storage": "sys_storage_config",
	"system.token": "sys_access_token", "system.changeset": "sys_changeset",
	"system.auditTrail": "sys_oper_log", "system.audit": "sys_oper_log",
	"monitor.job": "sys_job", "monitor.jobLog": "sys_job_log",
	"monitor.operlog": "sys_oper_log", "monitor.loginlog": "sys_login_log",
	"demo.item": "demo_item",
	// Backed by Redis and by the registry itself, not by a table.
	"monitor.online": "", "mcp.resource": "",
}

// ConventionTable turns a capability name into the table its namespace
// implies, e.g. shop.order.list -> shop_order. Callers check the result
// against the registered entities, so an unconventional name finds nothing.
func ConventionTable(name string) (string, bool) {
	parts := strings.Split(name, ".")
	if len(parts) < 3 {
		return "", false
	}
	return strings.Join(parts[:len(parts)-1], "_"), true
}

// CheckCoverage warns about list capabilities whose entity is not described in
// the knowledge registry. The model is then working blind on that table: it
// knows the capability exists but not what the rows mean.
//
// This is a warning, not a failure. It exists because the registry silently
// fell behind when the approval, notification and OAuth features landed, and
// nothing pointed it out.
func CheckCoverage() []string {
	registry.RLock()
	known := make(map[string]bool, len(registry.entities))
	for _, e := range registry.entities {
		known[e.Table] = true
	}
	registry.RUnlock()

	missing := []string{}
	for _, e := range capability.All() {
		if !strings.HasSuffix(e.Name, ".list") {
			continue
		}
		namespace := strings.TrimSuffix(e.Name, ".list")
		if table, ok := CapabilityEntities[namespace]; ok && (table == "" || known[table]) {
			continue
		}
		if table, ok := ConventionTable(e.Name); ok && known[table] {
			continue
		}
		missing = append(missing, e.Name)
	}
	sort.Strings(missing)
	return missing
}
