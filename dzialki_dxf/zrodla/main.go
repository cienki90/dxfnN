// dzialki_z_dxf – sprawdza, na jakich działkach ewidencyjnych leżą obiekty z pliku DXF
// (układ PL-2000 strefa 7 – EPSG:2178 lub strefa 8 – EPSG:2179) i zapisuje zestawienie
// do pliku Excel (.xlsx). Dane o działkach pobierane są z usługi ULDK GUGiK.
//
// Program to pojedynczy plik .exe – nie wymaga instalowania Pythona ani innych programów.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const wersja = "2.0"

type Cfg struct {
	Strefa       int
	Osie         string
	Warstwy      []string
	PominWarstwy []string
	RozbijBloki  bool
	ZamkJakoPow  bool
	Dokl         float64
	MinDl        float64
	MinPow       float64
	MaxZap       int
	Pauza        time.Duration
	Timeout      time.Duration
	Proxy        string
	URL          string
}

var flagaPrzerwania atomic.Bool

func przerwano() bool { return flagaPrzerwania.Load() }

func main() { os.Exit(uruchom(os.Args[1:])) }

func listaWzorcow(s string) []string {
	var out []string
	for _, w := range strings.Split(s, ",") {
		if w = strings.TrimSpace(w); w != "" {
			out = append(out, strings.ToLower(w))
		}
	}
	return out
}

func pasuje(warstwa string, wzorce []string) bool {
	w := strings.ToLower(warstwa)
	for _, p := range wzorce {
		if ok, _ := path.Match(p, w); ok {
			return true
		}
	}
	return false
}

