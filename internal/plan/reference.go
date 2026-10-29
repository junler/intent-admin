package plan

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/junler/intent-admin/internal/pkg/errcode"
)

var ErrPendingReference = errors.New("引用的写步骤尚未执行")
var stepReference = regexp.MustCompile(`^\$steps\[([0-9]+)\]\.result(?:\.(.+))?$`)
var fieldReference = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(?:\[\*\])?$`)

// Resolve supports only earlier-step field paths and array projections, plus
// the two explicit page-context arrays. No evaluation or arbitrary JSONPath.
func Resolve(args json.RawMessage, steps []Step, page map[string]any) (json.RawMessage, error) {
	var value any
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	if err := decodeJSON(args, &value); err != nil {
		return nil, errcode.BadRequestf("计划参数不是有效 JSON")
	}
	resolved, err := resolveValue(value, steps, page)
	if err != nil {
		return nil, err
	}
	return json.Marshal(resolved)
}
func resolveValue(v any, steps []Step, page map[string]any) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			next, err := resolveValue(val, steps, page)
			if err != nil {
				return nil, err
			}
			out[k] = next
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			next, err := resolveValue(val, steps, page)
			if err != nil {
				return nil, err
			}
			out[i] = next
		}
		return out, nil
	case string:
		if x == "$ctx.selectedIds" || x == "$ctx.visibleIds" {
			key := strings.TrimPrefix(x, "$ctx.")
			v, exists := page[key]
			if !exists {
				return nil, errcode.BadRequestf("页面上下文缺少 %s", key)
			}
			return v, nil
		}
		if !strings.HasPrefix(x, "$steps[") && !strings.HasPrefix(x, "$ctx.") {
			return x, nil
		}
		match := stepReference.FindStringSubmatch(x)
		if match == nil {
			return nil, errcode.BadRequestf("不支持的引用 %s", x)
		}
		index, _ := strconv.Atoi(match[1])
		if index >= len(steps) {
			return nil, errcode.BadRequestf("步骤只能引用已经出现的步骤")
		}
		step := steps[index]
		if step.Result == nil {
			return nil, ErrPendingReference
		}
		var result any
		if err := decodeJSON(encode(step.Result), &result); err != nil {
			return nil, err
		}
		if match[2] == "" {
			return result, nil
		}
		return project(result, strings.Split(match[2], "."))
	default:
		return v, nil
	}
}
func project(value any, path []string) (any, error) {
	if len(path) == 0 {
		return value, nil
	}
	token := path[0]
	if !fieldReference.MatchString(token) {
		return nil, errcode.BadRequestf("引用字段 %s 无效", token)
	}
	array := strings.HasSuffix(token, "[*]")
	field := strings.TrimSuffix(token, "[*]")
	m, ok := value.(map[string]any)
	if !ok {
		return nil, errcode.BadRequestf("引用 %s 的父值不是对象", field)
	}
	next, exists := m[field]
	if !exists {
		return nil, errcode.BadRequestf("引用字段 %s 不存在", field)
	}
	if !array {
		return project(next, path[1:])
	}
	items, ok := next.([]any)
	if !ok {
		return nil, errcode.BadRequestf("引用字段 %s 不是数组", field)
	}
	out := []any{}
	for _, item := range items {
		v, err := project(item, path[1:])
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
