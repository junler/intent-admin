package aiprovider

import (
	"context"
	"time"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
)

// BuildChatModel selects Ark's native SDK or the OpenAI-compatible adapter.
func BuildChatModel(ctx context.Context, c Config) (einomodel.ToolCallingChatModel, error) {
	c, err := Normalize(c)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(c.TimeoutSec) * time.Second
	var temp *float32
	if c.Temperature != nil {
		v := float32(*c.Temperature)
		temp = &v
	}
	if c.Adapter == "ark" {
		return ark.NewChatModel(ctx, &ark.ChatModelConfig{APIKey: c.APIKey, BaseURL: c.BaseURL, Model: c.Model, MaxTokens: c.MaxTokens, Temperature: temp, Timeout: &timeout})
	}
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{APIKey: c.APIKey, BaseURL: c.BaseURL, Model: c.Model, MaxTokens: c.MaxTokens, Temperature: temp, Timeout: timeout})
}
