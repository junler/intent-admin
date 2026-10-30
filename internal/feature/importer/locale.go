package importer

import (
	"regexp"
	"strings"
)

var englishColumns = map[string]string{
	"userName": "Username", "nickName": "Display name", "email": "Email", "phonenumber": "Phone number", "sex": "Gender", "status": "Status", "remark": "Notes",
	"roleName": "Role name", "roleKey": "Role key", "roleSort": "Sort order", "configName": "Configuration name", "configKey": "Configuration key", "configValue": "Configuration value", "configType": "Built in",
	"dictType": "Dictionary type", "dictLabel": "Dictionary label", "dictValue": "Dictionary value", "dictSort": "Sort order", "listClass": "Display style", "isDefault": "Default option",
	"name": "Name", "category": "Category", "amount": "Amount (cents)",
}
var englishErrors = map[string]string{
	"存在未声明列的数据":                           "Data found in undeclared columns",
	"不支持公式，请粘贴为值":                         "Formulas are not supported. Paste values instead.",
	"凭据类参数不能通过批量导入管理":                     "Credential settings cannot be imported",
	"唯一键已存在，导入只支持新增":                      "The unique key already exists. Imports can only create records.",
	"用户名需为 2–30 位字母、数字或 _.@-":             "Usernames must use 2–30 letters, digits, or _.@-",
	"邮箱格式不正确":                             "Invalid email address",
	"备注最多 500 字":                          "Notes cannot exceed 500 characters",
	"字典类型不存在":                             "The dictionary type does not exist",
	"名称或标识已存在":                            "The name or key already exists",
	"密码为 6–72 字节":                         "Passwords must contain 6–72 bytes",
	"未设置 sys.user.initPassword，请明确提供初始密码": "The initial password setting sys.user.initPassword is missing",
	"账号开关参数必须为 true 或 false":              "Account switch settings must be true or false",
}
var duplicateRow = regexp.MustCompile(`^与第 (\d+) 行的唯一键重复$`)
var fieldLength = regexp.MustCompile(`^([A-Za-z]+) 最多 (\d+) 字$`)

func englishError(message string) string {
	if v := englishErrors[message]; v != "" {
		return v
	}
	if v := duplicateRow.FindStringSubmatch(message); v != nil {
		return "The unique key duplicates row " + v[1]
	}
	if v := fieldLength.FindStringSubmatch(message); v != nil {
		return v[1] + " cannot exceed " + v[2] + " characters"
	}
	for _, col := range specs {
		for _, field := range col.Columns {
			label := englishColumns[field.Field]
			for _, name := range []string{field.Label, field.Field} {
				if message == name+"不能为空" || message == name+" 必填" {
					return label + " is required"
				}
				if message == name+"内容过长" {
					return label + " is too long"
				}
				if message == name+"超出支持范围" {
					return label + " is outside the supported range"
				}
				if message == name+"必须为非负整数" {
					return label + " must be a non-negative integer"
				}
				if message == name+" 不合法" {
					return label + " is invalid"
				}
			}
		}
	}
	return strings.TrimSpace(message)
}