func uruchom(args []string) int {
	fs := flag.NewFlagSet("dzialki_z_dxf", flag.ContinueOnError)
	wynik := fs.String("o", "", "plik wynikowy .xlsx (domyślnie <nazwa_dxf>_dzialki.xlsx obok pliku DXF)")
	strefa := fs.String("strefa", "auto", "strefa układu 2000: auto, 7 lub 8")
	osie := fs.String("osie", "auto", "auto, cad (X rysunku = Y geodezyjne 7xxxxxx) lub geo (X rysunku = X geodezyjne 5xxxxxx)")
	warstwy := fs.String("warstwy", "", "analizuj tylko te warstwy, rozdzielone przecinkami (można używać * i ?), np. \"KAN*,WOD*\"")
	pomin := fs.String("pomin-warstwy", "", "pomiń te warstwy, rozdzielone przecinkami (można używać * i ?)")
	rozbij := fs.Bool("rozbij-bloki", false, "analizuj pełną geometrię bloków (domyślnie tylko punkt wstawienia)")
	jakoLinie := fs.Bool("zamkniete-jako-linie", false, "zamknięte polilinie i okręgi traktuj jak linie, a nie powierzchnie")
	dokl := fs.Float64("dokladnosc-lukow", 0.05, "maksymalna odchyłka aproksymacji łuków [m]")
	minDl := fs.Float64("min-dlugosc", 0.01, "pomijaj działki, w których obiekt ma mniej niż tyle metrów")
	minPow := fs.Float64("min-powierzchnia", 0.01, "pomijaj działki, w których obiekt ma mniej niż tyle m²")
	maxZap := fs.Int("max-zapytan", 500, "maksymalna liczba zapytań do ULDK na jeden obiekt")
	pauza := fs.Float64("pauza", 0, "pauza między zapytaniami do ULDK [s]")
	timeout := fs.Float64("timeout", 30, "limit czasu jednego zapytania do ULDK [s]")
	proxy := fs.String("proxy", "", "serwer proxy, np. http://proxy.firma.pl:8080 (domyślnie z ustawień Windows)")
	adres := fs.String("uldk", "https://uldk.gugik.gov.pl/", "adres usługi ULDK")
	fs.Usage = func() {
		o := fs.Output()
		fmt.Fprintf(o, "dzialki_z_dxf %s – działki ewidencyjne dla obiektów z pliku DXF (PL-2000 strefa 7/8)\n\n", wersja)
		fmt.Fprintln(o, "Użycie:")
		fmt.Fprintln(o, "  dzialki_z_dxf.exe                       (otworzy okno wyboru pliku)")
		fmt.Fprintln(o, "  dzialki_z_dxf.exe projekt.dxf [opcje]")
		fmt.Fprintln(o, "  lub przeciągnij plik DXF na ikonę programu.")
		fmt.Fprintln(o, "\nOpcje:")
		fs.PrintDefaults()
	}

	var pliki []string
	reszta := args
	for {
		if err := fs.Parse(reszta); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		if fs.NArg() == 0 {
			break
		}
		pliki = append(pliki, fs.Arg(0))
		reszta = fs.Args()[1:]
	}

	cfg := &Cfg{
		Osie: *osie, Warstwy: listaWzorcow(*warstwy), PominWarstwy: listaWzorcow(*pomin),
		RozbijBloki: *rozbij, ZamkJakoPow: !*jakoLinie, Dokl: *dokl, MinDl: *minDl, MinPow: *minPow,
		MaxZap: *maxZap, Pauza: time.Duration(*pauza * float64(time.Second)),
		Timeout: time.Duration(*timeout * float64(time.Second)), Proxy: *proxy, URL: *adres,
	}
	switch *strefa {
	case "auto":
	case "7", "8":
		cfg.Strefa = int((*strefa)[0] - '0')
	default:
		fmt.Fprintln(os.Stderr, "Błąd: -strefa musi mieć wartość auto, 7 lub 8")
		return 2
	}
	if cfg.Osie != "auto" && cfg.Osie != "cad" && cfg.Osie != "geo" {
		fmt.Fprintln(os.Stderr, "Błąd: -osie musi mieć wartość auto, cad lub geo")
		return 2
	}
	if cfg.Dokl <= 0 {
		cfg.Dokl = 0.05
	}

	interaktywnie := wlasnaKonsola()
	if interaktywnie {
		defer func() {
			fmt.Print("\nNaciśnij Enter, aby zamknąć okno...")
			_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		}()
	}

	if len(pliki) == 0 {
		pliki = wybierzPlikiOknem()
	}
	if len(pliki) == 0 {
		fmt.Print("Podaj ścieżkę do pliku DXF (lub przeciągnij go tutaj) i naciśnij Enter: ")
		l, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		l = strings.Trim(strings.TrimSpace(l), `"'`)
		if l == "" {
			fmt.Println("Nie podano pliku DXF.")
			return 1
		}
		pliki = []string{l}
	}
	if *wynik != "" && len(pliki) > 1 {
		fmt.Println("Uwaga: opcja -o jest ignorowana przy kilku plikach DXF.")
		*wynik = ""
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		for range sig {
			if przerwano() {
				os.Exit(130)
			}
			flagaPrzerwania.Store(true)
			fmt.Println("\nPrzerywanie – zapisuję dotychczasowe wyniki (naciśnij Ctrl+C ponownie, aby wyjść natychmiast)...")
		}
	}()

	kod := 0
	for _, p := range pliki {
		if err := przetworz(p, *wynik, cfg); err != nil {
			fmt.Printf("\nBŁĄD (%s): %v\n", filepath.Base(p), err)
			kod = 1
		}
		if przerwano() {
			break
		}
	}
	return kod
}

type problem struct{ uchwyt, typ, warstwa, opis string }

type wynikObiektu struct {
	o       *Obiekt
	pomiary []Pomiar
}

