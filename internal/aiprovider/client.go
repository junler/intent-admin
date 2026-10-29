package aiprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type Config struct {
	Vendor, Adapter, BaseURL, APIKey, Model string
	Temperature                             *float64
	MaxTokens                               *int
	TimeoutSec                              int
}

func Normalize(c Config) (Config, error) {
	if p, ok := FindPreset(c.Vendor); ok {
		c.Adapter = p.Adapter
		if c.BaseURL == "" {
			c.BaseURL = p.BaseURL
		}
	} else {
		return c, fmt.Errorf("未知模型服务商 %s", c.Vendor)
	}
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return c, fmt.Errorf("模型地址必须是有效 HTTP(S) URL")
	}
	if c.APIKey == "" {
		return c, fmt.Errorf("请填写 API Key")
	}
	if c.Model == "" {
		return c, fmt.Errorf("请填写模型名称（方舟使用模型或推理接入点 ID）")
	}
	if c.TimeoutSec <= 0 {
		c.TimeoutSec = 60
	}
	if c.TimeoutSec > 300 {
		c.TimeoutSec = 300
	}
	return c, nil
}

type TestResult struct {
	OK                  bool     `json:"ok"`
	Reachable           bool     `json:"reachable"`
	SupportsToolCalling bool     `json:"supportsToolCalling"`
	Models              []string `json:"models"`
	LatencyMS           int64    `json:"latencyMs"`
	Error               string   `json:"error,omitempty"`
	ErrorKind           string   `json:"errorKind,omitempty"`
}

// Probe validates authentication and forces one harmless function call.
func Probe(ctx context.Context, c Config) (result TestResult) {
	start := time.Now()
	result.Models = []string{}
	defer func() { result.LatencyMS = time.Since(start).Milliseconds() }()
	var err error
	c, err = Normalize(c)
	if err != nil {
		result.Error = err.Error()
		result.ErrorKind = "config"
		return
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutSec)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		result.Error = "模型地址无效"
		result.ErrorKind = "config"
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	res, err := (&http.Client{Timeout: time.Duration(c.TimeoutSec) * time.Second}).Do(req)
	if err != nil {
		result.Error = "无法连接模型服务，请检查地址和网络"
		result.ErrorKind = "network"
		return
	}
	result.Reachable = true
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	_ = res.Body.Close()
	if res.StatusCode == 401 || res.StatusCode == 403 {
		result.Error = "API Key 无效或没有模型访问权限"
		result.ErrorKind = "invalid_key"
		return
	}
	if res.StatusCode >= 400 && res.StatusCode != 404 && res.StatusCode != 405 {
		result.Error = fmt.Sprintf("模型列表请求失败（HTTP %d）", res.StatusCode)
		result.ErrorKind = "upstream"
		return
	}
	if res.StatusCode < 400 {
		var body struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &body) == nil {
			for _, v := range body.Data {
				result.Models = append(result.Models, v.ID)
			}
		}
	}
	if len(result.Models) == 0 {
		result.Models = []string{c.Model}
	}
	chat, err := BuildChatModel(ctx, c)
	if err != nil {
		result.Error = "创建模型客户端失败：" + safeError(err, c.APIKey)
		result.ErrorKind = "config"
		return
	}
	tools := []*schema.ToolInfo{{Name: "echo", Desc: "Echo the supplied text unchanged", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"text": {Type: schema.String, Desc: "Text", Required: true}})}}
	bound, err := chat.WithTools(tools)
	if err != nil {
		result.Error = "模型不支持工具调用"
		result.ErrorKind = "unsupported_tool_calling"
		return
	}
	reply, err := bound.Generate(ctx, []*schema.Message{schema.UserMessage("You must call the echo tool exactly once with text='ok'. Do not answer in prose.")}, einomodel.WithToolChoice(schema.ToolChoiceForced))
	if err != nil {
		msg := safeError(err, c.APIKey)
		lower := strings.ToLower(msg)
		switch {
		case strings.Contains(lower, "401") || strings.Contains(lower, "403") || strings.Contains(lower, "api key") || strings.Contains(lower, "unauthorized"):
			result.Error = "API Key 无效或没有模型访问权限"
			result.ErrorKind = "invalid_key"
		case strings.Contains(lower, "tool") || strings.Contains(lower, "function"):
			result.Error = "模型不支持工具调用：" + msg
			result.ErrorKind = "unsupported_tool_calling"
		default:
			result.Error = "模型调用失败：" + msg
			result.ErrorKind = "upstream"
		}
		return
	}
	for _, call := range reply.ToolCalls {
		if call.Function.Name == "echo" {
			result.SupportsToolCalling = true
			break
		}
	}
	if !result.SupportsToolCalling {
		result.Error = "模型没有返回工具调用，请选择支持 function calling 的模型"
		result.ErrorKind = "unsupported_tool_calling"
		return
	}
	result.OK = true
	return
}
func safeError(err error, key string) string {
	s := strings.ReplaceAll(err.Error(), key, "[redacted]")
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
