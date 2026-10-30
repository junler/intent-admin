// Package importer validates XLSX files and confirms their entire contents
// through the capability executor in one database transaction and changeset.
package importer

type column struct {
	Field, Label string
	Required     bool
	Integer      bool
	Example      string
}

type spec struct {
	Base, Capability, Title string
	Columns                 []column
	Unique                  []string
}

var specs = []spec{
	{Base: "/system/user", Capability: "system.user.create", Title: "用户", Unique: []string{"userName"}, Columns: []column{
		{Field: "userName", Label: "用户名", Required: true, Example: "new_user"}, {Field: "nickName", Label: "昵称", Required: true, Example: "新用户"}, {Field: "email", Label: "邮箱"}, {Field: "phonenumber", Label: "手机号"}, {Field: "sex", Label: "性别", Example: "2"}, {Field: "status", Label: "状态", Example: "0"}, {Field: "remark", Label: "备注"},
	}},
	{Base: "/system/role", Capability: "system.role.create", Title: "角色", Unique: []string{"roleKey"}, Columns: []column{
		{Field: "roleName", Label: "角色名称", Required: true, Example: "新角色"}, {Field: "roleKey", Label: "权限标识", Required: true, Example: "new_role"}, {Field: "roleSort", Label: "排序", Integer: true, Example: "1"}, {Field: "status", Label: "状态", Example: "0"}, {Field: "remark", Label: "备注"},
	}},
	{Base: "/system/config", Capability: "system.config.create", Title: "参数", Unique: []string{"configKey"}, Columns: []column{
		{Field: "configName", Label: "参数名称", Required: true, Example: "展示标题"}, {Field: "configKey", Label: "参数键", Required: true, Example: "custom.displayTitle"}, {Field: "configValue", Label: "参数值", Example: "我的工作台"}, {Field: "configType", Label: "系统内置", Example: "N"}, {Field: "status", Label: "状态", Example: "0"}, {Field: "remark", Label: "备注"},
	}},
	{Base: "/system/dict/data", Capability: "system.dictData.create", Title: "字典数据", Unique: []string{"dictType", "dictValue"}, Columns: []column{
		{Field: "dictType", Label: "字典类型", Required: true, Example: "sys_normal_disable"}, {Field: "dictLabel", Label: "字典标签", Required: true, Example: "新选项"}, {Field: "dictValue", Label: "字典键值", Required: true, Example: "new_value"}, {Field: "dictSort", Label: "排序", Integer: true, Example: "1"}, {Field: "listClass", Label: "回显样式", Example: "default"}, {Field: "isDefault", Label: "默认值", Example: "N"}, {Field: "status", Label: "状态", Example: "0"}, {Field: "remark", Label: "备注"},
	}},
	{Base: "/demo/item", Capability: "demo.item.create", Title: "示例条目", Unique: []string{"category", "name"}, Columns: []column{
		{Field: "name", Label: "名称", Required: true, Example: "新条目"}, {Field: "category", Label: "分类", Example: "alpha"}, {Field: "amount", Label: "金额（分）", Integer: true, Example: "100"}, {Field: "status", Label: "状态", Example: "0"}, {Field: "remark", Label: "备注"},
	}},
}
