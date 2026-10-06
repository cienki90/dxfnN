package main

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Odczyt pliku DXF (ASCII)
// ---------------------------------------------------------------------------

type Para struct {
	K int
	V string
}

type Ent struct {
	Typ    string
	G      []Para
	Dzieci []*Ent
}

func (e *Ent) s(k int) string {
	for _, p := range e.G {
		if p.K == k {
			return p.V
		}
	}
	return ""
}

func (e *Ent) f(k int, d float64) float64 {
	for _, p := range e.G {
		if p.K == k {
			if v, err := strconv.ParseFloat(strings.TrimSpace(p.V), 64); err == nil {
				return v
			}
			return d
		}
	}
	return d
}

func (e *Ent) i(k int, d int) int {
	return int(e.f(k, float64(d)))
}

func (e *Ent) v3(k int) V3 { return V3{e.f(k, 0), e.f(k+10, 0), e.f(k+20, 0)} }

func (e *Ent) ekstruzja() V3 { return V3{e.f(210, 0), e.f(220, 0), e.f(230, 1)} }

type Blok struct {
	Nazwa string
	Baza  V3
	Ents  []*Ent
}

type Rysunek struct {
	Wersja    string
	Kodowanie string
	Model     []*Ent
	Bloki     map[string]*Blok
}

var reUnicode = regexp.MustCompile(`\\[Uu]\+([0-9A-Fa-f]{4})`)

func dekodujEscape(s string) string {
	if !strings.Contains(s, `\U+`) && !strings.Contains(s, `\u+`) {
		return s
	}
	return reUnicode.ReplaceAllStringFunc(s, func(m string) string {
		v, err := strconv.ParseUint(m[3:], 16, 32)
		if err != nil {
			return m
		}
		return string(rune(v))
	})
}

func czytajDXF(sciezka string) (*Rysunek, error) {
	dane, err := os.ReadFile(sciezka)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(dane, []byte("AutoCAD Binary DXF")) {
		return nil, errors.New("to binarny DXF – zapisz rysunek jako DXF ASCII (tekstowy)")
	}
	dane = bytes.TrimPrefix(dane, []byte("\xEF\xBB\xBF"))
	rys := &Rysunek{Bloki: map[string]*Blok{}}
	var tekst string
	if utf8.Valid(dane) {
		tekst, rys.Kodowanie = string(dane), "UTF-8"
	} else {
		tekst, rys.Kodowanie = dekodujCP1250(dane), "Windows-1250"
	}
	linie := strings.Split(tekst, "\n")

	sekcje := map[string][]*Ent{}
	sekcja, zmienna := "", ""
	poSECTION := false
	var biez *Ent
	zamknij := func() {
		if biez != nil && sekcja != "" {
			sekcje[sekcja] = append(sekcje[sekcja], biez)
		}
		biez = nil
	}
petla:
	for i := 0; i+1 < len(linie); i += 2 {
		kod, err := strconv.Atoi(strings.TrimSpace(linie[i]))
		if err != nil {
			return nil, fmt.Errorf("uszkodzony plik DXF (linia %d: %q)", i+1, strings.TrimSpace(linie[i]))
		}
		w := strings.TrimRight(linie[i+1], "\r")
		if kod != 1 && kod != 3 {
			w = strings.TrimSpace(w)
		}
		w = dekodujEscape(w)
		if poSECTION {
			poSECTION = false
			if kod == 2 {
				sekcja = w
				continue
			}
		}
		if kod == 0 {
			zamknij()
			switch w {
			case "SECTION":
				poSECTION = true
				continue
			case "ENDSEC":
				sekcja = ""
				continue
			case "EOF":
				break petla
			}
			biez = &Ent{Typ: w}
			continue
		}
		if biez != nil {
			biez.G = append(biez.G, Para{kod, w})
		} else if sekcja == "HEADER" {
			if kod == 9 {
				zmienna = w
			} else if zmienna == "$ACADVER" {
				rys.Wersja = w
			}
		}
	}
	zamknij()

	rys.Model = grupuj(sekcje["ENTITIES"])
	var blok *Blok
	for _, e := range grupuj(sekcje["BLOCKS"]) {
		switch e.Typ {
		case "BLOCK":
			blok = &Blok{Nazwa: e.s(2), Baza: e.v3(10)}
		case "ENDBLK":
			if blok != nil {
				rys.Bloki[blok.Nazwa] = blok
			}
			blok = nil
		default:
			if blok != nil {
				blok.Ents = append(blok.Ents, e)
			}
		}
	}
	if len(rys.Model) == 0 && len(sekcje["ENTITIES"]) == 0 {
		return rys, errors.New("plik nie zawiera sekcji ENTITIES – czy to na pewno DXF?")
	}
	return rys, nil
}

