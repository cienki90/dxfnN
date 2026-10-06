//go:build windows

package main

import (
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// wlasnaKonsola – true, gdy program uruchomiono dwuklikiem / przeciągnięciem pliku
// (konsola zostanie zamknięta po zakończeniu, więc trzeba poczekać na Enter).
func wlasnaKonsola() bool {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleProcessList")
	if proc.Find() != nil {
		return false
	}
	var ids [4]uint32
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&ids[0])), 4)
	return n == 1
}

// wybierzPlikiOknem otwiera standardowe okno wyboru plików Windows (przez wbudowany PowerShell).
func wybierzPlikiOknem() []string {
	skrypt := `[Console]::OutputEncoding=[Text.Encoding]::UTF8;` +
		`Add-Type -AssemblyName System.Windows.Forms;` +
		`$f=New-Object System.Windows.Forms.Form -Property @{TopMost=$true};` +
		`$d=New-Object System.Windows.Forms.OpenFileDialog;` +
		`$d.Title='Wskaż plik(i) DXF';` +
		`$d.Filter='Pliki DXF (*.dxf)|*.dxf|Wszystkie pliki (*.*)|*.*';` +
		`$d.Multiselect=$true;` +
		`if($d.ShowDialog($f) -eq 'OK'){ $d.FileNames -join '|' }`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-Command", skrypt)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	s := strings.TrimSpace(strings.TrimPrefix(string(out), "\ufeff"))
	if s == "" {
		return nil
	}
	return strings.Split(s, "|")
}

var (
	proxyRaz sync.Once
	proxyURL *url.URL
)

// proxySystemowe odczytuje serwer proxy z ustawień Windows (Opcje internetowe).
func proxySystemowe() *url.URL {
	proxyRaz.Do(func() {
		var h syscall.Handle
		klucz, _ := syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Internet Settings`)
		if syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, klucz, 0, syscall.KEY_READ, &h) != nil {
			return
		}
		defer syscall.RegCloseKey(h)
		var typ, wl, n uint32
		n = 4
		nazwa, _ := syscall.UTF16PtrFromString("ProxyEnable")
		if syscall.RegQueryValueEx(h, nazwa, nil, &typ, (*byte)(unsafe.Pointer(&wl)), &n) != nil || wl == 0 {
			return
		}
		buf := make([]uint16, 2048)
		n = uint32(len(buf) * 2)
		nazwa, _ = syscall.UTF16PtrFromString("ProxyServer")
		if syscall.RegQueryValueEx(h, nazwa, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &n) != nil {
			return
		}
		s := strings.TrimSpace(syscall.UTF16ToString(buf))
		if strings.Contains(s, "=") {
			wybrany := ""
			for _, cz := range strings.Split(s, ";") {
				kv := strings.SplitN(strings.TrimSpace(cz), "=", 2)
				if len(kv) != 2 {
					continue
				}
				if strings.EqualFold(kv[0], "https") || (wybrany == "" && strings.EqualFold(kv[0], "http")) {
					wybrany = kv[1]
				}
			}
			s = wybrany
		}
		if s == "" {
			return
		}
		if !strings.Contains(s, "://") {
			s = "http://" + s
		}
		if u, err := url.Parse(s); err == nil {
			proxyURL = u
		}
	})
	return proxyURL
}
