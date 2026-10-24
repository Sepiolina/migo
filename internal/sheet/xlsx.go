package sheet

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"

	"miservice/internal/jsx"
)

type xlsxBook struct {
	zr     *zip.ReadCloser
	files  map[string]*zip.File
	names  []string
	parts  map[string]string // sheet name -> zip path
	sst    []string
	sstSet bool
}

func openXLSX(p string) (Workbook, error) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, errf("not a valid .xlsx file: %w", err)
	}
	b := &xlsxBook{zr: zr, files: map[string]*zip.File{}, parts: map[string]string{}}
	for _, f := range zr.File {
		b.files[strings.ToLower(f.Name)] = f
	}
	if err := b.readWorkbook(); err != nil {
		zr.Close()
		return nil, err
	}
	return b, nil
}

func (b *xlsxBook) Close() error { return b.zr.Close() }

func (b *xlsxBook) SheetNames() []string { return b.names }

func (b *xlsxBook) open(name string) (io.ReadCloser, error) {
	f, ok := b.files[strings.ToLower(name)]
	if !ok {
		return nil, errf("corrupt .xlsx: missing part %s", name)
	}
	return f.Open()
}

func (b *xlsxBook) readWorkbook() error {
	// relationships: rId -> target
	rels := map[string]string{}
	if rc, err := b.open("xl/_rels/workbook.xml.rels"); err == nil {
		dec := xml.NewDecoder(rc)
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "Relationship" {
				var id, target string
				for _, a := range se.Attr {
					switch a.Name.Local {
					case "Id":
						id = a.Value
					case "Target":
						target = a.Value
					}
				}
				rels[id] = target
			}
		}
		rc.Close()
	}
	rc, err := b.open("xl/workbook.xml")
	if err != nil {
		return errf("not a valid .xlsx workbook (no xl/workbook.xml)")
	}
	defer rc.Close()
	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errf("corrupt .xlsx workbook.xml: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "sheet" {
			continue
		}
		var name, rid string
		for _, a := range se.Attr {
			switch {
			case a.Name.Local == "name":
				name = a.Value
			case a.Name.Local == "id" && a.Name.Space != "":
				rid = a.Value
			}
		}
		target := rels[rid]
		if target == "" {
			continue
		}
		var zp string
		if strings.HasPrefix(target, "/") {
			zp = strings.TrimPrefix(target, "/")
		} else {
			zp = path.Join("xl", target)
		}
		b.names = append(b.names, name)
		b.parts[name] = zp
	}
	return nil
}

var xmlEscRe = regexp.MustCompile(`_x([0-9A-Fa-f]{4})_`)

// unescapeX turns OOXML "_x000D_" escapes back into characters (SheetJS does
// the same for every string it reads).
func unescapeX(s string) string {
	if !strings.Contains(s, "_x") {
		return s
	}
	return xmlEscRe.ReplaceAllStringFunc(s, func(m string) string {
		n, err := strconv.ParseUint(m[2:6], 16, 32)
		if err != nil {
			return m
		}
		return string(rune(n))
	})
}

func (b *xlsxBook) loadSST() error {
	if b.sstSet {
		return nil
	}
	b.sstSet = true
	rc, err := b.open("xl/sharedStrings.xml")
	if err != nil {
		return nil // optional part
	}
	defer rc.Close()
	dec := xml.NewDecoder(rc)
	var cur strings.Builder
	inSI, inT, rph := false, false, 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errf("corrupt .xlsx sharedStrings.xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inSI = true
				cur.Reset()
			case "rPh":
				rph++
			case "t":
				if inSI && rph == 0 {
					inT = true
				}
			}
		case xml.CharData:
			if inT {
				cur.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inT = false
			case "rPh":
				rph--
			case "si":
				b.sst = append(b.sst, unescapeX(cur.String()))
				inSI = false
			}
		}
	}
	return nil
}

