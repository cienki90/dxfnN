package main

import (
	"archive/zip"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Minimalny zapis pliku .xlsx (Office Open XML) bez zewnętrznych bibliotek.

const (
	stylNaglowek = 1
	stylLiczba2  = 2 // 0.00
	stylLiczba1  = 3 // 0.0
)

type Arkusz struct {
	Nazwa    string
	Naglowki []string
	Wiersze  [][]any
	Formaty  map[int]int // indeks kolumny (od 0) -> styl
	MaxSzer  map[int]float64
}

func nazwaKolumny(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

func xmlEsc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r != 0xFFFE && r != 0xFFFF):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func komorkaTekst(ref, s string, styl int) string {
	st := ""
	if styl > 0 {
		st = fmt.Sprintf(` s="%d"`, styl)
	}
	sp := ""
	if strings.TrimSpace(s) != s {
		sp = ` xml:space="preserve"`
	}
	return fmt.Sprintf(`<c r="%s" t="inlineStr"%s><is><t%s>%s</t></is></c>`, ref, st, sp, xmlEsc(s))
}

func xmlArkusza(a *Arkusz) string {
	var b strings.Builder
	nk := len(a.Naglowki)
	nw := len(a.Wiersze) + 1
	ostatnia := nazwaKolumny(nk-1) + strconv.Itoa(nw)
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	fmt.Fprintf(&b, `<dimension ref="A1:%s"/>`, ostatnia)
	b.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/><selection pane="bottomLeft" activeCell="A2" sqref="A2"/></sheetView></sheetViews>`)
	b.WriteString(`<sheetFormatPr defaultRowHeight="15"/><cols>`)
	for i, n := range a.Naglowki {
		dl := float64(utf8.RuneCountInString(n))
		for j, w := range a.Wiersze {
			if j > 1000 {
				break
			}
			if i < len(w) && w[i] != nil {
				dl = math.Max(dl, float64(utf8.RuneCountInString(tekstWartosci(w[i]))))
			}
		}
		maks := 60.0
		if m, ok := a.MaxSzer[i]; ok {
			maks = m
		}
		sz := math.Min(math.Max(10, dl+2), maks)
		fmt.Fprintf(&b, `<col min="%d" max="%d" width="%.1f" customWidth="1"/>`, i+1, i+1, sz)
	}
	b.WriteString(`</cols><sheetData>`)
	b.WriteString(`<row r="1">`)
	for i, n := range a.Naglowki {
		b.WriteString(komorkaTekst(nazwaKolumny(i)+"1", n, stylNaglowek))
	}
	b.WriteString(`</row>`)
	for j, w := range a.Wiersze {
		r := strconv.Itoa(j + 2)
		fmt.Fprintf(&b, `<row r="%s">`, r)
		for i, v := range w {
			if v == nil {
				continue
			}
			ref := nazwaKolumny(i) + r
			switch x := v.(type) {
			case string:
				if x != "" {
					b.WriteString(komorkaTekst(ref, x, 0))
				}
			default:
				f, ok := liczba(v)
				if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
					continue
				}
				st := ""
				if s, ok := a.Formaty[i]; ok {
					st = fmt.Sprintf(` s="%d"`, s)
				}
				fmt.Fprintf(&b, `<c r="%s"%s><v>%s</v></c>`, ref, st, strconv.FormatFloat(f, 'f', -1, 64))
			}
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)
	if nw > 1 {
		fmt.Fprintf(&b, `<autoFilter ref="A1:%s"/>`, ostatnia)
	}
	b.WriteString(`<pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/></worksheet>`)
	return b.String()
}

func liczba(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	}
	return 0, false
}

func tekstWartosci(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', 2, 64)
	case int:
		return strconv.Itoa(x)
	}
	return ""
}

const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<numFmts count="1"><numFmt numFmtId="164" formatCode="0.0"/></numFmts>` +
	`<fonts count="2"><font><sz val="11"/><name val="Calibri"/><family val="2"/></font><font><b/><sz val="11"/><name val="Calibri"/><family val="2"/></font></fonts>` +
	`<fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>` +
	`<fill><patternFill patternType="solid"><fgColor rgb="FFDDEBF7"/><bgColor indexed="64"/></patternFill></fill></fills>` +
	`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="4"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
	`<xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1" applyAlignment="1"><alignment vertical="center" wrapText="1"/></xf>` +
	`<xf numFmtId="2" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
	`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/></cellXfs>` +
	`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`

func zapiszXLSX(sciezka string, arkusze []*Arkusz) error {
	f, err := os.Create(sciezka)
	if err != nil {
		return err
	}
	z := zip.NewWriter(f)
	pisz := func(nazwa, tresc string) error {
		w, err := z.Create(nazwa)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(tresc))
		return err
	}
	var ct, wbS, wbR, def strings.Builder
	ct.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
		`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	wbR.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i, a := range arkusze {
		n := i + 1
		fmt.Fprintf(&ct, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&wbS, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlEsc(a.Nazwa), n, n)
		fmt.Fprintf(&wbR, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
		if len(a.Wiersze) > 0 {
			fmt.Fprintf(&def, `<definedName name="_xlnm._FilterDatabase" localSheetId="%d" hidden="1">'%s'!$A$1:$%s$%d</definedName>`,
				i, xmlEsc(strings.ReplaceAll(a.Nazwa, "'", "''")), nazwaKolumny(len(a.Naglowki)-1), len(a.Wiersze)+1)
		}
		if err := pisz(fmt.Sprintf("xl/worksheets/sheet%d.xml", n), xmlArkusza(a)); err != nil {
			f.Close()
			return err
		}
	}
	ct.WriteString(`</Types>`)
	fmt.Fprintf(&wbR, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`, len(arkusze)+1)
	wb := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<bookViews><workbookView/></bookViews><sheets>` + wbS.String() + `</sheets>`
	if def.Len() > 0 {
		wb += `<definedNames>` + def.String() + `</definedNames>`
	}
	wb += `</workbook>`
	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`
	for _, e := range []struct{ n, t string }{
		{"[Content_Types].xml", ct.String()}, {"_rels/.rels", rels}, {"xl/workbook.xml", wb},
		{"xl/_rels/workbook.xml.rels", wbR.String()}, {"xl/styles.xml", stylesXML},
	} {
		if err := pisz(e.n, e.t); err != nil {
			f.Close()
			return err
		}
	}
	if err := z.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