// grupuj dołącza VERTEX do POLYLINE oraz ATTRIB do INSERT.
func grupuj(l []*Ent) []*Ent {
	var out []*Ent
	for i := 0; i < len(l); i++ {
		e := l[i]
		if e.Typ == "POLYLINE" || e.Typ == "INSERT" {
			dz := "VERTEX"
			if e.Typ == "INSERT" {
				dz = "ATTRIB"
			}
			j := i + 1
			for j < len(l) && l[j].Typ == dz {
				e.Dzieci = append(e.Dzieci, l[j])
				j++
			}
			if j < len(l) && l[j].Typ == "SEQEND" && (e.Typ == "POLYLINE" || len(e.Dzieci) > 0) {
				j++
			}
			i = j - 1
		}
		out = append(out, e)
	}
	return out
}

// ---------------------------------------------------------------------------
// Geometria 3D pomocnicza i układ OCS
// ---------------------------------------------------------------------------

type V3 struct{ X, Y, Z float64 }

type Xf func(V3) V3

func iloczynV(a, b V3) V3 {
	return V3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}

func normV(a V3) V3 {
	l := math.Sqrt(a.X*a.X + a.Y*a.Y + a.Z*a.Z)
	if l == 0 {
		return a
	}
	return V3{a.X / l, a.Y / l, a.Z / l}
}

// ocs zwraca przekształcenie OCS -> WCS (algorytm "arbitrary axis") lub nil dla (0,0,1).
func ocs(ex V3) Xf {
	if ex.X == 0 && ex.Y == 0 && ex.Z == 0 {
		return nil
	}
	az := normV(ex)
	if math.Abs(az.X) < 1e-12 && math.Abs(az.Y) < 1e-12 && az.Z > 0 {
		return nil
	}
	var ax V3
	if math.Abs(az.X) < 1.0/64 && math.Abs(az.Y) < 1.0/64 {
		ax = iloczynV(V3{0, 1, 0}, az)
	} else {
		ax = iloczynV(V3{0, 0, 1}, az)
	}
	ax = normV(ax)
	ay := iloczynV(az, ax)
	return func(v V3) V3 {
		return V3{
			v.X*ax.X + v.Y*ay.X + v.Z*az.X,
			v.X*ax.Y + v.Y*ay.Y + v.Z*az.Y,
			v.X*ax.Z + v.Y*ay.Z + v.Z*az.Z,
		}
	}
}

// zlozenie: najpierw pierwsze, potem drugie.
func zlozenie(pierwsze, drugie Xf) Xf {
	if pierwsze == nil {
		return drugie
	}
	if drugie == nil {
		return pierwsze
	}
	return func(v V3) V3 { return drugie(pierwsze(v)) }
}

type kontekst struct {
	xf    Xf
	skala float64
	gl    int
}

func (c kontekst) pt(v V3) P {
	if c.xf != nil {
		v = c.xf(v)
	}
	return P{v.X, v.Y}
}

func (c kontekst) z(xf Xf) kontekst {
	return kontekst{zlozenie(xf, c.xf), c.skala, c.gl}
}

// ---------------------------------------------------------------------------
// Aproksymacja łuków i krzywych
// ---------------------------------------------------------------------------

func liczbaSegmentow(r, kat, tol float64) int {
	kat = math.Abs(kat)
	krok := math.Pi / 8
	if r > tol && tol > 0 {
		krok = math.Min(krok, 2*math.Acos(1-tol/r))
	}
	n := int(math.Ceil(kat / krok))
	if n < 1 {
		n = 1
	}
	if n > 4096 {
		n = 4096
	}
	return n
}

// punktyLuku – punkty łuku od kąta a0 o rozwartości kat (rad, ze znakiem), z oboma końcami.
func punktyLuku(c P, r, a0, kat, tol float64) []P {
	n := liczbaSegmentow(r, kat, tol)
	out := make([]P, 0, n+1)
	for i := 0; i <= n; i++ {
		a := a0 + kat*float64(i)/float64(n)
		out = append(out, P{c.X + r*math.Cos(a), c.Y + r*math.Sin(a)})
	}
	return out
}