func wczytajObiekty(rys *Rysunek, cfg *Cfg) ([]*Obiekt, []problem, map[string]int, map[string]int) {
	kon := &Konwerter{cfg: cfg, bloki: rys.Bloki}
	var obiekty []*Obiekt
	var problemy []problem
	pominiete := map[string]int{}
	uklady := map[string]int{}
	for _, e := range rys.Model {
		if e.i(67, 0) == 1 { // obszar papieru
			continue
		}
		warstwa := e.s(8)
		if warstwa == "" {
			warstwa = "0"
		}
		uchwyt := e.s(5)
		if len(cfg.Warstwy) > 0 && !pasuje(warstwa, cfg.Warstwy) {
			continue
		}
		if pasuje(warstwa, cfg.PominWarstwy) {
			continue
		}
		var g Geo
		err := kon.geometria(e, kontekst{skala: 1}, &g)
		if errors.Is(err, errNieobslugiwany) {
			pominiete[e.Typ]++
			continue
		}
		if err != nil {
			problemy = append(problemy, problem{uchwyt, e.Typ, warstwa, "Błąd odczytu geometrii: " + err.Error()})
			continue
		}
		if g.pusty() {
			problemy = append(problemy, problem{uchwyt, e.Typ, warstwa, "Pusta geometria"})
			continue
		}
		box := pustyBox()
		for _, l := range g.Linie {
			for _, p := range l {
				box.dodaj(p)
			}
		}
		for _, r := range g.Ringi {
			for _, p := range r {
				box.dodaj(p)
			}
		}
		for _, p := range g.Pkt {
			box.dodaj(p)
		}
		c := box.srodek()
		strefa, osie := wykryjUklad(c.X, c.Y, cfg.Strefa, cfg.Osie)
		if strefa == 0 {
			s := "7/8"
			if cfg.Strefa != 0 {
				s = fmt.Sprint(cfg.Strefa)
			}
			problemy = append(problemy, problem{uchwyt, e.Typ, warstwa,
				fmt.Sprintf("Współrzędne (%.2f, %.2f) nie pasują do układu 2000 strefa %s – pominięto", c.X, c.Y, s)})
			continue
		}
		if osie == "geo" {
			zam := func(l []P) {
				for i, p := range l {
					l[i] = P{p.Y, p.X}
				}
			}
			for _, l := range g.Linie {
				zam(l)
			}
			for _, r := range g.Ringi {
				zam(r)
			}
			zam(g.Pkt)
			box = BBox{box.MinY, box.MinX, box.MaxY, box.MaxX}
		}
		o := &Obiekt{Lp: len(obiekty) + 1, Uchwyt: uchwyt, Typ: e.Typ, Warstwa: warstwa, Opis: opisObiektu(e),
			Strefa: strefa, Osie: osie, Linie: g.Linie, Pkt: g.Pkt, Box: box}
		if len(g.Ringi) > 0 {
			o.Pow = nowyArea(g.Ringi)
		}
		if o.Pow == nil && len(o.Linie) == 0 && len(o.Pkt) == 0 {
			problemy = append(problemy, problem{uchwyt, e.Typ, warstwa, "Pusta geometria"})
			continue
		}
		uklady[fmt.Sprintf("2000/%d (osie %s)", strefa, osie)]++
		obiekty = append(obiekty, o)
	}
	return obiekty, problemy, pominiete, uklady
}

func opisSlownika(m map[string]int) string {
	var k []string
	for n := range m {
		k = append(k, n)
	}
	sort.Strings(k)
	var cz []string
	for _, n := range k {
		cz = append(cz, fmt.Sprintf("%s: %d", n, m[n]))
	}
	if len(cz) == 0 {
		return "-"
	}
	return strings.Join(cz, ", ")
}

