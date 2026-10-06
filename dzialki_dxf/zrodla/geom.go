package main

import (
	"math"
	"sort"
)

// P – punkt 2D (X = wschód, Y = północ w układach płaskich).
type P struct{ X, Y float64 }

func (a P) add(b P) P          { return P{a.X + b.X, a.Y + b.Y} }
func (a P) sub(b P) P          { return P{a.X - b.X, a.Y - b.Y} }
func (a P) mul(k float64) P    { return P{a.X * k, a.Y * k} }
func cross(a, b P) float64     { return a.X*b.Y - a.Y*b.X }
func dot(a, b P) float64       { return a.X*b.X + a.Y*b.Y }
func dist(a, b P) float64      { return math.Hypot(a.X-b.X, a.Y-b.Y) }
func lerp(a, b P, t float64) P { return P{a.X + (b.X-a.X)*t, a.Y + (b.Y-a.Y)*t} }

type BBox struct{ MinX, MinY, MaxX, MaxY float64 }

func pustyBox() BBox {
	return BBox{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
}

func (b *BBox) dodaj(p P) {
	b.MinX = math.Min(b.MinX, p.X)
	b.MinY = math.Min(b.MinY, p.Y)
	b.MaxX = math.Max(b.MaxX, p.X)
	b.MaxY = math.Max(b.MaxY, p.Y)
}

func (b *BBox) dodajBox(o BBox) {
	b.dodaj(P{o.MinX, o.MinY})
	b.dodaj(P{o.MaxX, o.MaxY})
}

func (b BBox) pusty() bool { return b.MinX > b.MaxX }

func (b BBox) przecina(o BBox, eps float64) bool {
	return !(b.MaxX+eps < o.MinX || o.MaxX+eps < b.MinX || b.MaxY+eps < o.MinY || o.MaxY+eps < b.MinY)
}

func (b BBox) zawiera(p P, eps float64) bool {
	return p.X >= b.MinX-eps && p.X <= b.MaxX+eps && p.Y >= b.MinY-eps && p.Y <= b.MaxY+eps
}

func (b BBox) srodek() P { return P{(b.MinX + b.MaxX) / 2, (b.MinY + b.MaxY) / 2} }

func boxOdcinka(a, b P) BBox {
	return BBox{math.Min(a.X, b.X), math.Min(a.Y, b.Y), math.Max(a.X, b.X), math.Max(a.Y, b.Y)}
}

// Area – obszar zdefiniowany regułą parzysto-nieparzystą przez zbiór pierścieni
// (pierścienie domknięte niejawnie). Orientacja jest normalizowana: obszar zawsze po lewej.
type Area struct {
	Rings [][]P
	rbox  []BBox
	Box   BBox
}

func poleRing(r []P) float64 {
	if len(r) < 3 {
		return 0
	}
	o := r[0]
	s := 0.0
	for i := 1; i+1 < len(r); i++ {
		s += cross(r[i].sub(o), r[i+1].sub(o))
	}
	return s / 2
}

func punktWRing(p P, r []P) bool {
	in := false
	n := len(r)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		a, b := r[i], r[j]
		if (a.Y > p.Y) != (b.Y > p.Y) {
			x := a.X + (p.Y-a.Y)*(b.X-a.X)/(b.Y-a.Y)
			if p.X < x {
				in = !in
			}
		}
	}
	return in
}

func oczyscRing(r []P) []P {
	var out []P
	for _, p := range r {
		if len(out) == 0 || dist(p, out[len(out)-1]) > 1e-9 {
			out = append(out, p)
		}
	}
	for len(out) > 1 && dist(out[0], out[len(out)-1]) <= 1e-9 {
		out = out[:len(out)-1]
	}
	return out
}

func nowyArea(rings [][]P) *Area {
	a := &Area{Box: pustyBox()}
	for _, r := range rings {
		r = oczyscRing(r)
		if len(r) < 3 || math.Abs(poleRing(r)) < 1e-9 {
			continue
		}
		a.Rings = append(a.Rings, r)
	}
	if len(a.Rings) == 0 {
		return nil
	}
	for i, r := range a.Rings {
		glebokosc := 0
		for j, q := range a.Rings {
			if i != j && punktWRing(r[0], q) {
				glebokosc++
			}
		}
		chceCCW := glebokosc%2 == 0
		if (poleRing(r) > 0) != chceCCW {
			for l, k := 0, len(r)-1; l < k; l, k = l+1, k-1 {
				r[l], r[k] = r[k], r[l]
			}
		}
		b := pustyBox()
		for _, p := range r {
			b.dodaj(p)
		}
		a.rbox = append(a.rbox, b)
		a.Box.dodajBox(b)
	}
	return a
}

func (a *Area) pole() float64 {
	s := 0.0
	for _, r := range a.Rings {
		s += poleRing(r)
	}
	return s
}

func (a *Area) wewnatrz(p P) bool {
	if !a.Box.zawiera(p, 0) {
		return false
	}
	in := false
	for i, r := range a.Rings {
		b := a.rbox[i]
		if p.Y < b.MinY || p.Y > b.MaxY || p.X > b.MaxX {
			continue
		}
		if punktWRing(p, r) {
			in = !in
		}
	}
	return in
}

func odlOdOdcinka(p, a, b P) float64 {
	d := b.sub(a)
	l2 := dot(d, d)
	if l2 == 0 {
		return dist(p, a)
	}
	t := math.Max(0, math.Min(1, dot(p.sub(a), d)/l2))
	return dist(p, lerp(a, b, t))
}