// lukBulge – punkty segmentu polilinii z wybrzuszeniem (bez punktu początkowego).
func lukBulge(p1, p2 P, b, tol float64) []P {
	d := dist(p1, p2)
	if math.Abs(b) < 1e-12 || d == 0 {
		return []P{p2}
	}
	theta := 4 * math.Atan(b)
	r := d / (2 * math.Abs(math.Sin(theta/2)))
	h := d / 2 / math.Tan(theta/2)
	m := lerp(p1, p2, 0.5)
	dx, dy := (p2.X-p1.X)/d, (p2.Y-p1.Y)/d
	c := P{m.X - dy*h, m.Y + dx*h}
	a0 := math.Atan2(p1.Y-c.Y, p1.X-c.X)
	pts := punktyLuku(c, r, a0, theta, tol)
	pts[len(pts)-1] = p2
	return pts[1:]
}

type wierzcholek struct{ x, y, b float64 }

func punktyPolilinii(vs []wierzcholek, zamknieta bool, z, tol float64) []V3 {
	if len(vs) == 0 {
		return nil
	}
	out := []V3{{vs[0].x, vs[0].y, z}}
	n := len(vs)
	seg := n - 1
	if zamknieta {
		seg = n
	}
	for i := 0; i < seg; i++ {
		a, b := vs[i], vs[(i+1)%n]
		for _, q := range lukBulge(P{a.x, a.y}, P{b.x, b.y}, a.b, tol) {
			out = append(out, V3{q.X, q.Y, z})
		}
	}
	return out
}

// deBoor – punkt krzywej NURBS (współrzędne jednorodne).
func deBoor(t float64, p int, wezly []float64, ctrl [][4]float64) [4]float64 {
	n := len(ctrl)
	k := p
	for k < n-1 && !(t >= wezly[k] && t < wezly[k+1]) {
		k++
	}
	d := make([][4]float64, p+1)
	for j := 0; j <= p; j++ {
		d[j] = ctrl[j+k-p]
	}
	for r := 1; r <= p; r++ {
		for j := p; j >= r; j-- {
			den := wezly[j+1+k-r] - wezly[j+k-p]
			a := 0.0
			if den != 0 {
				a = (t - wezly[j+k-p]) / den
			}
			for c := 0; c < 4; c++ {
				d[j][c] = (1-a)*d[j-1][c] + a*d[j][c]
			}
		}
	}
	return d[p]
}

func punktySplajnu(stopien int, wezly, wagi []float64, ctrl, fit []V3) []V3 {
	if stopien >= 1 && len(ctrl) > stopien && len(wezly) == len(ctrl)+stopien+1 {
		h := make([][4]float64, len(ctrl))
		for i, c := range ctrl {
			w := 1.0
			if len(wagi) == len(ctrl) && wagi[i] > 0 {
				w = wagi[i]
			}
			h[i] = [4]float64{c.X * w, c.Y * w, c.Z * w, w}
		}
		t0, t1 := wezly[stopien], wezly[len(ctrl)]
		n := len(ctrl) * 16
		if n < 32 {
			n = 32
		}
		if n > 8000 {
			n = 8000
		}
		out := make([]V3, 0, n+1)
		for i := 0; i <= n; i++ {
			t := t0 + (t1-t0)*float64(i)/float64(n)
			v := deBoor(t, stopien, wezly, h)
			if v[3] != 0 {
				out = append(out, V3{v[0] / v[3], v[1] / v[3], v[2] / v[3]})
			}
		}
		return out
	}
	if len(fit) >= 2 {
		return fit
	}
	return ctrl
}

// ---------------------------------------------------------------------------
// Konwersja obiektów DXF na geometrię
// ---------------------------------------------------------------------------

var errNieobslugiwany = errors.New("nieobsługiwany typ obiektu")

type Geo struct {
	Linie [][]P
	Ringi [][]P
	Pkt   []P
}

func (g *Geo) pusty() bool { return len(g.Linie)+len(g.Ringi)+len(g.Pkt) == 0 }

type Konwerter struct {
	cfg   *Cfg
	bloki map[string]*Blok
}

