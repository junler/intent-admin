// Package system implements identity and system data policies independently of
// the HTTP, AI and MCP adapters. Every caller passes an explicit principal.
package system

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/junler/intent-admin/internal/pkg/paginate"
)

type Principal struct {
	UserID int64
	Name   string
	Super  bool
	Source string
}

// Input is the shared typed CRUD contract. Resource allowlists below determine
// which fields may reach SQL; binding an unrelated field never grants access.
type Input struct {
	paginate.Query
	ID                int64      `json:"id,omitempty" uri:"id" form:"id"`
	IDs               []int64    `json:"ids,omitempty" uri:"ids" collection_format:"csv"`
	UserID            *int64     `json:"userId,omitempty" form:"userId"`
	RoleID            *int64     `json:"roleId,omitempty" form:"roleId"`
	MenuID            *int64     `json:"menuId,omitempty" form:"menuId"`
	DictID            *int64     `json:"dictId,omitempty" form:"dictId"`
	DictCode          *int64     `json:"dictCode,omitempty" form:"dictCode"`
	ConfigID          *int64     `json:"configId,omitempty" form:"configId"`
	NoticeID          *int64     `json:"noticeId,omitempty" form:"noticeId"`
	TokenID           *int64     `json:"tokenId,omitempty" form:"tokenId"`
	UserName          *string    `json:"userName,omitempty" form:"userName"`
	NickName          *string    `json:"nickName,omitempty" form:"nickName"`
	Password          *string    `json:"password,omitempty"`
	OldPassword       *string    `json:"oldPassword,omitempty"`
	NewPassword       *string    `json:"newPassword,omitempty"`
	Email             *string    `json:"email,omitempty" form:"email"`
	Phonenumber       *string    `json:"phonenumber,omitempty" form:"phonenumber"`
	Sex               *string    `json:"sex,omitempty" form:"sex"`
	Avatar            *string    `json:"avatar,omitempty"`
	Status            *string    `json:"status,omitempty" form:"status"`
	Remark            *string    `json:"remark,omitempty"`
	AIMaxRisk         *string    `json:"aiMaxRisk,omitempty"`
	ClearRisk         bool       `json:"clearRisk,omitempty"`
	RoleName          *string    `json:"roleName,omitempty" form:"roleName"`
	RoleKey           *string    `json:"roleKey,omitempty" form:"roleKey"`
	RoleSort          *int       `json:"roleSort,omitempty"`
	MenuCheckStrictly *bool      `json:"menuCheckStrictly,omitempty"`
	MenuName          *string    `json:"menuName,omitempty" form:"menuName"`
	ParentID          *int64     `json:"parentId,omitempty" form:"parentId"`
	OrderNum          *int       `json:"orderNum,omitempty"`
	Path              *string    `json:"path,omitempty"`
	Component         *string    `json:"component,omitempty"`
	DefaultQuery      *string    `json:"query,omitempty"`
	IsFrame           *string    `json:"isFrame,omitempty"`
	IsCache           *string    `json:"isCache,omitempty"`
	MenuType          *string    `json:"menuType,omitempty" form:"menuType"`
	Visible           *string    `json:"visible,omitempty"`
	Perms             *string    `json:"perms,omitempty"`
	Icon              *string    `json:"icon,omitempty"`
	DictName          *string    `json:"dictName,omitempty" form:"dictName"`
	DictType          *string    `json:"dictType,omitempty" form:"dictType" uri:"dictType"`
	DictSort          *int       `json:"dictSort,omitempty"`
	DictLabel         *string    `json:"dictLabel,omitempty" form:"dictLabel"`
	DictValue         *string    `json:"dictValue,omitempty" form:"dictValue"`
	CssClass          *string    `json:"cssClass,omitempty"`
	ListClass         *string    `json:"listClass,omitempty"`
	IsDefault         *string    `json:"isDefault,omitempty"`
	ConfigName        *string    `json:"configName,omitempty" form:"configName"`
	ConfigKey         *string    `json:"configKey,omitempty" form:"configKey" uri:"configKey"`
	ConfigValue       *string    `json:"configValue,omitempty"`
	ConfigType        *string    `json:"configType,omitempty"`
	NoticeTitle       *string    `json:"noticeTitle,omitempty" form:"noticeTitle"`
	NoticeType        *string    `json:"noticeType,omitempty" form:"noticeType"`
	NoticeContent     *string    `json:"noticeContent,omitempty"`
	RoleIDs           *[]int64   `json:"roleIds,omitempty"`
	MenuIDs           *[]int64   `json:"menuIds,omitempty"`
	Permissions       *[]string  `json:"permissions,omitempty"`
	Name              *string    `json:"name,omitempty" form:"name"`
	Scopes            *[]string  `json:"scopes,omitempty"`
	MaxRisk           *string    `json:"maxRisk,omitempty"`
	ExpiresAt         *time.Time `json:"expiresAt,omitempty"`
}
type ListOut struct {
	Rows     []map[string]any `json:"rows"`
	Total    int64            `json:"total"`
	PageNum  int              `json:"pageNum"`
	PageSize int              `json:"pageSize"`
}
type Resource struct {
	Name, Table, ID, Label, Route, Perm string
	Fields, Required                    []string
	Lengths                             map[string]int
	Auth                                bool
	Export                              bool
}

