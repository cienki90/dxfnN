package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

const epsGeom = 1e-3 // tolerancja geometryczna [m]

// Obiekt – obiekt z rysunku we współrzędnych PL-2000 (wschód, północ).
type Obiekt struct {
	Lp                         int
	Uchwyt, Typ, Warstwa, Opis string
	Strefa                     int
	Osie                       string
	Linie                      [][]P
	Pow                        *Area
	Pkt                        []P
	Box                        BBox
	Uwagi                      []string
}

func (o *Obiekt) dlugosc() float64 {
	s := 0.0
	for _, l := range o.Linie {
		for i := 0; i+1 < len(l); i++ {
			s += dist(l[i], l[i+1])
		}
	}
	return s
}

func (o *Obiekt) pole() float64 {
	if o.Pow == nil {
		return 0
	}
	return o.Pow.pole()
}

// punktOdniesienia – punkt leżący na obiekcie (do kolumn X/Y w Excelu).
func (o *Obiekt) punktOdniesienia() P {
	if len(o.Pkt) > 0 && len(o.Linie) == 0 && o.Pow == nil {
		return o.Pkt[0]
	}
	if len(o.Linie) > 0 {
		cel := o.dlugosc() / 2
		for _, l := range o.Linie {
			for i := 0; i+1 < len(l); i++ {
				d := dist(l[i], l[i+1])
				if cel <= d && d > 0 {
					return lerp(l[i], l[i+1], cel/d)
				}
				cel -= d
			}
		}
	}
	if o.Pow != nil {
		c := o.Pow.Box.srodek()
		if o.Pow.wewnatrz(c) {
			return c
		}
		return o.Pow.Rings[0][0]
	}
	return o.Box.srodek()
}

type odcinek struct {
	a, b P
	l    float64
	box  BBox
}

// niepokryte zwraca fragmenty [t0,t1] odcinka, na których suma wag pokrycia < 1.
func niepokryte(pokr, blok []Iv) [][2]float64 {
	type zd struct{ t, w float64 }
	zds := []zd{{0, 0}, {1, 0}}
	for _, iv := range pokr {
		zds = append(zds, zd{iv.T0, iv.W}, zd{iv.T1, -iv.W})
	}
	for _, iv := range blok {
		zds = append(zds, zd{iv.T0, 1}, zd{iv.T1, -1})
	}
	sort.Slice(zds, func(i, j int) bool { return zds[i].t < zds[j].t })
	var out [][2]float64
	suma := 0.0
	for i := 0; i+1 < len(zds); i++ {
		suma += zds[i].w
		t0, t1 := zds[i].t, zds[i+1].t
		if t1-t0 < 1e-12 || suma > 0.999 {
			continue
		}
		if n := len(out); n > 0 && t0-out[n-1][1] < 1e-12 {
			out[n-1][1] = t1
		} else {
			out = append(out, [2]float64{t0, t1})
		}
	}
	return out
}

type Analizator struct {
	cfg *Cfg
	kl  *KlientULDK
	rej *Rejestr
}