func (k *Konwerter) dodajSciezke(g *Geo, pts []P, zamknieta, wypelniona bool) {
	var p2 []P
	for _, p := range pts {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) {
			continue
		}
		if len(p2) == 0 || dist(p, p2[len(p2)-1]) > 1e-9 {
			p2 = append(p2, p)
		}
	}
	if len(p2) >= 3 && dist(p2[0], p2[len(p2)-1]) < 1e-6 {
		zamknieta = true
		p2 = p2[:len(p2)-1]
	}
	switch len(p2) {
	case 0:
		return
	case 1:
		g.Pkt = append(g.Pkt, p2[0])
		return
	}
	if zamknieta && len(p2) >= 3 && (k.cfg.ZamkJakoPow || wypelniona) && math.Abs(poleRing(p2)) > 1e-6 {
		g.Ringi = append(g.Ringi, p2)
		return
	}
	if zamknieta && len(p2) >= 3 {
		p2 = append(p2, p2[0])
	}
	g.Linie = append(g.Linie, p2)
}

func transformuj(c kontekst, vs []V3) []P {
	out := make([]P, len(vs))
	for i, v := range vs {
		out[i] = c.pt(v)
	}
	return out
}

func rad(st float64) float64 { return st * math.Pi / 180 }

func normKat(a float64) float64 {
	for a <= 0 {
		a += 2 * math.Pi
	}
	for a > 2*math.Pi+1e-12 {
		a -= 2 * math.Pi
	}
	return a
}

