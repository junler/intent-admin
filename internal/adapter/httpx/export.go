package httpx

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	"github.com/junler/intent-admin/internal/pkg/exportfmt"
	"github.com/junler/intent-admin/internal/pkg/response"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// exportHandler renders the capability's rows as a spreadsheet.
//
// It reuses the list capability's input and query function unchanged. That is
// the whole point: the classic bug in admin systems is an export path with its
// own copy of the filter logic that silently diverges from the screen.
func exportHandler(e *capability.Entry, opts Options) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		actor, ok := capability.ActorFrom(ctx)
		if !ok {
			response.Fail(c, errcode.Unauthorizedf("请先登录"))
			return
		}
		// Export carries its own permission because reading one page on screen
		// and walking out with the whole table are different acts.
		if !actor.Can(e.ExportPerm()) {
			response.Fail(c, errcode.Forbiddenf("没有权限导出%s", e.Summary))
			return
		}
		// Export is not reachable from the AI or MCP entry points: a file
		// cannot be delivered over a streaming tool response, and letting a
		// model dump whole tables on a whim is an unnecessary exposure. The
		// agent instead navigates the user to the filtered page.
		switch actor.Source {
		case capability.SourceAI, capability.SourceMCP:
			response.Fail(c, errcode.Forbiddenf("导出功能只能在管理页面使用"))
			return
		}

		in := e.NewInput()
		if err := Bind(c, in); err != nil {
			response.Fail(c, err)
			return
		}
		if err := widenForExport(in, opts.ExportMaxRows); err != nil {
			response.Fail(c, errcode.BadRequestf("%v", err))
			return
		}

		out, err := capability.InvokeTyped(ctx, e.Name, in)
		if err != nil {
			response.Fail(c, err)
			return
		}
		rows, err := extractRows(out, e.Export.RowsField)
		if err != nil {
			response.Fail(c, err)
			return
		}
		if opts.ExportMaxRows > 0 && len(rows) >= opts.ExportMaxRows {
			if opts.AsyncExport != nil {
				queued, err := opts.AsyncExport(ctx, e, in)
				if err != nil {
					response.Fail(c, err)
					return
				}
				response.OK(c, queued)
				return
			}
			response.Fail(c, errcode.BadRequestf(
				"符合条件的数据超过 %d 条，请缩小筛选范围，或使用后台导出",
				opts.ExportMaxRows))
			return
		}

		renderOptions := opts
		if opts.DictLookup != nil {
			labels := map[string]string{}
			renderOptions.DictLookup = func(dictType, value string) string {
				key := dictType + "\x00" + value
				if label, ok := labels[key]; ok {
					return label
				}
				label := opts.DictLookup(dictType, value)
				labels[key] = label
				return label
			}
		}
		f, err := render(e, rows, renderOptions)
		if err != nil {
			response.Fail(c, err)
			return
		}
		defer func() { _ = f.Close() }()

		writeXLSX(c, f, fileName(e.Export.FileName))
	}
}

// writeTemplate emits a header-only workbook, used as the import template.
func writeTemplate(c *gin.Context, e *capability.Entry) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := e.Export.SheetName
	if err := renameDefaultSheet(f, sheet); err != nil {
		response.Fail(c, errcode.Internalf("生成模板失败").Wrap(err))
		return
	}
	if err := writeHeader(f, sheet, e.Export.Columns); err != nil {
		response.Fail(c, errcode.Internalf("生成模板失败").Wrap(err))
		return
	}
	writeXLSX(c, f, fileName(e.Export.SheetName+"_导入模板"))
}

func render(e *capability.Entry, rows []map[string]any, opts Options) (*excelize.File, error) {
	f := excelize.NewFile()
	sheet := e.Export.SheetName
	if err := renameDefaultSheet(f, sheet); err != nil {
		return nil, errcode.Internalf("生成表格失败").Wrap(err)
	}

	// Above the threshold the row-by-row API keeps the whole sheet in memory;
	// StreamWriter trades random access for bounded memory.
	if opts.ExportStreamThreshold > 0 && len(rows) >= opts.ExportStreamThreshold {
		if err := renderStream(f, sheet, e.Export.Columns, rows, opts); err != nil {
			return nil, errcode.Internalf("生成表格失败").Wrap(err)
		}
		return f, nil
	}

	if err := writeHeader(f, sheet, e.Export.Columns); err != nil {
		return nil, errcode.Internalf("生成表格失败").Wrap(err)
	}
	for i, row := range rows {
		for j, col := range e.Export.Columns {
			cell, err := excelize.CoordinatesToCellName(j+1, i+2)
			if err != nil {
				return nil, errcode.Internalf("生成表格失败").Wrap(err)
			}
			if err := f.SetCellValue(sheet, cell, cellValue(row, col, opts)); err != nil {
				return nil, errcode.Internalf("生成表格失败").Wrap(err)
			}
		}
	}
	return f, nil
}

func renderStream(f *excelize.File, sheet string, cols []capability.ExportColumn, rows []map[string]any, opts Options) error {
	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		return err
	}
	header := make([]any, len(cols))
	for i, c := range cols {
		header[i] = c.Label
	}
	if err := sw.SetRow("A1", header); err != nil {
		return err
	}
	for i, row := range rows {
		values := make([]any, len(cols))
		for j, col := range cols {
			values[j] = cellValue(row, col, opts)
		}
		cell, err := excelize.CoordinatesToCellName(1, i+2)
		if err != nil {
			return err
		}
		if err := sw.SetRow(cell, values); err != nil {
			return err
		}
	}
	return sw.Flush()
}