// splitRef parses "BA12" into 0-based (row, col).
func splitRef(ref string) (row, col int, ok bool) {
	i := 0
	for i < len(ref) && ((ref[i] >= 'A' && ref[i] <= 'Z') || (ref[i] >= 'a' && ref[i] <= 'z')) {
		c := ref[i]
		if c >= 'a' {
			c -= 32
		}
		col = col*26 + int(c-'A') + 1
		i++
	}
	if i == 0 || i == len(ref) {
		return 0, 0, false
	}
	n, err := strconv.Atoi(ref[i:])
	if err != nil || n < 1 {
		return 0, 0, false
	}
	return n - 1, col - 1, true
}

func parseDim(ref string) *rng {
	a, b, found := strings.Cut(ref, ":")
	r0, c0, ok := splitRef(a)
	if !ok {
		return nil
	}
	if !found {
		return &rng{r0, c0, r0, c0}
	}
	r1, c1, ok := splitRef(b)
	if !ok {
		return nil
	}
	return &rng{r0, c0, r1, c1}
}

func (b *xlsxBook) Rows(name string) ([][]string, error) {
	zp, ok := b.parts[name]
	if !ok {
		return nil, errf("sheet %q not found", name)
	}
	if err := b.loadSST(); err != nil {
		return nil, err
	}
	rc, err := b.open(zp)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var (
		entries      []cellEntry
		dim          *rng
		curRow       = -1
		curCol       = -1
		cellRow      int
		cellCol      int
		cellType     string
		inCell       bool
		inV, inIST   bool // <v> / <is><t>
		inIS         bool
		vText, isTxt strings.Builder
	)
	dec := xml.NewDecoder(rc)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errf("corrupt sheet xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dimension":
				for _, a := range t.Attr {
					if a.Name.Local == "ref" {
						dim = parseDim(a.Value)
					}
				}
			case "row":
				curRow++
				for _, a := range t.Attr {
					if a.Name.Local == "r" {
						if n, err := strconv.Atoi(a.Value); err == nil && n >= 1 {
							curRow = n - 1
						}
					}
				}
				curCol = -1
			case "c":
				inCell = true
				cellType = ""
				vText.Reset()
				isTxt.Reset()
				cellRow, cellCol = curRow, curCol+1
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "r":
						if r, c, ok := splitRef(a.Value); ok {
							cellRow, cellCol = r, c
						}
					case "t":
						cellType = a.Value
					}
				}
				curCol = cellCol
			case "v":
				if inCell {
					inV = true
				}
			case "is":
				if inCell {
					inIS = true
				}
			case "t":
				if inCell && inIS {
					inIST = true
				}
			}
		case xml.CharData:
			if inV {
				vText.Write(t)
			} else if inIST {
				isTxt.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "t":
				inIST = false
			case "is":
				inIS = false
			case "c":
				if inCell {
					inCell = false
					if v, ok := b.cellValue(cellType, vText.String(), isTxt.String()); ok && v != "" {
						entries = append(entries, cellEntry{cellRow, cellCol, v})
					}
				}
			}
		}
	}
	return buildGrid(entries, dim, false), nil
}

// cellValue applies SheetJS' per-type decoding and then String().
func (b *xlsxBook) cellValue(t, v, inline string) (string, bool) {
	switch t {
	case "inlineStr":
		return unescapeX(inline), true
	case "s":
		if v == "" {
			return "", false
		}
		idx, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || idx < 0 || idx >= len(b.sst) {
			return "", false
		}
		return b.sst[idx], true
	case "str":
		return unescapeX(v), true
	case "b":
		s := strings.TrimSpace(v)
		if s == "" {
			return "", false
		}
		if s == "1" || s == "true" {
			return "true", true
		}
		return "false", true
	case "e":
		return "", false
	case "d":
		return v, true
	default: // "n" or absent
		s := strings.TrimSpace(v)
		if s == "" {
			return "", false
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
				return jsx.NumToString(f), true
			}
			return v, true
		}
		return jsx.NumToString(f), true
	}
}
