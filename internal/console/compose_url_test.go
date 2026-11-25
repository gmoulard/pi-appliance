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

package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestComposeCandidates(t *testing.T) {
	got, err := composeCandidates("https://github.com/Websoft9/docker-library/blob/main/apps/wordpress/docker-compose.yml")
	if err != nil || !reflect.DeepEqual(got, []string{"https://raw.githubusercontent.com/Websoft9/docker-library/main/apps/wordpress/docker-compose.yml"}) {
		t.Fatalf("blob : %v %v", got, err)
	}
	got, err = composeCandidates("https://github.com/Websoft9/docker-library/tree/main/apps/wordpress/")
	if err != nil || len(got) != 4 || got[3] != "https://raw.githubusercontent.com/Websoft9/docker-library/main/apps/wordpress/docker-compose.yml" {
		t.Fatalf("tree : %v %v", got, err)
	}
	if got, _ := composeCandidates("https://exemple.org/compose.yaml"); !reflect.DeepEqual(got, []string{"https://exemple.org/compose.yaml"}) {
		t.Fatalf("autre : %v", got)
	}
	for _, bad := range []string{"", "ftp://exemple.org/c.yaml", "exemple.org/c.yaml", "file:///etc/passwd"} {
		if _, err := composeCandidates(bad); err == nil {
			t.Fatalf("%q accepté", bad)
		}
	}
}

func TestFetchCompose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/compose.yaml":
			w.Write([]byte("services:\n  web:\n    image: nginx:1.27\n"))
		case "/page":
			w.Write([]byte("<!DOCTYPE html><html></html>"))
		case "/gros":
			w.Write([]byte(strings.Repeat("a", maxComposeSize+1)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	f := newFixture(t)
	body := func(address string) string {
		data, _ := json.Marshal(ComposeFetchRequest{URL: address})
		return string(data)
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/compose/fetch", "application/json", body(server.URL+"/compose.yaml"), nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	cookie := f.admin(t)
	response := f.do(http.MethodPost, "/pi-appliance/api/compose/fetch", "application/json", body(server.URL+"/compose.yaml"), cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "nginx:1.27") {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	for address, code := range map[string]int{
		server.URL + "/absent": http.StatusNotFound,
		server.URL + "/page":   http.StatusBadGateway,
		server.URL + "/gros":   http.StatusBadGateway,
		"pas une adresse":      http.StatusBadRequest,
	} {
		if response := f.do(http.MethodPost, "/pi-appliance/api/compose/fetch", "application/json", body(address), cookie); response.Code != code {
			t.Fatalf("%s : code %d, %d attendu : %s", address, response.Code, code, response.Body.String())
		}
	}
}
