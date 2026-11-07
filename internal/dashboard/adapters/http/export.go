package http

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/SebastianP07/wallet-track-core-app/internal/dashboard/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

const (
	csvContentType = "text/csv; charset=utf-8"
	// Without it Excel reads the file as the system's legacy encoding and garbles every accented name.
	utf8BOM    = "\xef\xbb\xbf"
	csvNewline = "\r\n"
	csvHeader  = "kind,name,amount,due_date,status,tag" + csvNewline
	// What makes a spreadsheet treat a cell as a formula, full-width forms included.
	formulaTriggers = "=+-@＝＋－＠"
	cellBreaks      = ";\t\r\n"
)

func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	month, err := kernel.ParseYearMonth(r.URL.Query().Get("period"))
	if err != nil {
		platform.WriteProblem(w, http.StatusBadRequest, "The request is not valid.",
			platform.FieldError{Field: "period", Message: monthMessage})
		return
	}
	rows, err := h.Export.Execute(r.Context(), month)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	headers := w.Header()
	headers.Set("Content-Type", csvContentType)
	// The month has just been parsed, so nothing a client sent can reach the header.
	headers.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="wallet-track-%s.csv"`, month))
	// A failed write means the client is gone; there is nobody left to tell.
	if _, err := w.Write(encodeCSV(rows)); err != nil {
		return
	}
}

func encodeCSV(rows []domain.ExportRow) []byte {
	var out bytes.Buffer
	out.WriteString(utf8BOM)
	out.WriteString(csvHeader)
	for _, row := range rows {
		cells := []string{
			textCell(row.Kind),
			textCell(row.Name),
			strconv.FormatInt(int64(row.Amount), 10),
			textCell(dueDateText(row)),
			textCell(row.Status),
			textCell(row.Tag),
		}
		out.WriteString(strings.Join(cells, ","))
		out.WriteString(csvNewline)
	}
	return out.Bytes()
}

func dueDateText(row domain.ExportRow) string {
	if row.DueDate == nil {
		return ""
	}
	return row.DueDate.Format(dateLayout)
}

func textCell(value string) string {
	return `"` + strings.ReplaceAll(neutralizeFormula(value), `"`, `""`) + `"`
}

// neutralizeFormula makes a spreadsheet read the cell as text. A spreadsheet skips what is blank before a formula
// and may read a leading control character as the start of a new cell, so those count too. Amounts never pass
// here: they are integers the backend wrote, and a negative balance has to stay a number.
func neutralizeFormula(value string) string {
	first, _ := utf8.DecodeRuneInString(value)
	afterBlanks, _ := utf8.DecodeRuneInString(strings.TrimLeftFunc(value, isBlank))
	if unicode.IsControl(first) || strings.ContainsRune(formulaTriggers, afterBlanks) {
		return "'" + neutralizeAfterBreaks(value)
	}
	return neutralizeAfterBreaks(value)
}

// neutralizeAfterBreaks covers a spreadsheet that opens the file splitting on `;`, as Excel does in Colombia. It does
// not see this file's quotes as enclosing a cell, so it takes what follows a break inside a name as a cell of its own.
func neutralizeAfterBreaks(value string) string {
	var out strings.Builder
	afterBreak := false
	for _, r := range value {
		if afterBreak && !isBlank(r) {
			if strings.ContainsRune(formulaTriggers, r) {
				out.WriteRune('\'')
			}
			afterBreak = false
		}
		if strings.ContainsRune(cellBreaks, r) {
			afterBreak = true
		}
		out.WriteRune(r)
	}
	return out.String()
}

func isBlank(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsControl(r)
}
