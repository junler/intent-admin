// Package config loads and validates the application configuration.
//
// Precedence: explicit --config file > ./configs/config.yaml > defaults, with
// INTENT_ADMIN_* environment variables overriding everything. Nested keys use
// underscores, e.g. INTENT_ADMIN_DB_PASSWORD overrides db.password.
package config

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/junler/intent-admin/internal/pkg/secret"
)

const EnvPrefix = "INTENT_ADMIN"

// Placeholder values that must not survive into a production deployment.
const (
	placeholderJWTSecret  = "change-me-in-production"
	placeholderEncryptKey = "0123456789abcdef0123456789abcdef"
)

type Config struct {
	App       App       `mapstructure:"app"`
	DB        DB        `mapstructure:"db"`
	Redis     Redis     `mapstructure:"redis"`
	JWT       JWT       `mapstructure:"jwt"`
	Task      Task      `mapstructure:"task"`
	Changeset Changeset `mapstructure:"changeset"`
	Export    Export    `mapstructure:"export"`
	Storage   Storage   `mapstructure:"storage"`
	AI        AI        `mapstructure:"ai"`
	MCP       MCP       `mapstructure:"mcp"`
	Log       Log       `mapstructure:"log"`
}

type App struct {
	Name       string `mapstructure:"name"`
	Env        string `mapstructure:"env"` // dev | prod
	Port       int    `mapstructure:"port"`
	BasePath   string `mapstructure:"base_path"`
	EncryptKey string `mapstructure:"encrypt_key"`
}

func (a App) IsProd() bool { return a.Env == "prod" }

