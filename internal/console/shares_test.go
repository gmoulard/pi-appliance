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
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

const padForward = "/pi-appliance/api/auth/forward-pad"

// withEtherpad installe Etherpad « pad », réservé aux personnes qui ont un compte.
func withEtherpad(t *testing.T, f fixture) {
	t.Helper()
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	app, ok := catalog.Get(services.EtherpadApp)
	if !ok {
		t.Fatal("Etherpad absent du catalogue")
	}
	if _, err := f.manager.Install(context.Background(), app, "pad", nil); err != nil {
		t.Fatal(err)
	}
}

func padHeaders(uri string) map[string]string {
	return map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "pad.maison.example.org", "X-Forwarded-Uri": uri, "X-Forwarded-Method": "GET"}
}

// newShare crée un lien par l'API et renvoie son jeton.
func newShare(t *testing.T, f fixture, session *http.Cookie, pad string) (ShareReply, string) {
	t.Helper()
	response := f.do(http.MethodPost, "/pi-appliance/api/services/pad/shares", "application/json", `{"pad": "`+pad+`", "days": 7}`, session)
	if response.Code != http.StatusCreated {
		t.Fatalf("création du lien : %d %s", response.Code, response.Body.String())
	}
	reply := decode[ShareReply](t, response)
	link, err := url.Parse(reply.Link)
	if err != nil {
		t.Fatal(err)
	}
	return reply, link.Query().Get(shareParam)
}

func cookieNamed(response *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func TestShareAPI(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	withEtherpad(t, f)
	reply, token := newShare(t, f, admin, "https://pad.maison.example.org/p/r%C3%A9union?lang=fr")
	if reply.Share.Pad != "réunion" || !strings.HasPrefix(reply.Link, "https://pad.maison.example.org/p/r%C3%A9union?share=") || token == "" {
		t.Fatalf("lien %+v", reply)
	}
	list := decode[map[string][]auth.SharePublic](t, f.do(http.MethodGet, "/pi-appliance/api/services/pad/shares", "", "", admin))
	if len(list["shares"]) != 1 || list["shares"][0].ID != reply.Share.ID || strings.Contains(f.do(http.MethodGet, "/pi-appliance/api/services/pad/shares", "", "", admin).Body.String(), token) {
		t.Fatalf("liste %+v", list)
	}
	for _, body := range []string{`{"pad": "a/b", "days": 7}`, `{"pad": "", "days": 7}`, `{"pad": "notes", "days": 0}`, `{"pad": "notes", "days": 31}`, `{"pad": "` + strings.Repeat("a", 51) + `", "days": 1}`} {
		if response := f.do(http.MethodPost, "/pi-appliance/api/services/pad/shares", "application/json", body, admin); response.Code != http.StatusBadRequest {
			t.Fatalf("%s : %d", body, response.Code)
		}
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/services/pad/shares", "application/json", `{"pad": "notes", "days": 7}`, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : %d", response.Code)
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/services/absent/shares", "application/json", `{"pad": "notes", "days": 7}`, admin); response.Code != http.StatusNotFound {
		t.Fatalf("service absent : %d", response.Code)
	}
	if response := f.do(http.MethodDelete, "/pi-appliance/api/services/pad/shares/"+reply.Share.ID, "", "", admin); response.Code != http.StatusNoContent {
		t.Fatalf("retrait : %d", response.Code)
	}
	if response := f.do(http.MethodDelete, "/pi-appliance/api/services/pad/shares/"+reply.Share.ID, "", "", admin); response.Code != http.StatusNotFound {
		t.Fatalf("second retrait : %d", response.Code)
	}
	if _, ok := f.accounts.Share(token, "pad"); ok {
		t.Fatal("lien retiré encore valable")
	}
}

func TestShareNeedsUsersAccess(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	withEtherpad(t, f)
	if response := f.do(http.MethodPut, "/pi-appliance/api/services/pad/access", "application/json", `{"access": "public"}`, admin); response.Code != http.StatusOK {
		t.Fatalf("accès public : %d %s", response.Code, response.Body.String())
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/services/pad/shares", "application/json", `{"pad": "notes", "days": 7}`, admin); response.Code != http.StatusBadRequest {
		t.Fatalf("lien pour un Etherpad public : %d", response.Code)
	}
}

func TestForwardPadSession(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	withEtherpad(t, f)
	_, authorization, _ := f.manager.EtherpadLogin("pad.maison.example.org")
	if response := forwardTo(f, padForward, "192.168.1.20:5000", nil, padHeaders("/p/notes")); response.Code != http.StatusForbidden {
		t.Fatalf("appel hors de Traefik : %d", response.Code)
	}
	response := forwardTo(f, padForward, "127.0.0.1:5000", nil, padHeaders("/p/notes"))
	if response.Code != http.StatusFound || !strings.HasPrefix(response.Header().Get("Location"), "https://maison.example.org/pi-appliance/?return=") || response.Header().Get("Authorization") != "" {
		t.Fatalf("sans session : %d %v", response.Code, response.Header())
	}
	forward := forwardCookieFor(t, f, admin, padForward, padHeaders("/p/notes"))
	response = forwardTo(f, padForward, "127.0.0.1:5000", forward, padHeaders("/p/autre"))
	if response.Code != http.StatusOK || response.Header().Get("Remote-User") != "marie" || response.Header().Get("Authorization") != authorization || authorization == "" {
		t.Fatalf("avec session : %d %v", response.Code, response.Header())
	}
}