// znajdzDzialki pobiera kolejne działki, aż cały obiekt zostanie nimi pokryty.
func (an *Analizator) znajdzDzialki(o *Obiekt) ([]*Dzialka, error) {
	var segs []odcinek
	dodajOdc := func(a, b P) {
		if l := dist(a, b); l > 1e-9 {
			segs = append(segs, odcinek{a, b, l, boxOdcinka(a, b)})
		}
	}
	for _, l := range o.Linie {
		for i := 0; i+1 < len(l); i++ {
			dodajOdc(l[i], l[i+1])
		}
	}
	if o.Pow != nil {
		for _, r := range o.Pow.Rings {
			for i := range r {
				dodajOdc(r[i], r[(i+1)%len(r)])
			}
		}
	}
	pokr := make([][]Iv, len(segs))
	blok := make([][]Iv, len(segs))
	znane := map[*Dzialka]bool{}
	var lista []*Dzialka
	dodaj := func(d *Dzialka) bool {
		if znane[d] {
			return false
		}
		a := an.rej.geom(d, o.Strefa)
		if a == nil {
			return false
		}
		znane[d] = true
		lista = append(lista, d)
		for i, s := range segs {
			if s.box.przecina(a.Box, epsGeom) {
				pokr[i] = append(pokr[i], przytnijOdcinek(s.a, s.b, a, epsGeom)...)
			}
		}
		return true
	}
	for _, d := range an.rej.kandydaci(o.Strefa, o.Box) {
		dodaj(d)
	}
	zawiera := func(q P, klasa int) bool {
		for _, d := range lista {
			if k := an.rej.geom(d, o.Strefa).klasyfikuj(q, epsGeom); k == wObszarze || k == klasa {
				return true
			}
		}
		return false
	}
	zap := 0
	pytaj := func(q P) (*Dzialka, error) {
		zap++
		x := do2180(o.Strefa, q)
		d, err := an.kl.Dzialka(x.X, x.Y)
		if err != nil || d == nil {
			return nil, err
		}
		return an.rej.dodaj(d), nil
	}
	limit := an.cfg.MaxZap
	braki := 0.0

	// 1) linie i brzegi powierzchni
	promien := 0.5
	for zap < limit && !przerwano() {
		bi, bt0, bt1, blen := -1, 0.0, 0.0, 0.0
		for i, s := range segs {
			for _, u := range niepokryte(pokr[i], blok[i]) {
				if l := (u[1] - u[0]) * s.l; l > blen {
					bi, bt0, bt1, blen = i, u[0], u[1], l
				}
			}
		}
		if bi < 0 || blen < math.Max(an.cfg.MinDl, 1e-3) {
			break
		}
		s := segs[bi]
		tm := (bt0 + bt1) / 2
		m := lerp(s.a, s.b, tm)
		nrm := P{-(s.b.Y - s.a.Y) / s.l, (s.b.X - s.a.X) / s.l}
		postep, brak := false, false
		for _, q := range []P{m, m.add(nrm.mul(0.05)), m.sub(nrm.mul(0.05))} {
			if zawiera(q, -1) {
				continue
			}
			d, err := pytaj(q)
			if err != nil {
				return lista, err
			}
			if d == nil {
				brak = true
				continue
			}
			if dodaj(d) {
				postep = true
				break
			}
		}
		if postep {
			promien = 0.5
			continue
		}
		w0, w1 := bt0, bt1
		if blen > 1 {
			h := promien / s.l
			w0, w1 = math.Max(bt0, tm-h), math.Min(bt1, tm+h)
			promien = math.Min(promien*2, 50)
		}
		blok[bi] = append(blok[bi], Iv{w0, w1, 1})
		if brak {
			braki += (w1 - w0) * s.l
		}
	}

	// 2) wnętrze powierzchni – działki leżące w całości wewnątrz obiektu
	brakPola := 0.0
	if o.Pow != nil {
		calk := o.Pow.pole()
		tol := math.Max(an.cfg.MinPow, 0.001*calk)
		srodek := o.Box.srodek()
		pokryte := func() float64 {
			s := 0.0
			for _, d := range lista {
				s += polePrzeciecia(o.Pow, an.rej.geom(d, o.Strefa), srodek, epsGeom)
			}
			return s
		}
		var pustePkt []P
		krok := math.Max(0.5, math.Sqrt(calk/2000))
		brakPola = calk - pokryte()
		for przebieg := 0; przebieg < 8 && brakPola > tol && zap < limit && !przerwano(); przebieg++ {
			b := o.Pow.Box
			if (b.MaxX-b.MinX)*(b.MaxY-b.MinY)/(krok*krok) > 400000 || krok < 0.2 {
				break
			}
			for y := b.MinY + krok/2; y < b.MaxY && zap < limit; y += krok {
				for x := b.MinX + krok/2; x < b.MaxX && zap < limit; x += krok {
					q := P{x, y}
					if !o.Pow.wewnatrz(q) || zawiera(q, naGranicy) {
						continue
					}
					blisko := false
					for _, p := range pustePkt {
						if dist(p, q) < 3*krok {
							blisko = true
							break
						}
					}
					if blisko {
						continue
					}
					d, err := pytaj(q)
					if err != nil {
						return lista, err
					}
					if d == nil {
						pustePkt = append(pustePkt, q)
						continue
					}
					dodaj(d)
				}
			}
			brakPola = calk - pokryte()
			krok /= 2
		}
		if brakPola <= tol {
			brakPola = 0
		}
	}

	// 3) punkty
	for _, p := range o.Pkt {
		if zap >= limit || przerwano() {
			break
		}
		if zawiera(p, naGranicy) {
			continue
		}
		d, err := pytaj(p)
		if err != nil {
			return lista, err
		}
		if d == nil {
			o.Uwagi = append(o.Uwagi, fmt.Sprintf("Punkt (%.2f, %.2f) nie leży na żadnej działce z ULDK", p.Y, p.X))
			continue
		}
		dodaj(d)
	}

	if zap >= limit {
		o.Uwagi = append(o.Uwagi, fmt.Sprintf("Osiągnięto limit %d zapytań dla obiektu – wynik może być niepełny (zwiększ -max-zapytan)", limit))
	}
	if braki > 0.1 {
		o.Uwagi = append(o.Uwagi, fmt.Sprintf("Ok. %.1f m obiektu nie leży na żadnej działce z ULDK (brak danych w usłudze)", braki))
	}
	if brakPola > 0 {
		o.Uwagi = append(o.Uwagi, fmt.Sprintf("Ok. %.1f m² obiektu nie pokrywa się z działkami z ULDK", brakPola))
	}
	return lista, nil
}

