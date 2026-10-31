package automation

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/dbx"
	"github.com/junler/intent-admin/internal/knowledge"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	pgquery "github.com/wasilibs/go-pgquery"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type SQLInput struct {
	SQL    string `json:"sql" jsonschema:"单条 PostgreSQL SELECT，支持 JOIN、CTE、子查询、窗口和聚合。只使用 schema 返回的未限定表名，参数使用 $1、$2；禁止写入和凭据查询"`
	Params []any  `json:"params,omitempty" jsonschema:"按 $1、$2 顺序绑定参数。流程内可使用 ${today}、${yesterday}、${now}，每次执行重新计算"`
	Limit  int    `json:"limit,omitempty" jsonschema:"最多返回行数，默认 100，最大 500"`
}
type SQLResult struct {
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	Truncated bool             `json:"truncated"`
}
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type Table struct {
	Schema  string   `json:"-"`
	Name    string   `json:"name"`
	Columns []Column `json:"columns"`
}
type SchemaInput struct {
	Table string `json:"table,omitempty" jsonschema:"留空列出所有有权查询的数据表，或指定表名查看字段类型"`
}

func (s *Service) registerSQL() {
	capability.Register(capability.Capability[SchemaInput, []Table]{Meta: capability.Meta{Name: "automation.sql.schema", Summary: "发现当前用户可查询的 PostgreSQL 表结构和字段类型，编写 SQL 前先调用", Risk: capability.RiskLow, AIEnabled: true, MCPEnabled: true, HTTP: &capability.HTTPBinding{Method: "POST", Path: "/automation/sql/schema"}}, Fn: func(ctx context.Context, in SchemaInput) ([]Table, error) { return s.schema(ctx, in.Table) }})
	capability.Register(capability.Capability[SQLInput, SQLResult]{Meta: capability.Meta{Name: "automation.sql.query", Summary: "通用只读 SQL 查询：筛选、关联、统计、分组、排序、窗口计算，适用于所有 AI 交互和流程", Risk: capability.RiskLow, AIEnabled: true, MCPEnabled: true, HTTP: &capability.HTTPBinding{Method: "POST", Path: "/automation/sql/query"}, Examples: []capability.Example{{Utterance: "统计今天新增用户", Args: `{"sql":"SELECT count(*) AS count FROM sys_user WHERE create_time >= $1","params":["2026-10-31T00:00:00+08:00"]}`}}}, Fn: s.Query})
}