func (k *Konwerter) geometria(e *Ent, c kontekst, g *Geo) error {
	tol := k.cfg.Dokl / c.skala
	switch e.Typ {
	case "LINE":
		k.dodajSciezke(g, []P{c.pt(e.v3(10)), c.pt(e.v3(11))}, false, false)

	case "POINT":
		g.Pkt = append(g.Pkt, c.pt(e.v3(10)))

	case "LWPOLYLINE":
		var vs []wierzcholek
		for _, p := range e.G {
			v, _ := strconv.ParseFloat(p.V, 64)
			switch p.K {
			case 10:
				vs = append(vs, wierzcholek{x: v})
			case 20:
				if len(vs) > 0 {
					vs[len(vs)-1].y = v
				}
			case 42:
				if len(vs) > 0 {
					vs[len(vs)-1].b = v
				}
			}
		}
		zamk := e.i(70, 0)&1 != 0
		cc := c.z(ocs(e.ekstruzja()))
		k.dodajSciezke(g, transformuj(cc, punktyPolilinii(vs, zamk, e.f(38, 0), tol)), zamk, false)

	case "POLYLINE":
		fl := e.i(70, 0)
		if fl&(16|64) != 0 {
			return errNieobslugiwany // siatki 3D
		}
		zamk := fl&1 != 0
		if fl&8 != 0 { // polilinia 3D – współrzędne WCS
			var pts []P
			for _, v := range e.Dzieci {
				if v.i(70, 0)&16 == 0 {
					pts = append(pts, c.pt(v.v3(10)))
				}
			}
			k.dodajSciezke(g, pts, zamk, false)
			return nil
		}
		var vs []wierzcholek
		for _, v := range e.Dzieci {
			if v.i(70, 0)&16 != 0 {
				continue
			}
			vs = append(vs, wierzcholek{v.f(10, 0), v.f(20, 0), v.f(42, 0)})
		}
		cc := c.z(ocs(e.ekstruzja()))
		k.dodajSciezke(g, transformuj(cc, punktyPolilinii(vs, zamk, e.f(30, 0), tol)), zamk, false)

	case "CIRCLE", "ARC":
		cc := c.z(ocs(e.ekstruzja()))
		s := e.v3(10)
		r := e.f(40, 0)
		a0, kat := 0.0, 2*math.Pi
		if e.Typ == "ARC" {
			a0 = rad(e.f(50, 0))
			kat = normKat(rad(e.f(51, 360)) - a0)
		}
		pts := punktyLuku(P{s.X, s.Y}, r, a0, kat, tol)
		vs := make([]V3, len(pts))
		for i, p := range pts {
			vs[i] = V3{p.X, p.Y, s.Z}
		}
		k.dodajSciezke(g, transformuj(cc, vs), e.Typ == "CIRCLE", false)

	case "ELLIPSE":
		s := e.v3(10)
		m := e.v3(11)
		ratio := e.f(40, 1)
		t0, t1 := e.f(41, 0), e.f(42, 2*math.Pi)
		kat := normKat(t1 - t0)
		mn := iloczynV(normV(e.ekstruzja()), m)
		mn = V3{mn.X * ratio, mn.Y * ratio, mn.Z * ratio}
		r := math.Sqrt(m.X*m.X + m.Y*m.Y + m.Z*m.Z)
		n := liczbaSegmentow(r, kat, tol)
		vs := make([]V3, 0, n+1)
		for i := 0; i <= n; i++ {
			t := t0 + kat*float64(i)/float64(n)
			ct, st := math.Cos(t), math.Sin(t)
			vs = append(vs, V3{s.X + m.X*ct + mn.X*st, s.Y + m.Y*ct + mn.Y*st, s.Z + m.Z*ct + mn.Z*st})
		}
		k.dodajSciezke(g, transformuj(c, vs), kat >= 2*math.Pi-1e-6, false)

	case "SPLINE":
		var wezly, wagi []float64
		var ctrl, fit []V3
		for _, p := range e.G {
			v, _ := strconv.ParseFloat(p.V, 64)
			switch p.K {
			case 40:
				wezly = append(wezly, v)
			case 41:
				wagi = append(wagi, v)
			case 10:
				ctrl = append(ctrl, V3{X: v})
			case 20:
				if len(ctrl) > 0 {
					ctrl[len(ctrl)-1].Y = v
				}
			case 30:
				if len(ctrl) > 0 {
					ctrl[len(ctrl)-1].Z = v
				}
			case 11:
				fit = append(fit, V3{X: v})
			case 21:
				if len(fit) > 0 {
					fit[len(fit)-1].Y = v
				}
			case 31:
				if len(fit) > 0 {
					fit[len(fit)-1].Z = v
				}
			}
		}
		vs := punktySplajnu(e.i(71, 3), wezly, wagi, ctrl, fit)
		k.dodajSciezke(g, transformuj(c, vs), e.i(70, 0)&1 != 0, false)

	case "SOLID", "TRACE", "3DFACE":
		v := []V3{e.v3(10), e.v3(11), e.v3(12), e.v3(13)}
		cc := c
		if e.Typ != "3DFACE" {
			v[2], v[3] = v[3], v[2] // kolejność wierzchołków SOLID: 1,2,4,3
			cc = c.z(ocs(e.ekstruzja()))
		}
		k.dodajSciezke(g, transformuj(cc, v), true, true)

	case "HATCH":
		ringi, err := ringiKreskowania(e, tol)
		if err != nil {
			return err
		}
		cc := c.z(ocs(e.ekstruzja()))
		for _, r := range ringi {
			k.dodajSciezke(g, transformuj(cc, r), true, true)
		}

	case "TEXT":
		g.Pkt = append(g.Pkt, c.z(ocs(e.ekstruzja())).pt(e.v3(10)))

	case "MTEXT":
		g.Pkt = append(g.Pkt, c.pt(e.v3(10)))

	case "INSERT":
		k.wstawienie(e, c, g)

	default:
		return errNieobslugiwany
	}
	return nil
}

func (k *Konwerter) wstawienie(e *Ent, c kontekst, g *Geo) {
	ins := e.v3(10)
	o := ocs(e.ekstruzja())
	blok := k.bloki[e.s(2)]
	if !k.cfg.RozbijBloki || blok == nil || c.gl > 10 {
		g.Pkt = append(g.Pkt, c.z(o).pt(ins))
		return
	}
	sx, sy, sz := e.f(41, 1), e.f(42, 1), e.f(43, 1)
	rot := rad(e.f(50, 0))
	cr, sr := math.Cos(rot), math.Sin(rot)
	kol, wier := e.i(70, 1), e.i(71, 1)
	if kol < 1 {
		kol = 1
	}
	if wier < 1 {
		wier = 1
	}
	dk, dw := e.f(44, 0), e.f(45, 0)
	baza := blok.Baza
	skala := math.Max(math.Abs(sx), math.Abs(sy))
	if skala == 0 {
		skala = 1
	}
	przed := len(g.Linie) + len(g.Ringi) + len(g.Pkt)
	for w := 0; w < wier; w++ {
		for kk := 0; kk < kol; kk++ {
			ox, oy := float64(kk)*dk, float64(w)*dw
			xf := func(v V3) V3 {
				x := (v.X-baza.X)*sx + ox
				y := (v.Y-baza.Y)*sy + oy
				z := (v.Z - baza.Z) * sz
				return V3{ins.X + x*cr - y*sr, ins.Y + x*sr + y*cr, ins.Z + z}
			}
			dz := kontekst{zlozenie(zlozenie(xf, o), c.xf), c.skala * skala, c.gl + 1}
			for _, be := range blok.Ents {
				switch be.Typ {
				case "ATTDEF", "ATTRIB", "TEXT", "MTEXT":
					continue
				}
				_ = k.geometria(be, dz, g)
			}
		}
	}
	if len(g.Linie)+len(g.Ringi)+len(g.Pkt) == przed {
		g.Pkt = append(g.Pkt, c.z(o).pt(ins))
	}
}

