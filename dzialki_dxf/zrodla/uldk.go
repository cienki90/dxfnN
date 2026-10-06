package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Dzialka – działka ewidencyjna pobrana z ULDK.
type Dzialka struct {
	ID, Woj, Powiat, Gmina, Obreb, Numer string
	R2180                                [][]P
	strefy                               map[int]*Area
}

func (d *Dzialka) NrObrebu() string {
	cz := strings.Split(d.ID, ".")
	if len(cz) >= 3 {
		return cz[1]
	}
	return ""
}

func (d *Dzialka) Jednostka() string { return strings.Split(d.ID, ".")[0] }

type wpisCache struct{ d *Dzialka }

type KlientULDK struct {
	URL       string
	hc        *http.Client
	Pauza     time.Duration
	Proby     int
	Zapytan   int
	cache     map[[2]int64]wpisCache
	zamGeom   int // -1 nieznane, 0/1 – czy zamieniać osie w zwracanej geometrii
	Niezgodne int // odpowiedzi, w których działka nie zawierała punktu zapytania
}

const polaULDK = "geom_wkt,id,voivodeship,county,commune,region,parcel"

func nowyKlient(cfg *Cfg) (*KlientULDK, error) {
	proxy, err := funkcjaProxy(cfg.Proxy)
	if err != nil {
		return nil, err
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = proxy
	return &KlientULDK{
		URL:     cfg.URL,
		hc:      &http.Client{Timeout: cfg.Timeout, Transport: tr},
		Pauza:   cfg.Pauza,
		Proby:   4,
		cache:   map[[2]int64]wpisCache{},
		zamGeom: -1,
	}, nil
}

func funkcjaProxy(p string) (func(*http.Request) (*url.URL, error), error) {
	if p != "" {
		if !strings.Contains(p, "://") {
			p = "http://" + p
		}
		u, err := url.Parse(p)
		if err != nil {
			return nil, fmt.Errorf("błędny adres proxy: %v", err)
		}
		return http.ProxyURL(u), nil
	}
	return func(r *http.Request) (*url.URL, error) {
		u, err := http.ProxyFromEnvironment(r)
		if u != nil || err != nil {
			return u, err
		}
		return proxySystemowe(), nil
	}, nil
}

type bladULDK struct{ msg string }

func (b *bladULDK) Error() string { return b.msg }

func (k *KlientULDK) zapytaj(a, b float64) (*Dzialka, error) {
	adres := fmt.Sprintf("%s?request=GetParcelByXY&xy=%.3f,%.3f,2180&result=%s&srid=2180", k.URL, a, b, polaULDK)
	var ostatni error
	for proba := 0; proba < k.Proby; proba++ {
		if proba > 0 {
			time.Sleep(time.Duration(1<<proba) * time.Second)
		}
		if k.Pauza > 0 {
			time.Sleep(k.Pauza)
		}
		k.Zapytan++
		req, _ := http.NewRequest("GET", adres, nil)
		req.Header.Set("User-Agent", "dzialki_z_dxf/2.0")
		resp, err := k.hc.Do(req)
		if err != nil {
			ostatni = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			ostatni = err
			continue
		}
		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			ostatni = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode != 200 {
			return nil, &bladULDK{fmt.Sprintf("ULDK: HTTP %d", resp.StatusCode)}
		}
		return parsujULDK(string(body))
	}
	return nil, &bladULDK{fmt.Sprintf("Brak odpowiedzi z ULDK (%v). Sprawdź połączenie z internetem / proxy.", ostatni)}
}

func parsujULDK(t string) (*Dzialka, error) {
	t = strings.TrimPrefix(t, "\ufeff")
	var linie []string
	for _, l := range strings.Split(t, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			linie = append(linie, l)
		}
	}
	if len(linie) == 0 {
		return nil, nil
	}
	pola0 := strings.Fields(linie[0])
	st, err := strconv.Atoi(pola0[0])
	if err != nil {
		s := linie[0]
		if len(s) > 200 {
			s = s[:200]
		}
		return nil, &bladULDK{"Nieoczekiwana odpowiedź ULDK: " + s}
	}
	if st < 0 || len(linie) < 2 {
		return nil, nil
	}
	pola := strings.Split(linie[1], "|")
	gi := -1
	for i, p := range pola {
		if strings.Contains(strings.ToUpper(p), "POLYGON") {
			gi = i
			break
		}
	}
	if gi < 0 {
		return nil, nil
	}
	w := pola[gi]
	if i := strings.Index(w, ";"); i >= 0 {
		w = w[i+1:]
	}
	ringi, err := parsujWKT(w)
	if err != nil {
		return nil, err
	}
	var reszta []string
	for i, p := range pola {
		if i != gi {
			reszta = append(reszta, strings.TrimSpace(p))
		}
	}
	for len(reszta) < 6 {
		reszta = append(reszta, "")
	}
	return &Dzialka{ID: reszta[0], Woj: reszta[1], Powiat: reszta[2], Gmina: reszta[3], Obreb: reszta[4],
		Numer: reszta[5], R2180: ringi, strefy: map[int]*Area{}}, nil
}

