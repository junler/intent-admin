# intent-admin 实现计划

一个 **AI 原生**的 Go 后台管理系统。单二进制部署（前端嵌入），人与 AI 双入口等权操作，所有写操作可预览、可追溯、可一键回滚，并可作为 MCP Server 被外部 AI 客户端调用。

本文是**自包含的实施规格**：要建哪些表、哪些字段、哪些 Capability、什么权限、什么风险等级、分几个阶段交付、每阶段怎么验收，全部写在这里。按本文实现即可，不需要参照任何其它项目。

**已定型的决策见第 3 节，不要擅自更改；未定项集中在第 25 节。**

---

## 1. 项目定位与设计原则

### 1.1 不是「后台 + AI 聊天页」

把一个 AI 对话框塞进传统 CRUD 后台，得到的是「两套互不相干的东西共用一个侧边栏」：AI 能聊天但不能真正干活，或者能干活但没人敢让它干。本项目的前提是反过来的——**先假设 AI 会真的执行操作，再设计让这件事安全可控的系统**。

### 1.2 范式对比

| 维度 | 传统 CRUD 后台范式 | 本项目（AI 原生） |
| --- | --- | --- |
| 主要交互 | 菜单 → 页面 → 搜索表单 → 弹窗表单 | **双入口等权**：页面照旧可用；全局自然语言输入框感知当前页面，直接产出计划 |
| 操作单位 | 一次表单提交 = 一个接口调用 | **Plan**：有序多步，整体预览、整体执行、整体回滚 |
| 执行前 | 二次确认弹窗（"确定删除吗？"） | **dry-run 结构化预览**：将改哪些记录、哪些字段、从什么变成什么、有哪些会被跳过 |
| 执行后 | 操作日志记一行 URL | **Changeset**：before/after 快照 + 一键回滚 |
| 权限 | 角色 → 菜单 → 权限串 | 同上，**再叠一层风险等级门槛**：有权限 ≠ 授权 AI 代做 |
| 审计 | 谁调了什么接口 | **行为溯源**：哪个主体（人/agent/外部客户端）、因为什么意图、经过什么审批、改了什么、是否已回滚 |
| AI 的信息来源 | 无（人自己看文档） | **系统自省知识库**：字典语义、实体关系、业务规则机器可读注入 |
| 开发新功能 | 建表 → 写 6 个文件 → 配菜单（或用代码生成器） | 写一个 **Capability**，自动获得 HTTP 接口 / OpenAPI / AI 能力 / MCP 工具 |

### 1.3 五条设计原则

1. **单一执行路径** —— 人类页面、站内 AI、外部 MCP 客户端调用的是同一个 Capability，经过同一个执行器。权限、校验、审计、可逆性只实现一次，不可能有入口绕过。
2. **写操作默认可逆** —— 不可逆是例外，必须显式声明并在 UI 上标红。回滚能力对人类误操作同样生效，不是只给 AI 兜底。
3. **预览优先于确认** —— 「确定吗？」没有信息量。确认界面必须展示结构化 diff 和影响面。
4. **答案必须可核对** —— AI 的每个结论都带证据链接，点进去是同样筛选条件的页面。人能验证，才敢用。
5. **安全默认值** —— Capability 不声明风险等级则视为 `critical`；MCP 默认只读；AI 可执行的风险上限默认不含 `critical`。

---

## 2. 技术栈

### 后端