// --- HATCH -------------------------------------------------------------------

type kursor struct {
	g []Para
	i int
}

func (k *kursor) podglad() (Para, bool) {
	if k.i < len(k.g) {
		return k.g[k.i], true
	}
	return Para{}, false
}

func (k *kursor) liczba(kod int) (float64, error) {
	if k.i >= len(k.g) {
		return 0, fmt.Errorf("HATCH: nieoczekiwany koniec danych (oczekiwano kodu %d)", kod)
	}
	p := k.g[k.i]
	if p.K != kod {
		return 0, fmt.Errorf("HATCH: oczekiwano kodu %d, jest %d", kod, p.K)
	}
	k.i++
	return strconv.ParseFloat(strings.TrimSpace(p.V), 64)
}

func (k *kursor) calk(kod int) (int, error) {
	v, err := k.liczba(kod)
	return int(v), err
}

func (k *kursor) opcjonalna(kod int) (float64, bool) {
	if p, ok := k.podglad(); ok && p.K == kod {
		v, err := k.liczba(kod)
		return v, err == nil
	}
	return 0, false
}

func ringiKreskowania(e *Ent, tol float64) ([][]V3, error) {
	z := e.f(30, 0)
	k := &kursor{g: e.G}
	for k.i < len(k.g) && k.g[k.i].K != 91 {
		k.i++
	}
	if k.i >= len(k.g) {
		return nil, nil
	}
	nSciezek, err := k.calk(91)
	if err != nil {
		return nil, err
	}
	var wynik [][]V3
	blad := func(err error) ([][]V3, error) { return wynik, err }
	for s := 0; s < nSciezek; s++ {
		fl, err := k.calk(92)
		if err != nil {
			return blad(err)
		}
		var pts []P
		if fl&2 != 0 {
			maBulge, err := k.calk(72)
			if err != nil {
				return blad(err)
			}
			if _, err = k.calk(73); err != nil {
				return blad(err)
			}
			nw, err := k.calk(93)
			if err != nil {
				return blad(err)
			}
			vs := make([]wierzcholek, 0, nw)
			for i := 0; i < nw; i++ {
				x, err1 := k.liczba(10)
				y, err2 := k.liczba(20)
				if err1 != nil || err2 != nil {
					return blad(errors.Join(err1, err2))
				}
				b := 0.0
				if maBulge != 0 {
					b, _ = k.opcjonalna(42)
				}
				vs = append(vs, wierzcholek{x, y, b})
			}
			for _, v := range punktyPolilinii(vs, true, 0, tol) {
				pts = append(pts, P{v.X, v.Y})
			}
		} else {
			nk, err := k.calk(93)
			if err != nil {
				return blad(err)
			}
			for i := 0; i < nk; i++ {
				typ, err := k.calk(72)
				if err != nil {
					return blad(err)
				}
				var nowe []P
				switch typ {
				case 1:
					x1, _ := k.liczba(10)
					y1, _ := k.liczba(20)
					x2, _ := k.liczba(11)
					y2, err := k.liczba(21)
					if err != nil {
						return blad(err)
					}
					nowe = []P{{x1, y1}, {x2, y2}}
				case 2, 3:
					cx, _ := k.liczba(10)
					cy, _ := k.liczba(20)
					var mx, my float64
					if typ == 3 {
						mx, _ = k.liczba(11)
						my, _ = k.liczba(21)
					}
					r, _ := k.liczba(40)
					a0, _ := k.liczba(50)
					a1, _ := k.liczba(51)
					ccw, err := k.calk(73)
					if err != nil {
						return blad(err)
					}
					kat := normKat(rad(a1) - rad(a0))
					if math.Abs(a1-a0) >= 360-1e-9 {
						kat = 2 * math.Pi
					}
					rx := r
					if typ == 3 {
						rx = math.Hypot(mx, my)
					}
					n := liczbaSegmentow(rx, kat, tol)
					for j := 0; j <= n; j++ {
						t := rad(a0) + kat*float64(j)/float64(n)
						if ccw == 0 {
							t = -t
						}
						ct, st := math.Cos(t), math.Sin(t)
						if typ == 2 {
							nowe = append(nowe, P{cx + r*ct, cy + r*st})
						} else {
							nowe = append(nowe, P{cx + mx*ct - my*r*st, cy + my*ct + mx*r*st})
						}
					}
				case 4:
					st, _ := k.calk(94)
					rac, _ := k.calk(73)
					_, _ = k.calk(74)
					nkn, _ := k.calk(95)
					nc, err := k.calk(96)
					if err != nil {
						return blad(err)
					}
					wezly := make([]float64, 0, nkn)
					for j := 0; j < nkn; j++ {
						v, _ := k.liczba(40)
						wezly = append(wezly, v)
					}
					var ctrl []V3
					var wagi []float64
					for j := 0; j < nc; j++ {
						x, _ := k.liczba(10)
						y, err := k.liczba(20)
						if err != nil {
							return blad(err)
						}
						ctrl = append(ctrl, V3{x, y, 0})
						if rac != 0 {
							w, _ := k.opcjonalna(42)
							wagi = append(wagi, w)
						}
					}
					if p, ok := k.podglad(); ok && p.K == 97 {
						nf, _ := k.calk(97)
						for j := 0; j < nf; j++ {
							_, _ = k.liczba(11)
							_, _ = k.liczba(21)
						}
					}
					for {
						p, ok := k.podglad()
						if !ok || (p.K != 12 && p.K != 22 && p.K != 13 && p.K != 23) {
							break
						}
						k.i++
					}
					for _, v := range punktySplajnu(st, wezly, wagi, ctrl, nil) {
						nowe = append(nowe, P{v.X, v.Y})
					}
				default:
					return blad(fmt.Errorf("HATCH: nieznany typ krawędzi %d", typ))
				}
				if len(pts) > 0 && len(nowe) > 0 && dist(pts[len(pts)-1], nowe[0]) < 1e-6 {
					nowe = nowe[1:]
				}
				pts = append(pts, nowe...)
			}
		}
		if p, ok := k.podglad(); ok && p.K == 97 {
			n, _ := k.calk(97)
			for j := 0; j < n; j++ {
				if p, ok := k.podglad(); ok && p.K == 330 {
					k.i++
				}
			}
		}
		r := make([]V3, len(pts))
		for i, p := range pts {
			r[i] = V3{p.X, p.Y, z}
		}
		wynik = append(wynik, r)
	}
	return wynik, nil
}

