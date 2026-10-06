package main

import "math"

// Odwzorowanie Gaussa-Krügera (szeregi Krügera) na elipsoidzie GRS80.
// PL-2000 i PL-1992 są oparte na tym samym układzie odniesienia (ETRF/GRS80),
// więc przeliczenie między nimi nie wymaga transformacji datum.

type odwzorowanie struct {
	lon0, k0, e0, n0 float64
}

const (
	grsA = 6378137.0
	grsF = 1 / 298.257222101
)

var (
	tmA, tmE          float64
	alfa, beta, delta [5]float64
	pl1992            = odwzorowanie{19 * math.Pi / 180, 0.9993, 500000, -5300000}
	pl2000            = map[int]odwzorowanie{
		7: {21 * math.Pi / 180, 0.999923, 7500000, 0},
		8: {24 * math.Pi / 180, 0.999923, 8500000, 0},
	}
)

func init() {
	n := grsF / (2 - grsF)
	n2, n3, n4 := n*n, n*n*n, n*n*n*n
	tmA = grsA / (1 + n) * (1 + n2/4 + n4/64)
	tmE = math.Sqrt(grsF * (2 - grsF))
	// współczynniki szeregów Krügera do 4. rzędu (Karney 2011)
	alfa = [5]float64{0,
		n/2 - 2.0/3*n2 + 5.0/16*n3 + 41.0/180*n4,
		13.0/48*n2 - 3.0/5*n3 + 557.0/1440*n4,
		61.0/240*n3 - 103.0/140*n4,
		49561.0 / 161280 * n4}
	beta = [5]float64{0,
		n/2 - 2.0/3*n2 + 37.0/96*n3 - 1.0/360*n4,
		1.0/48*n2 + 1.0/15*n3 - 437.0/1440*n4,
		17.0/480*n3 - 37.0/840*n4,
		4397.0 / 161280 * n4}
	delta = [5]float64{0,
		2*n - 2.0/3*n2 - 2*n3 + 116.0/45*n4,
		7.0/3*n2 - 8.0/5*n3 - 227.0/45*n4,
		56.0/15*n3 - 136.0/35*n4,
		4279.0 / 630 * n4}
}

// naPlaski: szerokość, długość [rad] -> (wschód, północ) [m]
func (o odwzorowanie) naPlaski(lat, lon float64) (float64, float64) {
	s := math.Sin(lat)
	t := math.Sinh(math.Atanh(s) - tmE*math.Atanh(tmE*s))
	dl := lon - o.lon0
	xi := math.Atan2(t, math.Cos(dl))
	eta := math.Atanh(math.Sin(dl) / math.Sqrt(1+t*t))
	e, n := eta, xi
	for j := 1; j <= 4; j++ {
		jj := 2 * float64(j)
		e += alfa[j] * math.Cos(jj*xi) * math.Sinh(jj*eta)
		n += alfa[j] * math.Sin(jj*xi) * math.Cosh(jj*eta)
	}
	return o.e0 + o.k0*tmA*e, o.n0 + o.k0*tmA*n
}

// naElipsoide: (wschód, północ) [m] -> szerokość, długość [rad]
func (o odwzorowanie) naElipsoide(e, n float64) (float64, float64) {
	xi := (n - o.n0) / (o.k0 * tmA)
	eta := (e - o.e0) / (o.k0 * tmA)
	xp, ep := xi, eta
	for j := 1; j <= 4; j++ {
		jj := 2 * float64(j)
		xp -= beta[j] * math.Sin(jj*xi) * math.Cosh(jj*eta)
		ep -= beta[j] * math.Cos(jj*xi) * math.Sinh(jj*eta)
	}
	chi := math.Asin(math.Sin(xp) / math.Cosh(ep))
	lat := chi
	for j := 1; j <= 4; j++ {
		lat += delta[j] * math.Sin(2*float64(j)*chi)
	}
	return lat, o.lon0 + math.Atan2(math.Sinh(ep), math.Cos(xp))
}

// do2180 przelicza punkt (wschód, północ) z PL-2000 danej strefy do PL-1992.
func do2180(strefa int, p P) P {
	lat, lon := pl2000[strefa].naElipsoide(p.X, p.Y)
	x, y := pl1992.naPlaski(lat, lon)
	return P{x, y}
}

// z2180 przelicza punkt z PL-1992 do PL-2000 danej strefy.
func z2180(strefa int, p P) P {
	lat, lon := pl1992.naElipsoide(p.X, p.Y)
	x, y := pl2000[strefa].naPlaski(lat, lon)
	return P{x, y}
}

// Zakresy współrzędnych PL-2000 dla Polski (z zapasem).
const (
	nMin, nMax = 5_350_000.0, 6_150_000.0
	eLokMin    = 300_000.0
	eLokMax    = 700_000.0
)

// wykryjUklad zwraca strefę i konwencję osi albo strefa=0, gdy współrzędne nie pasują.
//
//	"cad": X rysunku = wschód (7xxxxxx/8xxxxxx), Y rysunku = północ
//	"geo": X rysunku = północ, Y rysunku = wschód
func wykryjUklad(x, y float64, wymStrefa int, wymOsie string) (int, string) {
	for _, w := range []struct {
		osie string
		e, n float64
	}{{"cad", x, y}, {"geo", y, x}} {
		if wymOsie != "auto" && wymOsie != w.osie {
			continue
		}
		if w.n < nMin || w.n > nMax {
			continue
		}
		s := int(math.Floor(w.e / 1e6))
		if _, ok := pl2000[s]; !ok {
			continue
		}
		if wymStrefa != 0 && s != wymStrefa {
			continue
		}
		lok := w.e - float64(s)*1e6
		if lok < eLokMin || lok > eLokMax {
			continue
		}
		return s, w.osie
	}
	return 0, ""
}
