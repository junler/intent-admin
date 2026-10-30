package importer

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func testSpec(name string) spec {
	for _, s := range specs {
		if s.Capability == name {
			return s
		}
	}
	panic(name)
}
func fileBytes(t *testing.T, headers []string, rows [][]any) []byte {
	t.Helper()
	b, e := workbook(headers, rows)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestParseRealXLSX(t *testing.T) {
	sp := testSpec("demo.item.create")
	data := fileBytes(t, []string{"名称*", "分类", "金额（分）", "状态"}, [][]any{{"first", "alpha", "9007199254740993", "0"}, {"first", "alpha", 1, "0"}, {"third", "alpha", "1.5", "0"}})
	rows, problems, total, err := parse(sp, bytes.NewReader(data))
	if err != nil || total != 3 || len(rows) != 1 || len(problems) != 2 {
		t.Fatalf("rows=%v problems=%v total=%d err=%v", rows, problems, total, err)
	}
	if !strings.Contains(string(rows[0].Arguments), "9007199254740993") {
		t.Fatal("integer precision lost")
	}
	if problems[0].Row != 3 || !strings.Contains(problems[0].Message, "第 2 行") {
		t.Fatal(problems)
	}
	if problems[1].Row != 4 {
		t.Fatal(problems)
	}
}
func TestRejectUnsupportedSensitiveHeaders(t *testing.T) {
	for _, header := range []string{"password", "roleIds", "permissions", "aiMaxRisk"} {
		_, _, _, err := parse(testSpec("system.user.create"), bytes.NewReader(fileBytes(t, []string{"用户名*", "昵称*", header}, [][]any{{"newuser", "测试用户", "secret-value"}})))
		if err == nil || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("header %s: %v", header, err)
		}
	}
}
func TestRejectCredentialSettingsAndNestedSecrets(t *testing.T) {
	data := fileBytes(t, []string{"configName", "configKey", "configValue"}, [][]any{
		{"one", "sys.user.initPassword", "credential-do-not-leak"},
		{"two", "custom.data", `{"nested":{"api_key":"credential-do-not-leak"}}`},
		{"three", "custom.safe", "true"},
	})
	rows, problems, _, err := parse(testSpec("system.config.create"), bytes.NewReader(data))
	if err != nil || len(rows) != 1 || len(problems) != 2 {
		t.Fatal(rows, problems, err)
	}
	raw, _ := json.Marshal(problems)
	if strings.Contains(string(raw), "credential-do-not-leak") {
		t.Fatal("credentials leaked in row errors")
	}
}
func TestRejectFormulasAndOversizedBatch(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	_ = f.SetCellStr("Sheet1", "A1", "名称*")
	_ = f.SetCellFormula("Sheet1", "A2", "1+1")
	b, _ := f.WriteToBuffer()
	rows, problems, total, err := parse(testSpec("demo.item.create"), bytes.NewReader(b.Bytes()))
	if err != nil || len(rows) != 0 || total != 1 || len(problems) != 1 || !strings.Contains(problems[0].Message, "公式") {
		t.Fatal(rows, problems, total, err)
	}
	dataRows := make([][]any, MaxRows+1)
	for i := range dataRows {
		dataRows[i] = []any{"row" + string(rune('a'+i))}
	}
	if _, _, _, err = parse(testSpec("demo.item.create"), bytes.NewReader(fileBytes(t, []string{"name"}, dataRows))); err == nil {
		t.Fatal("oversized batch accepted")
	}
	_, _ = f.NewSheet("Hidden")
	_ = f.SetSheetVisible("Hidden", false)
	b, _ = f.WriteToBuffer()
	if _, _, _, err = parse(testSpec("demo.item.create"), bytes.NewReader(b.Bytes())); err == nil {
		t.Fatal("unreviewed additional sheet accepted")
	}
}

func TestErrorWorkbookIsText(t *testing.T) {
	b, err := workbook([]string{"文件行号", "校验错误"}, [][]any{{2, "=HYPERLINK(\"https://example.com\")"}})
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	formula, _ := f.GetCellFormula("Sheet1", "B2")
	if formula != "" {
		t.Fatal("error report formula injection")
	}
}

func TestSortRangeValidatedBeforeApply(t *testing.T) {
	rows, problems, _, err := parse(testSpec("system.role.create"), bytes.NewReader(fileBytes(t, []string{"roleName", "roleKey", "roleSort"}, [][]any{{"new", "new", "2147483648"}})))
	if err != nil || len(rows) != 0 || len(problems) != 1 || !strings.Contains(problems[0].Message, "范围") {
		t.Fatal(rows, problems, err)
	}
}