type DB struct {
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	User         string `mapstructure:"user"`
	Password     string `mapstructure:"password"`
	Name         string `mapstructure:"name"`
	SSLMode      string `mapstructure:"ssl_mode"`
	TimeZone     string `mapstructure:"time_zone"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
	LogLevel     string `mapstructure:"log_level"` // silent | error | warn | info
}

// DSN renders the PostgreSQL connection string.
func (d DB) DSN() string {
	quote := func(value string) string {
		escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value)
		return "'" + escaped + "'"
	}
	// Quote empty values and credentials so pgx cannot consume the next
	// keyword as a password. Keep the validated IANA time zone unencoded:
	// GORM's PostgreSQL driver reads that value directly from the DSN.
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		quote(d.Host), d.Port, quote(d.User), quote(d.Password), quote(d.Name), quote(d.SSLMode), d.TimeZone)
}

type Redis struct {
	Addr      string `mapstructure:"addr"`
	Password  string `mapstructure:"password"`
	DB        int    `mapstructure:"db"`
	KeyPrefix string `mapstructure:"key_prefix"`
}

type JWT struct {
	Secret string        `mapstructure:"secret"`
	Expire time.Duration `mapstructure:"expire"`
	Issuer string        `mapstructure:"issuer"`
}

type Task struct {
	Embedded     bool          `mapstructure:"embedded"`
	Scheduler    bool          `mapstructure:"scheduler"`
	Concurrency  int           `mapstructure:"concurrency"`
	SyncInterval time.Duration `mapstructure:"sync_interval"`
}

type Changeset struct {
	RetainDays     int `mapstructure:"retain_days"`
	MaxItemsPerSet int `mapstructure:"max_items_per_set"`
}

type Export struct {
	MaxRows         int `mapstructure:"max_rows"`
	StreamThreshold int `mapstructure:"stream_threshold"`
	AsyncRetainDays int `mapstructure:"async_retain_days"`
}

type Storage struct {
	Bootstrap StorageBootstrap `mapstructure:"bootstrap"`
}

// StorageBootstrap seeds sys_storage_config on first start when the table is
// empty. Afterwards the database is authoritative — see plan §4B.
type StorageBootstrap struct {
	Driver           string `mapstructure:"driver"` // local | s3
	LocalDir         string `mapstructure:"local_dir"`
	BaseURL          string `mapstructure:"base_url"`
	URLMode          string `mapstructure:"url_mode"`
	PresignExpireSec int    `mapstructure:"presign_expire_sec"`
	Endpoint         string `mapstructure:"endpoint"`
	Region           string `mapstructure:"region"`
	Bucket           string `mapstructure:"bucket"`
	AccessKeyID      string `mapstructure:"access_key_id"`
	SecretKey        string `mapstructure:"secret_key"`
	UseSSL           bool   `mapstructure:"use_ssl"`
	PathStyle        bool   `mapstructure:"path_style"`
}

type AI struct {
	Bootstrap    AIBootstrap   `mapstructure:"bootstrap"`
	MaxRisk      string        `mapstructure:"max_risk"`
	MaxAffected  int           `mapstructure:"max_affected"`
	MaxToolCalls int           `mapstructure:"max_tool_calls"`
	MaxPlanSteps int           `mapstructure:"max_plan_steps"`
	PlanTTL      time.Duration `mapstructure:"plan_ttl"`
	SessionTTL   time.Duration `mapstructure:"session_ttl"`
}

// AIBootstrap seeds sys_ai_provider on first start when the table is empty.
type AIBootstrap struct {
	Vendor  string `mapstructure:"vendor"`
	APIKey  string `mapstructure:"api_key"`
	BaseURL string `mapstructure:"base_url"`
	Model   string `mapstructure:"model"`
}

type MCP struct {
	Enabled    bool          `mapstructure:"enabled"`
	Path       string        `mapstructure:"path"`
	ToolMode   string        `mapstructure:"tool_mode"` // meta | flat
	ToolTags   []string      `mapstructure:"tool_tags"`
	MaxRisk    string        `mapstructure:"max_risk"`
	ConfirmTTL time.Duration `mapstructure:"confirm_ttl"`
	RatePerMin int           `mapstructure:"rate_per_min"`
}

type Log struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"` // json | text
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.name", "intent-admin")
	v.SetDefault("app.env", "dev")
	v.SetDefault("app.port", 8080)
	v.SetDefault("app.base_path", "/api")

	v.SetDefault("db.host", "127.0.0.1")
	v.SetDefault("db.port", 5432)
	v.SetDefault("db.user", "postgres")
	v.SetDefault("db.name", "intent_admin")
	v.SetDefault("db.ssl_mode", "disable")
	v.SetDefault("db.time_zone", "Asia/Shanghai")
	v.SetDefault("db.max_open_conns", 50)
	v.SetDefault("db.max_idle_conns", 10)
	v.SetDefault("db.log_level", "warn")

	v.SetDefault("redis.addr", "127.0.0.1:6379")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.key_prefix", "intent-admin:")

	v.SetDefault("jwt.expire", "24h")
	v.SetDefault("jwt.issuer", "intent-admin")

	v.SetDefault("task.embedded", true)
	v.SetDefault("task.scheduler", true)
	v.SetDefault("task.concurrency", 10)
	v.SetDefault("task.sync_interval", "15s")

	v.SetDefault("changeset.retain_days", 90)
	v.SetDefault("changeset.max_items_per_set", 2000)

	v.SetDefault("export.max_rows", 50000)
	v.SetDefault("export.stream_threshold", 5000)
	v.SetDefault("export.async_retain_days", 7)

	v.SetDefault("storage.bootstrap.driver", "local")
	v.SetDefault("storage.bootstrap.local_dir", "./uploads")
	v.SetDefault("storage.bootstrap.base_url", "http://localhost:8080/uploads")
	v.SetDefault("storage.bootstrap.url_mode", "static")
	v.SetDefault("storage.bootstrap.presign_expire_sec", 3600)
	v.SetDefault("storage.bootstrap.use_ssl", true)
	v.SetDefault("storage.bootstrap.path_style", true)

	v.SetDefault("ai.max_risk", "high")
	v.SetDefault("ai.max_affected", 200)
	v.SetDefault("ai.max_tool_calls", 25)
	v.SetDefault("ai.max_plan_steps", 20)
	v.SetDefault("ai.plan_ttl", "30m")
	v.SetDefault("ai.session_ttl", "168h")

	v.SetDefault("mcp.enabled", true)
	v.SetDefault("mcp.path", "/mcp")
	v.SetDefault("mcp.tool_mode", "meta")
	v.SetDefault("mcp.max_risk", "low")
	v.SetDefault("mcp.confirm_ttl", "5m")
	v.SetDefault("mcp.rate_per_min", 60)

	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
}

