package automation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/junler/intent-admin/internal/capability"
)

func TestSQLProjectionAndBoundaries(t *testing.T) {
	tables := []Table{{Schema: "public", Name: "sys_user", Columns: []Column{{Name: "user_id"}, {Name: "user_name"}, {Name: "create_time"}, {Name: "del_flag"}}}}
	for _, sql := range []string{
		"SELECT count(*) AS count FROM sys_user WHERE create_time >= $1",
		"SELECT u.user_name, row_number() OVER (ORDER BY user_id) AS n FROM sys_user u",
		"WITH recent AS (SELECT * FROM sys_user WHERE create_time >= CURRENT_DATE) SELECT count(*) FROM recent",
		"SELECT date_trunc('day',create_time),count(*) FROM sys_user GROUP BY 1; -- trailing comment",
		"SELECT 1+2 AS total",
		"SELECT a.user_id FROM sys_user a JOIN sys_user b ON a.user_id=b.user_id",
	} {
		t.Run(sql, func(t *testing.T) {
			compiled, err := compileSQL(sql, tables)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(compiled, `FROM "public"."sys_user" WHERE del_flag='0'`) {
				t.Fatalf("missing safe projection: %s", compiled)
			}
		})
	}
	for _, sql := range []string{
		"DELETE FROM sys_user", "SELECT 1; SELECT 2",
		"WITH gone AS (DELETE FROM sys_user RETURNING *) SELECT * FROM gone",
		"SELECT * INTO stolen FROM sys_user", "SELECT * FROM sys_user FOR UPDATE",
		"SELECT * FROM public.sys_user", "SELECT * FROM ONLY sys_user",
		"SELECT * FROM pg_authid", "SELECT * FROM sys_access_token",
		"SELECT pg_read_file('/etc/passwd')", "SELECT pg_sleep(10)",
		"SELECT public.count(*) FROM sys_user", "SELECT 'sys_user'::regclass",
		"WITH sys_access_token AS (SELECT * FROM sys_access_token) SELECT * FROM sys_access_token",
		"WITH a AS (SELECT * FROM sys_access_token), sys_access_token AS (SELECT 1) SELECT * FROM a",
		"SELECT * FROM (WITH hidden AS (SELECT 1) SELECT * FROM hidden) a, hidden",
	} {
		t.Run(sql, func(t *testing.T) {
			if compiled, err := compileSQL(sql, tables); err == nil {
				t.Fatalf("unsafe SQL accepted: %s", compiled)
			}
		})
	}
	for _, column := range []string{"password", "token_hash", "api_key_enc", "secret_key_enc", "config_value", "job_params", "oper_param", "json_result"} {
		if safeColumn("sys_user", column) {
			t.Errorf("credential field %s allowed", column)
		}
	}
}

func TestResolvePreservesTypesAndRejectsInvalidReferences(t *testing.T) {
	values := map[string]any{"stats": map[string]any{"rows": []any{map[string]any{"count": json.Number("12"), "text": "中\"文"}}}}
	out, err := Resolve(map[string]any{"n": "${stats.rows.0.count}", "content": "今日新增 ${stats.rows.0.count} 人，${stats.rows.0.text}"}, values)
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["n"] != json.Number("12") || m["content"] != "今日新增 12 人，中\"文" {
		t.Fatalf("bad substitution: %#v", out)
	}
	projection, err := Resolve("${stats.rows.*.count}", values)
	if err != nil || len(projection.([]any)) != 1 {
		t.Fatalf("projection failed: %#v %v", projection, err)
	}
	for _, reference := range []string{"${unknown}", "${stats.rows.2.count}", "${stats.rows.-1.count}", "${stats.rows.0.missing}"} {
		if _, err := Resolve(reference, values); err == nil {
			t.Errorf("accepted %s", reference)
		}
	}
}

func TestFlowPreviewAndPermissionBoundaries(t *testing.T) {
	capability.Install(capability.Hooks{Tx: func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }})
	writes := 0
	previews := 0
	type Input struct {
		Content string `json:"content"`
	}
	capability.Register(capability.Capability[capability.Empty, map[string]any]{Meta: capability.Meta{Name: "fixture.flowStats", Summary: "统计", Risk: capability.RiskLow, Perm: "fixture:read", AIEnabled: true, MCPEnabled: true}, Fn: func(context.Context, capability.Empty) (map[string]any, error) {
		return map[string]any{"rows": []any{map[string]any{"count": 12, "alert": false}}}, nil
	}})
	capability.Register(capability.Capability[Input, capability.Empty]{Meta: capability.Meta{Name: "fixture.flowWrite", Summary: "写入", Risk: capability.RiskMedium, Perm: "fixture:write", AIEnabled: true, MCPEnabled: true, Mutating: true}, Fn: func(_ context.Context, in Input) (capability.Empty, error) {
		writes++
		if in.Content != "新增 12 人" {
			t.Errorf("unresolved content %q", in.Content)
		}
		return capability.Empty{}, nil
	}, DryRun: func(_ context.Context, in Input) (*capability.Preview, error) {
		previews++
		if in.Content != "新增 12 人" {
			t.Errorf("unresolved preview %q", in.Content)
		}
		return &capability.Preview{Summary: in.Content, Affected: 1}, nil
	}})
	ctx := capability.WithActor(context.Background(), &capability.Actor{UserID: 7, Source: capability.SourceAI, MaxRisk: capability.RiskHigh, Perms: []string{"fixture:read", "fixture:write"}})
	flow := Flow{Steps: []Step{{Name: "stats", Capability: "fixture.flowStats"}, {Name: "send", Capability: "fixture.flowWrite", Args: map[string]any{"content": "新增 ${stats.rows.0.count} 人"}}}}
	if _, err := Run(ctx, flow, true, true); err != nil {
		t.Fatal(err)
	}
	if writes != 0 || previews != 1 {
		t.Fatal("preview performed a write")
	}
	if _, err := Run(ctx, flow, true, false); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatal("flow did not execute")
	}
	if _, err := Run(ctx, flow, false, false); err == nil {
		t.Fatal("read flow wrote")
	}
	for _, a := range []*capability.Actor{{Source: capability.SourceMCP, ReadOnly: true, MaxRisk: capability.RiskHigh, Perms: []string{capability.SuperPerm}}, {Source: capability.SourceAI, MaxRisk: capability.RiskLow, Perms: []string{capability.SuperPerm}}, {Source: capability.SourceAI, MaxRisk: capability.RiskHigh, Perms: []string{"fixture:read"}}} {
		if _, err := Run(capability.WithActor(context.Background(), a), flow, true, false); err == nil {
			t.Fatal("flow bypassed authorization")
		}
	}
	if writes != 1 {
		t.Fatal("rejected flow had side effects")
	}
	flow.Steps[1].When = "${stats.rows.0.alert}"
	out, err := Run(ctx, flow, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if writes != 1 || out.Results["send"].(map[string]any)["skipped"] != true {
		t.Fatal("false condition did not skip write")
	}
	flow.Steps[1].When = "${stats.rows.0.count}"
	if _, err := Run(ctx, flow, true, false); err == nil {
		t.Fatal("non-boolean condition accepted")
	}

}
