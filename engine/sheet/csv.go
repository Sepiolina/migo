package sheet

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// CSVSheet is the one sheet name a CSV file has.
const CSVSheet = "Sheet1"

// maxCSVBytes bounds what a CSV reader loads into memory.
const maxCSVBytes = 64 << 20

// csvBook is a CSV or other delimited text file read as a one-sheet workbook.
type csvBook struct{ rows [][]string }

func (b *csvBook) SheetNames() []string { return []string{CSVSheet} }
func (b *csvBook) Rows(name string) ([][]string, error) {
	if name != CSVSheet {
		return nil, errf("a CSV file has one sheet, %q", CSVSheet)
	}
	return b.rows, nil
}
func (b *csvBook) Close() error { return nil }

// looksLikeText reports whether a file prefix is plain text: no NUL bytes,
// which every binary spreadsheet format contains near its start.
func looksLikeText(prefix []byte) bool {
	return len(prefix) > 0 && bytes.IndexByte(prefix, 0) < 0
}

// openCSV reads a delimited text file. The delimiter (comma, semicolon, tab or
// pipe) is the one that splits the first line into the most fields; a UTF-8
// byte order mark is dropped, and text that is not UTF-8 is read as
// Windows-1252, the usual encoding of CSV saved by Excel on Windows.
func openCSV(path string) (Workbook, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCSVBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCSVBytes {
		return nil, errors.New("CSV file exceeds 64 MiB")
	}
	rows, err := ParseCSV(data)
	if err != nil {
		return nil, err
	}
	return &csvBook{rows: rows}, nil
}

// ParseCSV decodes delimited text into rows (see openCSV).
func ParseCSV(data []byte) ([][]string, error) {
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	text := string(data)
	if !utf8.ValidString(text) {
		text = decode1252(data)
	}
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = Delimiter(text)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, errf("CSV: %v", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	// Pad to a rectangle, as spreadsheet sheets are.
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	for i, row := range rows {
		if len(row) < width {
			rows[i] = append(row, make([]string, width-len(row))...)
		}
	}
	return rows, nil
}

// Delimiter picks the separator that splits the first line (outside quotes)
// into the most fields; comma wins ties.
func Delimiter(text string) rune {
	line := text
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		line = text[:i]
	}
	best, bestN := ',', 0
	for _, d := range []rune{',', ';', '\t', '|'} {
		n, quoted := 0, false
		for _, c := range line {
			if c == '"' {
				quoted = !quoted
			} else if c == d && !quoted {
				n++
			}
		}
		if n > bestN {
			best, bestN = d, n
		}
	}
	return best
}

// cp1252 maps bytes 0x80-0x9F; the rest of Windows-1252 equals Latin-1.
var cp1252 = [32]rune{
	'€', 0x81, '‚', 'ƒ', '„', '…', '†', '‡', 'ˆ', '‰', 'Š', '‹', 'Œ', 0x8D, 'Ž', 0x8F,
	0x90, '‘', '’', '“', '”', '•', '–', '—', '˜', '™', 'š', '›', 'œ', 0x9D, 'ž', 'Ÿ',
}

func decode1252(data []byte) string {
	var b strings.Builder
	b.Grow(len(data))
	for _, c := range data {
		switch {
		case c < 0x80:
			b.WriteByte(c)
		case c < 0xA0:
			b.WriteRune(cp1252[c-0x80])
		default:
			b.WriteRune(rune(c))
		}
	}
	return b.String()
}
