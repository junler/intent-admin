// Package aiprovider builds provider clients independently of HTTP and capabilities.
package aiprovider

type Preset struct {
	Vendor      string   `json:"vendor"`
	Name        string   `json:"name"`
	BaseURL     string   `json:"baseUrl"`
	Adapter     string   `json:"adapter"`
	Models      []string `json:"models"`
	DocURL      string   `json:"docUrl"`
	NeedsRegion bool     `json:"needsRegion"`
}

func Presets() []Preset {
	return []Preset{
		{Vendor: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", Adapter: "openai", Models: []string{"deepseek-chat", "deepseek-reasoner"}, DocURL: "https://platform.deepseek.com/api_keys"},
		{Vendor: "ark", Name: "火山方舟（豆包）", BaseURL: "https://ark.cn-beijing.volces.com/api/v3", Adapter: "ark", Models: []string{}, DocURL: "https://console.volcengine.com/ark", NeedsRegion: true},
		{Vendor: "dashscope", Name: "阿里云百炼", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Adapter: "openai", Models: []string{"qwen-plus", "qwen-max", "qwen-turbo"}, DocURL: "https://bailian.console.aliyun.com/"},
		{Vendor: "moonshot", Name: "月之暗面（Kimi）", BaseURL: "https://api.moonshot.cn/v1", Adapter: "openai", Models: []string{"moonshot-v1-8k", "kimi-k2-0711-preview"}, DocURL: "https://platform.moonshot.cn/console/api-keys"},
		{Vendor: "zhipu", Name: "智谱（GLM）", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Adapter: "openai", Models: []string{"glm-4-plus", "glm-4-flash"}, DocURL: "https://open.bigmodel.cn/"},
		{Vendor: "siliconflow", Name: "硅基流动", BaseURL: "https://api.siliconflow.cn/v1", Adapter: "openai", Models: []string{"deepseek-ai/DeepSeek-V3"}, DocURL: "https://cloud.siliconflow.cn/account/ak"},
		{Vendor: "openai", Name: "OpenAI", BaseURL: "https://api.openai.com/v1", Adapter: "openai", Models: []string{"gpt-4.1", "gpt-4.1-mini"}, DocURL: "https://platform.openai.com/api-keys"},
		{Vendor: "custom", Name: "自定义 OpenAI 兼容服务", Adapter: "openai", Models: []string{}},
	}
}
func FindPreset(vendor string) (Preset, bool) {
	for _, p := range Presets() {
		if p.Vendor == vendor {
			return p, true
		}
	}
	return Preset{}, false
}

type ModelInfo struct {
	ProviderID   int64    `json:"providerId"`
	Name         string   `json:"name"`
	DefaultModel string   `json:"defaultModel"`
	Models       []string `json:"models"`
	IsDefault    bool     `json:"isDefault"`
}