type Pomiar struct {
	D        *Dzialka
	Dl, Pw   float64
	Npkt     int
	Udzial   float64
	maDl     bool
	maPw     bool
	maUdzial bool
}

func (an *Analizator) zmierz(o *Obiekt, lista []*Dzialka) []Pomiar {
	dlCalk, pwCalk := o.dlugosc(), o.pole()
	srodek := o.Box.srodek()
	var wynik []Pomiar
	for _, d := range lista {
		a := an.rej.geom(d, o.Strefa)
		if a == nil || !a.Box.przecina(o.Box, epsGeom) {
			continue
		}
		var m Pomiar
		m.D = d
		if len(o.Linie) > 0 {
			m.maDl = true
			for _, l := range o.Linie {
				m.Dl += dlugoscWObszarze(l, a, epsGeom)
			}
		}
		if o.Pow != nil {
			m.maPw = true
			m.Pw = math.Max(0, polePrzeciecia(o.Pow, a, srodek, epsGeom))
		}
		for _, p := range o.Pkt {
			if a.klasyfikuj(p, epsGeom) != pozaObszarem {
				m.Npkt++
			}
		}
		if m.Dl < an.cfg.MinDl && m.Pw < an.cfg.MinPow && m.Npkt == 0 {
			continue
		}
		switch {
		case pwCalk > 0:
			m.Udzial, m.maUdzial = 100*m.Pw/pwCalk, true
		case dlCalk > 0:
			m.Udzial, m.maUdzial = 100*m.Dl/dlCalk, true
		case len(o.Pkt) > 0:
			m.Udzial, m.maUdzial = 100*float64(m.Npkt)/float64(len(o.Pkt)), true
		}
		wynik = append(wynik, m)
	}
	sort.Slice(wynik, func(i, j int) bool { return naturalnie(wynik[i].D.ID, wynik[j].D.ID) })
	return wynik
}

// naturalnie porównuje identyfikatory działek z uwzględnieniem liczb (2 < 10/2 < 10/10).
func naturalnie(a, b string) bool {
	ca, cb := kawalki(a), kawalki(b)
	for i := 0; i < len(ca) && i < len(cb); i++ {
		x, y := ca[i], cb[i]
		if x == y {
			continue
		}
		dx, dy := x != "" && unicode.IsDigit(rune(x[0])), y != "" && unicode.IsDigit(rune(y[0]))
		if dx && dy {
			xs, ys := strings.TrimLeft(x, "0"), strings.TrimLeft(y, "0")
			if len(xs) != len(ys) {
				return len(xs) < len(ys)
			}
			if xs != ys {
				return xs < ys
			}
			continue
		}
		return strings.ToLower(x) < strings.ToLower(y)
	}
	return len(ca) < len(cb)
}

func kawalki(s string) []string {
	var out []string
	var b strings.Builder
	cyfra := false
	for i, r := range s {
		c := unicode.IsDigit(r)
		if i > 0 && c != cyfra {
			out = append(out, b.String())
			b.Reset()
		}
		cyfra = c
		b.WriteRune(r)
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}
