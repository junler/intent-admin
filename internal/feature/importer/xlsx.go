package importer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
	system "github.com/junler/intent-admin/internal/service/system"
)

const MaxRows = 200
const MaxFileBytes = 5 << 20
const maxCellBytes = 4096

type RowError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}
type inputRow struct {
	Number    int             `json:"number"`
	Arguments json.RawMessage `json:"arguments"`
}

func parse(s spec, data io.Reader) ([]inputRow, []RowError, int, error) {
	f, err := excelize.OpenReader(data, excelize.Options{UnzipSizeLimit: 32 << 20, UnzipXMLSizeLimit: 8 << 20})
	if err != nil {
		return nil, nil, 0, errcode.BadRequestf("无法读取 XLSX 文件，请使用导入模板")
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) != 1 {
		return nil, nil, 0, errcode.BadRequestf("导入文件必须只包含一个工作表")
	}
	sheet := sheets[0]
	it, err := f.Rows(sheet)
	if err != nil {
		return nil, nil, 0, errcode.BadRequestf("工作表不可读取")
	}
	defer it.Close()
	if !it.Next() {
		return nil, nil, 0, errcode.BadRequestf("文件缺少表头")
	}
	headers, err := it.Columns()
	if err != nil {
		return nil, nil, 0, errcode.BadRequestf("表头不可读取")
	}
	if len(headers) == 0 || len(headers) > len(s.Columns) {
		return nil, nil, 0, errcode.BadRequestf("表头数量不正确，请使用导入模板")
	}
	byName := map[string]column{}
	for _, c := range s.Columns {
		byName[c.Field] = c
		byName[c.Label] = c
		byName[englishColumns[c.Field]] = c
		if c.Required {
			byName[c.Label+"*"] = c
			byName[englishColumns[c.Field]+"*"] = c
		}
	}
	cols := make([]column, len(headers))
	seen := map[string]bool{}
	for i, h := range headers {
		c, ok := byName[strings.TrimSpace(h)]
		if !ok || seen[c.Field] {
			return nil, nil, 0, errcode.BadRequestf("第 %d 列表头不受支持或重复，请使用模板（不接受密码、授权等字段）", i+1)
		}
		seen[c.Field] = true
		cols[i] = c
		axis, _ := excelize.CoordinatesToCellName(i+1, 1)
		if formula, _ := f.GetCellFormula(sheet, axis); formula != "" {
			return nil, nil, 0, errcode.BadRequestf("表头不能使用公式")
		}
	}
	for _, c := range s.Columns {
		if c.Required && !seen[c.Field] {
			return nil, nil, 0, errcode.BadRequestf("缺少必填列：%s", c.Label)
		}
	}
	rows := []inputRow{}
	errors := []RowError{}
	unique := map[string]int{}
	total := 0
	number := 1
	for it.Next() {
		number++
		if number > MaxRows+1 {
			return nil, nil, 0, errcode.BadRequestf("一次最多导入 %d 行，请删除尾部多余行后分批导入", MaxRows)
		}
		cells, e := it.Columns(excelize.Options{RawCellValue: true})
		if e != nil {
			return nil, nil, 0, errcode.BadRequestf("第 %d 行无法读取", number)
		}
		empty := true
		for _, v := range cells {
			if strings.TrimSpace(v) != "" {
				empty = false
				break
			}
		}
		if empty { // Formula-only rows can have an empty cached value.
			for i := range cols {
				axis, _ := excelize.CoordinatesToCellName(i+1, number)
				v, _ := f.GetCellFormula(sheet, axis)
				if v != "" {
					empty = false
					break
				}
			}
		}
		if empty {
			continue
		}
		total++
		args := map[string]any{}
		message := ""
		if len(cells) > len(cols) {
			message = "存在未声明列的数据"
		}
		for i, c := range cols {
			v := ""
			if i < len(cells) {
				v = strings.TrimSpace(cells[i])
			}
			axis, _ := excelize.CoordinatesToCellName(i+1, number)
			if formula, _ := f.GetCellFormula(sheet, axis); formula != "" {
				message = "不支持公式，请粘贴为值"
				break
			}
			if len(v) > maxCellBytes {
				message = c.Label + "内容过长"
				break
			}
			if c.Required && v == "" {
				message = c.Label + "不能为空"
				break
			}
			if v == "" {
				continue
			}
			if c.Integer {
				n, e := strconv.ParseInt(v, 10, 64)
				if e != nil || n < 0 {
					message = c.Label + "必须为非负整数"
					break
				}
				if c.Field != "amount" && n > math.MaxInt32 {
					message = c.Label + "超出支持范围"
					break
				}
				args[c.Field] = n
			} else {
				args[c.Field] = v
			}
		}
		if message == "" && s.Capability == "system.config.create" {
			key, _ := args["configKey"].(string)
			if system.IsCredentialConfigKey(key) || capability.IsSensitive(key) || containsSecrets(args["configValue"]) {
				message = "凭据类参数不能通过批量导入管理"
			}
		}
		if message == "" {
			parts := make([]any, 0, len(s.Unique))
			for _, k := range s.Unique {
				v := args[k]
				if v == nil {
					v = ""
				}
				parts = append(parts, v)
			}
			keyBytes, _ := json.Marshal(parts)
			key := string(keyBytes)
			if first := unique[key]; first != 0 {
				message = fmt.Sprintf("与第 %d 行的唯一键重复", first)
			} else {
				unique[key] = number
			}
		}
		if message != "" {
			errors = append(errors, RowError{number, message})
			continue
		}
		raw, _ := json.Marshal(args)
		rows = append(rows, inputRow{number, raw})
	}
	if err := it.Error(); err != nil {
		return nil, nil, 0, errcode.BadRequestf("工作表读取失败")
	}
	if total == 0 {
		return nil, nil, 0, errcode.BadRequestf("文件中没有可导入的数据")
	}
	return rows, errors, total, nil
}

func containsSecrets(value any) bool {
	switch v := value.(type) {
	case string:
		var inner any
		if json.Unmarshal([]byte(v), &inner) == nil {
			return containsSecrets(inner)
		}
		return strings.Contains(v, "-----BEGIN ") || strings.HasPrefix(v, "iadm_")
	case map[string]any:
		for k, x := range v {
			if capability.IsSensitive(k) || containsSecrets(x) {
				return true
			}
		}
	case []any:
		for _, x := range v {
			if containsSecrets(x) {
				return true
			}
		}
	}
	return false
}

func workbook(headers []string, rows [][]any) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	sheet := f.GetSheetName(0)
	for i, h := range headers {
		axis, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellStr(sheet, axis, h)
	}
	for r, row := range rows {
		for col, v := range row {
			axis, _ := excelize.CoordinatesToCellName(col+1, r+2)
			_ = f.SetCellStr(sheet, axis, fmt.Sprint(v))
		}
	}
	style, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "FFFFFF"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"1677FF"}, Pattern: 1}})
	last, _ := excelize.CoordinatesToCellName(len(headers), 1)
	// Text columns preserve leading zeros in usernames and phone numbers.
	// Integer columns are parsed explicitly when the completed file is read.
	textStyle, _ := f.NewStyle(&excelize.Style{NumFmt: 49})
	_ = f.SetColStyle(sheet, "A:"+strings.TrimRight(last, "1"), textStyle)
	_ = f.SetCellStyle(sheet, "A1", last, style)
	_ = f.SetColWidth(sheet, "A", strings.TrimRight(last, "1"), 24)
	_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	var b bytes.Buffer
	err := f.Write(&b)
	return b.Bytes(), err
}