// Load reads configuration from path (optional) plus environment overrides.
// An empty path searches ./configs/config.yaml; a missing file is not an error
// so that container deployments can run on environment variables alone.
func Load(path string) (*Config, error) {
	v := viper.New()
	setDefaults(v)

	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	bindEnvironment(v, reflect.TypeOf(Config{}), "")

	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath("./configs")
		v.AddConfigPath(".")
	}
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if path != "" || !asConfigNotFound(err, &notFound) {
			return nil, fmt.Errorf("读取配置失败: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func asConfigNotFound(err error, target *viper.ConfigFileNotFoundError) bool {
	e, ok := err.(viper.ConfigFileNotFoundError)
	if ok {
		*target = e
	}
	return ok
}

// Validate enforces the startup rules from plan §19. Refusing to boot on a
// missing or placeholder secret is deliberate: a silently weak key is worse
// than a failed deploy.
func (c *Config) Validate() error {
	if c.JWT.Expire <= 0 {
		return fmt.Errorf("jwt.expire 必须大于 0")
	}
	if strings.TrimSpace(c.JWT.Secret) == "" {
		return fmt.Errorf("jwt.secret 必填（可用 openssl rand -base64 32 生成）")
	}
	if _, err := secret.ParseKey(c.App.EncryptKey); err != nil {
		return fmt.Errorf("app.encrypt_key 无效: %w", err)
	}
	if c.App.IsProd() {
		if c.JWT.Secret == placeholderJWTSecret {
			return fmt.Errorf("生产环境不能使用示例 jwt.secret")
		}
		if c.App.EncryptKey == placeholderEncryptKey {
			return fmt.Errorf("生产环境不能使用示例 app.encrypt_key")
		}
	}
	switch c.App.Env {
	case "dev", "prod":
	default:
		return fmt.Errorf("app.env 必须是 dev 或 prod，当前为 %q", c.App.Env)
	}
	if !strings.HasPrefix(c.App.BasePath, "/") {
		return fmt.Errorf("app.base_path 必须以 / 开头")
	}
	if _, err := time.LoadLocation(c.DB.TimeZone); err != nil {
		return fmt.Errorf("db.time_zone 无效: %w", err)
	}
	return nil
}

// Warnings returns non-fatal configuration risks to log loudly at startup.
// These are choices that widen the blast radius of an AI or external client
// mistake, so operators should see them on every boot, not just in docs.
func (c *Config) Warnings() []string {
	var out []string
	if c.AI.MaxRisk == "critical" {
		out = append(out, "ai.max_risk=critical：critical 操作仍需人工在页面执行，建议使用 high 明确自动化边界")
	}
	if c.MCP.Enabled && c.MCP.MaxRisk != "low" {
		out = append(out, fmt.Sprintf("mcp.max_risk=%s：持有访问令牌的外部客户端可以修改数据，请确认这是你要的", c.MCP.MaxRisk))
	}
	if c.App.IsProd() && c.Task.Scheduler {
		out = append(out, "task.scheduler=true：多副本部署时只能有一个副本开启，否则定时任务会重复入队")
	}
	return out
}

// Viper only unmarshals known keys. Bind every declared leaf so deployments
// using environment variables alone can set secrets and bootstrap fields even
// when those fields intentionally have no default value.
func bindEnvironment(v *viper.Viper, t reflect.Type, prefix string) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		key := field.Tag.Get("mapstructure")
		if key == "" || key == "-" {
			continue
		}
		if prefix != "" {
			key = prefix + "." + key
		}
		if field.Type.Kind() == reflect.Struct {
			bindEnvironment(v, field.Type, key)
		} else {
			_ = v.BindEnv(key)
		}
	}
}
