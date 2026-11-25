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
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/services"
)

// Docker Compose libre sous un chemin (TODO-2.72) et domaine de redirection
// (TODO-3.18) : l'exemple de NautiLink, https://nautilink.moulard.org vers
// https://<adresse du Pi>/nautilink/.
func TestCustomSubPathAndRedirect(t *testing.T) {
	f, file := servicesFixture(t)
	admin := f.admin(t)
	installedApp(t, f, "nautilink", services.CustomApp, services.AccessPublic, "services: {}")
	installedApp(t, f, "www", "site-web", services.AccessPublic, "")

	if view := internetViews(t, f, admin)["nautilink"]; view.InternetReason != services.PathUnsupported {
		t.Fatalf("Docker Compose libre publié d'office : %+v", view)
	}
	put := func(path, body string) *http.Response {
		response := f.do(http.MethodPut, "/pi-appliance/api/services/"+path, "application/json", body, admin)
		return response.Result()
	}
	if response := put("nautilink/subpath", `{"sub_path": true}`); response.StatusCode != http.StatusOK {
		t.Fatalf("chemin : %d", response.StatusCode)
	}
	if response := put("www/subpath", `{"sub_path": true}`); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("service du catalogue réglé : %d", response.StatusCode)
	}
	if view := internetViews(t, f, admin)["nautilink"]; view.InternetAddress != "https://maison.example.org/nautilink/" {
		t.Fatalf("publication : %+v", view)
	}
	if response := put("nautilink/redirects", `{"hosts": ["maison.example.org"]}`); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("adresse du Pi acceptée en redirection : %d", response.StatusCode)
	}
	if response := put("nautilink/redirects", `{"hosts": ["NautiLink.moulard.org"]}`); response.StatusCode != http.StatusOK {
		t.Fatalf("redirection : %d", response.StatusCode)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, expected := range []string{
		// Chemin gardé : pas de stripPrefix pour nautilink.
		"middlewares: [pi-nautilink-internet-slash@file, pi-appliance-errors@file]",
		"rule: \"Host(`nautilink.moulard.org`)\"",
		"middlewares: [pi-nautilink-redirect@file]",
		"service: noop@internal",
		"replacement: 'https://maison.example.org/nautilink/${1}'",
		"permanent: true",
	} {
		if !strings.Contains(content, expected) {
			t.Errorf("services-internet.yml sans %q :\n%s", expected, content)
		}
	}
	if strings.Contains(content, "pi-nautilink-internet-strip") {
		t.Errorf("chemin retiré :\n%s", content)
	}
	// Service plus publié (maison seulement) : plus de redirection.
	if response := f.do(http.MethodPut, "/pi-appliance/api/services/nautilink/access", "application/json", `{"access": "home"}`, admin); response.Code != http.StatusOK {
		t.Logf("accès maison : %d %s (service de test sans override)", response.Code, response.Body.String())
	}
}

func TestValidRedirectHost(t *testing.T) {
	for host, ok := range map[string]bool{"nautilink.moulard.org": true, "a.b": true, "localhost": false, "a`b.org": false, "": false, "a..b": false} {
		if validRedirectHost(host) != ok {
			t.Errorf("%q : %v", host, !ok)
		}
	}
}