func renameDefaultSheet(f *excelize.File, name string) error {
	const def = "Sheet1"
	if name == def || name == "" {
		return nil
	}
	return f.SetSheetName(def, name)
}

func writeHeader(f *excelize.File, sheet string, cols []capability.ExportColumn) error {
	style, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"F2F3F5"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	if err != nil {
		return err
	}
	for i, col := range cols {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return err
		}
		if err := f.SetCellValue(sheet, cell, col.Label); err != nil {
			return err
		}
		name, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return err
		}
		if err := f.SetCellStyle(sheet, cell, cell, style); err != nil {
			return err
		}
		width := col.Width
		if width <= 0 {
			width = 16
		}
		if err := f.SetColWidth(sheet, name, name, width); err != nil {
			return err
		}
	}
	return f.SetPanes(sheet, &excelize.Panes{
		Freeze: true, Split: false, XSplit: 0, YSplit: 1,
		TopLeftCell: "A2", ActivePane: "bottomLeft",
	})
}

// cellValue resolves one column's value for one row, applying dictionary
// translation and formatting so the spreadsheet reads like the screen.
func cellValue(row map[string]any, col capability.ExportColumn, opts Options) any {
	v := lookupPath(row, col.Field)
	if v == nil {
		return ""
	}
	if col.Dict != "" && opts.DictLookup != nil {
		if text := opts.DictLookup(col.Dict, fmt.Sprint(v)); text != "" {
			return text
		}
	}
	switch col.Format {
	case capability.FormatDateTime:
		return formatTime(v, "2006-01-02 15:04:05")
	case capability.FormatDate:
		return formatTime(v, "2006-01-02")
	case capability.FormatMoney:
		return exportfmt.Money(v)
	default:
		return v
	}
}

// lookupPath resolves a dotted field path into nested maps.
func lookupPath(row map[string]any, path string) any {
	if !strings.Contains(path, ".") {
		return row[path]
	}
	var cur any = row
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[part]
		if !ok {
			return nil
		}
	}
	return cur
}

func formatTime(v any, layout string) any {
	switch t := v.(type) {
	case time.Time:
		if t.IsZero() {
			return ""
		}
		return t.Local().Format(layout)
	case *time.Time:
		if t == nil || t.IsZero() {
			return ""
		}
		return t.Local().Format(layout)
	case string:
		for _, in := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
			if parsed, err := time.Parse(in, t); err == nil {
				return parsed.Local().Format(layout)
			}
		}
		return t
	default:
		return v
	}
}

// extractRows pulls the row slice out of a capability's output. The output is
// a concrete Go type, so reflection is used once here rather than forcing
// every list capability to return a map.
func extractRows(out any, field string) ([]map[string]any, error) {
	v := reflect.ValueOf(out)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, nil
		}
		v = v.Elem()
	}

	var rowsValue reflect.Value
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		rowsValue = v
	case reflect.Struct:
		rowsValue = fieldByJSONName(v, field)
	case reflect.Map:
		rowsValue = v.MapIndex(reflect.ValueOf(field))
	}
	if !rowsValue.IsValid() {
		return nil, errcode.Internalf("导出失败：功能 %s 的出参里找不到行集合字段 %q", field, field)
	}
	for rowsValue.Kind() == reflect.Interface || rowsValue.Kind() == reflect.Pointer {
		if rowsValue.IsNil() {
			return nil, nil
		}
		rowsValue = rowsValue.Elem()
	}
	if rowsValue.Kind() != reflect.Slice && rowsValue.Kind() != reflect.Array {
		return nil, errcode.Internalf("导出失败：字段 %q 不是数组", field)
	}

	rows := make([]map[string]any, 0, rowsValue.Len())
	for i := range rowsValue.Len() {
		item := rowsValue.Index(i).Interface()
		m, err := toMap(item)
		if err != nil {
			return nil, err
		}
		rows = append(rows, m)
	}
	return rows, nil
}

// fieldByJSONName finds a struct field by its json tag name, falling back to
// the Go field name.
func fieldByJSONName(v reflect.Value, name string) reflect.Value {
	t := v.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if tag == name || strings.EqualFold(f.Name, name) {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

func toMap(item any) (map[string]any, error) {
	if m, ok := item.(map[string]any); ok {
		return m, nil
	}
	raw := capability.SanitizeValue(item)
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, errcode.Internalf("导出失败：行数据不是对象")
	}
	return m, nil
}

func fileName(pattern string) string {
	name := strings.ReplaceAll(pattern, "{date}", time.Now().Format("20060102150405"))
	if !strings.HasSuffix(name, ".xlsx") {
		name += ".xlsx"
	}
	return name
}

// writeXLSX streams the workbook with a Content-Disposition that survives
// non-ASCII names. The RFC 5987 `filename*` form is required: without it
// Chinese filenames arrive mangled in several browsers.
func writeXLSX(c *gin.Context, f *excelize.File, name string) {
	c.Header("Content-Type", xlsxContentType)
	c.Header("Content-Disposition", fmt.Sprintf(
		`attachment; filename="export.xlsx"; filename*=UTF-8''%s`, url.PathEscape(name)))
	c.Header("Cache-Control", "no-store")
	if err := f.Write(c.Writer); err != nil {
		// Headers are already sent, so the envelope is no longer available.
		// Log and drop the connection rather than appending junk to the file.
		_ = c.Error(err)
	}
}
