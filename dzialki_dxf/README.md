# dzialki_z_dxf.exe – działki ewidencyjne dla obiektów z pliku DXF

Program sprawdza, na których działkach ewidencyjnych leżą obiekty z pliku DXF w układzie
**PL-2000 strefa 7 (EPSG:2178) lub strefa 8 (EPSG:2179)**. Wynik zapisuje do pliku Excel.
Działki pobiera z usługi **ULDK GUGiK**, więc potrzebny jest internet.

To jeden plik `.exe`, który działa na Windows 10 i 11 (64-bit). **Nie trzeba niczego instalować.**

## Uruchomienie

Do wyboru:
- **dwuklik** na `dzialki_z_dxf.exe` otwiera okno wyboru pliku (można zaznaczyć kilka plików DXF);
- **przeciągnięcie** pliku DXF (albo kilku) na ikonę programu;
- uruchomienie w wierszu poleceń: `.\dzialki_z_dxf.exe projekt.dxf [opcje]`.

Wynik `<nazwa_pliku>_dzialki.xlsx` trafia do tego samego folderu co plik DXF. Jeśli ten plik jest
otwarty w Excelu, program zapisze wynik jako `..._dzialki(1).xlsx`.

Przy pierwszym uruchomieniu Windows może pokazać okno „System Windows ochronił komputer”, bo
program nie ma podpisu cyfrowego. Kliknij **Więcej informacji → Uruchom mimo to**.

## Układ współrzędnych

- Strefa 7 lub 8 jest rozpoznawana osobno dla każdego obiektu po pierwszej cyfrze współrzędnej Y
  (7 xxx xxx albo 8 xxx xxx). W jednym rysunku mogą być obiekty z obu stref.
- Kolejność osi też jest rozpoznawana sama. Działa zapis CAD (X rysunku = Y geodezyjne) i zapis
  odwrotny (X rysunku = X geodezyjne).
- Obiekty o innych współrzędnych są pomijane i trafiają do arkusza „Problemy”.

## Opcje (wiersz poleceń)

| Opcja | Znaczenie |
|---|---|
| `-o wynik.xlsx` | nazwa pliku wynikowego |
| `-strefa 7` / `-strefa 8` | wymuszenie strefy |
| `-osie cad` / `-osie geo` | wymuszenie kolejności osi |
| `-warstwy "KAN*,WOD*"` | analiza tylko wybranych warstw (`*` i `?` jako symbole wieloznaczne) |
| `-pomin-warstwy "OPIS*,RAMKA"` | pominięcie wybranych warstw |
| `-rozbij-bloki` | pełna geometria bloków (domyślnie liczy się tylko punkt wstawienia) |
| `-zamkniete-jako-linie` | zamknięte polilinie i okręgi jako linie, a nie powierzchnie |
| `-proxy http://serwer:port` | serwer proxy (domyślnie brany z ustawień Windows) |
| `-max-zapytan 500` | limit zapytań do ULDK na jeden obiekt |

Pełną listę opcji wyświetla `.\dzialki_z_dxf.exe -h`.

## Obsługiwane obiekty

LINE, LWPOLYLINE, POLYLINE (2D i 3D), ARC, CIRCLE, ELLIPSE, SPLINE, HATCH, SOLID, TRACE, 3DFACE,
POINT, INSERT (bloki), TEXT, MTEXT. Inne obiekty, np. wymiary, są pomijane, a ich liczba jest
podana w arkuszu „Informacje”. Program czyta DXF tekstowy (ASCII) z polskimi znakami w UTF-8
albo Windows-1250. DXF binarny trzeba najpierw zapisać jako tekstowy.

## Wynik w Excelu

| Arkusz | Zawartość |
|---|---|
| Obiekty-działki | każda para obiekt–działka: warstwa, typ, uchwyt, opis, identyfikator działki, województwo, powiat, gmina, obręb, nr działki, długość lub powierzchnia obiektu w działce, udział % |
| Zestawienie działek | lista działek bez powtórzeń z liczbą obiektów, sumą długości i powierzchni oraz warstwami |
| Problemy | obiekty pominięte i błędy |
| Informacje | parametry analizy, liczba zapytań, czas |

Długości i powierzchnie liczone są z granic działek pobranych z ULDK. **Nie są to powierzchnie
ewidencyjne z EGiB.**

## Jak to działa

Dla każdego obiektu program pyta ULDK o działkę w punkcie leżącym na obiekcie. Potem odejmuje tę
działkę od obiektu i pyta o kolejny punkt z części, która nie jest jeszcze pokryta. Powtarza to,
aż cały obiekt będzie pokryty działkami. Wnętrza powierzchni są dodatkowo sprawdzane siatką
punktów, więc znajdują się też działki leżące w całości wewnątrz obiektu. Pobrane działki są
zapamiętywane i używane dla kolejnych obiektów.

Przeliczenie PL-2000 → PL-1992 program wykonuje sam (odwzorowanie Gaussa-Krügera, GRS80).

## Kod źródłowy

Kod jest w folderze `zrodla/` (Go, tylko biblioteka standardowa). Kompilacja:
`GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o ../dzialki_z_dxf.exe .`