// SQL access follows the visible list capability for each table. Raw execution
// payloads and credential values never enter the generated data projections.
func permittedTables(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	a, ok := capability.ActorFrom(ctx)
	if !ok {
		return out
	}
	for _, e := range capability.All() {
		if !strings.HasSuffix(e.Name, ".list") || e.Mutating || e.Risk != capability.RiskLow || !e.AIEnabled || !e.MCPEnabled || !a.Can(e.Perm) {
			continue
		}
		namespace := strings.TrimSuffix(e.Name, ".list")
		table, ok := knowledge.CapabilityEntities[namespace]
		if !ok {
			table, _ = knowledge.ConventionTable(e.Name)
		}
		if table != "" {
			out[table] = true
		}
	}
	// Relation tables inherit the corresponding user/role administration grant.
	if out["sys_user"] {
		out["sys_user_role"] = true
		out["sys_user_perm"] = true
	}
	if out["sys_role"] {
		out["sys_role_menu"] = true
	}
	return out
}
func safeColumn(table, column string) bool {
	if capability.IsSensitive(column) {
		return false
	}
	switch column {
	case "config_value", "job_params", "params", "payload", "steps", "messages", "history", "page_context", "result", "oper_param", "json_result", "error_msg", "exception_info":
		return false
	}
	return true
}
func (s *Service) schema(ctx context.Context, filter string) ([]Table, error) {
	allowed := permittedTables(ctx)
	if filter != "" && !allowed[filter] {
		return nil, errcode.Forbiddenf("没有查询该数据表的权限")
	}
	var rows []struct {
		TableSchema string
		TableName   string
		ColumnName  string
		DataType    string
	}
	if err := dbx.DBFrom(ctx, s.DB).Raw("SELECT table_schema,table_name,column_name,data_type FROM information_schema.columns WHERE table_schema=current_schema() ORDER BY table_name,ordinal_position").Scan(&rows).Error; err != nil {
		return nil, errcode.Internalf("读取表结构失败")
	}
	byName := map[string]*Table{}
	for _, row := range rows {
		if !allowed[row.TableName] || (filter != "" && row.TableName != filter) || !safeColumn(row.TableName, row.ColumnName) {
			continue
		}
		t := byName[row.TableName]
		if t == nil {
			t = &Table{Schema: row.TableSchema, Name: row.TableName}
			byName[row.TableName] = t
		}
		t.Columns = append(t.Columns, Column{Name: row.ColumnName, Type: row.DataType})
	}
	out := []Table{}
	for _, t := range byName {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// compileSQL validates the PostgreSQL AST, then shadows every accessible table
// with a projection which removes credentials before expressions can see them.
// Unqualified, inherited table references are mandatory; ONLY and qualified
// references would bypass CTE shadowing and are rejected.
func compileSQL(sql string, tables []Table) (string, error) {
	if len(sql) == 0 || len(sql) > 32000 {
		return "", errcode.BadRequestf("SQL 长度必须为 1 到 32000 字节")
	}
	tree, err := pgquery.ParseToJSON(sql)
	if err != nil {
		return "", errcode.BadRequestf("SQL 语法无效，请使用 PostgreSQL SELECT")
	}
	var root map[string]any
	if json.Unmarshal([]byte(tree), &root) != nil {
		return "", errcode.BadRequestf("SQL 解析失败")
	}
	stmts, _ := root["stmts"].([]any)
	if len(stmts) != 1 {
		return "", errcode.BadRequestf("只能执行单条只读 SELECT")
	}
	statement := stmts[0].(map[string]any)["stmt"].(map[string]any)
	if statement["SelectStmt"] == nil {
		return "", errcode.BadRequestf("只能执行只读 SELECT")
	}
	allowed := map[string]bool{}
	ctes := []string{}
	for _, t := range tables {
		allowed[t.Name] = true
		columns := []string{}
		softDelete := false
		for _, c := range t.Columns {
			columns = append(columns, quote(c.Name))
			softDelete = softDelete || c.Name == "del_flag"
		}
		if len(columns) == 0 {
			continue
		}
		projection := quote(t.Name) + " AS MATERIALIZED (SELECT " + strings.Join(columns, ",") + " FROM " + quote(t.Schema) + "." + quote(t.Name)
		if softDelete {
			projection += " WHERE del_flag='0'"
		}
		ctes = append(ctes, projection+")")
	}
	if err := checkSQLNode(statement, allowed, map[string]bool{}); err != nil {
		return "", err
	}
	// The parser accepts a final semicolon; remove it before embedding.
	parsed, err := pgquery.Parse(sql)
	if err != nil {
		return "", errcode.BadRequestf("SQL 解析失败")
	}
	sql, err = pgquery.Deparse(parsed)
	if err != nil {
		return "", errcode.BadRequestf("SQL 解析失败")
	}
	prefix := ""
	if len(ctes) > 0 {
		prefix = "WITH " + strings.Join(ctes, ",") + " "
	}
	return prefix + "SELECT * FROM (\n" + sql + "\n) AS ai_query_result LIMIT $", nil
}

var sqlFunctions = map[string]bool{}

func init() {
	for _, name := range strings.Fields("count sum avg min max abs round ceil ceiling floor trunc mod power sqrt sign greatest least coalesce nullif concat concat_ws lower upper length char_length substring substr trim btrim ltrim rtrim replace left right split_part strpos position regexp_replace regexp_match regexp_matches string_agg array_agg json_agg jsonb_agg json_build_object jsonb_build_object json_object_agg jsonb_object_agg json_array_length jsonb_array_length json_extract_path_text jsonb_extract_path_text jsonb_array_elements jsonb_array_elements_text row_to_json to_json to_jsonb jsonb_pretty to_char to_date to_timestamp date_trunc date_part extract age now current_date current_timestamp make_date make_interval timezone generate_series unnest array_length cardinality array_to_string row_number rank dense_rank percent_rank cume_dist ntile lag lead first_value last_value nth_value bool_and bool_or every stddev stddev_pop stddev_samp variance var_pop var_samp percentile_cont percentile_disc mode grouping") {
		sqlFunctions[name] = true
	}
}
func astNames(v any) []string {
	out := []string{}
	items, _ := v.([]any)
	for _, item := range items {
		m, _ := item.(map[string]any)
		s, _ := m["String"].(map[string]any)
		name, _ := s["sval"].(string)
		out = append(out, name)
	}
	return out
}
func checkSQLNode(value any, tables, scope map[string]bool) error {
	switch node := value.(type) {
	case []any:
		for _, item := range node {
			if err := checkSQLNode(item, tables, scope); err != nil {
				return err
			}
		}
	case map[string]any:
		if selectNode, ok := node["SelectStmt"].(map[string]any); ok {
			next := map[string]bool{}
			for k, v := range scope {
				next[k] = v
			}
			if with, ok := selectNode["withClause"].(map[string]any); ok {
				if entries, ok := with["ctes"].([]any); ok {
					for _, entry := range entries {
						m, _ := entry.(map[string]any)
						cte, _ := m["CommonTableExpr"].(map[string]any)
						name, _ := cte["ctename"].(string)
						if with["recursive"] == true {
							next[name] = true
						}
						if err := checkSQLNode(cte["ctequery"], tables, next); err != nil {
							return err
						}
						next[name] = true
					}
				}
			}
			for key, item := range selectNode {
				if key != "withClause" {
					if err := checkSQLNode(map[string]any{key: item}, tables, next); err != nil {
						return err
					}
				}
			}
			return nil
		}
		for key, item := range node {
			if (strings.HasSuffix(key, "Stmt") && key != "SelectStmt") || key == "IntoClause" || key == "intoClause" || key == "lockingClause" || key == "RangeTableSample" || key == "CollateClause" {
				return errcode.BadRequestf("SQL 不支持写入、锁定或扩展执行语法")
			}
			if key == "RangeVar" {
				m, _ := item.(map[string]any)
				name, _ := m["relname"].(string)
				if m["schemaname"] != nil || m["catalogname"] != nil || m["inh"] != true || (!tables[name] && !scope[name]) {
					return errcode.Forbiddenf("SQL 只能使用有权查询的未限定表名，不支持 ONLY")
				}
			}
			if key == "FuncCall" {
				m, _ := item.(map[string]any)
				names := astNames(m["funcname"])
				if len(names) == 2 && names[0] == "pg_catalog" {
					names = names[1:]
				}
				if len(names) != 1 || !sqlFunctions[names[0]] {
					return errcode.BadRequestf("SQL 函数不可用，请使用标准查询、统计或日期函数")
				}
			}
			if key == "A_Expr" {
				m, _ := item.(map[string]any)
				if len(astNames(m["name"])) > 1 {
					return errcode.BadRequestf("SQL 不支持自定义模式运算符")
				}
			}
			if key == "TypeName" || key == "typeName" {
				m, _ := item.(map[string]any)
				names := astNames(m["names"])
				if len(names) == 2 && names[0] == "pg_catalog" {
					names = names[1:]
				}
				if len(names) != 1 || !strings.Contains(" text varchar bpchar bool boolean int2 int4 int8 integer bigint smallint numeric decimal float4 float8 real double timestamp timestamptz date time timetz interval json jsonb uuid ", " "+names[0]+" ") {
					return errcode.BadRequestf("SQL 不支持该类型转换")
				}
			}
			if err := checkSQLNode(item, tables, scope); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) Query(ctx context.Context, in SQLInput) (SQLResult, error) {
	out := SQLResult{Columns: []string{}, Rows: []map[string]any{}}
	if in.Limit == 0 {
		in.Limit = 100
	}
	if in.Limit < 1 || in.Limit > 500 || len(in.Params) > 100 {
		return out, errcode.BadRequestf("查询最多 500 行和 100 个参数")
	}
	tables, err := s.schema(ctx, "")
	if err != nil {
		return out, err
	}
	sql, err := compileSQL(in.SQL, tables)
	if err != nil {
		return out, err
	}
	// Use QueryContext directly: GORM's question-mark substitution conflicts with
	// PostgreSQL JSON operators. The driver binds all $n parameters as values.
	sql += strconv.Itoa(len(in.Params) + 1)
	params := append(append([]any{}, in.Params...), in.Limit+1)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	db := dbx.DBFrom(ctx, s.DB).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	_, ambient := db.Statement.ConnPool.(gorm.TxCommitter)
	run := func(tx *gorm.DB) error {
		pool := tx.Statement.ConnPool
		if !ambient {
			if _, err := pool.ExecContext(ctx, "SET TRANSACTION READ ONLY"); err != nil {
				return err
			}
		}
		var searchPath string
		if err := pool.QueryRowContext(ctx, "SELECT current_setting('search_path')").Scan(&searchPath); err != nil {
			return err
		}
		if _, err := pool.ExecContext(ctx, "SELECT set_config('search_path', 'pg_catalog', true)"); err != nil {
			return err
		}
		defer func() { _, _ = pool.ExecContext(ctx, "SELECT set_config('search_path', $1, true)", searchPath) }()

		rows, err := tx.Statement.ConnPool.QueryContext(ctx, sql, params...)
		if err != nil {
			return err
		}
		defer rows.Close()
		out.Columns, err = rows.Columns()
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, c := range out.Columns {
			if seen[c] {
				return errcode.BadRequestf("查询结果列名重复，请使用 AS 设置唯一别名")
			}
			seen[c] = true
		}
		bytes := 0
		for rows.Next() {
			if len(out.Rows) >= in.Limit {
				out.Truncated = true
				break
			}
			values := make([]any, len(out.Columns))
			dest := make([]any, len(values))
			for i := range values {
				dest[i] = &values[i]
			}
			if err := rows.Scan(dest...); err != nil {
				return err
			}
			row := map[string]any{}
			for i, c := range out.Columns {
				if b, ok := values[i].([]byte); ok {
					values[i] = string(b)
				}
				row[c] = values[i]
			}
			raw, _ := json.Marshal(row)
			bytes += len(raw)
			if bytes > 64<<10 {
				out.Truncated = true
				break
			}
			out.Rows = append(out.Rows, row)
		}
		return rows.Err()
	}
	if ambient {
		err = run(db)
	} else {
		err = db.Transaction(run)
	}
	if err != nil {
		return SQLResult{}, errcode.BadRequestf("SQL 查询失败，请核对字段、参数类型和唯一列别名，或缩小查询范围")
	}
	return out, nil
}