// ---------------------------------------------------------------------------
// Opisy obiektów
// ---------------------------------------------------------------------------

var (
	reMtextKod   = regexp.MustCompile(`\\[ACFHQTWfhqtwacp][^;\\{}]*;`)
	reMtextProst = regexp.MustCompile(`\\[LlOoKkNn~]`)
)

func czystyTekst(s string) string {
	s = strings.NewReplacer(`\P`, " ", `\X`, " ", "%%c", "Ø", "%%C", "Ø", "%%d", "°", "%%D", "°",
		"%%p", "±", "%%P", "±").Replace(s)
	s = reMtextKod.ReplaceAllString(s, "")
	s = reMtextProst.ReplaceAllString(s, "")
	s = strings.NewReplacer("{", "", "}", "", `\\`, `\`).Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func opisObiektu(e *Ent) string {
	switch e.Typ {
	case "TEXT":
		return czystyTekst(e.s(1))
	case "MTEXT":
		var b strings.Builder
		for _, p := range e.G {
			if p.K == 3 {
				b.WriteString(p.V)
			}
		}
		b.WriteString(e.s(1))
		return czystyTekst(b.String())
	case "INSERT":
		var at []string
		for _, a := range e.Dzieci {
			at = append(at, a.s(2)+"="+czystyTekst(a.s(1)))
		}
		s := "Blok: " + e.s(2)
		if len(at) > 0 {
			s += " (" + strings.Join(at, "; ") + ")"
		}
		return s
	}
	return ""
}
