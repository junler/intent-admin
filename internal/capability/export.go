package capability

import (
	"errors"
	"fmt"
)

// Column formats understood by the export renderer.
const (
	FormatDateTime = "datetime"
	FormatDate     = "date"
	FormatMoney    = "money" // integer cents in, yuan out
)

// ExportColumn is one spreadsheet column.
type ExportColumn struct {
	// Field is the key in a row object; dotted paths reach into nested maps.
	Field string
	// Label is the Chinese header text.
	Label string
	Width float64
	// Dict, when set, translates the raw value through that dictionary so the
	// spreadsheet reads "正常" rather than "0" — matching what the page shows.
	Dict string
	// Format applies a value transformation; see the Format* constants.
	Format string
}

// ExportSpec turns a list capability into an exportable one. Declaring it
// generates {path}/export and {path}/template, both reusing this capability's
// filter input and query function — which is what keeps the exported rows and
// the on-screen rows from drifting apart.
type ExportSpec struct {
	SheetName string
	// FileName may contain {date}, replaced at run time.
	FileName string
	// Perm is the permission required to export; empty falls back to Meta.Perm.
	// Convention: {module}:{entity}:export, declared as an F-type menu button.
	Perm string
	// RowsField names the field of the output holding the row slice.
	// Defaults to "rows".
	RowsField string
	Columns   []ExportColumn
}

func (s *ExportSpec) normalize(capName string) error {
	if s == nil {
		return nil
	}
	if len(s.Columns) == 0 {
		return errors.New("Export.Columns 不能为空")
	}
	for i, c := range s.Columns {
		if c.Field == "" || c.Label == "" {
			return fmt.Errorf("Export.Columns[%d] 的 Field 与 Label 都必填", i)
		}
		switch c.Format {
		case "", FormatDateTime, FormatDate, FormatMoney:
		default:
			return fmt.Errorf("Export.Columns[%d] 的 Format %q 不支持", i, c.Format)
		}
	}
	if s.SheetName == "" {
		s.SheetName = "数据"
	}
	if s.FileName == "" {
		s.FileName = capName + "_{date}"
	}
	if s.RowsField == "" {
		s.RowsField = "rows"
	}
	return nil
}

// ExportPerm returns the permission guarding export for this capability.
func (e *Entry) ExportPerm() string {
	if e.Export == nil {
		return e.Perm
	}
	if e.Export.Perm != "" {
		return e.Export.Perm
	}
	return e.Perm
}
