package model

import "time"

// IdentityAudit records who created or last edited an identity record.
type IdentityAudit struct {
	CreateBy   string    `gorm:"column:create_by" json:"createBy,omitempty"`
	CreateTime time.Time `gorm:"column:create_time" json:"createTime"`
	UpdateBy   string    `gorm:"column:update_by" json:"updateBy,omitempty"`
	UpdateTime time.Time `gorm:"column:update_time" json:"updateTime"`
	Remark     *string   `gorm:"column:remark" json:"remark,omitempty"`
}

// User is the persisted account. Password and deletion state never reach JSON.
type User struct {
	UserID      int64      `gorm:"column:user_id;primaryKey;autoIncrement" json:"userId"`
	UserName    string     `gorm:"column:user_name" json:"userName"`
	NickName    string     `gorm:"column:nick_name" json:"nickName"`
	UserType    string     `gorm:"column:user_type" json:"userType"`
	Email       string     `gorm:"column:email" json:"email"`
	Phonenumber string     `gorm:"column:phonenumber" json:"phonenumber"`
	Sex         string     `gorm:"column:sex" json:"sex"`
	Password    string     `gorm:"column:password" json:"-"`
	Avatar      string     `gorm:"column:avatar" json:"avatar"`
	Status      string     `gorm:"column:status" json:"status"`
	DelFlag     string     `gorm:"column:del_flag" json:"-"`
	LoginIP     string     `gorm:"column:login_ip" json:"loginIp,omitempty"`
	LoginDate   *time.Time `gorm:"column:login_date" json:"loginDate,omitempty"`
	AIMaxRisk   *string    `gorm:"column:ai_max_risk" json:"aiMaxRisk,omitempty"`
	IdentityAudit
}

func (User) TableName() string { return "sys_user" }

// Role contributes permissions only while active and not deleted.
type Role struct {
	RoleID            int64  `gorm:"column:role_id;primaryKey;autoIncrement" json:"roleId"`
	RoleName          string `gorm:"column:role_name" json:"roleName"`
	RoleKey           string `gorm:"column:role_key" json:"roleKey"`
	RoleSort          int    `gorm:"column:role_sort" json:"roleSort"`
	MenuCheckStrictly bool   `gorm:"column:menu_check_strictly" json:"menuCheckStrictly"`
	Status            string `gorm:"column:status" json:"status"`
	DelFlag           string `gorm:"column:del_flag" json:"-"`
	IdentityAudit
}

func (Role) TableName() string { return "sys_role" }

// Menu describes a directory (M), page (C), or button permission (F).
type Menu struct {
	MenuID    int64  `gorm:"column:menu_id;primaryKey;autoIncrement" json:"menuId"`
	MenuName  string `gorm:"column:menu_name" json:"menuName"`
	ParentID  int64  `gorm:"column:parent_id" json:"parentId"`
	OrderNum  int    `gorm:"column:order_num" json:"orderNum"`
	Path      string `gorm:"column:path" json:"path"`
	Component string `gorm:"column:component" json:"component"`
	Query     string `gorm:"column:query" json:"query"`
	IsFrame   string `gorm:"column:is_frame" json:"isFrame"`
	IsCache   string `gorm:"column:is_cache" json:"isCache"`
	MenuType  string `gorm:"column:menu_type" json:"menuType"`
	Visible   string `gorm:"column:visible" json:"visible"`
	Status    string `gorm:"column:status" json:"status"`
	Perms     string `gorm:"column:perms" json:"perms"`
	Icon      string `gorm:"column:icon" json:"icon"`
	DelFlag   string `gorm:"column:del_flag" json:"-"`
	IdentityAudit
}

func (Menu) TableName() string { return "sys_menu" }
