//go:build !windows

package main

import "net/url"

func wlasnaKonsola() bool { return false }

func wybierzPlikiOknem() []string { return nil }

func proxySystemowe() *url.URL { return nil }
