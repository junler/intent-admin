package aitool

import (
	"context"

	"github.com/junler/intent-admin/internal/pkg/errcode"
)

// An unknown model-generated topic is recoverable. Returning it as a tool
// failure would terminate the agent before it can discover the correct form.
func (t *Tools) getKnowledge(ctx context.Context, topic string) (any, error) {
	if t.Knowledge == nil {
		return nil, errcode.Internalf("知识库不可用")
	}
	out, err := t.Knowledge.Get(ctx, topic)
	if err != nil && errcode.From(err).Code == errcode.NotFound {
		return map[string]any{
			"found": false,
			"topic": topic,
			"hint":  "未找到该知识主题，请使用 overview、rules、真实表名或字典类型继续查询；表单字段请通过 list_forms、describe_form 获取，再用 fill_form 填写。不要因为知识主题不存在而终止填表任务。",
		}, nil
	}
	return out, err
}