| 用途 | 选型 | 说明 |
| --- | --- | --- |
| 语言 | [Go](https://go.dev/) 1.25+ | Capability 层依赖泛型 |
| CLI | `github.com/spf13/cobra` | 单二进制多子命令：`serve`/`worker`/`mcp`/`migrate`/`version` |
| 工具库 | `github.com/duke-git/lancet/v2`（v2.3.7+） | 700+ 工具函数，**优先复用，不自己造轮子**（见 3.1） |
| Web 框架 | `github.com/gin-gonic/gin` | |
| ORM | `gorm.io/gorm` + `gorm.io/driver/postgres` | |
| 数据库 | PostgreSQL 14+ | changeset 的 before/after 快照用 `jsonb` |
| 缓存 / 会话 | Redis 6+（`github.com/redis/go-redis/v9`） | |
| 异步任务 + 定时调度 | `github.com/hibiken/asynq` | 其内部已用 robfig/cron 解析 cronspec，**不叠加独立调度层** |
| AI Agent | `github.com/cloudwego/eino` 的 `adk` + `github.com/cloudwego/eino-ext/components/model/openai` | |
| MCP Server | `github.com/modelcontextprotocol/go-sdk/mcp`（官方 SDK） | Streamable HTTP + stdio |
| 迁移 | `github.com/pressly/goose/v3` | 嵌入式版本化 SQL |
| 配置 | `github.com/spf13/viper` | yaml + 环境变量覆盖 |
| JWT | `github.com/golang-jwt/jwt/v5` | |
| 参数校验 | `github.com/go-playground/validator/v10` | 结构体 tag 校验 |
| JSON Schema | `github.com/google/jsonschema-go` | OpenAPI / AI / MCP 共用；MCP SDK 内部也用它 |
| Excel 导出 | `github.com/xuri/excelize/v2` | 列表导出与导入模板；大数据量用其 `StreamWriter` |
| 对象存储 | `github.com/minio/minio-go/v7` | MinIO 与所有 S3 兼容服务（AWS S3、阿里云 OSS、腾讯云 COS、R2、七牛）。比 aws-sdk-go-v2 轻得多，API 也更直接 |
| 密钥加密 | 标准库 `crypto/aes` + `crypto/cipher`（AES-256-GCM） | 加密存储模型 API Key 与存储 Secret。**不要**用 lancet 的 `cryptor`（无 GCM，且这块要完全可控） |
| 主机指标 | `github.com/shirou/gopsutil/v4` | |
| 日志 | 标准库 `log/slog` | JSON handler |
| 密码 | `golang.org/x/crypto/bcrypt` | cost 10 |
| 验证码 | `github.com/mojocn/base64Captcha` | |

### 前端

| 用途 | 选型 |
| --- | --- |
| 构建 | Vite 7+ + TypeScript 5+ |
| 框架 | React 19 |
| UI | Ant Design v6 + `@ant-design/icons` |
| CSS | Tailwind CSS v4（`@tailwindcss/vite`） |
| 路由 | `react-router` v7 |
| 状态 | `zustand` |
| HTTP | `axios` |
| 图表 | `echarts` + `echarts-for-react` |
| Markdown | `react-markdown` + `remark-gfm` |
| 差异视图 | 自建 `<DiffTable>` / `<DiffFields>` 组件（基于 antd Table/Descriptions），不引入 diff 库 |
| 代码质量 | ESLint + Prettier |

> **antd + Tailwind 必做配置**（官方方案，不照做会样式互相覆盖）：
> 1. `src/styles/global.css` 首行声明层序：`@layer theme, base, antd, components, utilities;` 然后 `@import 'tailwindcss';`
> 2. 应用根部用 `<StyleProvider layer>`（`@ant-design/cssinjs`）包裹 `<ConfigProvider>`，把 antd 样式降权进 `antd` 层。

---

## 3. 架构决策（已定型）

| # | 决策 | 结论 |
| --- | --- | --- |
| D1 | 仓库形态 | 单仓库，前端 `dist/` 用 `embed.FS` 嵌入 Go 二进制，单文件部署 |
| D2 | **统一 Capability 层** | 所有业务功能注册为强类型 Capability（`Fn` + `DryRun` + `Undo` + 元数据）。**HTTP handler 由泛型适配器自动生成**，不手写。注册表是 OpenAPI、AI 工具目录、MCP 工具目录的唯一数据源 |
| D3 | **AI 调用方式** | **进程内直接调用 Capability**（不走回环 HTTP）。权限、校验、审计、changeset 由执行器统一施加 |
| D4 | **执行骨架** | `Plan → Preview(dry-run) → Apply → Changeset → Undo`。**人类页面的写操作同样走这条链路**，因此回滚对人类误操作也有效 |
| D5 | **风险等级门槛** | 每个 Capability 声明 `Risk: low/medium/high/critical`，决定 AI 的确认策略与可执行上限。不声明 = `critical` |
| D6 | 交互形态 | **双入口等权**：保留完整页面式 CRUD（约 25 个页面）；同时顶栏常驻全局自然语言输入框，感知当前页面上下文 |
| D7 | 权限模型 | 角色权限 + 用户额外授权，**仅加权**（有效权限 = ⋃角色菜单权限 ∪ 用户额外授权）。无 deny、无部门、无数据权限范围 |
| D8 | 菜单与路由 | 后端 `sys_menu` 下发菜单树（`GET /api/getRouters`），前端用 `import.meta.glob` 组件注册表映射 `component` 字符串 |
| D9 | **系统自省知识库** | 字典语义、实体关系、业务规则做成机器可读知识注入 agent，不让模型猜 |
| D10 | **页面上下文感知** | 前端每次 AI 请求携带当前路由、主 capability、筛选条件、选中行、可见行 ID |
| D11 | **多步计划与编排** | v1 范围内。步骤间支持结果引用（`$steps[0].result.ids`） |
| D12 | MCP Server | 内置。主传输 Streamable HTTP（`/mcp`），stdio 可选。PAT 鉴权。**默认 `mcp.max_risk: low`（只读）** |
| D13 | 不实现 | 代码生成器、在线表单构建器、部门管理、岗位管理、数据权限范围、自然语言→SQL、每个列表页单独的 NL 筛选框（改为全局输入框） |
| D14 | 响应协议 | 统一信封 `{code, msg, data}`，`code == 200` 成功；分页数据 `data: {rows, total, pageNum, pageSize}` |
| D15 | CLI 形态 | 单二进制 + cobra 子命令，不产出多个可执行文件 |
| D16 | 迁移策略 | goose 版本化 SQL（嵌入二进制），生产禁用 `AutoMigrate` |
| D17 | 定时任务调用目标 | 白名单处理器注册表，**禁止**基于字符串反射/eval 的 `invoke_target` |
| D18 | 开源交付 | 仓库卫生、中英双语 README、**MIT License**、贡献指南、语义化版本与发布流程属于交付范围，见第 21 节 |
| **D19** | **模型配置模块** | 多服务商配置，**在后台界面管理**（不只是 yaml）。内置常用厂商预设，用户选厂商后只需填 API Key 即可用。Key **AES-256-GCM 加密入库**，接口只返回掩码 |
| **D20** | **存储配置模块** | 对象存储（S3 / MinIO 及各家 S3 兼容服务）**在后台界面管理**，同时保留 `local` 驱动保证开箱可跑。Secret 同样加密入库 |
| **D21** | **配置类 Capability 不对 AI 开放** | `sys_ai_provider` / `sys_storage_config` / 访问令牌 的写操作强制 `AIEnabled=false` + `MCPEnabled=false`，`Risk=high`。理由见 4.10 |

### 3.1 依赖使用约定

**lancet —— 优先复用，禁止重复实现**

动手写任何通用工具函数前先查 lancet：

| 场景 | 包 | 典型函数 |
| --- | --- | --- |
| 切片 | `slice` | `Unique` `Filter` `Map` `Contain` `Difference` `Chunk` `ToMap` |
| Map | `maputil` | `Keys` `Values` `Merge` `Filter` |
| 字符串 | `strutil` | `CamelCase` `SnakeCase` `Truncate` `IsBlank` `HideString` |
| 转换 | `convertor` | `ToString` `ToInt` `ToJson` `StructToMap` `CopyProperties`（**diff 计算会大量用**） |
| 时间 | `datetime` | `FormatTimeToStr` `BeginOfDay` `AddDay` `BetweenSeconds` |
| 哈希 | `cryptor` | `Sha256` `Md5String`（**密码仍用 bcrypt**） |
| 随机 | `random` | `RandString` `UUIdV4`（PAT、会话 ID、changeset ID） |
| 指针 | `pointer` | `Of` `Unwrap`（大量用于可选字段与 MCP annotations） |
| 文件 | `fileutil` | `IsExist` `CreateDir` `FileSize` `MiMeType` |
| 即席校验 | `validator` | `IsEmail` `IsChineseMobile` `IsJSON` `IsIp` |

**与 go-playground/validator 的分工**：Capability 入参的结构体校验一律走 `validate:"..."` tag（执行器统一调用）；lancet 的 `validator` 只用于 service 内部的即席判断。不要两处定义同一条规则。

**禁止**在 `internal/pkg` 下新建 `sliceutil.go` / `strutil.go` 这类与 lancet 重叠的文件。`internal/pkg` 只放项目特有的东西（响应信封、错误码、分页、diff 工具、SSE 写入器等）。

**cobra 与 viper 配合**：根命令定义持久化 flag（`--config`、`--log-level`），在 `PersistentPreRunE` 里加载配置并做启动期强校验，子命令只负责装配运行。所有子命令共享 `bootstrap`。

---

## 4. 核心设计一：Capability 层

这是整个项目的骨架，**必须先把这一层做对再铺业务**。

### 4.1 定义

```go
// internal/capability/capability.go
package capability

type Risk string
const (
    RiskLow      Risk = "low"      // 纯查询、刷新缓存
    RiskMedium   Risk = "medium"   // 改单条记录、启停
    RiskHigh     Risk = "high"     // 批量改、删除、授权变更
    RiskCritical Risk = "critical" // 删角色、改超管、清空日志、改自己的权限
)

type Source string
const (
    SourceHTTP Source = "http"   // 人在页面上操作
    SourceAI   Source = "ai"     // 站内 AI Agent
    SourceMCP  Source = "mcp"    // 外部 MCP 客户端
    SourceTask Source = "task"   // 定时任务
)

type HTTPBinding struct {
    Method string // GET/POST/PUT/DELETE
    Path   string // "/system/user/:userId"
}

type Example struct {
    Utterance string // "把用户 test01 停用"
    Args      string // `{"userId": 3, "status": "1"}`
}

type Meta struct {
    Name     string   // 全局唯一，点分：system.user.changeStatus
    Summary  string   // 中文功能说明（OpenAPI / AI / MCP 描述）
    Tags     []string // ["系统管理","用户"]
    Perm     string   // "system:user:edit"；空串 = 登录即可

    Risk       Risk // 必填。零值在注册时被强制视为 RiskCritical 并打 WARN
    Mutating   bool // 写操作或有副作用
    Idempotent bool
    Reversible bool // 是否提供了 Undo

    HTTP       *HTTPBinding // nil = 不暴露 HTTP
    AIEnabled  bool
    MCPEnabled bool

    Examples []Example // few-shot，显著提升小模型参数拼装成功率
    LogTitle string    // 审计标题
    BizType  int       // 0其它 1新增 2修改 3删除 4授权 5导出 6导入 7强退 8清空

    // 导出：非空时自动生成 {path}/export 与 {path}/template 端点（见 4.9）
    Export *ExportSpec
}

type ExportSpec struct {
    SheetName string         // "用户数据"
    FileName  string         // "用户_{date}"，{date} 运行时替换
    Perm      string         // 导出权限串；空则沿用 Meta.Perm
    Columns   []ExportColumn
    // 取数据的字段路径：输出结构里哪个字段是行集合，默认 "rows"
    RowsField string
}

type ExportColumn struct {
    Field  string // 行对象里的字段名（支持 "a.b" 嵌套）
    Label  string // 表头中文
    Width  float64
    Dict   string // 非空时按字典把值翻译成可读文本
    Format string // "datetime" | "date" | "money" | ""
}

type Capability[I, O any] struct {
    Meta
    // 业务执行。Mutating 时须在事务内完成，并通过 ctx 上的 Recorder 记录变更项
    Fn func(ctx context.Context, in I) (O, error)
    // 预演。Mutating == true 时必填（否则注册时 panic）
    DryRun func(ctx context.Context, in I) (*Preview, error)
    // 补偿。Reversible == true 时必填
    Undo func(ctx context.Context, undo json.RawMessage) error
}

func Register[I, O any](c Capability[I, O])
```

`I` 必须是结构体，字段 tag 同时描述四种用途：

```go
type ChangeStatusIn struct {
    UserID int64  `json:"userId" uri:"userId" validate:"required,gt=0" jsonschema:"用户 ID"`
    Status string `json:"status" validate:"required,oneof=0 1"    jsonschema:"目标状态：0 正常，1 停用"`
}
```

- `uri` → gin 路径参数 ｜ `form` → query ｜ `json` → JSON body、AI/MCP 入参
- `validate` → validator 规则 ｜ `jsonschema` → 字段描述，进 OpenAPI/AI/MCP schema

### 4.2 Preview（结构化预览）

```go
type Op string
const (OpCreate Op = "create"; OpUpdate Op = "update"; OpDelete Op = "delete"; OpSkip Op = "skip")

type FieldDiff struct {
    Field  string `json:"field"`  // "status"
    Label  string `json:"label"`  // "状态"
    Before any    `json:"before"` // "0"
    After  any    `json:"after"`  // "1"
    // 字典字段同时给出可读文本，供 UI 显示「正常 → 停用」
    BeforeText string `json:"beforeText,omitempty"`
    AfterText  string `json:"afterText,omitempty"`
}

type Change struct {
    Entity string      `json:"entity"` // "sys_user"
    ID     string      `json:"id"`
    Label  string      `json:"label"`  // "test01（张三）"，给人看的标识
    Op     Op          `json:"op"`
    Fields []FieldDiff `json:"fields,omitempty"`
    Reason string      `json:"reason,omitempty"` // Op == OpSkip 时说明原因
}

type Preview struct {
    Summary  string   `json:"summary"`  // "将停用 4 个用户，跳过 1 个（超管）"
    Affected int      `json:"affected"` // 实际会变更的条数
    Skipped  int      `json:"skipped"`
    Changes  []Change `json:"changes"`  // 上限 200 条，超出截断并在 Summary 说明
    Warnings []string `json:"warnings"`
    Evidence *Evidence `json:"evidence,omitempty"` // 供前端生成「去核对」链接
    Reversible bool   `json:"reversible"`          // false 时 UI 必须标红提示不可回滚
}

type Evidence struct {
    Route   string         `json:"route"`   // "/system/user"
    Filters map[string]any `json:"filters"` // 点进去即为同样筛选条件
}
```

**DryRun 必须是只读的**，不得产生任何副作用。执行器会在 `DryRun` 调用期间把 GORM session 置为只读（`gorm.Session{DryRun: false}` 配合一个禁止写的 callback 拦截 `Create/Update/Delete`），确保实现者即使写错也不会改数据——这一层防护必须有测试。

### 4.3 降低 DryRun / Undo 的编写成本（关键）

如果每个写操作都要手写 dry-run 和 undo，这个设计会因为太烦而被绕过。所以提供泛型助手覆盖 90% 的 CRUD 场景：

```go
// internal/capability/dryrun
func ForUpdate[M any](ctx, db, id any, patch map[string]any, opt LabelOpt) (*Preview, error)
func ForUpdateMany[M any](ctx, db, ids []any, patch map[string]any, opt LabelOpt) (*Preview, error)
func ForDelete[M any](ctx, db, ids []any, opt LabelOpt) (*Preview, error)
func ForCreate[M any](ctx, draft *M, opt LabelOpt) (*Preview, error)

// internal/capability/undo
func RestoreFields(ctx, db, table string, id any, before map[string]any) error
func RestoreSoftDeleted(ctx, db, table string, ids []any) error
func DeleteCreated(ctx, db, table string, ids []any) error
```

于是一个典型 Capability 只需：

```go
capability.Register(capability.Capability[ChangeStatusIn, Empty]{
    Meta: capability.Meta{
        Name: "system.user.changeStatus", Summary: "启用或停用用户",
        Tags: []string{"系统管理", "用户"}, Perm: "system:user:edit",
        Risk: capability.RiskMedium, Mutating: true, Reversible: true, Idempotent: true,
        HTTP: &capability.HTTPBinding{Method: "PUT", Path: "/system/user/changeStatus"},
        AIEnabled: true, MCPEnabled: true,
        LogTitle: "用户管理", BizType: 2,
        Examples: []capability.Example{
            {Utterance: "把用户 test01 停用", Args: `{"userId":3,"status":"1"}`},
            {Utterance: "恢复 3 号用户", Args: `{"userId":3,"status":"0"}`},
        },
    },
    DryRun: func(ctx context.Context, in ChangeStatusIn) (*capability.Preview, error) {
        return dryrun.ForUpdate[model.SysUser](ctx, db, in.UserID,
            map[string]any{"status": in.Status},
            dryrun.LabelOpt{Route: "/system/user", DictFields: map[string]string{"status": "sys_normal_disable"}})
    },
    Fn:   userSvc.ChangeStatus,   // 内部用 recorder.Update(...) 记录变更项
    Undo: undo.RestoreFieldsFn("sys_user"),
})
```

**不可逆的操作**（清空日志、强退用户、刷新缓存）：`Reversible: false`，`DryRun` 仍要写（说明将影响多少条），UI 标红「此操作不可回滚」。

### 4.4 注册表

```go
type Entry struct {
    Meta
    InputSchema  *jsonschema.Schema
    OutputSchema *jsonschema.Schema
    newInput     func() any
    invoke       func(ctx context.Context, in any) (any, error)
    dryRun       func(ctx context.Context, in any) (*Preview, error)
    undo         func(ctx context.Context, data json.RawMessage) error
}

func Lookup(name string) (*Entry, bool)
func All() []*Entry
// 按调用主体裁剪：权限 + 入口开关 + 风险上限
func Visible(perms []string, src Source, maxRisk Risk) []*Entry
func Search(perms []string, src Source, maxRisk Risk, keyword string, limit int) []*Entry
```

`Visible` / `Search` 是 AI 与 MCP 工具目录的来源：**调用方看不到自己无权执行、或超出风险上限的功能**。模型从一开始就不会尝试越权操作，省掉大量无效往返。

### 4.5 执行器（唯一执行路径）

```go
type Actor struct {
    Type     string // human | agent | mcp_client | system
    UserID   int64
    UserName string
    Source   Source
    Perms    []string // 有效权限集；超管为 ["*:*:*"]
    MaxRisk  Risk     // 该主体允许执行的风险上限
    IP, UserAgent string
    TokenID  int64  // Source == SourceMCP
    Intent   string // 原始自然语言请求（AI/MCP 入口有值）
    PlanID   string // 属于某个 Plan 时
}

func WithActor(ctx context.Context, a *Actor) context.Context
func ActorFrom(ctx context.Context) (*Actor, bool)

func Preview(ctx context.Context, name string, raw json.RawMessage) (*Preview, error)
func InvokeTyped(ctx context.Context, name string, in any) (any, error)   // HTTP 入口，入参已绑定
func InvokeJSON(ctx context.Context, name string, raw json.RawMessage) (any, error) // AI/MCP 入口
```

`Invoke*` 汇聚到同一个内部 `run()`，固定顺序：

1. `Lookup`，不存在 → `NotFound`
2. 入口可见性：`src == ai && !AIEnabled` → `NotFound`（不泄露存在性）；`mcp` 同理
3. **风险门槛**：`Risk > actor.MaxRisk` → `errcode.RiskExceeded`，错误体里带 `requiredRisk` 与建议（"请在页面手动执行"）
4. 权限：`Perm != ""` 且不满足 → `Forbidden`（超管 `*:*:*` 直通）
5. 入参反序列化（`DisallowUnknownFields`）+ validator 校验 → `BadRequest`
6. `defer recover()` → 记 500 并写审计
7. **开启事务**，在 ctx 注入 `Recorder`；执行 `Fn`
8. `Mutating` 时：从 Recorder 取变更项，写 `sys_changeset` + `sys_changeset_item`（与业务同一事务，保证审计不丢）
9. 提交事务
10. 出参敏感字段脱敏（4.7）
11. 异步写 `sys_oper_log`（含 actor_type / source / intent / plan_id / changeset_id / risk / approval_mode）

**权限、风险门槛、校验、审计、changeset 只在这里实现一次。** 这是选择 Capability 层而非「AI 直调 service」的根本原因。

### 4.6 Recorder（变更采集）

```go
type Recorder interface {
    Create(entity, id, label string, after map[string]any)
    Update(entity, id, label string, before, after map[string]any)
    Delete(entity, id, label string, before map[string]any)
    Skip(entity, id, label, reason string)
    // 复杂操作可直接提供自定义 undo 载荷
    SetUndo(data any)
}
func RecorderFrom(ctx context.Context) Recorder
```

service 层在写数据时顺手调一次 `recorder.Update(...)`。这是唯一需要业务代码配合的地方，`CONTRIBUTING.md` 要重点说明。

> 考虑过用 GORM callback 自动采集 before/after，但对批量更新、原生 SQL、级联软删除不可靠。选择显式记录：多一行代码，换确定性。

### 4.7 出参与审计脱敏

统一按字段名（大小写不敏感）剔除：`password` `passwd` `salt` `token` `access_token` `refresh_token` `secret` `private_key` `token_hash`。支持结构体 tag `sensitive:"true"` 显式标记。审计日志的入参、changeset 的 before/after 走同一套脱敏。

### 4.8 HTTP 适配器

```go
// internal/adapter/httpx
func Mount(g *gin.RouterGroup, entries []*capability.Entry)
func OpenAPI(entries []*capability.Entry) ([]byte, error) // OpenAPI 3.1
```

生成的 handler 固定逻辑：`ShouldBindUri → ShouldBindQuery → ShouldBindJSON`（有 body 时）填充同一入参结构体 → `InvokeTyped` → 包信封。

**额外自动生成**：每个 `Mutating` Capability 自动获得一个 `POST {path}/preview` 的预览端点（权限与原 Capability 一致，`Risk` 视为 `low`，因为只读）。前端页面的删除/批量操作按钮先调它拿 diff，再弹预览确认框——**人类页面因此天然获得 dry-run**，不需要为页面单独写预览逻辑。

无法走 Capability 的接口（multipart 上传、SSE 流）保留手写 gin handler，**不进注册表**，因此不会暴露给 AI / MCP。

### 4.9 Excel 导出（自动生成，复用列表 Capability）

导出最容易写成"每个列表再写一遍查询逻辑"，于是筛选条件与列表页不一致，这是传统后台的经典 bug 来源。本项目**不单独写导出 Capability**：列表 Capability 声明 `Export` 后，`httpx` 适配器自动生成两个端点。

| 端点 | 说明 |
| --- | --- |
| `POST {path}/export` | 入参与列表 Capability **完全相同**（同一个筛选结构体），内部调同一个 `Fn`，用 excelize 渲染后返回 xlsx |
| `GET {path}/template` | 用同一套 `Columns` 生成只有表头的空模板，供批量导入使用 |

声明示例：

```go
Export: &capability.ExportSpec{
    SheetName: "用户数据", FileName: "用户_{date}", Perm: "system:user:export",
    Columns: []capability.ExportColumn{
        {Field: "userId",     Label: "用户编号", Width: 10},
        {Field: "userName",   Label: "登录名",   Width: 16},
        {Field: "nickName",   Label: "昵称",     Width: 16},
        {Field: "status",     Label: "状态",     Width: 10, Dict: "sys_normal_disable"},
        {Field: "loginDate",  Label: "最后登录", Width: 20, Format: "datetime"},
    },
},
```

于是「列表、OpenAPI、AI 能力、MCP 工具、导出、导入模板」六件事由一处定义驱动，筛选条件天然一致。

**执行规则**：

- **权限**：`ExportSpec.Perm`（空则沿用 `Meta.Perm`）。约定为 `{模块}:{实体}:export`，在菜单里作为 F 类型按钮权限
- **风险等级**：导出按 `medium` 处理（虽是只读，但有批量数据外带风险），**必入操作日志**（`BizType=5`，记录筛选条件与导出行数）
- **AI / MCP 不开放导出** —— 流式响应里塞不了文件，且让 AI 随手导出全库数据是不必要的风险面。AI 的正确行为是输出 `navigate` 把用户带到列表页并预填筛选，由人点导出按钮。这一条在 system prompt 里写明
- **分页参数被忽略**：导出时执行器把 `pageSize` 置为导出上限，不受页面分页影响。上限 `export.max_rows`（默认 50000）
- **大数据量**：行数 > `export.stream_threshold`（默认 5000）时用 excelize 的 `StreamWriter` 逐行写，避免全量驻留内存
- **超限转异步**：预估行数 > `export.max_rows` 时拒绝同步导出，返回提示并给出「转为后台导出」选项 → 投递 Asynq 任务 `system:exportAsync` → 生成文件写入 `sys_upload` → 完成后站内通知，用户到文件管理页下载。异步导出文件 7 天后由清理任务删除
- **字典翻译**：`Dict` 非空的列用知识库/字典缓存把 `0` 渲染成 `正常`，与页面显示一致
- **响应头**：`Content-Type: application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`，`Content-Disposition: attachment; filename*=UTF-8''<urlencode(名称)>.xlsx`（必须用 `filename*` 形式，否则中文文件名在部分浏览器乱码）

**前端**：`PageTable` 的工具栏统一提供导出按钮，由 `useExport(capability)` hook 实现——带上当前筛选条件 POST 到 `/export`，用 blob 下载。页面无需为导出写任何额外代码。

> 批量**导入**（上传 xlsx 写库）不在 v1 范围。模板下载已就绪，导入时可复用同一套 `Columns` 做解析与校验。

### 4.10 凭证加密与配置类 Capability 的隔离（D19 / D20 / D21）

模型 API Key 与对象存储 Secret 要存库（因为要在后台界面里配），这带来两类必须处理的风险。

#### 加密存储

```go
// internal/pkg/secret
func Encrypt(plain string) (string, error) // AES-256-GCM，输出 base64(nonce||ciphertext)
func Decrypt(cipher string) (string, error)
func Mask(plain string) string            // "sk-abc...wxyz" → "sk-ab••••••wxyz"
```

- 密钥来自配置 `app.encrypt_key`（32 字节，base64 或 hex）。**为空拒绝启动**，与 `jwt.secret` 同等对待
- 每次加密生成随机 nonce，拼在密文前
- `app.encrypt_key` 变更后旧密文无法解密 —— 文档必须写明这一点，并提供 `intent-admin migrate rekey --old-key=...` 子命令做密钥轮换（遍历两张表解密后用新密钥重新加密）
- **接口永不返回明文**：列表与详情只返回 `Mask()` 结果。更新时该字段留空 = 保留原值，填了 = 替换
- `before`/`after` 快照、操作日志入参、Excel 导出一律按 4.7 的敏感字段规则剔除（字段名含 `api_key` / `secret_access_key` / `_enc`）

#### 为什么配置类 Capability 绝不能对 AI 开放

模型配置里有 `base_url`。如果 AI 能改它，一次提示注入就能把 `base_url` 指向攻击者的服务器——此后**所有会话内容与业务数据都会被转发出去**，而系统表面上一切正常。存储配置同理（改 endpoint 即可劫持全部上传文件）。访问令牌管理也一样（可用受限令牌造全权令牌）。

这三类的写操作强制：

```go
Risk: capability.RiskHigh, AIEnabled: false, MCPEnabled: false,
```

读操作（列表/详情）可以 `AIEnabled: true`（便于 AI 回答"当前用的是哪个模型"），但返回值已掩码。

**这是硬约束，注册时校验**：`internal/feature` 初始化后跑一次自检，若发现 `sys_ai_provider` / `sys_storage_config` / `sys_access_token` 相关的 `Mutating` Capability 带了 `AIEnabled` 或 `MCPEnabled`，**直接 panic 拒绝启动**。靠人记住不可靠，让程序拦住。

---

## 4A. 模型配置模块（D19）

### 4A.1 目标

用户在后台「系统管理 → 模型服务」页面：选厂商 → 填 API Key → 点测试 → 启用。不需要碰配置文件、不需要重启。

### 4A.2 内置厂商预设

预设在 Go 代码里声明（`internal/aiprovider/presets.go`），不入库。用户选中后自动填好 `base_url` 与候选模型，只需填 Key。

| vendor | 名称 | base_url | 适配方式 |
| --- | --- | --- | --- |
| `deepseek` | DeepSeek | `https://api.deepseek.com/v1` | `eino-ext/components/model/openai` |
| `ark` | 火山方舟（豆包） | 由 ark 组件管理 | **`eino-ext/components/model/ark`**（原生组件，非 openai 兼容层） |
| `dashscope` | 阿里云百炼（通义千问） | `https://dashscope.aliyuncs.com/compatible-mode/v1` | openai |
| `moonshot` | 月之暗面（Kimi） | `https://api.moonshot.cn/v1` | openai |
| `zhipu` | 智谱（GLM） | `https://open.bigmodel.cn/api/paas/v4` | openai |
| `siliconflow` | 硅基流动 | `https://api.siliconflow.cn/v1` | openai |
| `openai` | OpenAI | `https://api.openai.com/v1` | openai |
| `custom` | 自定义（任意 OpenAI 兼容） | 用户填 | openai |

```go
type Preset struct {
    Vendor      string   // "deepseek"
    Name        string   // "DeepSeek"
    BaseURL     string   // 可被用户覆盖
    Adapter     string   // "openai" | "ark"
    Models      []string // 候选模型，供下拉选择；用户仍可手填
    DocURL      string   // 获取 API Key 的页面，前端显示「去获取」链接
    NeedsRegion bool
}
func Presets() []Preset
```

`Models` 只是候选值，不是白名单——厂商上新模型时用户可直接手填，不用等本项目更新。

> `ark` 用独立适配器是因为 eino-ext 对火山方舟有原生组件，走它比套 OpenAI 兼容层更稳。实现时 `internal/aiprovider` 暴露统一的 `BuildChatModel(cfg) (model.BaseChatModel, error)`，内部按 `Adapter` 分支，上层（第 15 节的 Agent 装配）不感知差异。

### 4A.3 连通性测试

`system.aiProvider.test`（`Risk: low`，只读不落库）：

1. 调上游 `GET {base_url}/models`（ark 走其等价接口），验证 Key 有效
2. **发一个最小的 function calling 探测请求**：带一个 `echo(text)` 工具，看模型是否返回 tool_call
3. 返回 `{ok, reachable, supportsToolCalling, models[], latencyMs, error?}`

第 2 步是关键。**不支持 tool calling 的模型在本项目里完全不可用**（Agent 的 5 个工具全靠它）。必须在配置阶段就查出来，而不是让用户配好之后在会话里遇到一堆莫名其妙的失败。前端对 `supportsToolCalling == false` 的 provider 显示红色警告并禁止设为默认。

### 4A.4 配置优先级

1. 数据库中 `enabled='0'` 且 `is_default=true` 的 provider —— 正常路径
2. 数据库中任一 enabled 的 provider
3. yaml 的 `ai.*` —— **仅用于首次部署引导**：容器化部署时可用环境变量注入，启动时若库里无 provider 则自动创建一条（vendor 取 `ai.vendor`，默认 `custom`）。之后以库为准

这样既满足"后台可配"，也不破坏"docker run 带几个环境变量就能跑"。

### 4A.5 会话中的模型选择

`sys_ai_plan` / 会话记录里存 `provider_id` + `model`。会话页顶部的模型选择器按 provider 分组展示，切换后保存在会话上。provider 被禁用或删除时，已有会话回退到默认 provider 并在界面提示。

---

## 4B. 存储配置模块（D20）

### 4B.1 驱动

| driver | 说明 |
| --- | --- |
| `s3` | 走 `minio-go/v7`，兼容 MinIO / AWS S3 / 阿里云 OSS / 腾讯云 COS / Cloudflare R2 / 七牛 |
| `local` | 本地磁盘。**保留**，用于零外部依赖的快速体验与单机部署 |

内置预设（同样在代码里声明）：

| preset | endpoint 形态 | path_style |
| --- | --- | --- |
| MinIO | 用户填（如 `minio:9000`） | **true** |
| AWS S3 | `s3.{region}.amazonaws.com` | false |
| 阿里云 OSS | `oss-{region}.aliyuncs.com` | false |
| 腾讯云 COS | `cos.{region}.myqcloud.com` | false |
| Cloudflare R2 | `{account}.r2.cloudflarestorage.com` | false |
| 七牛 Kodo | `s3-{region}.qiniucs.com` | false |

`path_style` 是 MinIO 接入最常见的坑（不开会导致 bucket 被当成子域名解析而失败），所以做成预设里的显式字段而不是让用户猜。

### 4B.2 配置项与测试

字段见 9.4。连通性测试 `system.storage.test`（`Risk: low`）：

1. `BucketExists` 检查桶是否存在与可访问
2. 上传一个 1KB 探针文件 `.intent-admin-probe`
3. 生成访问 URL 并回读校验
4. 删除探针文件
5. 返回 `{ok, bucketExists, writable, readable, publicUrlOk, latencyMs, error?}`

第 3 步（回读公网 URL）单独报告，因为"能上传但公网访问不到"是最常见的配置错误（桶权限 / `base_url` 填错），分开报告才能让用户知道该改哪里。

### 4B.3 上传链路

```text
POST /api/system/upload (multipart, 手写 handler，不进注册表)
  → 校验扩展名与大小
  → 按 biz_type 生成对象键：{biz_type}/{yyyy}/{MM}/{uuid}{ext}
  → 取当前默认存储配置 → driver 上传
  → 写 sys_upload（含 storage_id，便于配置变更后仍能定位文件）
  → 返回 {uploadId, url, fileName, size}
```

`sys_upload.storage_id` 记录当时用的存储配置——换了存储后老文件仍能通过原配置生成 URL，不至于全站图片失效。

私有桶的访问：`sys_storage_config.url_mode` 为 `presigned` 时，文件 URL 通过 `GET /api/system/upload/:id/url` 实时签发预签名链接（默认有效期 1 小时），而不是存死链。

---

## 5. 核心设计二：Plan → Preview → Apply → Undo

### 5.1 Plan 结构

```go
type PlanStep struct {
    Seq        int             `json:"seq"`
    Capability string          `json:"capability"`
    Args       json.RawMessage `json:"args"`    // 可含步骤引用，见 5.2
    Note       string          `json:"note"`    // AI 给人看的这一步意图
    Preview    *capability.Preview `json:"preview,omitempty"` // 预演结果
    Status     string          `json:"status"`  // pending|previewed|applied|failed|skipped
    Error      string          `json:"error,omitempty"`
}

type Plan struct {
    ID       string      `json:"id"`
    Intent   string      `json:"intent"`   // 用户原话
    Steps    []PlanStep  `json:"steps"`
    MaxRisk  Risk        `json:"maxRisk"`  // 所有步骤中的最高风险
    Affected int         `json:"affected"` // 汇总影响条数
    Mode     string      `json:"mode"`     // all | stepwise（用户选的执行方式）
    Status   string      `json:"status"`   // draft|previewed|approved|executing|done|partial|aborted
}
```

### 5.2 步骤间引用（让 Plan 真正可用的关键）

多步任务的典型形态是「先查出一批，再改这批」。第 2 步的参数依赖第 1 步的结果，不解决这个问题 Plan 就只是摆设。

约定：参数值为形如 `"$steps[0].result.rows[*].userId"` 的字符串时，视为引用，在执行/预演前按 JSONPath 子集求值。支持的语法刻意收窄到三种，避免实现成一个表达式引擎：

- `$steps[N].result.<字段路径>` —— 取标量
- `$steps[N].result.<数组路径>[*].<字段>` —— 取数组，结果为切片
- `$ctx.selectedIds` / `$ctx.visibleIds` —— 取当前页面上下文（见第 7 节）

**预演时的处理顺序**（重要）：
1. 按 seq 遍历步骤
2. 遇到 `Risk == low && !Mutating` 的只读步骤 → **真实执行**（只读安全），结果存入步骤上下文供后续引用
3. 遇到写步骤 → 求值引用后调 `DryRun`，得到 Preview；**不执行**
4. 若某个写步骤的结果被后续步骤引用（如「新建角色，再给它授权」），dry-run 阶段无法得到真实 ID → 该步骤标记 `Preview.Warnings += "依赖上一步结果，实际值在执行时确定"`，并把引用保留到执行阶段

### 5.3 Apply

1. 校验 Plan 状态为 `previewed` 且未过期（Plan TTL 30 分钟，过期需重新预演——避免用户看的是 10 分钟前的数据状态）
2. **二次校验**：重新跑一遍所有写步骤的 `DryRun`，与用户确认时看到的 Preview 做比对。若影响面发生变化（条数变了、关键字段 before 变了）→ 中止并提示「数据已变化，请重新确认」。**这是防 TOCTOU 的必要步骤**，不能省
3. 创建一个 `sys_changeset`（整个 Plan 一个 changeset）
4. 按 seq 顺序执行，每步的变更项写入该 changeset
5. 某步失败 → 默认**停止后续步骤**，changeset 状态 `partial`，已执行部分保留且可回滚（不自动回滚：自动回滚可能比停下来更危险）。Plan 可配置 `onError: stop | continue`
6. `mode == stepwise` 时每步执行前推送一次确认事件

### 5.4 Changeset 与回滚

```text
sys_changeset
  id, actor_type, actor_id, actor_name, source, intent, plan_id,
  status(applied|partial|rolled_back|rollback_failed),
  risk, summary, affected_count, approval_mode, approved_by,
  created_at, rolled_back_at, rolled_back_by

sys_changeset_item
  id, changeset_id, seq, capability, entity, entity_id, entity_label, op,
  args_json, before_json, after_json, undo_json,
  status(applied|failed|skipped|rolled_back), error_msg, created_at
```

**回滚**：`system.changeset.rollback`（`Risk: high`，需 `system:changeset:rollback` 权限）

1. 倒序遍历 `status == applied` 的 items
2. 按 item 的 `capability` 找到 Entry，调其 `Undo(undo_json)`
3. 单项失败 → 继续处理其余项，最后汇总；changeset 状态 `rollback_failed`，失败项在 UI 上明确列出
4. 回滚本身也产生一个新的 changeset（`intent: "回滚 #<id>"`），形成可追溯的链条。**回滚的回滚** = 再回滚一次那个新 changeset
5. 不可逆项（`Reversible == false`）在回滚时标记 `skipped` 并说明原因

**保留策略**：changeset 默认保留 90 天，`before_json`/`after_json` 只存**变更涉及的字段**而非整行（避免 jsonb 膨胀）。超期由定时任务清理，清理前的统计信息并入操作日志。

### 5.5 这套机制对人类入口同样生效（D4）

页面上的"删除""批量停用"按钮：

```text
点击 → POST /system/user/remove/preview → 弹出 <DiffTable> 预览框
     → 用户确认 → DELETE /system/user/:ids → 生成 changeset
     → Toast 显示「已停用 4 个用户 · 撤销」，点撤销即回滚
```

**这是产品层面的实际收益**：人类误删同样能一键撤销。所以可逆性不是"给 AI 兜底的成本"，而是整个系统的能力升级。单条低风险操作（如改个备注）可配置跳过预览直接执行，但仍进 changeset。

---

## 6. 核心设计三：系统自省知识库（D9）

AI 在 admin 场景的致命问题是幻觉——猜错 `status=0` 的含义、猜错实体关系、不知道超管不能删，然后生成一个看似合理的破坏性操作。解决办法是把系统知识喂给它，而不是让它猜。

### 6.1 三类知识

```go
// internal/knowledge
type Knowledge struct {
    Enums    []EnumDef     // 字典语义
    Entities []EntityDef   // 实体与关系
    Rules    []RuleDef     // 业务规则
}

type EnumDef struct {
    DictType string            // "sys_normal_disable"
    Summary  string            // "通用状态"
    Values   map[string]string // {"0":"正常","1":"停用"}
    UsedBy   []string          // ["sys_user.status","sys_role.status"]
}

type EntityDef struct {
    Table    string   // "sys_user"
    Summary  string   // "系统用户"
    KeyField string   // "user_id"
    LabelField string // "user_name"
    Relations []Relation // {Kind:"many2many", Target:"sys_role", Via:"sys_user_role", Summary:"用户的角色"}
    Notes    []string // "status 用 sys_normal_disable 字典；del_flag='2' 表示已软删除"
}

type RuleDef struct {
    Scope   string // "sys_user" 或 "*"
    Rule    string // "超级管理员账号不可删除、不可停用、不可被非超管修改"
    Enforced bool  // true 表示代码层已硬约束（模型违反时会收到错误而非静默成功）
}
```

- `Enums` **从 `sys_dict_data` 动态生成**（后台改字典，AI 的理解同步更新），缓存 30 分钟
- `Entities` / `Rules` 在 Go 代码里声明，与模型放一起，review 时容易发现不同步

### 6.2 注入策略

不要把全部知识塞进 system prompt（会挤占 context 且大部分用不上）。分层：

- **常驻注入**（约 500 token）：系统概览、最常用的 5 个字典、全局业务规则（超管保护、软删除语义）
- **按需工具**：`get_knowledge(topic)` —— `topic` 可以是表名、字典类型或 `"rules"`。AI 在不确定时自己查
- **Capability 的 Examples 就地提供**：`describe_capability` 返回值里带该 Capability 的 `Examples`，这是最有效的 few-shot

### 6.3 Capability 浏览器页面

把注册表和知识库做成一个可浏览页面（`/ai/capabilities`）：列出所有 Capability 的名称、说明、风险等级、所需权限、是否可逆、是否对 AI/MCP 开放、示例。

这既是给开发者的文档（开源项目的加分项），也是给管理员的审计工具——"AI 到底能对我的系统做什么"必须一眼可见，而不是埋在代码里。

---

## 7. 核心设计四：页面上下文与全局输入框（D6 / D10）

### 7.1 全局自然语言输入框

顶栏常驻（不是侧边栏里的某个"AI 助手"菜单），`Cmd/Ctrl+K` 聚焦。**它感知用户当前所在页面**。

三种输出形态，由后端判定并在 SSE 里告知前端：

| 输出 | 场景 | 前端行为 |
| --- | --- | --- |
`answer` | 查询类 | 在面板内渲染答案 + 证据链接
`plan` | 涉及写操作 | 渲染计划预览卡片，等待确认
`navigate` | 意图是"去看某个东西" | 跳转目标路由并**预填筛选条件**，同时在面板里说明"已为你筛选 X"

第三种很重要：「上周登录失败超过 3 次的账号」这类请求，最好的回应不是在聊天框里列一个表格，而是**把用户带到登录日志页并填好筛选条件**——人可以继续排序、翻页、导出，而且能验证筛选条件对不对。这就是原则 4「答案必须可核对」的落地。

### 7.2 页面上下文协议

```ts
type PageContext = {
  route: string;                    // "/system/user"
  capability?: string;              // 当前页主 capability："system.user.list"
  filters?: Record<string, unknown>;// 当前筛选条件（原样取自页面筛选表单）
  selectedIds?: (string | number)[];// 勾选的行，上限 500
  visibleIds?: (string | number)[]; // 当前页可见行 ID，上限 200
  selectedLabels?: string[];        // 勾选行的可读标识，便于 AI 在回复里称呼
};
```

每次调用 `/ai/chat/stream` 时随请求发送。后端转成一段紧凑描述注入，并把 `selectedIds` / `visibleIds` 注册为 Plan 的可引用变量（`$ctx.selectedIds`）。

效果：
- 在用户列表页勾选 3 行后说「把这几个停用」→ 直接生成针对这 3 个 ID 的计划
- 在登录日志页说「这些人里谁最近没登录过」→ AI 知道"这些人"指当前筛选结果
- 在任务页说「为什么昨晚失败了」→ AI 知道上下文是定时任务

**安全约束**：`selectedIds` 只是提示，**不绕过权限**——执行时仍逐条过权限与风险门槛。前端传来的 ID 不可信，后端必须重新校验归属与可见性。

### 7.3 双入口等权的具体含义

页面侧保留完整能力（约 25 个页面，含弹窗表单 CRUD、批量操作、导出），但复用 AI 侧的基础设施：

| 能力 | 页面入口 | AI 入口 |
| --- | --- | --- |
| 执行业务 | 自动生成的 HTTP 端点 | `invoke_capability` |
| 预览 | 自动生成的 `/preview` 端点 → `<DiffTable>` | Plan 预演 → 同一个 `<DiffTable>` 组件 |
| 回滚 | Toast 上的「撤销」+ 变更历史页 | 同 |
| 审计 | 同一张表，`actor_type=human` | `actor_type=agent` |

**前端的预览确认框和 AI 的计划卡片共用同一套 diff 渲染组件**，这是双入口不变成两套代码的关键。

---

## 8. 仓库结构

```text
intent-admin/
├── main.go                       # 仅 cobra.Execute()
├── internal/
│   ├── cli/                      # cobra 命令：root/serve/worker/mcp/migrate/version
│   ├── bootstrap/                # 依赖装配、生命周期、优雅关闭（所有子命令共用）
│   ├── config/
│   ├── model/                    # GORM 模型
│   ├── service/                  # 业务逻辑（不感知传输层；写数据时调 Recorder）
│   ├── capability/               # ★ 核心
│   │   ├── capability.go         #   Capability[I,O]、Meta、Risk、Register
│   │   ├── registry.go           #   注册表、Visible/Search（权限+入口+风险裁剪）
│   │   ├── executor.go           #   唯一执行路径
│   │   ├── preview.go            #   Preview/Change/FieldDiff
│   │   ├── recorder.go           #   变更采集
│   │   ├── actor.go              #   Actor 上下文
│   │   ├── dryrun/               #   通用 dry-run 助手
│   │   └── undo/                 #   通用 undo 助手
│   ├── plan/                     # ★ Plan 生成、引用求值、预演、执行、状态机
│   ├── changeset/                # ★ changeset 落库、查询、回滚
│   ├── knowledge/                # ★ 系统自省知识库
│   ├── adapter/                  # ★ 三个入口，都只依赖 capability
│   │   ├── httpx/                #   → gin handler + /preview 端点 + OpenAPI
│   │   ├── aitool/               #   → Eino ADK tool
│   │   └── mcpx/                 #   → MCP tool + PAT 鉴权
│   ├── feature/                  # 按模块注册 Capability（system/monitor/dashboard/...）
│   ├── middleware/               # recover/requestid/cors/jwt/ratelimit
│   ├── agent/                    # Eino ADK 装配、会话、checkpoint、SSE、意图路由
│   ├── task/                     # Asynq client/server、处理器注册表、provider
│   ├── pkg/                      # response/errcode/paginate/diff/captcha/sysinfo/sse
│   └── web/
│       ├── embed.go              # //go:embed all:dist
│       └── dist/                 # 构建期填充（含占位 index.html）
├── migrations/
├── web/                          # 前端源码
├── configs/config.example.yaml
├── Makefile · Dockerfile · docker-compose.yml
├── README.md · README.en.md · CONTRIBUTING.md · LICENSE · ...
└── docs/implementation-plan.md
```

Go module：`github.com/junler/intent-admin`。

**依赖方向严格单向**：

```text
feature ──> capability ──> service ──> model
                 ▲              │
plan ────────────┤              └──> pkg
changeset ───────┤
knowledge ───────┤
adapter/{httpx,aitool,mcpx} ──> capability, plan
agent ──> plan, knowledge, capability
bootstrap ──> 全部
```

`service` **不得**引用 `capability` / `plan` / `adapter`。

**嵌入机制**：`internal/web/dist/index.html` 提交占位文件保证后端单独可编译；`make build` 先构建前端再同步产物。开发期前端走 Vite dev server 代理 `/api`。

---

## 9. 数据库设计

`migrations/000001_init.sql` 建表，`000002_seed.sql` 种子。

`status`：`'0'` 正常 / `'1'` 停用。`del_flag`：`'0'` 存在 / `'2'` 删除（GORM 自定义软删除字段）。审计字段 `create_by/create_time/update_by/update_time/remark`。

所有表都带审计五件套（除纯日志表与关联表）：`create_by varchar(64)`、`create_time timestamptz`、`update_by varchar(64)`、`update_time timestamptz`、`remark varchar(500)`。下面的字段清单省略这五个，实现时统一补上。

**表清单**：`sys_user` `sys_role` `sys_menu` `sys_user_role` `sys_role_menu` `sys_user_perm` `sys_access_token` `sys_dict_type` `sys_dict_data` `sys_config` `sys_notice` `sys_upload` `sys_job` `sys_job_log` `sys_oper_log` `sys_login_log` `sys_ai_provider` `sys_storage_config` `sys_ai_plan` `sys_changeset` `sys_changeset_item`，共 21 张。**不要**额外建部门、岗位、代码生成相关的表（D13）。

### 9.1 用户、角色、菜单、授权

**`sys_user`** —— 系统用户

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `user_id` | `int` PK identity | |
| `user_name` | `varchar(30)` not null unique | 登录名 |
| `nick_name` | `varchar(30)` not null | 昵称 |
| `user_type` | `varchar(2)` default `'00'` | 预留 |
| `email` | `varchar(50)` default `''` | |
| `phonenumber` | `varchar(20)` default `''` | |
| `sex` | `char(1)` default `'0'` | 字典 `sys_user_sex`：0 男 1 女 2 未知 |
| `password` | `varchar(200)` not null | bcrypt |
| `avatar` | `varchar(255)` default `''` | |
| `status` | `char(1)` default `'0'` | 0 正常 1 停用 |
| `del_flag` | `char(1)` default `'0'` | 0 存在 2 删除 |
| `login_ip` | `varchar(128)` default `''` | |
| `login_date` | `timestamptz` null | |
| `ai_max_risk` | `varchar(10)` null | 该用户的 AI 风险上限；null = 用全局配置。只能低于全局 |

索引：`(user_name)` unique（含 `del_flag='0'` 的部分索引）、`(status, del_flag)`。

**`sys_role`** —— 角色

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `role_id` | `int` PK identity | |
| `role_name` | `varchar(30)` not null | |
| `role_key` | `varchar(100)` not null | 权限字符标识；`'admin'` 为超管 |
| `role_sort` | `int` default 0 | 显示顺序 |
| `menu_check_strictly` | `boolean` default false | 菜单树选择项是否父子联动（前端用） |
| `status` | `char(1)` default `'0'` | |
| `del_flag` | `char(1)` default `'0'` | |

**不设** `data_scope` / `dept_check_strictly`——本项目无数据权限范围、无部门（D7/D13）。

**`sys_menu`** —— 菜单与权限点

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `menu_id` | `int` PK identity | |
| `menu_name` | `varchar(50)` not null | |
| `parent_id` | `int` not null default 0 | 0 = 顶级 |
| `order_num` | `int` default 0 | 同级升序排序 |
| `path` | `varchar(200)` default `''` | 路由路径 |
| `component` | `varchar(255)` null | 前端组件标识，映射见 17.2；`Layout`/`ParentView`/`InnerLink` 为保留值 |
| `query` | `varchar(255)` default `''` | 路由默认 query 参数（JSON 串） |
| `is_frame` | `char(1)` default `'1'` | 0 是外链 1 否 |
| `is_cache` | `char(1)` default `'0'` | 0 缓存页面 1 不缓存 |
| `menu_type` | `char(1)` not null | `M` 目录 / `C` 菜单 / `F` 按钮 |
| `visible` | `char(1)` default `'0'` | 0 显示 1 隐藏（隐藏但路由仍可访问） |
| `status` | `char(1)` default `'0'` | 1 停用时不下发、不贡献权限 |
| `perms` | `varchar(100)` default `''` | 权限串，如 `system:user:list`。`C` 类型**必填**（见 12.2） |
| `icon` | `varchar(100)` default `''` | 图标名 |
| `del_flag` | `char(1)` default `'0'` | |

索引：`(parent_id, order_num)`、`(perms)`。

**`sys_user_role`**：`(user_id int, role_id int)`，PK `(user_id, role_id)`。

**`sys_role_menu`**：`(role_id int, menu_id int)`，PK `(role_id, menu_id)`。

**`sys_user_perm`** —— 用户额外授权（D7 的"仅加权"）

`(user_id int, perm varchar(100), create_by varchar(64), create_time timestamptz)`，PK `(user_id, perm)`。

**`sys_access_token`** —— 个人访问令牌（PAT，供 MCP 与外部 API 使用）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `token_id` | `int` PK identity | |
| `user_id` | `int` not null | 归属用户 |
| `name` | `varchar(64)` not null | 用户自定义名称 |
| `token_hash` | `char(64)` not null unique | `sha256(明文)`；**明文不入库** |
| `prefix` | `varchar(16)` not null | 明文前 12 位，用于列表展示识别 |
| `scopes` | `text` null | 逗号分隔的权限串白名单；空 = 不额外收窄 |
| `max_risk` | `varchar(10)` null | 该令牌的风险上限 |
| `expires_at` | `timestamptz` null | null = 永不过期 |
| `last_used_at` | `timestamptz` null | 更新节流到 1 分钟一次 |
| `status` | `char(1)` default `'0'` | |
| `del_flag` | `char(1)` default `'0'` | |

索引：`(token_hash)` unique、`(user_id, del_flag)`。

### 9.2 字典、参数、公告、文件

**`sys_dict_type`**：`dict_id int PK`、`dict_name varchar(100)`、`dict_type varchar(100) not null unique`、`status char(1)`、`del_flag char(1)`。

**`sys_dict_data`**：`dict_code int PK`、`dict_sort int`、`dict_label varchar(100)`、`dict_value varchar(100)`、`dict_type varchar(100)`、`css_class varchar(100)`、`list_class varchar(100)`（antd Tag 颜色）、`is_default char(1)`、`status char(1)`、`del_flag char(1)`。索引 `(dict_type, dict_sort)`。

> 这两张表同时是知识库 `Enums` 的数据源（见 6.1），字典改动后 AI 的理解同步更新。

**`sys_config`**：`config_id int PK`、`config_name varchar(100)`、`config_key varchar(100) unique`、`config_value varchar(500)`、`config_type char(1)`（Y 系统内置不可删）、`status char(1)`、`del_flag char(1)`。

**`sys_notice`**：`notice_id int PK`、`notice_title varchar(100)`、`notice_type char(1)`（1 通知 2 公告）、`notice_content text`、`status char(1)`、`del_flag char(1)`。

**`sys_upload`**：`upload_id varchar(36) PK`、`file_name varchar(255)`（原始名）、`object_key varchar(500)`（对象键 / 本地相对路径）、`url varchar(500)`、`ext varchar(32)`、`size bigint`、`mime varchar(128)`、`biz_type varchar(32)`（`avatar`/`attach`/`export`）、**`storage_id int`**（当时所用的存储配置，见 4B.3）、`status char(1)`、`del_flag char(1)`。索引 `(biz_type, create_time desc)`、`(storage_id)`。

### 9.3 模型服务配置：`sys_ai_provider`（D19）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `provider_id` | `int` PK identity | |
| `name` | `varchar(64)` not null | 显示名，如"生产-DeepSeek" |
| `vendor` | `varchar(32)` not null | 预设标识：`deepseek`/`ark`/`dashscope`/`moonshot`/`zhipu`/`siliconflow`/`openai`/`custom` |
| `adapter` | `varchar(16)` not null | `openai` / `ark`，由预设决定 |
| `base_url` | `varchar(255)` | 预设自动填，可覆盖；`ark` 可为空 |
| `api_key_enc` | `text` not null | **AES-256-GCM 加密**，接口只返回掩码（4.10） |
| `default_model` | `varchar(100)` not null | |
| `models` | `jsonb` | 该 provider 可用模型列表，测试连通性时自动刷新 |
| `supports_tool_calling` | `boolean` default false | 由连通性测试写入；false 时不可设为默认 |
| `temperature` | `numeric(3,2)` null | 可选覆盖 |
| `max_tokens` | `int` null | 可选覆盖 |
| `timeout_sec` | `int` default 60 | |
| `is_default` | `boolean` default false | **全局唯一**，用部分唯一索引约束 |
| `last_tested_at` | `timestamptz` | |
| `last_test_result` | `jsonb` | 最近一次测试的完整结果 |
| `sort` | `int` default 0 | |
| `status` | `char(1)` default `'1'` | **默认停用**，测试通过后才启用 |
| `del_flag` | `char(1)` default `'0'` | |

索引：`(status, del_flag, sort)`；唯一索引 `unique (is_default) where is_default = true and del_flag = '0'`。

### 9.4 存储配置：`sys_storage_config`（D20）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `storage_id` | `int` PK identity | |
| `name` | `varchar(64)` not null | 显示名 |
| `driver` | `varchar(16)` not null | `s3` / `local` |
| `preset` | `varchar(32)` | `minio`/`aws`/`oss`/`cos`/`r2`/`kodo`/`custom` |
| `endpoint` | `varchar(255)` | `s3` 必填 |
| `region` | `varchar(64)` | |
| `bucket` | `varchar(128)` | `s3` 必填 |
| `access_key_id` | `varchar(255)` | |
| `secret_key_enc` | `text` | **AES-256-GCM 加密** |
| `use_ssl` | `boolean` default true | |
| `path_style` | `boolean` default false | **MinIO 必须 true**（见 4B.1） |
| `base_url` | `varchar(255)` | 公网访问前缀；`url_mode='static'` 时用它拼 URL |
| `url_mode` | `varchar(12)` default `'static'` | `static`（公开桶）/ `presigned`（私有桶，实时签发） |
| `presign_expire_sec` | `int` default 3600 | |
| `local_dir` | `varchar(255)` | `driver='local'` 时的目录 |
| `max_size` | `bigint` default 20971520 | 单文件上限（字节） |
| `allow_ext` | `jsonb` | 允许的扩展名数组 |
| `is_default` | `boolean` default false | **全局唯一**，部分唯一索引约束 |
| `last_tested_at` | `timestamptz` | |
| `last_test_result` | `jsonb` | |
| `status` | `char(1)` default `'1'` | 默认停用 |
| `del_flag` | `char(1)` default `'0'` | |

索引：唯一索引 `unique (is_default) where is_default = true and del_flag = '0'`。

> 两张表的 `_enc` 字段都必须进 4.7 的脱敏字段名列表，且 changeset 的 before/after 不记录它们（只记"已变更"的事实，不记值）。

### 9.5 定时任务

**`sys_job`**

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `job_id` | `int` PK identity | |
| `job_name` | `varchar(64)` not null | |
| `job_group` | `varchar(64)` default `'DEFAULT'` | 映射到 Asynq 队列 |
| `invoke_target` | `varchar(100)` not null | **处理器注册表里的处理器名**，必须命中白名单（D17） |
| `job_params` | `text` null | JSON 入参 |
| `cron_expression` | `varchar(255)` not null | **标准 5 字段**或 `@every` 描述符（见 14.4） |
| `misfire_policy` | `varchar(20)` default `'3'` | 映射到重试与超时 |
| `concurrent` | `char(1)` default `'1'` | 1 禁止并发 0 允许 |
| `status` | `char(1)` default `'1'` | **默认停用**，避免新建即跑 |
| `del_flag` | `char(1)` default `'0'` | |

**`sys_job_log`**：`job_log_id bigserial PK`、`job_id int`、`job_name varchar(64)`、`job_group varchar(64)`、`invoke_target varchar(100)`、`job_params text`、`task_id varchar(64)`（Asynq task id）、`job_message varchar(500)`、`status char(1)`（0 成功 1 失败）、`exception_info text`、`cost_time int`（毫秒）、`create_time timestamptz`。索引 `(job_id, create_time desc)`、`(create_time desc)`。

### 9.6 行为溯源：`sys_oper_log`

不是传统的"接口访问日志"，而是带意图与审批链条的行为记录。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `oper_id` | `bigserial` PK | |
| `title` | `varchar(50)` | 来自 `Meta.LogTitle` |
| `business_type` | `int` | 来自 `Meta.BizType` |
| `capability` | `varchar(100)` | Capability 名 |
| `method` | `varchar(100)` | 处理函数标识 |
| `request_method` | `varchar(10)` | GET/POST/… |
| `oper_url` | `varchar(255)` | |
| `oper_param` | `varchar(2000)` | 入参，脱敏 + 截断 |
| `json_result` | `varchar(2000)` | 响应摘要，脱敏 + 截断 |
| `status` | `char(1)` | 0 成功 1 失败 |
| `error_msg` | `varchar(2000)` | |
| `cost_time` | `int` | 毫秒 |
| `oper_name` | `varchar(64)` | 操作者名 |
| `oper_ip` | `varchar(128)` | |
| `oper_location` | `varchar(255)` | |
| `oper_time` | `timestamptz` | |
| **`actor_type`** | `varchar(12)` default `'human'` | `human` / `agent` / `mcp_client` / `system` |
| **`source`** | `varchar(10)` default `'http'` | `http` / `ai` / `mcp` / `task` |
| **`intent`** | `text` | 原始自然语言请求（AI / MCP 入口有值） |
| **`risk`** | `varchar(10)` | 该 Capability 的风险等级 |
| **`approval_mode`** | `varchar(20)` | `auto` / `confirmed` / `diff_confirmed` / `manual` |
| **`approved_by`** | `varchar(64)` | |
| **`plan_id`** | `varchar(36)` | 关联 Plan |
| **`changeset_id`** | `varchar(36)` | 关联变更集 |
| **`token_id`** | `int` | `source='mcp'` 时的令牌 |

索引：`(oper_time desc)`、`(actor_type, oper_time desc)`、`(changeset_id)`、`(plan_id)`、`(capability, oper_time desc)`。

### 9.7 登录日志：`sys_login_log`

`info_id bigserial PK`、`user_name varchar(50)`、`ipaddr varchar(128)`、`login_location varchar(255)`、`browser varchar(64)`、`os varchar(64)`、`status char(1)`（0 成功 1 失败）、`msg varchar(255)`、`login_time timestamptz`。索引 `(login_time desc)`、`(user_name, login_time desc)`、`(status, login_time desc)`。

### 9.8 Plan 与 Changeset

```sql
sys_ai_plan
  plan_id varchar(36) pk, user_id int, session_id varchar(36),
  intent text, steps jsonb, max_risk varchar(10),
  affected_count int, mode varchar(10), status varchar(16),
  page_context jsonb,                      -- 生成时的页面上下文，便于复现
  create_time timestamptz, expire_time timestamptz,
  index (user_id, create_time desc)

sys_changeset
  changeset_id varchar(36) pk,
  actor_type varchar(12), actor_id int, actor_name varchar(64),
  source varchar(10), intent text, plan_id varchar(36),
  status varchar(20),                      -- applied|partial|rolled_back|rollback_failed
  risk varchar(10), summary varchar(500), affected_count int,
  approval_mode varchar(20), approved_by varchar(64),
  create_time timestamptz,
  rolled_back_at timestamptz, rolled_back_by varchar(64),
  rollback_of varchar(36),                 -- 本 changeset 是对哪个 changeset 的回滚
  index (create_time desc), index (actor_type, create_time desc), index (plan_id)

sys_changeset_item
  item_id bigserial pk, changeset_id varchar(36),
  seq int, capability varchar(100),
  entity varchar(64), entity_id varchar(64), entity_label varchar(255),
  op varchar(10),                          -- create|update|delete|skip
  args jsonb, before jsonb, after jsonb, undo jsonb,
  status varchar(12), error_msg text, create_time timestamptz,
  index (changeset_id, seq)
```

`before`/`after` **只存变更涉及的字段**，不存整行，避免 jsonb 膨胀。

### 9.9 种子数据

- 超管：`admin` / `123456`（bcrypt），角色 `admin`（`role_key='admin'`），权限 `*:*:*`
- 菜单：
  - 系统管理（用户、角色、菜单、字典、参数、通知公告、文件管理、访问令牌、**模型服务**、**存储配置**）
  - 系统监控（在线用户、定时任务、任务日志、操作日志、登录日志、服务监控、缓存监控）
  - **变更与审计**（变更历史、行为溯源）
  - **AI**（AI 会话、能力浏览器）
- 字典：用户性别、菜单状态、系统开关、任务分组、任务状态、通知类型、通知状态、操作类型、系统状态、**调用来源**、**主体类型**、**风险等级**、**变更集状态**、**审批方式**、**模型厂商**、**存储驱动**、**URL 模式**
- 参数：`sys.account.captchaEnabled`、`sys.account.registerUser`、`sys.user.initPassword`、`sys.index.sideTheme`、`sys.changeset.retainDays`

### 9.10 超管保护

`role_key == 'admin'` 或权限含 `*:*:*` 视为超管，跳过权限校验、菜单全开。**service 层硬约束**（不是靠权限配置）：超管账号不可删除、不可停用、不可被非超管修改角色或密码。这同时写入知识库的 `Rules`，`Enforced: true`。

---

## 10. 统一约定

### 10.1 响应信封

```go
type Body struct {
    Code int    `json:"code"`
    Msg  string `json:"msg"`
    Data any    `json:"data,omitempty"`
}
type Page struct {
    Rows     any   `json:"rows"`
    Total    int64 `json:"total"`
    PageNum  int   `json:"pageNum"`
    PageSize int   `json:"pageSize"`
}
```

业务失败返回 HTTP 200 由 `code` 区分；401/403 用真实状态码便于前端跳转。错误码（`internal/pkg/errcode`）：`200` 成功、`400` 参数错误、`401` 未登录/过期、`403` 无权限、`404` 不存在、`409` 冲突、`412` **数据已变化（预览失效）**、`423` **风险等级超限**、`429` 限流、`500` 服务异常。

### 10.2 分页

`pageNum`（默认 1）、`pageSize`（默认 10，上限 100）、`orderByColumn` + `isAsc`（排序字段必须在该 Capability 自己的白名单内，防注入）。

### 10.3 中间件链

`RequestID → Recover → Logger → CORS → RateLimit → JWTAuth`，之后由 Capability 执行器负责权限、风险门槛、审计、changeset（**不再有独立的 Permission / OperLog 中间件**——挪进执行器，使 AI/MCP 入口共享）。

**JWTAuth**：解析 `Authorization: Bearer <token>`，载荷含 `uuid`；用 `uuid` 查 Redis `login_token:{uuid}` 取用户快照。Redis 无此 key = 已登出/被强退 → 401。构造 `Actor{Type:"human", Source:SourceHTTP, MaxRisk:RiskCritical}` 注入 context（人在页面上可以执行 critical 操作，这正是 critical 的定义）。白名单：登录、注册、验证码、静态资源、`/healthz`、`/mcp`。

---

## 11. 认证与会话

| 接口 | 说明 |
| --- | --- |
| `POST /api/login` | 账号密码 + 可选验证码。生成 `uuid` 写 Redis `login_token:{uuid}`（TTL = `jwt.expire`，默认 24h），签发 JWT；写登录日志 |
| `POST /api/logout` | 删 Redis token key |
| `POST /api/register` | 受 `sys.account.registerUser` 开关控制 |
| `GET /api/captchaImage` | 受 `sys.account.captchaEnabled` 控制，存 Redis `captcha:{uuid}` TTL 5min，一次性消费 |
| `GET /api/getInfo` | `{user, roles, permissions, aiMaxRisk}` |
| `GET /api/getRouters` | 当前用户可见菜单树 |
| `POST /api/refreshToken` | 剩余有效期 < 1/3 时刷新 Redis TTL（不换 token） |

密码 bcrypt。登录失败计数存 Redis，同账号连续失败 5 次锁定 10 分钟。在线用户 = `SCAN login_token:*`（**禁用 KEYS**），强退 = 删 key。

### 个人访问令牌（PAT）

| 接口 | 说明 |
| --- | --- |
| `GET /api/system/token/list` | 我的令牌（只返回 `prefix`） |
| `POST /api/system/token` | 创建；明文 `iadm_<32字节base62>` **只在创建响应返回一次**，库里只存 sha256 |
| `DELETE /api/system/token/:id` | 吊销 |

令牌权限 = 该用户有效权限集 ∩ `scopes`；令牌风险上限 = `min(用户 ai_max_risk, token.max_risk, mcp.max_risk)`。`last_used_at` 更新节流到 1 分钟一次。

---

## 12. 权限与风险模型

### 12.1 有效权限集（D7）

```text
effectivePerms(user) =
    { m.perms | m ∈ menus(roles(user)), m.perms != '', m.status == '0' }
  ∪ { p | p ∈ sys_user_perm[user.id] }
```

超管直接得 `{"*:*:*"}`。缓存 Redis `user_perms:{userId}`（TTL 30min），角色/菜单/用户授权变更时主动失效。

### 12.2 菜单树派生（D8）

菜单树**完全从 `effectivePerms` 派生**，让角色授权与用户额外授权自然合并：

- `menu_type='F'`（按钮）：不入路由，只贡献权限串
- `menu_type='C'`（菜单）：`perms == ''` 或 `perms ∈ effectivePerms` 时可见
- `menu_type='M'`（目录）：有可见后代时可见
- `status='1'` 不下发；`visible='1'` 侧边栏隐藏但路由可访问

> 约定：新建 `C` 菜单**必须**填 `perms`。空 `perms` 等价「登录即可见」，仅用于首页。

### 12.3 风险等级门槛（D5）

**权限回答"能不能做"，风险等级回答"AI 能不能代你做"。** 两者独立，同时满足才执行。

| Risk | 典型操作 | AI 行为 | 外部 MCP（默认） |
| --- | --- | --- | --- |
| `low` | 所有查询、刷新缓存、预览 | 直接执行，不打扰用户 | 可用 |
| `medium` | 改单条记录、启停单个用户/任务、立即执行一次任务 | 单次确认（展示 summary） | 拒绝 |
| `high` | 批量修改、删除、角色授权变更、回滚 changeset | **必须展示 diff 后确认** | 拒绝 |
| `critical` | 删角色、改超管、清空日志、修改自己的权限、吊销全部令牌 | **只能提案**，回复里给出「去页面执行」的链接，不提供执行按钮 | 拒绝 |

- 全局上限：`ai.max_risk`（默认 `high`）
- 用户级：`sys_user.ai_max_risk`，只能**下调**不能超过全局
- 实际上限 = `min(全局, 用户级)`；MCP 另取 `min(…, mcp.max_risk, token.max_risk)`
- 超限时执行器返回 `423 RiskExceeded`，错误体带 `{capability, risk, allowed, hint, route}`，AI 据此生成人类可操作的指引

**Capability 未声明 Risk** → 注册时强制视为 `critical` 并打 WARN。安全默认值，宁可难用不可危险。

### 12.4 用户额外授权界面

用户管理 → 「单独授权」：权限树（即菜单树，勾选 F/C 取其 `perms`）→ 写 `sys_user_perm`。界面提示「此处授权仅在角色权限之外叠加，不能收回角色已有权限」。同一抽屉内可设置该用户的 **AI 风险上限**。

---

## 13. 功能模块与 Capability 清单

HTTP 前缀 `/api`。`Risk` 列即 AI 确认策略。所有 `Mutating` 项自动获得 `{path}/preview`。

### 13.1 系统管理

| Capability | HTTP | 权限 | Risk | 可逆 |
| --- | --- | --- | --- | --- |
| `system.user.list` | `GET /system/user/list` | `system:user:list` | low | — |
| `system.user.get` | `GET /system/user/:userId` | `system:user:query` | low | — |
| `system.user.create` | `POST /system/user` | `system:user:add` | medium | ✓ |
| `system.user.update` | `PUT /system/user` | `system:user:edit` | medium | ✓ |
| `system.user.remove` | `DELETE /system/user/:userIds` | `system:user:remove` | high | ✓（软删除可恢复） |
| `system.user.changeStatus` | `PUT /system/user/changeStatus` | `system:user:edit` | medium | ✓ |
| `system.user.resetPwd` | `PUT /system/user/resetPwd` | `system:user:resetPwd` | high | ✗ |
| `system.user.authRole` | `PUT /system/user/authRole` | `system:user:edit` | high | ✓ |
| `system.user.authPerm` | `PUT /system/user/authPerm` | `system:user:authPerm` | **critical** | ✓ |
| `system.user.setAiRisk` | `PUT /system/user/aiRisk` | `system:user:authPerm` | **critical** | ✓ |
| `system.role.list/get/create/update` | `/system/role/...` | `system:role:*` | low/medium | ✓ |
| `system.role.remove` | `DELETE /system/role/:ids` | `system:role:remove` | **critical** | ✓ |
| `system.role.authMenu` | `PUT /system/role/authMenu` | `system:role:edit` | high | ✓ |
| `system.menu.*` | `/system/menu/...` | `system:menu:*` | low/medium；`remove` high | ✓ |
| `system.dictType.*` / `system.dictData.*` | `/system/dict/...` | `system:dict:*` | low/medium | ✓ |
| `system.dict.refreshCache` | `DELETE /system/dict/refreshCache` | `system:dict:edit` | low | ✗ |
| `system.config.*` | `/system/config/...` | `system:config:*` | low/medium | ✓ |
| `system.notice.*` | `/system/notice/...` | `system:notice:*` | low/medium | ✓ |
| `system.token.list/create/revoke` | `/system/token/...` | 登录即可（仅自己的） | low/medium | ✗ |
| `system.aiProvider.list` / `get` / `presets` | `/system/ai-provider/...` | `system:aiprovider:list/query` | low | — |
| `system.aiProvider.test` | `POST /system/ai-provider/test` | `system:aiprovider:edit` | low | — |
| `system.aiProvider.create` / `update` / `setDefault` / `changeStatus` | `/system/ai-provider/...` | `system:aiprovider:add/edit` | **high** | ✓ |
| `system.aiProvider.remove` | `DELETE /system/ai-provider/:id` | `system:aiprovider:remove` | **high** | ✓ |
| `system.storage.list` / `get` / `presets` | `/system/storage/...` | `system:storage:list/query` | low | — |
| `system.storage.test` | `POST /system/storage/test` | `system:storage:edit` | low | — |
| `system.storage.create` / `update` / `setDefault` / `changeStatus` | `/system/storage/...` | `system:storage:add/edit` | **high** | ✓ |
| `system.storage.remove` | `DELETE /system/storage/:id` | `system:storage:remove` | **high** | ✓ |

> **模型服务与存储配置的写操作一律 `AIEnabled=false` + `MCPEnabled=false`**（D21）。读操作可对 AI 开放（返回值已掩码），便于 AI 回答"当前用的是哪个模型"。启动时有自检强制这一点，违反直接 panic（见 4.10）。

个人中心（`profile` / `updatePwd`）：登录即可，**`AIEnabled=false`**（不让 AI 改当前登录者自己的密码）。头像与文件上传走手写 handler，不进注册表。

**导出**：`system.user.list`、`system.role.list`、`system.config.list`、`system.dictData.list` 声明 `Export`，自动获得 `/export` 与 `/template` 端点，权限分别为 `system:{user,role,config,dict}:export`（菜单里建对应 F 类型按钮）。

### 13.2 系统监控

| Capability | 权限 | Risk |
| --- | --- | --- |
| `monitor.online.list` | `monitor:online:list` | low |
| `monitor.online.forceLogout` | `monitor:online:forceLogout` | high（不可逆） |
| `monitor.operlog.list` | `monitor:operlog:list` | low |
| `monitor.operlog.remove` / `clean` | `monitor:operlog:remove` | high / **critical**（不可逆） |
| `monitor.loginlog.list` / `remove` / `clean` / `unlock` | `monitor:loginlog:*` | low / high / critical / medium |
| `monitor.server.info` | `monitor:server:list` | low |
| `monitor.cache.info/names/keys/value` | `monitor:cache:list` | low |
| `monitor.cache.clearName` / `clearKey` / `clearAll` | `monitor:cache:remove` | medium / medium / high（不可逆） |
| `monitor.job.list/get/create/update/changeStatus/run` | `monitor:job:*` | low/medium | 
| `monitor.job.remove` | `monitor:job:remove` | high | 
| `monitor.jobLog.list` / `remove` / `clean` | `monitor:job:*` | low / high / critical |

**导出**：`monitor.operlog.list`、`monitor.loginlog.list`、`monitor.jobLog.list` 声明 `Export`，权限 `monitor:{operlog,loginlog,job}:export`。日志类导出最容易触发大数据量，务必验证异步导出路径。

**缓存监控安全约束**：`names` 只返回白名单（`login_token` `captcha` `dict` `config` `user_perms` `ai:session` `ratelimit` `dashboard` `knowledge`）；`keys` 用 `SCAN MATCH <prefix>:*` 上限 500 条；禁止任意前缀查询与 `FLUSHDB`；`clearAll` 只清白名单前缀。

### 13.3 变更与审计（新模块）

| Capability | HTTP | 权限 | Risk |
| --- | --- | --- | --- |
| `system.changeset.list` | `GET /system/changeset/list` | `system:changeset:list` | low |
| `system.changeset.get` | `GET /system/changeset/:id` | `system:changeset:query` | low |
| `system.changeset.rollback` | `POST /system/changeset/:id/rollback` | `system:changeset:rollback` | high |
| `system.auditTrail.list` | `GET /system/audit/list` | `monitor:operlog:list` | low |

「行为溯源」页面即 `sys_oper_log` 的升级视图：可按主体类型、来源、风险等级、意图关键词、是否已回滚筛选，详情展示 intent → plan → changeset 的链条。

### 13.4 AI 与能力自省

| 接口 | 说明 | 权限 |
| --- | --- | --- |
| `GET /api/ai/capabilities` | 能力浏览器数据（名称/说明/风险/权限/可逆/入口开关/示例） | `ai:capability:list` |
| `GET /api/ai/knowledge` | 知识库快照（供能力浏览器展示） | `ai:capability:list` |
| `GET /api/ai/models` | 可用模型 + 默认模型 | `ai:agent:use` |
| `GET /api/ai/sessions` / `:id` / `DELETE :id` | 会话管理 | `ai:agent:use` |
| `POST /api/ai/chat/stream` | SSE 对话（携带 PageContext） | `ai:agent:use` |
| `GET /api/ai/plans/:id` | Plan 详情 | `ai:agent:use` |
| `POST /api/ai/plans/:id/apply` | 执行 Plan（`mode`、`stepSeq`） | `ai:agent:use` |
| `POST /api/ai/plans/:id/abort` | 放弃 Plan | `ai:agent:use` |
| `POST /api/ai/stop` | 中止当前生成 | `ai:agent:use` |

这些是手写 handler（SSE / 状态机），**不进 Capability 注册表**，因此 AI 无法调用自身。

### 13.5 首页大盘

`dashboard.overview` → `GET /dashboard/overview?days=7|30&refresh=false`，权限 `dashboard:view`，Risk `low`。

指标：用户总数与启停分布、角色数、在线人数、今日登录数、近 N 天登录趋势、**近 N 天按主体类型（人/AI/MCP）拆分的操作量与失败率**、**changeset 数量与回滚率**、定时任务成功失败、Asynq 队列积压（`asynq.Inspector`）。

缓存 Redis `dashboard:overview:{days}`，TTL 1h，按本地日期分桶；`refresh=true` 跳过读缓存并回写；Redis 失败降级直查库。启动预热 + 每小时刷新。

> 「AI 操作占比」和「回滚率」是这个系统特有的健康指标——回滚率持续偏高说明 AI 的 Plan 质量有问题或风险分级设得不合理。

---

## 14. 异步任务与定时任务（Asynq）

### 14.1 为什么只用 asynq

asynq 的 `Scheduler` / `PeriodicTaskManager` 内部已用 robfig/cron 解析 cronspec 并驱动触发，再自己搭一层 cron 调度器是重复建设。**调度与执行都交给 asynq**，只在「保存任务时校验表达式 + 展示下次执行时间」这一点上直接复用 asynq 已有的 robfig/cron 依赖做纯解析。

### 14.2 进程形态

- `intent-admin serve`：HTTP 服务，按配置可内嵌 worker（`task.embedded`）与 `PeriodicTaskManager`（`task.scheduler`）
- `intent-admin worker`：独立 worker，可多副本
- **`PeriodicTaskManager` 全局只能一个实例**。多副本时只给一个副本设 `task.scheduler: true`；另加保险：启动时抢 Redis 锁 `scheduler:leader`（`SET NX EX 30`，每 10s 续期），抢不到就不启动并打 WARN

### 14.3 处理器注册表（D17）

```go
type TaskHandler struct {
    Name        string // "system:cleanOperLog"
    Summary     string // 中文说明，后台下拉选择用
    ParamsProto any    // JSON 入参原型，前端据此渲染表单
    Fn          func(ctx context.Context, params json.RawMessage) error
}
func RegisterTask(h TaskHandler)
func TaskHandlers() []TaskHandler
```

`sys_job.invoke_target` 存处理器名，**必须命中注册表**；`job_params` 存 JSON 入参。保存时校验名称存在且入参可反序列化。前端「调用目标」是下拉框而非自由文本。

内置：`system:cleanOperLog`、`system:cleanLoginLog`、`system:cleanJobLog`、`system:cleanChangeset`（按 `sys.changeset.retainDays`）、`system:refreshDashboard`、`system:refreshKnowledge`、`system:exportAsync`（大数据量后台导出，见 4.9）、`system:cleanExportFiles`、`demo:noParams`、`demo:withParams`。

`system:exportAsync` 仅供已认证的导出适配器内部投递，不出现在用户可选的定时任务处理器中。导出用户 ID 必须取自当前 Actor，禁止通过任务参数指定其他用户以借用其权限；执行前及文件交付前都重新校验该用户的当前有效权限。

任务执行时 `Actor{Type:"system", Source:SourceTask, MaxRisk:RiskHigh}`，变更同样进 changeset（这样定时任务误删也能回滚）。

### 14.4 cronspec 格式约定

asynq 用 **robfig/cron 标准 5 字段**：`分 时 日 月 周`（`0 2 * * *` = 每天 02:00），外加 `@every <duration>` `@daily` `@hourly` 等描述符。

**必须注意**：这与 Quartz 风格的 6–7 字段表达式（含秒、含年）**不兼容**，`?` `L` `W` `#` 等 Quartz 扩展符也不支持。很多 Java 系后台用的是 Quartz 格式，前端 cron 生成器组件**不要照搬那一套**，必须按 5 字段 + 描述符实现。最小间隔 1 分钟；需要秒级用 `@every 30s`。

**保存时校验 + 下次执行时间**：provider 要到下次 sync 才发现表达式非法，那时用户已离开页面。所以在 Capability 里直接用 `cron.ParseStandard`：

```go
sched, err := cron.ParseStandard(in.CronExpression)
if err != nil { return errcode.BadRequestf("cron 表达式无效：%v", err) }
next := sched.Next(time.Now())   // 列表页展示「下次执行时间」
```

只用 parser，不创建 `cron.Cron` 实例、不自己调度。robfig/cron 本就是 asynq 的依赖。

### 14.5 动态定时任务

```go
func (p *DBConfigProvider) GetConfigs() ([]*asynq.PeriodicTaskConfig, error) {
    jobs, err := p.repo.ListEnabled(context.Background()) // status='0' AND del_flag='0'
    if err != nil { return nil, err }                      // 返回错误时 manager 保留上次配置
    out := make([]*asynq.PeriodicTaskConfig, 0, len(jobs))
    for _, j := range jobs {
        out = append(out, &asynq.PeriodicTaskConfig{
            Cronspec: j.CronExpression,
            Task:     asynq.NewTask(taskTypeFor(j), payloadFor(j)),
            Opts:     optsFor(j),
        })
    }
    return out, nil
}
mgr, _ := asynq.NewPeriodicTaskManager(asynq.PeriodicTaskManagerOpts{
    RedisConnOpt: redisOpt, PeriodicTaskConfigProvider: provider,
    SyncInterval: 15 * time.Second,
})
```

后台启停/改表达式 **15 秒内生效**，无需重启。provider 报错时保留上次成功配置（数据库短暂不可用不会让所有任务停摆），但要记日志。

`taskTypeFor(job)` 形如 `job:{invoke_target}`，payload 带 `jobID` + `job_params`，便于 worker 写 `sys_job_log` 时回溯。

### 14.6 防并发与投递选项

- `concurrent == '1'` → `asynq.Unique(ttl)`，`ttl` 取 cron 间隔（`sched.Next` 连取两次相减，上限 24h）
- `misfire_policy` → `asynq.MaxRetry(n)` + `asynq.Timeout(d)`
- `job_group` → `asynq.Queue(...)`（`DEFAULT` → `default`）
- `monitor.job.run` → 直接 `client.Enqueue`，不走 provider、不加 `Unique`

### 14.7 执行与日志

worker 侧统一 `asynq.MiddlewareFunc`：记录耗时、`recover` panic、写 `sys_job_log`。`asynq.Config.IsFailure` 区分「业务性跳过」与「真失败」，避免白耗重试次数。

### 14.8 队列

`critical`(6) / `default`(3) / `low`(1)，`asynq.Config{Concurrency: 10, Queues: {...}, IsFailure: ...}`。

---

## 15. AI Agent（Eino ADK）

### 15.1 工具集

Agent 有 5 个工具（比上一版多两个，对应新增的自省与计划能力）：

| 工具 | 入参 | 行为 |
| --- | --- | --- |
| `list_capabilities` | `{keyword?}` | `capability.Search(actor.Perms, SourceAI, actor.MaxRisk, kw, 30)` → `[{name, summary, risk, mutating}]` |
| `describe_capability` | `{name}` | 入参 JSON Schema、是否可逆、风险等级、**Examples** |
| `get_knowledge` | `{topic}` | 按需取知识：表名 / 字典类型 / `"rules"` / `"overview"` |
| `query` | `{name, args}` | **只读专用**：仅允许 `Risk == low && !Mutating` 的 Capability，直接执行返回结果。查询不需要走 Plan，避免每次问个数都要确认 |
| `propose_plan` | `{intent, steps[]}` | **所有写操作的唯一出口**：提交计划 → 系统预演 → 推送给用户确认。Agent 不能直接执行写操作 |

**关键设计**：Agent 无法直接执行任何写操作。它只能 `propose_plan`，由 `internal/plan` 预演并交给用户。这把「AI 会不会乱改数据」从一个需要信任模型的问题，变成一个由代码结构保证的问题。

### 15.2 意图路由

`propose_plan` 返回后，后端判定输出形态（见 7.1）：

按以下顺序判定，第一个命中即生效：

1. **含写步骤** → 输出 `plan`（预览卡片）。其中 `critical` 步骤标记为不可执行，卡片上显示「需在页面手动执行」+ 路由链接
2. **单步只读且是「列表类」查询** → 输出 `navigate`：跳到该 Capability 对应的页面并预填筛选条件
3. **其余全只读** → 直接执行并输出 `answer` + 证据链接

「列表类」的判定：该 Capability 的出参是行集合（`Export.RowsField` 或输出 schema 顶层有数组字段）**且**有对应的前端路由（由 `Evidence.Route` 提供）。反之，**聚合/计数类**查询（返回单值或少量统计，如"有几个停用用户"）走第 3 条直接给答案。

> 这条规则的依据：跳页面的价值在于"人能继续排序、翻页、导出、核对"。只要一个数字时跳页面是干扰而不是帮助。

### 15.3 确认机制

**用显式 Plan 状态机，不用 ADK 的中断/恢复。**

上一版设计用 `tool.StatefulInterrupt` + `CheckPointStore` 做 HITL。但现在写操作统一走 Plan，而 Plan 需要持久化（要能在页面上重新打开、要进 changeset、要支持逐步执行），用 gob 序列化的 agent 运行状态做载体既不好查也不好展示。所以：

- Agent 调 `propose_plan` → 落库 `sys_ai_plan`（status `draft`）→ 预演 → `previewed` → **工具返回「计划 #<id> 已提交用户确认」并终止本轮生成**
- 用户确认走独立接口 `POST /api/ai/plans/:id/apply`，与 agent 运行解耦
- 执行结果作为新一轮对话的输入回给 agent（「计划 #x 已执行，4 条成功」），由它继续或收尾

好处：Plan 可以关掉页面明天再确认；可以由另一个有权限的人确认；执行过程不依赖 agent 进程存活。ADK 的 `CheckPointStore` 仍保留用于会话内的长工具链恢复，但不承担确认语义。

### 15.4 模型接入

```go
cm, _ := openai.NewChatModel(ctx, &openai.ChatModelConfig{
    APIKey: cfg.AI.APIKey, Model: cfg.AI.Model, BaseURL: cfg.AI.BaseURL,
})
agent, _ := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    Name: "admin-assistant", Description: "后台管理助手",
    Instruction: systemPrompt, Model: cm,
    ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools}},
})
runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true, CheckPointStore: redisStore})
```

模型必须支持 function calling。`GET /api/ai/models` 透传上游 `GET {BaseURL}/models`（10s 超时，失败降级为仅返回配置的默认模型 + warning）。

**System Prompt 结构**（中文，分段清晰）：
1. 角色与边界：你是本后台的管理助手，只能通过 5 个工具操作系统
2. 硬规则：查询用 `query`；任何写操作必须 `propose_plan`，不得声称已执行；不确定字段含义时先 `get_knowledge`；不要臆造 capability 名；不要在回复里泄露密码哈希或 token
3. 常驻知识（约 500 token，见 6.2）
4. 当前页面上下文（见 7.2）
5. 回复风格：查询结果用简洁表格；给出结论时附证据；计划提交后明确告知用户去确认

### 15.5 会话存储

| Key | 内容 | TTL |
| --- | --- | --- |
| `ai:session:{userId}:{sessionId}` | 标题、模型、更新时间、展示消息、模型消息历史 | 7 天 |
| `ai:session:index:{userId}` | zset，score = updatedAt | 7 天 |
| `ai:checkpoint:{sessionId}` | ADK `CheckPointStore` 快照 | 7 天 |
| `ai:running:{userId}:{sessionId}` | `SET NX EX 600` 运行锁 | 10 分钟 |

Plan 与 changeset 在 PostgreSQL，**不随会话过期**——这是审计数据。会话过期后仍能在「变更历史」里查到当时做了什么、因为什么意图。

### 15.6 SSE 协议

响应头：`Content-Type: text/event-stream; charset=utf-8`、`Cache-Control: no-cache, no-transform`、`Connection: keep-alive`、`X-Accel-Buffering: no`。每 15s 发 `: ping`。事件 `data: {json}\n\n`。

| type | 载荷 |
| --- | --- |
| `start` | `{sessionId}` |
| `delta` | `{text}` 增量回复 |
| `tool_call` | `{name, summary}` |
| `tool_result` | `{name, ok, brief}` |
| `answer` | `{text, evidence[]}` |
| `navigate` | `{route, filters, note}` |
| `plan` | 完整 `Plan` 对象（含每步 Preview） |
| `step_result` | `{planId, seq, status, error?}`（逐步执行时） |
| `applied` | `{planId, changesetId, summary, reversible}` |
| `done` | `{session}` |
| `error` | `{message, code}` |

实现：从 `runner.Query/Run` 的 `iter` 逐个 `Next()` 取 `AgentEvent`，映射后写 `http.ResponseWriter` 并 `Flush()`。客户端断开触发 `ctx.Done()` 取消底层调用。

### 15.7 限制与安全

- 单轮最大工具调用 25 次
- `query` 结果序列化后截断到 16 KB 再给模型
- `propose_plan` 的 steps 上限 20 步
- Plan 影响条数超过 `ai.max_affected`（默认 200）→ 强制 `Risk` 升为 `critical`，只能人工执行
- 限流：每用户每分钟 20 次 chat
- 入口权限 `ai:agent:use` 默认只给 admin 角色
- 页面显著提示：「会话内容与查询到的业务数据会发送给所配置的模型服务商」
- 所有 AI 发起的操作 `actor_type='agent'`、`source='ai'`，带 `intent` 原话

---

## 16. MCP Server（D12）

让 Claude Code、Claude Desktop 等客户端直接操作本后台。与站内 Agent **共用 Capability 注册表与执行器**，不另维护工具定义。

### 16.1 传输

| 传输 | 入口 | 用途 |
| --- | --- | --- |
| Streamable HTTP（主） | `POST\|GET /mcp` | 远程接入。`mcp.NewStreamableHTTPHandler(getServer, nil)` 用 `gin.WrapH` 挂到 gin，走独立 PAT 中间件 |
| stdio（可选） | `intent-admin mcp` | 本地客户端 / 调试。`server.Run(ctx, &mcp.StdioTransport{})` |

### 16.2 鉴权

MCP 规范对 HTTP 传输推荐 OAuth 2.1 + 受保护资源元数据，完整实现成本高。**一期用 PAT**：

```text
Authorization: Bearer iadm_<32字节base62>
```

中间件：`sha256(token)` 查 `sys_access_token`（校验 status/del_flag/expires_at）→ 取 user_id → 算有效权限集 ∩ scopes → 构造 `Actor{Type:"mcp_client", Source:SourceMCP, MaxRisk: min(mcp.max_risk, token.max_risk, user.ai_max_risk)}`。失败返回 401 + `WWW-Authenticate: Bearer`。

> 二期若需兼容强制走 OAuth 发现的客户端，再补 `/.well-known/oauth-protected-resource`。文档要明确当前是 PAT。

### 16.3 工具暴露

配置 `mcp.tool_mode`：

| 模式 | 行为 | 适用 |
| --- | --- | --- |
| `meta`（默认） | 5 个元工具，与站内 Agent 一致 | Capability 数量多（约 80–100），避免挤占客户端 context |
| `flat` | 每个可见 Capability 用 `mcp.AddTool` 注册为独立工具，带完整 schema 与 annotations | 配合 `mcp.tool_tags` 白名单裁剪到少量功能时体验更好 |

`flat` 注册示例：

```go
mcp.AddTool(srv, &mcp.Tool{
    Name:        strings.ReplaceAll(e.Name, ".", "_"),
    Description: e.Summary,
    InputSchema: e.InputSchema,
    Annotations: &mcp.ToolAnnotations{
        Title:           e.Summary,
        ReadOnlyHint:    !e.Mutating,
        DestructiveHint: pointer.Of(e.Risk == capability.RiskHigh || e.Risk == capability.RiskCritical),
        IdempotentHint:  e.Idempotent,
        OpenWorldHint:   pointer.Of(false), // 闭域：只操作本系统
    },
}, handlerFor(e))
```

### 16.4 写操作的两步确认

MCP 客户端自己会做工具审批，但不能只依赖它。服务端强制两步：

1. 客户端调写 Capability → **不执行**，返回 dry-run `Preview` + 一个 `confirmToken`（Redis 存 5 分钟，绑定 capability + args 哈希 + actor）
2. 客户端需再调 `confirm_plan{confirmToken}` 才真正执行；args 哈希不匹配则拒绝

这样即使客户端自动批准了第一次调用，模型也必须「看到」影响面才能继续，且参数不能在两步之间被改。

三道闸共同生效：`mcp.max_risk`（默认 `low`，即只读）、token 的 `scopes` 与 `max_risk`、两步确认。

### 16.5 可见性与审计

- 只暴露 `MCPEnabled == true` 的 Capability。默认 `MCPEnabled = AIEnabled`，但以下强制 false：个人中心改密、访问令牌管理（防止用受限令牌造全权令牌）、AI 与 Plan 相关接口、`system.user.authPerm`、`system.user.setAiRisk`
- 所有 MCP 调用写 `sys_oper_log`（`actor_type='mcp_client'`、`source='mcp'`、`token_id`）与 changeset
- 限流：按 `token_id` 每分钟 60 次

### 16.6 Resources（二期可选）

把知识库暴露为 MCP resources（`knowledge://overview`、`dict://{type}`、`entity://{table}`），便于客户端缓存上下文、减少工具调用。一期不做。

### 16.7 客户端配置（写进 README）

```json
{
  "mcpServers": {
    "intent-admin": {
      "type": "http",
      "url": "https://admin.example.com/mcp",
      "headers": { "Authorization": "Bearer iadm_xxxxxxxx" }
    }
  }
}
```

---

## 17. 前端架构

### 17.1 目录

```text
web/src/
├── api/                  # 按模块的 axios 封装
├── components/
│   ├── diff/             # ★ DiffTable、DiffFields、PreviewModal（页面与 AI 共用）
│   ├── plan/             # ★ PlanCard、PlanStep、RiskBadge
│   ├── omnibox/          # ★ 全局自然语言输入框（Cmd+K）
│   ├── config/           # ★ SecretInput（掩码显示/留空保留）、ConnTestResult、PresetPicker
│   ├── ai/               # AI 面板、消息流、证据链接
│   └── ...               # Auth、DictTag、DictSelect、PageTable、SearchForm、MenuTree、FileUpload、Markdown
├── hooks/                # useDict、usePerm、useTable、useSSE、usePageContext、usePreviewAction
├── layouts/              # BasicLayout（Sider + Header(含 Omnibox) + Tabs + Content + AI 侧面板）
├── pages/                # 与 sys_menu.component 一一对应
│   ├── login/index.tsx
│   ├── index.tsx                                  # 大盘
│   ├── system/{user,role,menu,dict,config,notice,upload,token}/index.tsx
│   ├── system/{ai-provider,storage}/index.tsx     # ★ 模型服务、存储配置
│   ├── system/user/profile/index.tsx
│   ├── monitor/{online,operlog,loginlog,server,cache,job}/index.tsx
│   ├── monitor/job/log.tsx
│   ├── audit/{changeset,trail}/index.tsx          # ★ 变更历史、行为溯源
│   ├── ai/{agent,capabilities}/index.tsx          # ★ AI 会话、能力浏览器
│   └── error/{403,404}.tsx
├── router/{index.tsx,registry.ts}
├── stores/               # useUserStore、useAppStore、useDictStore、usePageContextStore、useAiStore
├── styles/global.css
└── utils/                # request.ts、auth.ts、sse.ts、format.ts、download.ts
```

页面数约 27 个，符合「双入口等权」。

### 17.2 组件注册表（D8）

```ts
const modules = import.meta.glob('../pages/**/*.tsx');
export function resolveComponent(component: string) {
  const loader = modules[`../pages/${component}.tsx`] ?? modules[`../pages/${component}/index.tsx`];
  if (!loader) return () => <NotFound />;   // 菜单配错不白屏
  return lazy(loader as never);
}
```

`component` 为 `Layout` → `BasicLayout`；`ParentView` → 透传；`InnerLink` → iframe。后端菜单树转 `RouteObject[]` 交给 react-router，外层 `<Suspense>` + 错误边界。

### 17.3 页面上下文上报

```ts
// hooks/usePageContext.ts —— 列表页统一调用，自动写入 store
usePageContext({
  capability: 'system.user.list',
  filters: searchForm.values,
  selectedIds: selectedRowKeys,
  selectedLabels: selectedRows.map(r => `${r.userName}（${r.nickName}）`),
  visibleIds: dataSource.map(r => r.userId),
});
```

Omnibox 与 AI 面板从 `usePageContextStore` 读取并随请求发送。列表页只需这一个 hook 调用即获得 AI 上下文感知能力。

### 17.4 预览-执行-撤销（页面侧）

```ts
// hooks/usePreviewAction.ts
const { run } = usePreviewAction({
  capability: 'system.user.remove',
  previewUrl: '/system/user/remove/preview',
  executeUrl: (ids) => `/system/user/${ids.join(',')}`,
  method: 'DELETE',
});
// run(ids) → 调 preview → 弹 <PreviewModal> → 确认 → 执行 → Toast「已删除 N 条 · 撤销」
```

`<PreviewModal>` 内部就是 `<DiffTable>`，和 AI 计划卡片用的是同一个组件。`Reversible === false` 时 Modal 标题区显示红色「此操作不可回滚」。

低风险单条操作（改备注、调排序）可传 `skipPreview: true` 直接执行，仍进 changeset 仍可撤销。

### 17.5 Omnibox

顶栏常驻输入框 + `Cmd/Ctrl+K`。提交后在右侧 AI 面板展开结果，三种形态：

- `answer` → Markdown + 证据链接（点击跳转目标页并预填筛选）
- `navigate` → 直接跳转并预填，面板里留一行说明
- `plan` → `<PlanCard>`：步骤列表 + 每步 diff 折叠 + 影响面汇总 + 风险徽标 + 「全部执行 / 逐步确认 / 取消」；含 `critical` 步骤时该步显示「需在页面手动执行」+ 跳转链接

### 17.6 变更历史页

列表：时间、主体（人/AI/MCP + 名称）、意图、影响条数、风险、状态、能否回滚。
详情：Plan 步骤 + 每条 item 的 `<DiffFields>`（before → after）、回滚按钮、若是回滚产生的则显示 `rollback_of` 链接。

**这是这个项目最能体现差异化的页面**，UI 要做细：diff 要清晰、字典值要显示可读文本、回滚要二次确认并展示将恢复什么。

### 17.7 请求层

- `baseURL` 来自 `VITE_API_BASE`（开发 `/api` 由 Vite 代理到 `http://localhost:8080`，生产同源）
- 请求拦截注入 `Authorization`；GET 加时间戳防缓存
- 响应拦截：`code !== 200` → `message.error`；401 → 清 token 跳登录；403 → 403 页；**412 → 提示「数据已变化，请重新预览」并自动重新拉 preview**；**423 → 提示风险超限并给出「去页面执行」链接**
- 重复请求节流（相同 method+url+params 在 500ms 内只发一次）
- **SSE 必须用 `fetch` + `ReadableStream` 手动解析**，不能用 `EventSource`（需要 POST + `Authorization` 头，两者都不支持）

### 17.8 字典

`useDict('sys_user_sex')` 拉 `GET /system/dict/data/type/:dictType` 缓存到 `useDictStore`（会话内只拉一次）。`<DictTag>` / `<DictSelect>` 按 `listClass` 渲染颜色。**`<DiffFields>` 也用它把 `status: 0→1` 渲染成「正常 → 停用」**。

---

## 18. 前端嵌入与静态服务

```go
//go:embed all:dist
var dist embed.FS
func Handler() http.Handler  // SPA：命中文件返回文件，否则 index.html
```

- 挂载顺序：`/api/**`、`/mcp`、`/healthz` 优先，其余交给 SPA handler
- `index.html` → `Cache-Control: no-cache`；`assets/*`（内容 hash）→ `public, max-age=31536000, immutable`
- 支持 gzip/br 预压缩文件（存在 `.gz`/`.br` 且客户端接受时直接返回）
- 未命中的 `/api/**` 返回 JSON 404，不落到 index.html

---

## 19. 配置

`configs/config.example.yaml`，支持 `INTENT_ADMIN_` 前缀环境变量覆盖。

```yaml
app:
  name: intent-admin
  env: dev            # dev | prod
  port: 8080
  base_path: /api
  encrypt_key: ""     # 32 字节（base64 或 hex），必填。用于加密模型 API Key 与存储 Secret
db:
  host: 127.0.0.1
  port: 5432
  user: postgres
  password: ""
  name: intent_admin
  max_open_conns: 50
  max_idle_conns: 10
  log_level: warn
redis:
  addr: 127.0.0.1:6379
  password: ""
  db: 0
  key_prefix: "intent-admin:"
jwt:
  secret: ""          # 必填，为空拒绝启动
  expire: 24h
  issuer: intent-admin
task:
  embedded: true
  scheduler: true     # 多副本只允许一个副本为 true（另有 Redis leader 锁兜底）
  concurrency: 10
  sync_interval: 15s
changeset:
  retain_days: 90
  max_items_per_set: 2000   # 超出则拒绝执行，提示拆分操作
export:
  max_rows: 50000           # 同步导出上限，超出转后台异步导出
  stream_threshold: 5000    # 超过此行数改用 excelize StreamWriter
  async_retain_days: 7      # 异步导出文件保留天数
# 存储与模型的实际配置在「系统管理 → 存储配置 / 模型服务」页面里维护（D19/D20），
# 下面两段只用于首次部署引导：库里没有配置时据此自动创建一条，之后以库为准。
storage:
  bootstrap:
    driver: local       # local | s3
    local_dir: ./uploads
    base_url: http://localhost:8080/uploads
    # driver: s3 时用下面这些（docker-compose 里默认指向内置 MinIO）
    endpoint: ""
    region: ""
    bucket: ""
    access_key_id: ""
    secret_key: ""
    path_style: true    # MinIO 必须 true
ai:
  bootstrap:
    vendor: ""            # deepseek | ark | dashscope | moonshot | zhipu | siliconflow | openai | custom
    api_key: ""
    base_url: ""          # vendor 为预设时留空，自动取预设值
    model: ""
  max_risk: high          # AI 可执行的风险上限：low|medium|high|critical
  max_affected: 200       # 单个 Plan 影响条数上限，超出强制人工执行
  max_tool_calls: 25
  max_plan_steps: 20
  plan_ttl: 30m
  session_ttl: 168h
mcp:
  enabled: true
  path: /mcp
  tool_mode: meta         # meta | flat
  tool_tags: []           # 为空 = 不按 tag 裁剪
  max_risk: low           # ★ 默认只读。调高前请阅读 README 安全说明
  confirm_ttl: 5m
log:
  level: info
  format: json
```

**启动期强校验**：`jwt.secret` 或 `app.encrypt_key` 为空 → 拒绝启动；`app.encrypt_key` 长度不足 32 字节 → 拒绝启动；`app.env == prod` 且二者为示例值 → 拒绝启动；`ai.max_risk == critical` 或 `mcp.max_risk != low` → 启动日志打醒目 WARN；数据库/Redis 连不通 → 拒绝启动；**配置类 Capability 自检失败（带了 `AIEnabled`/`MCPEnabled`）→ panic**（见 4.10）。密钥不进仓库（`.gitignore` 忽略 `configs/config.yaml`、`*.local.yaml`、`.env`）。

---

## 20. 构建与部署

### Makefile

```text
make help
make dev           # air 热重载后端（serve）+ vite dev（代理 /api → :8080）
make dev-backend / dev-web
make migrate-up / migrate-down / migrate-status / seed
make build-web     # cd web && pnpm build && 同步 dist → internal/web/dist
make build         # build-web + go build -trimpath -ldflags "-s -w -X ...Version/Commit/BuildTime" -o bin/intent-admin .
make test / lint / docker
make release       # goreleaser
```

### CLI（D15）

```text
intent-admin serve   [--config f] [--port 8080]
intent-admin worker  [--config f] [--concurrency 10]
intent-admin mcp     [--config f]
intent-admin migrate up|down|status|version|seed|rekey
intent-admin version
```

### Dockerfile（多阶段）

1. `node:22-alpine` 构建前端
2. `golang:1.25-alpine` 复制 dist → `internal/web/dist` 后 `go build`（`CGO_ENABLED=0`）
3. `gcr.io/distroless/static`，只放二进制 + 时区数据，非 root，`EXPOSE 8080`

### docker-compose.yml

`postgres` + `redis` + `app`，本地一键起。生产按需拆 worker 副本（只一个开 `task.scheduler`）。

### Nginx 要点

```nginx
location /api/ai/chat/stream { proxy_buffering off; proxy_read_timeout 600s; }
location /mcp               { proxy_buffering off; proxy_read_timeout 600s; }
```

### CI

PR：`lint` → `test`（起 PG/Redis service 容器）→ `build`。打 tag：goreleaser 多平台二进制 + GHCR 多架构镜像。**不做自动上线**。

---

## 21. 开源工程规范（D18）

### 21.0 命名规范（已定，不要改）

| 项 | 取值 |
| --- | --- |
| 仓库 / 项目名 | `intent-admin` |
| 品牌名（README 标题、界面 logo、侧边栏、文档正文称呼） | **Intent** |
| Go module path | `github.com/junler/intent-admin` |
| 二进制名 | `intent-admin`（子命令 `serve`/`worker`/`mcp`/`migrate`/`version`） |
| 环境变量前缀 | `INTENT_ADMIN_` |
| 默认数据库名 | `intent_admin` |
| Redis key 前缀 | `intent-admin:` |
| PAT 明文前缀 | `iadm_` |
| 容器镜像 | `ghcr.io/junler/intent-admin` |

命名取"意图驱动"之意，对应核心执行链路「意图 → 计划 → 预览 → 执行 → 回滚」。界面与文案里用 **Intent** 作为产品名，`intent-admin` 只在仓库、module、二进制、镜像这类技术标识处出现。

### 21.1 仓库文件

| 文件 | 要求 |
| --- | --- |
| `LICENSE` | 推荐 **MIT**。待确认，见 25.9 |
| `README.md` | 中文主文档：定位、与传统后台的范式对比、特性、技术栈全表、截图/录屏、快速开始、配置、MCP 接入、安全说明、路线图 |
| `README.en.md` | 英文版，与中文同步 |
| `CONTRIBUTING.md` | 环境搭建、提交规范、代码风格、测试要求、PR 流程，**以及「新增一个 Capability」完整示例**（含 DryRun/Undo/Recorder 怎么写）|
| `CODE_OF_CONDUCT.md` | Contributor Covenant 2.1 |
| `SECURITY.md` | 漏洞私下报告渠道、支持版本、响应时限 |
| `CHANGELOG.md` | Keep a Changelog，由 release 生成 |
| `.github/ISSUE_TEMPLATE/` | `bug_report.yml`、`feature_request.yml`（表单式，强制填版本、复现步骤、环境） |
| `.github/PULL_REQUEST_TEMPLATE.md` | 变更说明、关联 issue、测试方式、是否破坏兼容 |
| `.github/workflows/` | `ci.yml`、`release.yml` |
| `.github/dependabot.yml` | gomod + npm + actions 周更 |
| `.golangci.yml` | `govet` `staticcheck` `errcheck` `revive` `gosec` `ineffassign` `misspell` |
| `.editorconfig` / `.gitattributes` | 统一换行与缩进 |

### 21.2 提交与版本

- **Conventional Commits**，CI 用 commitlint 校验 PR 标题
- 语义化版本。`v0.x` 允许 breaking change，但 CHANGELOG 必须有 `BREAKING CHANGE` 段落写明迁移方式
- `main` 保持可发布，功能走 `feat/xxx` + PR，禁止直推
- Tag 触发 release：linux/darwin/windows × amd64/arm64 二进制 + `checksums.txt` + GHCR 镜像

### 21.3 开源友好的代码要求

1. **零私有业务痕迹** —— 不留私有域名、内部表名、个人账号、特定公司的业务模块。本项目只包含通用后台能力，任何领域业务（电商、教育、CRM…）都不进主仓库
2. **开箱可跑** —— `docker compose up` 后能直接登录，种子数据自带完整菜单与示例数据
3. **配置全可覆盖** —— 任何配置项都有环境变量形式
4. **新增功能门槛低** —— `CONTRIBUTING.md` 的「新增一个 Capability」一节是本项目架构的卖点，要写得能照着做
5. **注释与文案语言** —— 代码注释英文（面向国际贡献者），界面与错误文案中文。待确认，见 25.6
6. **测试可在 CI 跑通** —— `testcontainers-go` 在 Actions 上可用；另提供 `-short` 跳过集成测试的模式

### 21.4 文档站（二期可选）

`docs/` 下用 VitePress 搭静态站，GitHub Pages 发布。一期先把 README 与 `docs/` 的 Markdown 写全。

---

## 22. 测试策略

### 22.1 必须覆盖（按重要性排序）

1. **Capability 执行器** —— 权限拒绝；入口可见性（`AIEnabled`/`MCPEnabled` 为 false 时 AI/MCP 调用返回 404 而非 403）；**风险门槛拒绝并返回 423**；未声明 Risk 被当作 critical；未知字段拒绝；validator 生效；panic 被捕获；审计含正确 actor_type/source/intent；changeset 与业务在同一事务（业务失败则 changeset 不落库）；出参脱敏
2. **DryRun 只读保证** —— 在 `DryRun` 中尝试写数据**必须被拦截并报错**，这是防止实现者犯错的护栏，必须有专门测试
3. **Preview → Apply 的 TOCTOU 防护** —— 预览后数据被改动，Apply 必须返回 412 而非照旧执行
4. **回滚** —— update/create/soft-delete 三类 undo 正确；倒序执行；部分失败时状态为 `rollback_failed` 且失败项可见；回滚产生新 changeset；回滚的回滚能恢复
5. **Plan 引用求值** —— `$steps[0].result.rows[*].userId`、`$ctx.selectedIds` 正确求值；只读步骤在预演时真实执行、写步骤不执行；引用越界/类型不符时报清晰错误
6. **AI 不能直接写** —— 构造一个让 agent 调写 Capability 的场景，验证只有 `propose_plan` 这一条路；`query` 工具传入写 Capability 被拒绝
7. **权限与菜单** —— 有效权限集合并（角色 ∪ 用户额外授权）；超管直通；停用角色/菜单不贡献权限；菜单树派生（目录无可见子节点不下发、`visible='1'` 隐藏但可访问、空 perms 的 C 菜单）；缓存失效时机
8. **页面上下文不可信** —— 前端传入他人 ID 时执行仍被权限拦截
9. **MCP** —— `max_risk=low` 时写 Capability 不出现在 tools/list 且直接调用被拒；两步确认的 `confirmToken` 绑定参数哈希（改参数后拒绝）、过期后拒绝；令牌管理类 Capability 不可通过 MCP 调用；annotations 映射正确
10. **PAT** —— hash 校验；过期/吊销后拒绝；scopes 与 max_risk 收窄生效；明文不可二次读取
11. **认证** —— token 过期；Redis key 被删（强退）；登录失败锁定与解锁；验证码一次性
12. **定时任务** —— `invoke_target` 不在白名单时拒绝保存；非法 cronspec（含 6 字段 Quartz 风格表达式）保存时即被拒；「下次执行时间」正确；provider 过滤停用任务；provider 报错时保留上次配置；禁止并发生效；任务变更进 changeset
13. **缓存监控** —— 只允许白名单前缀；SCAN 上限；`clearAll` 不触碰白名单外 key
14. **知识库** —— Enums 随 `sys_dict_data` 变更刷新；`get_knowledge` 未知 topic 返回明确错误而非空
15. **凭证加密与掩码** —— `Encrypt`/`Decrypt` 往返正确；每次加密 nonce 不同（同一明文两次密文不等）；列表/详情接口**永不返回明文**，只返回掩码；更新时该字段留空保留原值、填了才替换；`_enc` 字段不出现在操作日志入参、changeset 快照、Excel 导出里；`app.encrypt_key` 错误时解密失败并给出可读错误而非 panic；`migrate rekey` 能把两张表的密文整体换钥
16. **配置类 Capability 隔离** —— 启动自检能抓出误标 `AIEnabled`/`MCPEnabled` 的配置类 Capability 并 panic；AI 入口调 `system.aiProvider.update` 返回 404；MCP 的 tools/list 不含这些写操作
17. **连通性测试** —— 模型测试能正确识别"Key 无效"、"不支持 tool calling"、"网络不通"三种失败并分别报告；`supports_tool_calling=false` 的 provider 不能被设为默认；存储测试能区分"桶不可访问"、"能写不能读"、"公网 URL 访问不到"
18. **默认配置唯一性** —— 并发把两条记录设为默认时，部分唯一索引保证只有一条成功，另一条返回可读冲突错误
19. **存储驱动** —— `local` 与 `s3` 行为一致（上传、生成 URL、删除）；MinIO 场景 `path_style=true` 生效；`url_mode=presigned` 时 URL 带签名且过期后失效；`sys_upload.storage_id` 让换存储后老文件仍可访问
20. **Excel 导出** —— 导出内容与同样筛选条件的列表结果逐行一致；分页参数被忽略；字典列翻译成可读文本；无导出权限时 403；中文文件名在 `Content-Disposition` 中正确编码；超过 `stream_threshold` 走 StreamWriter 且内存不随行数线性增长；超过 `max_rows` 转异步且返回明确提示；**AI/MCP 入口调用 `/export` 被拒**

### 22.2 手段

- `capability` / `plan` / `changeset` / `service` 层：`testcontainers-go` 起真实 PostgreSQL + Redis（本项目 SQL 与事务语义不简单，sqlmock 不够）
- HTTP 层：`httptest`
- MCP 层：官方 SDK 的 `mcp.NewInMemoryTransports` 做端到端
- Agent 层：用一个 stub ChatModel（固定返回预设 tool call 序列）测意图路由与 Plan 生成，不依赖真实模型
- 前端：`vitest` 覆盖 request 拦截器（412/423 分支）、SSE 解析、权限 hook、菜单树转换、diff 渲染、PageContext 收集

---

## 23. 实施阶段

每阶段结束须可运行、可验证。

### Phase 0 — 脚手架与 Capability 层（最关键）

- Go module、目录骨架、**cobra 命令骨架**、viper 配置、slog、优雅关闭
- GORM + Redis 连接、`GET /healthz`
- goose 迁移接入 `migrate` 子命令
- 响应信封、错误码（含 412/423）、分页、中间件链（除 JWT）
- **`internal/capability`：Capability 定义、Risk、注册表、执行器、Recorder、Preview 结构**
- **`internal/capability/dryrun` + `undo` 通用助手**
- **DryRun 只读护栏**（GORM callback 拦截写操作）
- **`internal/adapter/httpx`：泛型 gin 适配器 + 反射入参绑定 + 自动 `/preview` `/export` `/template` 端点 + OpenAPI 3.1 + Swagger UI**
- Excel 导出渲染器（excelize，含字典翻译、格式化、StreamWriter 分支）
- 用 2–3 个真实 Capability（`demo.item.list/update/remove`，配一张 demo 表）端到端验证
- `.golangci.yml`、`LICENSE`、`.editorconfig`、CI 骨架（开源卫生从第一天建好）
- 前端 Vite + React + antd v6 + Tailwind v4 脚手架（`@layer` + `StyleProvider layer`）、BasicLayout 空壳、request 拦截器
- **验收**：
  1. 注册一个 Capability 后无需写 handler 即出现 HTTP 接口、出现在 `/api/openapi.json`、`uri/form/json` 三类参数都正确绑定
  2. `Mutating` Capability 自动获得 `/preview` 端点并返回正确 diff
  3. 在 `DryRun` 里写数据会被拦截报错
  4. 声明 `Export` 的 Capability 自动获得 `/export`，导出结果的筛选条件与列表一致、字典列显示可读文本、中文文件名不乱码
  5. 前端能显示 antd + Tailwind 混用页面且样式不冲突

> 这是地基。若 Capability 层设计有问题，越往后改代价越大。**此阶段结束必须做一次设计复核再往下走。**

### Phase 1 — 认证、权限、风险门槛

- `000001_init.sql` 全表（含 changeset / plan / 审计升级字段）+ `000002_seed.sql` 种子
- 模型、用户/角色/菜单查询 service
- 登录/登出/注册/验证码/getInfo/getRouters/refreshToken（手写 handler）
- JWTAuth 中间件、Redis 在线 token、Actor 注入
- 执行器接入权限校验 + **风险门槛**
- 前端登录页、用户 store、动态路由与组件注册表、`Auth` 组件与 `usePerm`
- **验收**：admin 登录后侧边栏按菜单树渲染；无权限用户看不到菜单、直接调接口 403；把 `ai.max_risk` 设为 `low` 后用 AI actor 调 medium Capability 返回 423；删 Redis key 后请求立即 401

### Phase 2 — Changeset 与回滚（先于业务模块）

- `internal/changeset`：落库、查询、回滚引擎
- 执行器接入 changeset 写入（与业务同一事务）
- `system.changeset.list/get/rollback` Capability
- 前端 `<DiffTable>` / `<DiffFields>` / `<PreviewModal>`、`usePreviewAction` hook、变更历史页
- **验收**：用 Phase 0 的 demo Capability 做一次更新 → 变更历史出现记录 → 点回滚 → 数据恢复 → 产生一条回滚 changeset；不可逆 Capability 的回滚被标 skipped 并说明原因

> 把可逆性放在业务模块之前做，是为了让后续每个模块一写出来就自带预览与回滚，而不是最后回头改 30 个 Capability。

### Phase 3 — 系统管理

- 用户（含 authRole / authPerm / setAiRisk）、角色、菜单、字典、参数、通知公告的全部 Capability（含 DryRun / Undo / Recorder）
- 执行器接入操作日志（含 actor_type / intent / risk / approval_mode）
- 字典/参数 Redis 缓存与刷新
- 用户/角色/参数/字典数据的 `Export` 声明
- 前端 6 个页面 + `PageTable`（含导出按钮）/ `SearchForm` / `DictTag` / `MenuTree` / `useExport`
- 个人中心（资料、改密、头像）
- 访问令牌页（为 Phase 7 的 MCP 准备）
- **`internal/pkg/secret`：AES-256-GCM 加解密 + 掩码 + `migrate rekey` 子命令**
- **模型服务配置（D19）**：`sys_ai_provider` 表、厂商预设、CRUD、连通性测试（含 tool calling 探测）、设为默认、前端页面（`PresetPicker` + `SecretInput` + `ConnTestResult`）
- **存储配置（D20）**：`sys_storage_config` 表、预设、CRUD、连通性测试、前端页面
- **文件上传与文件管理页**：`local` + `s3` 两个驱动（minio-go）、`presigned` URL 模式、`sys_upload.storage_id`
- **配置类 Capability 自检**（启动时 panic 拦截，见 4.10）
- **验收**：
  1. 六模块增删改查全通，**每个写操作都有预览与撤销**；给普通用户单独授权后重新登录可见对应菜单与按钮；操作日志密码字段已脱敏
  2. 导出内容与当前页筛选结果一致；无导出权限时按钮不显示、直接调接口 403
  3. 在模型服务页选 DeepSeek → 只填 Key → 点测试 → 返回可用模型列表且 `supportsToolCalling=true` → 设为默认
  4. 故意填错 Key、填一个不支持 tool calling 的模型、填不通的 base_url，三种失败分别给出可读提示
  5. 列表与详情里 Key 只显示掩码；编辑时留空保存后 Key 未变
  6. 在存储配置页选 MinIO → 填 endpoint/bucket/密钥 → 测试通过 → 上传一张图片能正常显示；切到 `presigned` 模式后 URL 带签名且过期失效
  7. 把 `system.aiProvider.update` 误标 `AIEnabled: true` 后启动直接 panic

### Phase 4 — 监控与审计

- 在线用户（SCAN + 强退）、操作日志、登录日志、服务监控（gopsutil）、缓存监控（白名单 + SCAN）
- 行为溯源页（按主体/来源/风险/意图筛选，intent → plan → changeset 链条）
- 日志类 `Export` 声明 + 大数据量异步导出链路（`system:exportAsync` → `sys_upload` → 站内通知 → 下载）
- 前端 6 个页面
- **验收**：能强退另一个浏览器会话；缓存监控查不到白名单外 key；行为溯源能从一条日志跳到对应 changeset 并回滚；导出 6 万行日志时自动转异步并在文件管理页可下载

### Phase 5 — 异步任务与定时任务

- Asynq client/server/队列、处理器注册表 + 内置处理器（含 `cleanChangeset`）
- DB 驱动 `PeriodicTaskConfigProvider`、`scheduler:leader` 锁
- 保存时 cronspec 校验 + 下次执行时间
- 任务 CRUD/启停/立即执行/任务日志
- 前端定时任务 + 任务日志页面（调用目标下拉框，cron 生成器按 **5 字段**）
- **验收**：新建 cron 任务 15s 内生效并产生执行日志；停用后停止；禁止并发不重叠；填 6 字段 Quartz 风格表达式保存即报错并给出可读提示；任务的数据变更进 changeset

### Phase 6 — 知识库、Plan、AI Agent

- `internal/knowledge`：Enums 从字典动态生成、Entities/Rules 声明、分层注入、`get_knowledge` 工具
- `internal/plan`：Plan 状态机、引用求值、预演（只读步骤真实执行 + 写步骤 dry-run）、Apply（含 TOCTOU 二次校验）、逐步执行
- Eino ADK 装配、5 个工具、意图路由（answer / navigate / plan）
- SSE 端点、会话 CRUD、模型列表、Plan 接口
- 前端 Omnibox（Cmd+K）、AI 侧面板、`<PlanCard>`、`usePageContext` 接入各列表页、能力浏览器页
- **验收**：
  1. 「有哪些角色」→ 直接 `answer`（低风险查询不打扰）
  2. 「上周登录失败超过 3 次的账号」→ `navigate` 到登录日志页并预填筛选
  3. 「把用户 test01 停用」→ `plan` 卡片含 diff（status 正常 → 停用）→ 确认 → 执行 → Toast 可撤销 → 变更历史 `actor_type=agent` 且带 intent 原话
  4. 在用户列表勾选 3 行说「把这几个停用」→ 计划精确针对这 3 个 ID
  5. 「删除 admin 角色」→ 因 critical 只给提案与页面链接，无执行按钮
  6. 「把所有用户都停用」影响数超 `max_affected` → 强制人工执行
  7. 无 `system:user:edit` 权限的用户让 AI 停用用户 → AI 回答找不到该功能（目录已裁剪）
  8. 预览后在另一个浏览器改掉该用户 → Apply 返回 412 要求重新确认
  9. 问「status=1 是什么意思」→ AI 通过 `get_knowledge` 答出"停用"而非猜测

### Phase 7 — MCP Server

- PAT 中间件（含 max_risk 收窄）、`mcpx` 适配器（meta + flat）、Streamable HTTP 挂载、`intent-admin mcp` stdio
- 两步确认（`confirmToken` 绑定参数哈希）、annotations 映射、按 token 限流、审计 `actor_type=mcp_client`
- README 写客户端配置
- **验收**：
  1. Claude Code 配好 `/mcp` 后能列能力并查询用户列表
  2. `mcp.max_risk=low` 时写 Capability 不在 tools/list 且直接调用被拒
  3. 调高后：第一次调用返回 Preview + confirmToken，不改数据；改参数后用同一 token 被拒；confirm 后执行并进 changeset
  4. 令牌管理类 Capability 通过 MCP 不可见

### Phase 8 — 大盘与打包发布

- 首页大盘（含按主体拆分的操作量、回滚率、队列积压、Redis 缓存 + refresh + 预热 + 降级）
- `embed.FS` + SPA handler + 缓存头 + 预压缩
- Makefile 全量、Dockerfile、docker-compose（**内置 MinIO 服务**）、Nginx 示例、goreleaser、release 工作流
- 第 21 节全部仓库文件 + `AGENTS.md` + 截图/录屏
- **验收**：
  1. `make build` 产出单个二进制，在只有 PG + Redis 的机器上 `./intent-admin serve` 即出完整后台（存储用 `local` 驱动，无需对象存储）
  2. `docker compose up` 后能用 admin/123456 登录，MinIO 已自动配好可直接上传，并走通「AI 提案 → 确认 → 执行 → 撤销」全链路
  3. 按 README 从零开始 10 分钟内能跑起来（找没参与开发的人实测）——包括配好一个模型服务商
  4. 打 tag 后 release 产出全平台二进制 + 镜像

---

## 24. 风险与注意事项

1. **Capability 层是地基** —— 泛型 + 反射绑定要在 Phase 0 一次做对并有测试。坑：`uri`/`form`/`json` 三类 tag 填同一结构体的覆盖顺序；指针字段与零值区分（部分更新用指针 + `omitempty`）
2. **DryRun / Undo 的编写成本是本设计最大的落地风险** —— 如果通用助手（4.3）覆盖不够，开发者会倾向于不写或写错。Phase 0 就要把助手做到"典型 CRUD 3–5 行搞定"，并在 CI 里检查：`Mutating == true` 但 `DryRun == nil` 的 Capability 直接让测试失败
3. **DryRun 必须只读** —— 靠约定不够，必须有 GORM 层的硬拦截 + 测试
4. **TOCTOU** —— 预览与执行之间数据会变。Apply 前的二次 dry-run 比对不能省，否则「我看到的是停用 4 个」和「实际停用了 9 个」会同时发生
5. **changeset 体积** —— `before`/`after` 只存变更字段；`max_items_per_set` 兜底；90 天清理。否则批量操作会把 jsonb 写爆
6. **回滚不是万能的** —— 级联影响（删角色后用户已被改过）无法完美还原。回滚 UI 必须说明"仅还原本次变更涉及的字段"，不要给用户"时光机"的错觉
7. **风险等级要在 review 里把关** —— 这是本项目的安全边界。PR 模板要求说明新增 Capability 的 Risk 依据；`.golangci.yml` 无法检查语义，靠人
8. **Eino ADK 与 MCP SDK API 仍在演进** —— 实施时以 `go doc` 和官方文档当前版本为准核对签名；本文代码片段是设计意图，不保证逐字可编译
9. **模型必须支持 function calling** —— 不支持会直接导致 AI 不可用，`/api/ai/models` 失败时要给明确提示
10. **弱模型会乱拼参数** —— Capability 的 `Examples` 是主要缓解手段，务必认真写；`describe_capability` 返回里一定带上
11. **MCP 默认只读是有意的** —— 调高 `mcp.max_risk` 意味着持有 PAT 的外部客户端可以改生产数据。两步确认降低了风险但不消除。文档要写得醒目
12. **PAT 是提权面** —— 令牌管理类 Capability 必须 `MCPEnabled=false`，否则可用受限令牌造全权令牌
13. **页面上下文不可信** —— 前端传的 `selectedIds` 只是提示，执行时必须逐条过权限
14. **SSE 经 Nginx 需 `proxy_buffering off`** + 放长 `proxy_read_timeout`
15. **`app.encrypt_key` 一旦丢失，库里的 Key 与 Secret 全部不可恢复** —— 必须在 README 和配置注释里醒目说明，并提醒随数据库一起备份。`migrate rekey` 只能在旧钥还在手上时用
16. **配置类 Capability 是提示注入的高价值目标** —— 能改 `base_url` 就等于能把全部会话数据导向任意服务器。D21 的启动自检是硬防线，不要为了"方便"把它关掉
17. **minio-go 对各家 S3 兼容服务的差异** —— `path_style`、region 的处理各家不同，MinIO 与阿里云 OSS 是最容易踩坑的两家。预设里要把实测可用的组合固化下来，不要让用户试
18. **`PeriodicTaskManager` 单实例约束** —— 多副本同时跑会重复入队
19. **Redis 禁用 `KEYS`** —— 在线用户、缓存监控一律 `SCAN`
20. **数据合规** —— AI 与 MCP 都会把业务数据发给模型侧，页面与文档须明确提示，默认不给普通角色 `ai:agent:use`
21. **超管保护** —— 删除/停用/改角色/改密的 service 层必须硬拦超管账号，并写入知识库 Rules

---

## 25. 扩展功能实施状态

已定事项不再列出（License=MIT、注释语言=英文、文件存储=S3/MinIO + 后台可配、模型配置=后台多厂商可配、不做 Gitee 镜像、`navigate` 与逐步确认粒度按下述默认实现）。以下扩展已在完整交付中实现：

1. **Plan 跨人审批**—— 已实现指定审批人、私有通知、审批待办、批准/驳回/撤销/过期与重新审批；执行时实时校验双方权限与差异。
2. **changeset 选择性回滚**—— 已实现条目选择、原子补偿组、部分回滚状态、逐项冲突和反向恢复。
3. **批量导入（xlsx → 写库）**—— 已实现五类业务实体的模板、逐行校验、错误报告、只读预览、二次校验、原子提交与单变更集回滚。
4. **界面 i18n**—— 已实现中文/英文切换，覆盖导航、表单、预览、审批、任务和已知接口提示。
5. **队列控制台**—— 已实现与 Ant Design Pro 一致的内置管理页，提供任务列表/详情、重试、归档、取消、删除和暂停/恢复；按权限与 Redis 命名空间隔离。
6. **MCP 扩展**—— 已实现 OAuth 2.1 PKCE、公开客户端注册、显式授权、令牌轮换/撤销，以及实时鉴权的 Resources。
7. **定时任务的内置处理器清单** —— 当前只有日志/changeset 清理、知识库刷新、异步导出与演示任务。若有具体业务任务需预置，随时可加（加一个 `RegisterTask` 即可）。

### 已采纳的默认实现（无需再确认，但记录依据）

- **`navigate` 触发边界**：后端规则判定。**列表类**只读查询（返回行集合）→ `navigate` 跳页面并预填筛选；**聚合/计数类**查询（返回单值或少量统计）→ 直接 `answer`。理由：跳页面的价值在于"人能继续操作和核对"，只要一个数字时跳页面是干扰。
- **逐步确认粒度**：`mode=stepwise` 时每步都推送 `step_result` 事件，但前端**只对 `Risk >= high` 的步骤阻塞等待确认**，`low`/`medium` 直接连续执行。理由：每步都点确认会让多步任务变得比手工操作还慢，失去意义。