func przetworz(plikDXF, plikWyn string, cfg *Cfg) error {
	t0 := time.Now()
	abs, _ := filepath.Abs(plikDXF)
	fmt.Printf("\nWczytywanie %s ...\n", abs)
	rys, err := czytajDXF(plikDXF)
	if err != nil {
		return err
	}
	obiekty, problemy, pominiete, uklady := wczytajObiekty(rys, cfg)
	for u, n := range uklady {
		fmt.Printf("  układ %s: %d obiektów\n", u, n)
	}
	if len(pominiete) > 0 {
		fmt.Printf("  pominięte typy: %s\n", opisSlownika(pominiete))
	}
	fmt.Printf("Obiektów do analizy: %d, problemów: %d\n", len(obiekty), len(problemy))
	if len(obiekty) == 0 && len(problemy) > 0 {
		fmt.Println("UWAGA: żaden obiekt nie ma współrzędnych w układzie 2000 strefa 7/8 – sprawdź arkusz „Problemy”.")
	}

	kl, err := nowyKlient(cfg)
	if err != nil {
		return err
	}
	an := &Analizator{cfg: cfg, kl: kl, rej: nowyRejestr()}
	var wyniki []wynikObiektu
	for i, o := range obiekty {
		if przerwano() {
			break
		}
		lista, err := an.znajdzDzialki(o)
		if err != nil {
			var bu *bladULDK
			if errors.As(err, &bu) && len(an.rej.lista) == 0 {
				return err // usługa niedostępna od początku – nie ma sensu kontynuować
			}
			problemy = append(problemy, problem{o.Uchwyt, o.Typ, o.Warstwa, err.Error()})
			fmt.Printf("[%d/%d] %s %s: %v\n", i+1, len(obiekty), o.Typ, o.Uchwyt, err)
			continue
		}
		pom := an.zmierz(o, lista)
		wyniki = append(wyniki, wynikObiektu{o, pom})
		var opis []string
		for _, m := range pom {
			ob := m.D.Obreb
			if ob == "" {
				ob = m.D.NrObrebu()
			}
			opis = append(opis, fmt.Sprintf("%s dz. %s", ob, m.D.Numer))
		}
		s := strings.Join(opis, ", ")
		if s == "" {
			s = "brak działki"
		}
		if len(s) > 160 {
			s = fmt.Sprintf("%d działek", len(pom))
		}
		fmt.Printf("[%d/%d] %-10s %-6s %-20s -> %s\n", i+1, len(obiekty), o.Typ, o.Uchwyt, o.Warstwa, s)
	}

	if plikWyn == "" {
		plikWyn = strings.TrimSuffix(plikDXF, filepath.Ext(plikDXF)) + "_dzialki.xlsx"
	}
	info := [][]any{
		{"Plik DXF", abs},
		{"Data analizy", time.Now().Format("2006-01-02 15:04")},
		{"Program", "dzialki_z_dxf " + wersja},
		{"Źródło danych o działkach", "ULDK GUGiK – https://uldk.gugik.gov.pl (GetParcelByXY)"},
		{"Wersja DXF / kodowanie", rys.Wersja + " / " + rys.Kodowanie},
		{"Układy w pliku", opisSlownika(uklady)},
		{"Liczba przeanalizowanych obiektów", len(wyniki)},
		{"Liczba znalezionych działek", 0},
		{"Liczba zapytań do ULDK", kl.Zapytan},
		{"Czas analizy [s]", float64(time.Since(t0).Milliseconds()) / 1000},
		{"Pominięte typy obiektów", opisSlownika(pominiete)},
		{"Bloki", map[bool]string{true: "pełna geometria", false: "punkt wstawienia"}[cfg.RozbijBloki]},
		{"Zamknięte polilinie i okręgi", map[bool]string{true: "powierzchnie", false: "linie"}[cfg.ZamkJakoPow]},
		{"Próg długości / powierzchni", fmt.Sprintf("%g m / %g m²", cfg.MinDl, cfg.MinPow)},
		{"Uwaga", "Długości i powierzchnie liczone w układzie 2000 z granic działek z ULDK – nie są to powierzchnie ewidencyjne z EGiB."},
	}
	if przerwano() {
		info = append(info, []any{"UWAGA", "Analiza przerwana przez użytkownika – wyniki niepełne"})
	}
	if kl.Niezgodne > 0 {
		ost := fmt.Sprintf("ULDK %d razy zwrócił działkę, która nie zawiera punktu zapytania – takie odpowiedzi pominięto.", kl.Niezgodne)
		info = append(info, []any{"UWAGA", ost})
		fmt.Println("UWAGA: " + ost)
	}
	arkusze, nDz := budujArkusze(wyniki, problemy, info)
	plik, err := zapiszZWyborem(plikWyn, arkusze)
	if err != nil {
		return err
	}
	fmt.Printf("\nZnaleziono %d działek. Zapytań do ULDK: %d. Czas: %.0f s.\nWynik: %s\n",
		nDz, kl.Zapytan, time.Since(t0).Seconds(), plik)
	return nil
}

