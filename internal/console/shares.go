// This file is part of pi-appliance <https://github.com/gmoulard/pi-appliance>.
// Copyright (c) 2026 Guillaume Moulard.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package console

import (
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
)

// Liens de partage d'un document Etherpad (spécification 6.3, 8.2) : une
// personne connectée crée un lien vers un seul document pour quelqu'un sans
// compte. Le lien https://<Etherpad>/p/<document>?share=<jeton> passe par le
// middleware Traefik pi-appliance-pad (forwardPad), qui échange le jeton
// contre un cookie limité à l'adresse d'Etherpad puis n'accepte, avec ce
// cookie, que les adresses de ce document.

const (
	shareCookie = "pi_share"
	shareParam  = "share"
)

var padPattern = regexp.MustCompile(`^[\p{L}\p{N}._~-]{1,50}$`)

// ShareRequest : le document (son nom ou son adresse) et la durée du lien, en jours.
type ShareRequest struct {
	Pad  string `json:"pad"`
	Days int    `json:"days"`
}

// ShareReply : le lien, montré une seule fois (seul son SHA-256 est gardé).
type ShareReply struct {
	Share auth.SharePublic `json:"share"`
	Link  string           `json:"link"`
}

// padName : le nom du document, ou celui d'une adresse …/p/<document> collée.
func padName(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if i := strings.LastIndex(input, "/p/"); i >= 0 {
		rest := input[i+len("/p/"):]
		if j := strings.IndexAny(rest, "/?#"); j >= 0 {
			rest = rest[:j]
		}
		decoded, err := url.PathUnescape(rest)
		if err != nil {
			return "", false
		}
		input = decoded
	}
	return input, padPattern.MatchString(input)
}

func (c *console) listShares(w http.ResponseWriter, r *http.Request) {
	account, _ := c.account(r)
	name := r.PathValue("name")
	if _, err := c.Services.ShareReady(name); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shares": c.Accounts.Shares(name, account)})
}

func (c *console) createShare(w http.ResponseWriter, r *http.Request) {
	var request ShareRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	account, _ := c.account(r)
	instance, err := c.Services.ShareReady(r.PathValue("name"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	pad, ok := padName(request.Pad)
	if !ok {
		writeError(w, http.StatusBadRequest, "Le nom du document doit faire 1 à 50 caractères (lettres, chiffres, point, tiret, trait bas) ; vous pouvez aussi coller l'adresse du document.")
		return
	}
	if request.Days < 1 || time.Duration(request.Days)*24*time.Hour > auth.ShareMaxLifetime {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Le lien marche de 1 à %d jours.", int(auth.ShareMaxLifetime/(24*time.Hour))))
		return
	}
	share, token, err := c.Accounts.CreateShare(account.Name, instance.Name, pad, time.Duration(request.Days)*24*time.Hour)
	if err != nil {
		writeShareError(w, err)
		return
	}
	link := "https://" + instance.Address + "/p/" + url.PathEscape(pad) + "?" + shareParam + "=" + url.QueryEscape(token)
	writeJSON(w, http.StatusCreated, ShareReply{Share: share, Link: link})
}

func (c *console) revokeShare(w http.ResponseWriter, r *http.Request) {
	account, _ := c.account(r)
	if err := c.Accounts.RevokeShare(r.PathValue("name"), r.PathValue("id"), account); err != nil {
		writeShareError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeShareError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "Ce lien de partage n'existe plus.")
	case errors.Is(err, auth.ErrTooManyShares):
		writeError(w, http.StatusConflict, "Il y a déjà trop de liens de partage : retirez ceux qui ne servent plus.")
	case errors.Is(err, auth.ErrInvalid):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), auth.ErrInvalid.Error()+" : "))
	default:
		log.Printf("liens de partage : %v", err)
		writeError(w, http.StatusInternalServerError, "Les liens de partage sont illisibles.")
	}
}

