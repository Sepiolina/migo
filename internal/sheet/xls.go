package sheet

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"unicode/utf16"

	"github.com/richardlehane/mscfb"

	"miservice/internal/jsx"
)

// BIFF8 record ids used here.
const (
	recBOF        = 0x0809
	recEOF        = 0x000A
	recContinue   = 0x003C
	recBoundSheet = 0x0085
	recSST        = 0x00FC
	recLabelSST   = 0x00FD
	recLabel      = 0x0204
	recRString    = 0x00D6
	recNumber     = 0x0203
	recRK         = 0x027E
	recMulRK      = 0x00BD
	recFormula    = 0x0006
	recString     = 0x0207
	recBoolErr    = 0x0205
	recDimensions = 0x0200
	recFilePass   = 0x002F
)

type xlsSheetInfo struct {
	name   string
	offset uint32
	typ    byte
}

type xlsBook struct {
	data   []byte
	sheets []xlsSheetInfo
	sst    []string
}

func openXLS(p string) (Workbook, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	doc, err := mscfb.New(f)
	if err != nil {
		return nil, errf("not a valid .xls file: %w", err)
	}
	var data []byte
	var sawPackage bool
	for entry, err := doc.Next(); err == nil; entry, err = doc.Next() {
		switch entry.Name {
		case "Workbook", "Book":
			if data == nil {
				data, err = io.ReadAll(entry)
				if err != nil {
					return nil, errf("corrupt .xls workbook stream: %w", err)
				}
			}
		case "EncryptedPackage":
			sawPackage = true
		}
	}
	if data == nil {
		if sawPackage {
			return nil, errf("the workbook is password-protected; remove the password and upload again")
		}
		return nil, ErrUnsupported
	}
	b := &xlsBook{data: data}
	if err := b.parseGlobals(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *xlsBook) Close() error { return nil }

func (b *xlsBook) SheetNames() []string {
	out := make([]string, len(b.sheets))
	for i, s := range b.sheets {
		out[i] = s.name
	}
	return out
}

// record iterates BIFF records. next returns false at the end of the stream.
type recIter struct {
	d   []byte
	pos int
}

func (r *recIter) next() (id uint16, body []byte, ok bool) {
	if r.pos+4 > len(r.d) {
		return 0, nil, false
	}
	id = binary.LittleEndian.Uint16(r.d[r.pos:])
	n := int(binary.LittleEndian.Uint16(r.d[r.pos+2:]))
	start := r.pos + 4
	end := start + n
	if end > len(r.d) {
		end = len(r.d)
	}
	r.pos = end
	return id, r.d[start:end], true
}

func (b *xlsBook) parseGlobals() error {
	it := &recIter{d: b.data}
	first := true
	depth := 0
	for {
		id, body, ok := it.next()
		if !ok {
			break
		}
		switch id {
		case recBOF:
			if first {
				first = false
				if len(body) < 2 {
					return ErrUnsupported
				}
				if v := binary.LittleEndian.Uint16(body); v != 0x0600 {
					return errf("unsupported .xls version 0x%04X: only Excel 97-2003 (BIFF8) is supported; re-save the file as .xlsx", v)
				}
			}
			depth++
		case recEOF:
			depth--
			if depth <= 0 {
				return nil
			}
		case recFilePass:
			return errf("the workbook is password-protected; remove the password and upload again")
		case recBoundSheet:
			if len(body) < 8 {
				continue
			}
			s := xlsSheetInfo{offset: binary.LittleEndian.Uint32(body), typ: body[5]}
			cch := int(body[6])
			utf := body[7]&1 != 0
			chars := body[8:]
			if utf {
				if len(chars) >= cch*2 {
					u := make([]uint16, cch)
					for i := range u {
						u[i] = binary.LittleEndian.Uint16(chars[i*2:])
					}
					s.name = string(utf16.Decode(u))
				}
			} else if len(chars) >= cch {
				rs := make([]rune, cch)
				for i := range rs {
					rs[i] = rune(chars[i])
				}
				s.name = string(rs)
			}
			b.sheets = append(b.sheets, s)
		case recSST:
			chunks := [][]byte{append([]byte(nil), body...)}
			// gather CONTINUE records that belong to the SST
			for it.pos+4 <= len(it.d) && binary.LittleEndian.Uint16(it.d[it.pos:]) == recContinue {
				_, cb, _ := it.next()
				chunks = append(chunks, cb)
			}
			b.sst = parseSST(chunks)
		}
	}
	return nil
}

// chunkReader reads across CONTINUE record boundaries.
type chunkReader struct {
	chunks [][]byte
	ci, p  int
}

func (c *chunkReader) ensure() bool {
	for c.ci < len(c.chunks) && c.p >= len(c.chunks[c.ci]) {
		c.ci++
		c.p = 0
	}
	return c.ci < len(c.chunks)
}

func (c *chunkReader) u8() (byte, bool) {
	if !c.ensure() {
		return 0, false
	}
	v := c.chunks[c.ci][c.p]
	c.p++
	return v, true
}

func (c *chunkReader) u16() (uint16, bool) {
	a, ok1 := c.u8()
	b, ok2 := c.u8()
	return uint16(a) | uint16(b)<<8, ok1 && ok2
}

func (c *chunkReader) u32() (uint32, bool) {
	a, ok1 := c.u16()
	b, ok2 := c.u16()
	return uint32(a) | uint32(b)<<16, ok1 && ok2
}

func (c *chunkReader) skip(n int) {
	for n > 0 && c.ensure() {
		k := len(c.chunks[c.ci]) - c.p
		if k > n {
			k = n
		}
		c.p += k
		n -= k
	}
}

// xlString reads an XLUnicodeString / XLUnicodeRichExtendedString.
func (c *chunkReader) xlString() (string, bool) {
	cch, ok := c.u16()
	if !ok {
		return "", false
	}
	flags, ok := c.u8()
	if !ok {
		return "", false
	}
	var runs, ext int
	if flags&0x08 != 0 {
		r, _ := c.u16()
		runs = int(r)
	}
	if flags&0x04 != 0 {
		e, _ := c.u32()
		ext = int(e)
	}
	wide := flags&1 != 0
	u := make([]uint16, 0, cch)
	remaining := int(cch)
	for remaining > 0 {
		if c.ci >= len(c.chunks) {
			break
		}
		if c.p >= len(c.chunks[c.ci]) {
			// string continues in the next CONTINUE record, which starts
			// with a fresh "compressed / wide" flag byte
			c.ci++
			c.p = 0
			if c.ci >= len(c.chunks) || len(c.chunks[c.ci]) == 0 {
				break
			}
			wide = c.chunks[c.ci][0]&1 != 0
			c.p = 1
			continue
		}
		avail := len(c.chunks[c.ci]) - c.p
		if wide {
			n := avail / 2
			if n > remaining {
				n = remaining
			}
			if n == 0 {
				break
			}
			for i := 0; i < n; i++ {
				u = append(u, binary.LittleEndian.Uint16(c.chunks[c.ci][c.p+i*2:]))
			}
			c.p += n * 2
			remaining -= n
		} else {
			n := avail
			if n > remaining {
				n = remaining
			}
			for i := 0; i < n; i++ {
				u = append(u, uint16(c.chunks[c.ci][c.p+i]))
			}
			c.p += n
			remaining -= n
		}
	}
	c.skip(runs*4 + ext)
	return string(utf16.Decode(u)), true
}

func parseSST(chunks [][]byte) []string {
	if len(chunks[0]) < 8 {
		return nil
	}
	unique := int(binary.LittleEndian.Uint32(chunks[0][4:]))
	chunks[0] = chunks[0][8:]
	cr := &chunkReader{chunks: chunks}
	out := make([]string, 0, unique)
	for i := 0; i < unique; i++ {
		s, ok := cr.xlString()
		if !ok {
			break
		}
		out = append(out, s)
	}
	return out
}

func rkValue(rk uint32) float64 {
	var v float64
	if rk&2 != 0 {
		v = float64(int32(rk) >> 2)
	} else {
		v = math.Float64frombits(uint64(rk&0xFFFFFFFC) << 32)
	}
	if rk&1 != 0 {
		v /= 100
	}
	return v
}

func (b *xlsBook) Rows(name string) ([][]string, error) {
	var info *xlsSheetInfo
	for i := range b.sheets {
		if b.sheets[i].name == name {
			info = &b.sheets[i]
			break
		}
	}
	if info == nil {
		return nil, errf("sheet %q not found", name)
	}
	if info.typ != 0 { // chart / macro / VB module sheets hold no cells
		return nil, nil
	}
	if int(info.offset) >= len(b.data) {
		return nil, errf("corrupt .xls: bad sheet offset")
	}
	it := &recIter{d: b.data, pos: int(info.offset)}
	var (
		entries     []cellEntry
		dim         *rng
		depth       int
		pendingStr  = -1 // index in entries... set when FORMULA yields a string
		pendRow     int
		pendCol     int
		havePending bool
	)
	_ = pendingStr
	add := func(row, col int, v string) {
		if v != "" {
			entries = append(entries, cellEntry{row, col, v})
		}
	}
	for {
		id, body, ok := it.next()
		if !ok {
			break
		}
		if id == recBOF {
			depth++
			continue
		}
		if id == recEOF {
			depth--
			if depth <= 0 {
				break
			}
			continue
		}
		if depth != 1 {
			continue
		}
		switch id {
		case recDimensions:
			if len(body) >= 12 {
				rw0 := int(binary.LittleEndian.Uint32(body))
				rw1 := int(binary.LittleEndian.Uint32(body[4:]))
				c0 := int(binary.LittleEndian.Uint16(body[8:]))
				c1 := int(binary.LittleEndian.Uint16(body[10:]))
				if rw1 > rw0 && c1 > c0 {
					dim = &rng{rw0, c0, rw1 - 1, c1 - 1}
				}
			}
		case recLabelSST:
			if len(body) >= 10 {
				row := int(binary.LittleEndian.Uint16(body))
				col := int(binary.LittleEndian.Uint16(body[2:]))
				idx := int(binary.LittleEndian.Uint32(body[6:]))
				if idx >= 0 && idx < len(b.sst) {
					add(row, col, b.sst[idx])
				}
			}
		case recLabel, recRString:
			if len(body) >= 8 {
				row := int(binary.LittleEndian.Uint16(body))
				col := int(binary.LittleEndian.Uint16(body[2:]))
				cr := &chunkReader{chunks: [][]byte{body[6:]}}
				if s, ok := cr.xlString(); ok {
					add(row, col, s)
				}
			}
		case recNumber:
			if len(body) >= 14 {
				row := int(binary.LittleEndian.Uint16(body))
				col := int(binary.LittleEndian.Uint16(body[2:]))
				f := math.Float64frombits(binary.LittleEndian.Uint64(body[6:]))
				add(row, col, jsx.NumToString(f))
			}
		case recRK:
			if len(body) >= 10 {
				row := int(binary.LittleEndian.Uint16(body))
				col := int(binary.LittleEndian.Uint16(body[2:]))
				add(row, col, jsx.NumToString(rkValue(binary.LittleEndian.Uint32(body[6:]))))
			}
		case recMulRK:
			if len(body) >= 6 {
				row := int(binary.LittleEndian.Uint16(body))
				col := int(binary.LittleEndian.Uint16(body[2:]))
				n := (len(body) - 6) / 6
				for i := 0; i < n; i++ {
					rk := binary.LittleEndian.Uint32(body[4+i*6+2:])
					add(row, col+i, jsx.NumToString(rkValue(rk)))
				}
			}
		case recBoolErr:
			if len(body) >= 8 {
				row := int(binary.LittleEndian.Uint16(body))
				col := int(binary.LittleEndian.Uint16(body[2:]))
				if body[7] == 0 { // boolean (errors read as blank)
					if body[6] != 0 {
						add(row, col, "true")
					} else {
						add(row, col, "false")
					}
				}
			}
		case recFormula:
			havePending = false
			if len(body) >= 14 {
				row := int(binary.LittleEndian.Uint16(body))
				col := int(binary.LittleEndian.Uint16(body[2:]))
				res := body[6:14]
				if res[6] == 0xFF && res[7] == 0xFF {
					switch res[0] {
					case 0: // string result follows in a STRING record
						havePending, pendRow, pendCol = true, row, col
					case 1:
						if res[2] != 0 {
							add(row, col, "true")
						} else {
							add(row, col, "false")
						}
					}
					// 2 = error, 3 = empty string -> blank
				} else {
					add(row, col, jsx.NumToString(math.Float64frombits(binary.LittleEndian.Uint64(res))))
				}
			}
		case recString:
			if havePending {
				// the STRING record may itself be followed by CONTINUEs
				chunks := [][]byte{body}
				for it.pos+4 <= len(it.d) && binary.LittleEndian.Uint16(it.d[it.pos:]) == recContinue {
					_, cb, _ := it.next()
					chunks = append(chunks, cb)
				}
				cr := &chunkReader{chunks: chunks}
				if s, ok := cr.xlString(); ok {
					add(pendRow, pendCol, s)
				}
				havePending = false
			}
		}
	}
	return buildGrid(entries, dim, true), nil
}