// zapiszZWyborem zapisuje plik; jeśli jest zablokowany (otwarty w Excelu), używa innej nazwy.
func zapiszZWyborem(plik string, arkusze []*Arkusz) (string, error) {
	baza := strings.TrimSuffix(plik, filepath.Ext(plik))
	for i := 0; i < 20; i++ {
		p := plik
		if i > 0 {
			p = fmt.Sprintf("%s(%d).xlsx", baza, i)
		}
		err := zapiszXLSX(p, arkusze)
		if err == nil {
			if i > 0 {
				fmt.Printf("Plik %s był zablokowany (otwarty w Excelu?) – zapisano pod inną nazwą.\n", filepath.Base(plik))
			}
			return p, nil
		}
		if !errors.Is(err, os.ErrPermission) && !strings.Contains(strings.ToLower(err.Error()), "used by another process") &&
			!strings.Contains(err.Error(), "being used") {
			return "", fmt.Errorf("nie można zapisać %s: %v", p, err)
		}
	}
	return "", fmt.Errorf("nie można zapisać %s – zamknij plik w Excelu", plik)
}

func zaokr(v float64, n int) float64 {
	p := 1.0
	for i := 0; i < n; i++ {
		p *= 10
	}
	return float64(int64(v*p+0.5*sgn(v))) / p
}