// forwardPad : authentification déléguée d'Etherpad (middleware
// pi-appliance-pad). Une personne connectée ouvre tout Etherpad ; avec un lien
// de partage, seulement son document. Dans les deux cas, l'en-tête
// Authorization connecte Traefik au compte interne d'Etherpad, qui ne permet la
// connexion temps réel qu'aux documents dont la page a été ouverte (etherpad.go).
func (c *console) forwardPad(w http.ResponseWriter, r *http.Request) {
	if !trustedProxy(r) {
		writeError(w, http.StatusForbidden, "Réservé à Traefik.")
		return
	}
	if c.loginCallback(w, r) {
		return
	}
	host := requestHostOf(r.Header.Get("X-Forwarded-Host"))
	service, authorization, ready := c.Services.EtherpadLogin(host)
	if account, ok := c.forwardAccount(r); ok {
		remoteHeaders(w, account)
		if ready {
			w.Header().Set("Authorization", authorization)
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	if !ready {
		c.forwardLogin(w, r, false)
		return
	}
	path, rawQuery, _ := strings.Cut(r.Header.Get("X-Forwarded-Uri"), "?")
	query, _ := url.ParseQuery(rawQuery)
	page := r.Header.Get("X-Forwarded-Method") == http.MethodGet || r.Header.Get("X-Forwarded-Method") == http.MethodHead
	if query.Has(shareParam) {
		token := query.Get(shareParam)
		share, ok := c.Accounts.Share(token, service)
		if !ok || !sharedPath(path, share.Pad) {
			sharePage(w, http.StatusGone, "Ce lien de partage ne marche plus. Demandez-en un nouveau à la personne qui vous l'a envoyé.", "")
			return
		}
		// Le jeton quitte l'adresse : il ne reste ni dans Etherpad ni dans l'en-tête Referer.
		query.Del(shareParam)
		target := "https://" + host + path
		if r.Header.Get("X-Forwarded-Proto") == "http" {
			target = "http://" + host + path
		}
		if encoded := query.Encode(); encoded != "" {
			target += "?" + encoded
		}
		http.SetCookie(w, &http.Cookie{
			Name:     shareCookie,
			Value:    token,
			Path:     "/",
			Expires:  share.ExpiresAt,
			HttpOnly: true,
			Secure:   r.Header.Get("X-Forwarded-Proto") != "http",
			SameSite: http.SameSiteLaxMode,
		})
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// NE PAS renvoyer une adresse relative : Traefik la lirait par rapport à l'adresse de la console.
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	cookie, err := r.Cookie(shareCookie)
	if err != nil {
		c.forwardLogin(w, r, false)
		return
	}
	share, ok := c.Accounts.Share(cookie.Value, service)
	switch {
	case !ok:
		http.SetCookie(w, &http.Cookie{Name: shareCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: r.Header.Get("X-Forwarded-Proto") != "http", SameSite: http.SameSiteLaxMode})
		if !page {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sharePage(w, http.StatusGone, "Ce lien de partage ne marche plus. Demandez-en un nouveau à la personne qui vous l'a envoyé.", "")
	case !sharedPath(path, share.Pad):
		if !page {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		sharePage(w, http.StatusForbidden, fmt.Sprintf("Votre lien de partage n'ouvre que le document « %s ».", share.Pad), "/p/"+url.PathEscape(share.Pad))
	default:
		w.Header().Set("Authorization", authorization)
		w.WriteHeader(http.StatusOK)
	}
}

// sharedPath dit si une adresse d'Etherpad est permise avec le lien du
// document pad : ses pages /p/<pad>… et les fichiers communs (scripts, styles,
// connexion temps réel), pas un autre document ni l'administration.
// NE PAS décoder le chemin entier avant de le découper : Etherpad prend le
// document de /p/<segment> puis décode ce segment (un %2F y reste dans le nom).
func sharedPath(path, pad string) bool {
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "//") || strings.Contains(path, "\\") {
		return false
	}
	segments := strings.Split(path[1:], "/")
	decoded := make([]string, len(segments))
	for i, segment := range segments {
		value, err := url.PathUnescape(segment)
		if err != nil || value == "." || value == ".." || strings.ContainsAny(value, "/\\") {
			return false
		}
		decoded[i] = value
	}
	first := strings.ToLower(decoded[0])
	switch {
	case first == "p":
		return len(decoded) > 1 && decoded[1] == pad
	case strings.HasPrefix(first, "admin"), first == "api", first == "oidc", first == "interaction":
		return false
	}
	return true
}

// requestHostOf : adresse sans le port, en minuscules.
func requestHostOf(host string) string {
	if strings.ContainsAny(host, "/\\@ ") {
		return ""
	}
	if i := strings.LastIndex(host, ":"); i >= 0 && !strings.Contains(host[i:], "]") {
		host = host[:i]
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

// sharePage : page lisible par une personne sans compte, avec un lien de retour au document.
func sharePage(w http.ResponseWriter, status int, text, back string) {
	messagePage(w, status, "Etherpad", text, back, "Revenir au document")
}

// messagePage : courte page d'explication, avec un lien (back, backText) s'il y en a un.
func messagePage(w http.ResponseWriter, status int, title, text, back, backText string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(status)
	link := ""
	if back != "" {
		link = `<p><a href="` + html.EscapeString(back) + `">` + html.EscapeString(backText) + `</a></p>`
	}
	fmt.Fprintf(w, `<!doctype html><html lang="fr"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title></head><body style="font-family: sans-serif; max-width: 36rem; margin: 3rem auto; padding: 0 1rem; line-height: 1.5"><p>%s</p>%s</body></html>`, html.EscapeString(title), html.EscapeString(text), link)
}