func parsujWKT(s string) ([][]P, error) {
	var ringi [][]P
	start := -1
	for i, ch := range s {
		switch ch {
		case '(':
			start = i + 1
		case ')':
			if start >= 0 {
				var r []P
				for _, para := range strings.Split(s[start:i], ",") {
					f := strings.Fields(para)
					if len(f) < 2 {
						continue
					}
					x, e1 := strconv.ParseFloat(f[0], 64)
					y, e2 := strconv.ParseFloat(f[1], 64)
					if e1 != nil || e2 != nil {
						return nil, errors.New("błędna geometria WKT z ULDK")
					}
					r = append(r, P{x, y})
				}
				ringi = append(ringi, r)
				start = -1
			}
		}
	}
	return ringi, nil
}

func zamienOsie(ringi [][]P) [][]P {
	out := make([][]P, len(ringi))
	for i, r := range ringi {
		out[i] = make([]P, len(r))
		for j, p := range r {
			out[i][j] = P{p.Y, p.X}
		}
	}
	return out
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Dzialka zwraca działkę zawierającą punkt (x, y) w EPSG:2180 (wschód, północ) albo nil.
// Kolejność osi w zapytaniu i odpowiedzi rozpoznawana jest przy pierwszym trafieniu.
func (k *KlientULDK) Dzialka(x, y float64) (*Dzialka, error) {
	klucz := [2]int64{int64(math.Round(x * 100)), int64(math.Round(y * 100))}
	if w, ok := k.cache[klucz]; ok {
		return w.d, nil
	}
	pt := P{x, y}
	// ULDK przyjmuje xy=wschód,północ (tak jak wtyczki QGIS) i zwraca WKT w tej samej kolejności.
	// Kontrola: zwrócona działka musi zawierać punkt; w razie potrzeby zamieniamy osie
	// tylko w geometrii odpowiedzi (miejsce zapytania jest wtedy na pewno właściwe).
	d, err := k.zapytaj(x, y)
	if err != nil {
		return nil, err
	}
	var wynik *Dzialka
	if d != nil {
		warGeom := []bool{false, true}
		if k.zamGeom >= 0 {
			warGeom = []bool{k.zamGeom == 1}
		}
		for _, zg := range warGeom {
			r := d.R2180
			if zg {
				r = zamienOsie(r)
			}
			if ar := nowyArea(r); ar != nil && ar.odleglosc(pt) < 0.5 {
				d.R2180 = r
				k.zamGeom = b2i(zg)
				wynik = d
				break
			}
		}
		if wynik == nil {
			k.Niezgodne++
		}
	}
	k.cache[klucz] = wpisCache{wynik}
	return wynik, nil
}

// Rejestr – pobrane działki (bez powtórzeń) z geometrią w strefach PL-2000.
type Rejestr struct {
	poID  map[string]*Dzialka
	lista []*Dzialka
}

func nowyRejestr() *Rejestr { return &Rejestr{poID: map[string]*Dzialka{}} }

func (r *Rejestr) dodaj(d *Dzialka) *Dzialka {
	klucz := d.ID
	if klucz == "" {
		klucz = fmt.Sprintf("?%v", d.R2180[0][0])
	}
	if ist, ok := r.poID[klucz]; ok {
		return ist
	}
	r.poID[klucz] = d
	r.lista = append(r.lista, d)
	return d
}

func (r *Rejestr) geom(d *Dzialka, strefa int) *Area {
	if a, ok := d.strefy[strefa]; ok {
		return a
	}
	ringi := make([][]P, len(d.R2180))
	for i, rr := range d.R2180 {
		ringi[i] = make([]P, len(rr))
		for j, p := range rr {
			ringi[i][j] = z2180(strefa, p)
		}
	}
	a := nowyArea(ringi)
	d.strefy[strefa] = a
	return a
}

func (r *Rejestr) kandydaci(strefa int, b BBox) []*Dzialka {
	var out []*Dzialka
	for _, d := range r.lista {
		if a := r.geom(d, strefa); a != nil && a.Box.przecina(b, 0.01) {
			out = append(out, d)
		}
	}
	return out
}
