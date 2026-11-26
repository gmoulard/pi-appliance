// This file is part of pi-appliance <https://github.com/gmoulard/pi-appliance>.
// Copyright (c) 2026 Guillaume Moulard.
//
// Licensed under the Creative Commons Attribution-NonCommercial-NoDerivatives
// 4.0 International License (CC BY-NC-ND 4.0), with the additional permissions
// of the LICENSE file at the root of the project: this file may be shared and
// integrated into another project unmodified, keeping this notice and a link to
// the project; modified versions may only be published to propose them to the
// official repository. Commercial use requires a separate licence from the
// author: guillaume@moulard.org.
//
// See <https://creativecommons.org/licenses/by-nc-nd/4.0/> and the LICENSE file.

package tunnel

import (
	"net/netip"
	"strings"
	"testing"
)

func TestVisitLine(t *testing.T) {
	v := Visit{Port: 443, From: netip.MustParseAddrPort("203.0.113.7:51000")}
	got, err := ParseVisit(strings.TrimSpace(v.Line()))
	if err != nil || got != v {
		t.Fatalf("%+v %v", got, err)
	}
	for _, line := range []string{"", "VISIT 22 203.0.113.7:1", "VISIT 80 nimporte", "HELLO 80 203.0.113.7:1", "VISIT 80 203.0.113.7:1 en-trop"} {
		if _, err := ParseVisit(line); err == nil {
			t.Errorf("%q accepté", line)
		}
	}
}

func TestProxyHeader(t *testing.T) {
	v := Visit{Port: 443, From: netip.MustParseAddrPort("203.0.113.7:51000")}
	if got := ProxyHeader(v, netip.MustParseAddrPort("127.0.0.1:8443")); got != "PROXY TCP4 203.0.113.7 127.0.0.1 51000 8443\r\n" {
		t.Fatalf("%q", got)
	}
	v.From = netip.MustParseAddrPort("[2001:db8::1]:51000")
	if got := ProxyHeader(v, netip.MustParseAddrPort("127.0.0.1:8443")); got != "PROXY TCP6 2001:db8::1 ::ffff:127.0.0.1 51000 8443\r\n" {
		t.Fatalf("%q", got)
	}
}