func sgn(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

func budujArkusze(wyniki []wynikObiektu, problemy []problem, info [][]any) ([]*Arkusz, int) {
	a1 := &Arkusz{
		Nazwa: "Obiekty-działki",
		Naglowki: []string{"Lp. obiektu", "Uchwyt (handle)", "Typ", "Warstwa", "Opis", "Układ",
			"X obiektu (północ)", "Y obiektu (wschód)", "Identyfikator działki", "Województwo", "Powiat", "Gmina",
			"Obręb", "Nr obrębu", "Nr działki", "Długość w działce [m]", "Powierzchnia w działce [m²]",
			"Liczba punktów w działce", "Udział w obiekcie [%]", "Uwagi"},
		Formaty: map[int]int{6: stylLiczba2, 7: stylLiczba2, 15: stylLiczba2, 16: stylLiczba2, 18: stylLiczba1},
	}
	type zbior struct {
		d               *Dzialka
		strefa          int
		obiekty         map[string]bool
		warstwy         map[string]bool
		dl, pw          float64
		pkt             int
		maDl, maPw, maP bool
	}
	zest := map[*Dzialka]*zbior{}
	for _, w := range wyniki {
		o := w.o
		ref := o.punktOdniesienia()
		wsp := []any{o.Lp, o.Uchwyt, o.Typ, o.Warstwa, o.Opis, fmt.Sprintf("2000/%d", o.Strefa),
			zaokr(ref.Y, 2), zaokr(ref.X, 2)}
		uwagi := strings.Join(o.Uwagi, "; ")
		if len(w.pomiary) == 0 {
			if uwagi == "" {
				uwagi = "Nie znaleziono działki"
			}
			wiersz := append(append([]any{}, wsp...), make([]any, 11)...)
			a1.Wiersze = append(a1.Wiersze, append(wiersz, uwagi))
			continue
		}
		for _, m := range w.pomiary {
			d := m.D
			var dl, pw, np, ud any
			if m.maDl {
				dl = m.Dl
			}
			if m.maPw {
				pw = m.Pw
			}
			if len(o.Pkt) > 0 {
				np = m.Npkt
			}
			if m.maUdzial {
				ud = m.Udzial
			}
			wiersz := append(append([]any{}, wsp...), d.ID, d.Woj, d.Powiat, d.Gmina, d.Obreb, d.NrObrebu(), d.Numer,
				dl, pw, np, ud, uwagi)
			a1.Wiersze = append(a1.Wiersze, wiersz)
			z := zest[d]
			if z == nil {
				z = &zbior{d: d, strefa: o.Strefa, obiekty: map[string]bool{}, warstwy: map[string]bool{}}
				zest[d] = z
			}
			z.obiekty[o.Uchwyt] = true
			z.warstwy[o.Warstwa] = true
			z.dl += m.Dl
			z.pw += m.Pw
			z.pkt += m.Npkt
			z.maDl = z.maDl || m.maDl
			z.maPw = z.maPw || m.maPw
			z.maP = z.maP || len(o.Pkt) > 0
		}
	}

	a2 := &Arkusz{
		Nazwa: "Zestawienie działek",
		Naglowki: []string{"Lp.", "Identyfikator działki", "Województwo", "Powiat", "Gmina", "Obręb", "Nr obrębu",
			"Nr działki", "Pow. geometryczna działki [m²]", "Liczba obiektów", "Łączna długość [m]",
			"Łączna powierzchnia [m²]", "Liczba punktów", "Warstwy", "Uchwyty obiektów"},
		Formaty: map[int]int{8: stylLiczba2, 10: stylLiczba2, 11: stylLiczba2},
		MaxSzer: map[int]float64{14: 80},
	}
	var lista []*zbior
	for _, z := range zest {
		lista = append(lista, z)
	}
	sort.Slice(lista, func(i, j int) bool { return naturalnie(lista[i].d.ID, lista[j].d.ID) })
	rej := &Rejestr{}
	for i, z := range lista {
		d := z.d
		powD := 0.0
		if a := rej.geom(d, z.strefa); a != nil {
			powD = a.pole()
		}
		var dl, pw, np any
		if z.maDl {
			dl = z.dl
		}
		if z.maPw {
			pw = z.pw
		}
		if z.maP {
			np = z.pkt
		}
		a2.Wiersze = append(a2.Wiersze, []any{i + 1, d.ID, d.Woj, d.Powiat, d.Gmina, d.Obreb, d.NrObrebu(), d.Numer,
			zaokr(powD, 2), len(z.obiekty), dl, pw, np, strings.Join(klucze(z.warstwy), ", "),
			strings.Join(klucze(z.obiekty), ", ")})
	}

	a3 := &Arkusz{Nazwa: "Problemy", Naglowki: []string{"Uchwyt (handle)", "Typ", "Warstwa", "Problem"},
		MaxSzer: map[int]float64{3: 100}}
	for _, p := range problemy {
		a3.Wiersze = append(a3.Wiersze, []any{p.uchwyt, p.typ, p.warstwa, p.opis})
	}

	for _, w := range info {
		if w[0] == "Liczba znalezionych działek" {
			w[1] = len(lista)
		}
	}
	a4 := &Arkusz{Nazwa: "Informacje", Naglowki: []string{"Parametr", "Wartość"}, Wiersze: info,
		MaxSzer: map[int]float64{1: 100}}
	return []*Arkusz{a1, a2, a3, a4}, len(lista)
}

func klucze(m map[string]bool) []string {
	var k []string
	for n := range m {
		k = append(k, n)
	}
	sort.Slice(k, func(i, j int) bool { return naturalnie(k[i], k[j]) })
	return k
}