func TestForwardPadShare(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	withEtherpad(t, f)
	_, authorization, _ := f.manager.EtherpadLogin("pad.maison.example.org")
	reply, token := newShare(t, f, admin, "notes")

	response := forwardTo(f, padForward, "172.18.0.2:5000", nil, padHeaders("/p/notes?share="+url.QueryEscape(token)+"&lang=fr"))
	share := cookieNamed(response, shareCookie)
	if response.Code != http.StatusFound || response.Header().Get("Location") != "https://pad.maison.example.org/p/notes?lang=fr" {
		t.Fatalf("échange du jeton : %d %q", response.Code, response.Header().Get("Location"))
	}
	if share == nil || share.Value != token || share.Domain != "" || share.Path != "/" || !share.HttpOnly || !share.Secure || share.SameSite != http.SameSiteLaxMode || share.Expires.IsZero() {
		t.Fatalf("cookie du lien : %+v", share)
	}
	if response.Header().Get("Authorization") != "" {
		t.Fatal("en-tête Authorization renvoyé au navigateur")
	}

	allowed := []string{"/p/notes", "/p/notes/timeslider", "/p/notes/export/txt", "/p/n%6Ftes", "/P/notes", "/static/js/pad.js", "/socket.io/?EIO=4&transport=websocket", "/locales.json", "/favicon.ico"}
	for _, uri := range allowed {
		response := forwardTo(f, padForward, "127.0.0.1:5000", share, padHeaders(uri))
		if response.Code != http.StatusOK || response.Header().Get("Authorization") != authorization || response.Header().Get("Remote-User") != "" {
			t.Fatalf("%s : %d %v", uri, response.Code, response.Header())
		}
	}
	refused := []string{"/p/autre", "/p/notes%2Fautre", "/p/", "/p", "/admin/", "/admin-auth/", "/ADMIN", "/api/1/listAllPads", "/oidc/auth", "/interaction/x", "//p/autre", "/static/../p/autre", "/static/%2e%2e/p/autre", "/p/notes2"}
	for _, uri := range refused {
		response := forwardTo(f, padForward, "127.0.0.1:5000", share, padHeaders(uri))
		if response.Code != http.StatusForbidden || response.Header().Get("Authorization") != "" {
			t.Fatalf("%s : %d %v", uri, response.Code, response.Header())
		}
	}
	post := padHeaders("/socket.io/?EIO=4&transport=polling")
	post["X-Forwarded-Method"] = "POST"
	if response := forwardTo(f, padForward, "127.0.0.1:5000", share, post); response.Code != http.StatusOK {
		t.Fatalf("connexion temps réel : %d", response.Code)
	}
	if response := forwardTo(f, padForward, "127.0.0.1:5000", share, padHeaders("/p/autre")); !strings.Contains(response.Body.String(), "/p/notes") {
		t.Fatalf("page sans retour au document : %s", response.Body.String())
	}

	// Le lien ne vaut que pour son Etherpad.
	other := padHeaders("/p/notes")
	other["X-Forwarded-Host"] = "cloud.maison.example.org"
	if response := forwardTo(f, padForward, "127.0.0.1:5000", share, other); response.Code != http.StatusFound || response.Header().Get("Authorization") != "" {
		t.Fatalf("autre service : %d", response.Code)
	}
	if response := forwardTo(f, padForward, "127.0.0.1:5000", nil, padHeaders("/p/autre?share="+url.QueryEscape(token))); response.Code != http.StatusGone || cookieNamed(response, shareCookie) != nil {
		t.Fatalf("jeton sur un autre document : %d", response.Code)
	}
	if response := forwardTo(f, padForward, "127.0.0.1:5000", nil, padHeaders("/p/notes?share=faux")); response.Code != http.StatusGone || !strings.Contains(response.Body.String(), "ne marche plus") {
		t.Fatalf("faux jeton : %d", response.Code)
	}
	if response := forwardTo(f, padForward, "127.0.0.1:5000", &http.Cookie{Name: shareCookie, Value: "faux"}, padHeaders("/p/notes")); response.Code != http.StatusGone {
		t.Fatalf("faux cookie : %d", response.Code)
	}

	if response := f.do(http.MethodDelete, "/pi-appliance/api/services/pad/shares/"+reply.Share.ID, "", "", admin); response.Code != http.StatusNoContent {
		t.Fatalf("retrait : %d", response.Code)
	}
	response = forwardTo(f, padForward, "127.0.0.1:5000", share, padHeaders("/p/notes"))
	if cleared := cookieNamed(response, shareCookie); response.Code != http.StatusGone || cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("lien retiré : %d %+v", response.Code, cleared)
	}
	post["X-Forwarded-Uri"] = "/socket.io/"
	if response := forwardTo(f, padForward, "127.0.0.1:5000", share, post); response.Code != http.StatusUnauthorized {
		t.Fatalf("connexion temps réel après retrait : %d", response.Code)
	}
}

func TestShareExpires(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	withEtherpad(t, f)
	_, token := newShare(t, f, admin, "notes")
	now := time.Now()
	f.accounts.Now = func() time.Time { return now.Add(8 * 24 * time.Hour) }
	if response := forwardTo(f, padForward, "127.0.0.1:5000", &http.Cookie{Name: shareCookie, Value: token}, padHeaders("/p/notes")); response.Code != http.StatusGone {
		t.Fatalf("lien expiré : %d", response.Code)
	}
}

func TestShareRemovedWithService(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	withEtherpad(t, f)
	_, token := newShare(t, f, admin, "notes")
	if response := f.do(http.MethodDelete, "/pi-appliance/api/services/pad?data=delete", "", "", admin); response.Code != http.StatusNoContent {
		t.Fatalf("désinstallation : %d %s", response.Code, response.Body.String())
	}
	if _, ok := f.accounts.Share(token, "pad"); ok {
		t.Fatal("lien d'un Etherpad désinstallé encore valable")
	}
}