var Resources = []Resource{
	{Name: "user", Table: "sys_user", ID: "user_id", Label: "user_name", Route: "/system/user", Perm: "user", Fields: strings.Fields("user_name nick_name email phonenumber sex avatar status remark"), Required: strings.Fields("user_name nick_name"), Lengths: map[string]int{"user_name": 30, "nick_name": 30, "email": 50, "phonenumber": 20, "avatar": 255}, Auth: true, Export: true},
	{Name: "role", Table: "sys_role", ID: "role_id", Label: "role_name", Route: "/system/role", Perm: "role", Fields: strings.Fields("role_name role_key role_sort menu_check_strictly status remark"), Required: strings.Fields("role_name role_key"), Lengths: map[string]int{"role_name": 30, "role_key": 100}, Auth: true, Export: true},
	{Name: "menu", Table: "sys_menu", ID: "menu_id", Label: "menu_name", Route: "/system/menu", Perm: "menu", Fields: strings.Fields("menu_name parent_id order_num path component query is_frame is_cache menu_type visible status perms icon remark"), Required: strings.Fields("menu_name menu_type"), Lengths: map[string]int{"menu_name": 50, "path": 200, "component": 255, "query": 255, "perms": 100, "icon": 100}, Auth: true},
	{Name: "dictType", Table: "sys_dict_type", ID: "dict_id", Label: "dict_name", Route: "/system/dict/type", Perm: "dict", Fields: strings.Fields("dict_name dict_type status remark"), Required: strings.Fields("dict_name dict_type"), Lengths: map[string]int{"dict_name": 100, "dict_type": 100}},
	{Name: "dictData", Table: "sys_dict_data", ID: "dict_code", Label: "dict_label", Route: "/system/dict/data", Perm: "dict", Fields: strings.Fields("dict_sort dict_label dict_value dict_type css_class list_class is_default status remark"), Required: strings.Fields("dict_label dict_value dict_type"), Lengths: map[string]int{"dict_label": 100, "dict_value": 100, "dict_type": 100, "css_class": 100, "list_class": 100}, Export: true},
	{Name: "config", Table: "sys_config", ID: "config_id", Label: "config_name", Route: "/system/config", Perm: "config", Fields: strings.Fields("config_name config_key config_value config_type status remark"), Required: strings.Fields("config_name config_key"), Lengths: map[string]int{"config_name": 100, "config_key": 100, "config_value": 500}, Export: true},
	{Name: "notice", Table: "sys_notice", ID: "notice_id", Label: "notice_title", Route: "/system/notice", Perm: "notice", Fields: strings.Fields("notice_title notice_type notice_content status remark"), Required: strings.Fields("notice_title"), Lengths: map[string]int{"notice_title": 100, "notice_content": 100000}},
}

func Spec(name string) Resource {
	for _, r := range Resources {
		if r.Name == name {
			return r
		}
	}
	panic("unknown system resource")
}
func Camel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}
func Snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + 32)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func Data(in Input) map[string]any {
	raw, _ := json.Marshal(in)
	var d map[string]any
	_ = json.Unmarshal(raw, &d)
	out := map[string]any{}
	for k, v := range d {
		if n, ok := v.(float64); ok {
			v = int64(n)
		}
		out[Snake(k)] = v
	}
	return out
}
func View(row map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range row {
		if k == "password" || k == "token_hash" || k == "del_flag" {
			continue
		}
		if b, ok := v.([]byte); ok {
			v = string(b)
		}
		if k == "config_value" && IsCredentialConfigKey(fmt.Sprint(row["config_key"])) {
			v = "***"
		}
		out[Camel(k)] = v
	}
	return out
}

type Change struct {
	ID            int64
	Label, Op     string
	Before, After map[string]any
	Password      string
	SecretConfig  bool
	ConfigKey     string
}

func IsCredentialConfigKey(key string) bool {
	key = strings.ToLower(key)
	for _, part := range []string{"password", "passwd", "secret", "token", "apikey", "api_key", "privatekey", "private_key", "credential"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}