func (a *Area) naBrzegu(p P, eps float64) bool {
	for i, r := range a.Rings {
		if !a.rbox[i].zawiera(p, eps) {
			continue
		}
		n := len(r)
		for k := 0; k < n; k++ {
			c, e := r[k], r[(k+1)%n]
			if p.X < math.Min(c.X, e.X)-eps || p.X > math.Max(c.X, e.X)+eps ||
				p.Y < math.Min(c.Y, e.Y)-eps || p.Y > math.Max(c.Y, e.Y)+eps {
				continue
			}
			if odlOdOdcinka(p, c, e) <= eps {
				return true
			}
		}
	}
	return false
}

// odleglosc punktu od obszaru (0 wewnątrz).
func (a *Area) odleglosc(p P) float64 {
	if a.wewnatrz(p) {
		return 0
	}
	m := math.Inf(1)
	for _, r := range a.Rings {
		n := len(r)
		for k := 0; k < n; k++ {
			m = math.Min(m, odlOdOdcinka(p, r[k], r[(k+1)%n]))
		}
	}
	return m
}

const (
	pozaObszarem = 0
	wObszarze    = 1
	naGranicy    = 2
)

func (a *Area) klasyfikuj(p P, eps float64) int {
	if !a.Box.zawiera(p, eps) {
		return pozaObszarem
	}
	if a.naBrzegu(p, eps) {
		return naGranicy
	}
	if a.wewnatrz(p) {
		return wObszarze
	}
	return pozaObszarem
}

// Iv – fragment odcinka [T0,T1] z wagą: 1 wewnątrz obszaru, 0.5 na jego granicy.
type Iv struct{ T0, T1, W float64 }

func parametryPrzeciec(a, b P, ar *Area, eps float64, out []float64) []float64 {
	d := b.sub(a)
	l2 := dot(d, d)
	if l2 == 0 {
		return out
	}
	sb := boxOdcinka(a, b)
	for ri, r := range ar.Rings {
		if !ar.rbox[ri].przecina(sb, eps) {
			continue
		}
		n := len(r)
		for i := 0; i < n; i++ {
			c, e := r[i], r[(i+1)%n]
			if math.Max(c.X, e.X) < sb.MinX-eps || math.Min(c.X, e.X) > sb.MaxX+eps ||
				math.Max(c.Y, e.Y) < sb.MinY-eps || math.Min(c.Y, e.Y) > sb.MaxY+eps {
				continue
			}
			f := e.sub(c)
			den := cross(d, f)
			if math.Abs(den) > 1e-12*math.Sqrt(l2*dot(f, f)) {
				w := c.sub(a)
				t := cross(w, f) / den
				u := cross(w, d) / den
				if t > 0 && t < 1 && u >= -1e-9 && u <= 1+1e-9 {
					out = append(out, t)
				}
			}
			t := dot(c.sub(a), d) / l2
			if t > 0 && t < 1 && dist(lerp(a, b, t), c) <= eps {
				out = append(out, t)
			}
		}
	}
	return out
}

// przytnijOdcinek zwraca fragmenty odcinka a-b leżące w obszarze (lub na jego granicy).
func przytnijOdcinek(a, b P, ar *Area, eps float64) []Iv {
	if !boxOdcinka(a, b).przecina(ar.Box, eps) {
		return nil
	}
	ts := parametryPrzeciec(a, b, ar, eps, []float64{0, 1})
	sort.Float64s(ts)
	var res []Iv
	for i := 0; i+1 < len(ts); i++ {
		t0, t1 := ts[i], ts[i+1]
		if t1-t0 < 1e-12 {
			continue
		}
		var w float64
		switch ar.klasyfikuj(lerp(a, b, (t0+t1)/2), eps) {
		case wObszarze:
			w = 1
		case naGranicy:
			w = 0.5
		default:
			continue
		}
		if n := len(res); n > 0 && res[n-1].W == w && t0-res[n-1].T1 < 1e-12 {
			res[n-1].T1 = t1
		} else {
			res = append(res, Iv{t0, t1, w})
		}
	}
	return res
}

// polePrzeciecia liczy pole części wspólnej dwóch obszarów z twierdzenia Greena:
// brzeg A∩B to fragmenty brzegu A leżące w B oraz fragmenty brzegu B leżące w A.
// Wspólne fragmenty granic mają wagę 0,5 z każdej strony (zgodne kierunki sumują się,
// przeciwne znoszą).
func polePrzeciecia(a, b *Area, o P, eps float64) float64 {
	if !a.Box.przecina(b.Box, eps) {
		return 0
	}
	s := 0.0
	zbierz := func(x, y *Area) {
		for ri, r := range x.Rings {
			if !x.rbox[ri].przecina(y.Box, eps) {
				continue
			}
			n := len(r)
			for i := 0; i < n; i++ {
				p, q := r[i], r[(i+1)%n]
				for _, iv := range przytnijOdcinek(p, q, y, eps) {
					s += iv.W * cross(lerp(p, q, iv.T0).sub(o), lerp(p, q, iv.T1).sub(o))
				}
			}
		}
	}
	zbierz(a, b)
	zbierz(b, a)
	return s / 2
}

// dlugoscWObszarze – długość polilinii wewnątrz obszaru (fragmenty na granicy liczone w połowie).
func dlugoscWObszarze(l []P, ar *Area, eps float64) float64 {
	s := 0.0
	for i := 0; i+1 < len(l); i++ {
		dl := dist(l[i], l[i+1])
		for _, iv := range przytnijOdcinek(l[i], l[i+1], ar, eps) {
			s += (iv.T1 - iv.T0) * dl * iv.W
		}
	}
	return s
}
