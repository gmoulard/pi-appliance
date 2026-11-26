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
	"fmt"
	"html"
	"io/fs"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Petites pages HTML servies par la console hors de son interface : messages
// (lien de partage, connexion périmée) et pages d'erreur derrière Traefik
// (« introuvable », « service indisponible »). Elles parlent la langue du
// navigateur (Accept-Language, ou ?lang=), avec les traductions de la console
// (web/i18n/<code>.js), et finissent par le même pied de page que la console :
// version, commit, date, licence et code source (TODO-5.21).

const (
	// RepoURL : dépôt git de pi-appliance.
	RepoURL = "https://github.com/gmoulard/pi-appliance"
	// LicenseURL : texte de la licence (CC BY-NC-ND 4.0 et permissions supplémentaires).
	LicenseURL = RepoURL + "/blob/main/LICENSE"
)

// pageKeys : phrases françaises des pages servies par Go, traduites dans les
// fichiers web/i18n/<code>.js comme celles de la console (i18n_test.go).
var pageKeys = []string{
	"Code public, licence CC BY-NC-ND 4.0",
	"Code source",
	"commit",
	"du {0}",
	"{0} {1} {2}",
	"janvier", "février", "mars", "avril", "mai", "juin",
	"juillet", "août", "septembre", "octobre", "novembre", "décembre",
	"Page introuvable",
	"Cette page n'existe pas, ou le service qui la donne est arrêté ou désinstallé.",
	"Service indisponible",
	"Cockpit ne s'ouvre que depuis la maison. Un administrateur peut l'ouvrir aussi depuis internet, dans la carte Cockpit de la console.",
	"Cockpit n'est pas prêt pour l'instant. Réessayez dans une minute.",
	"Activez d'abord dans la console la double authentification, exigée pour ce compte.",
	"Ce service ne répond pas pour l'instant : il est peut-être en train de démarrer. Réessayez dans une minute ; s'il ne répond toujours pas, regardez son état dans la console.",
	"Réessayer",
	"Ouvrir la console",
	"Cette connexion n'est plus valable : elle ne sert qu'une fois, pendant deux minutes, dans le navigateur qui l'a demandée.",
	"Ouvrir de nouveau la page",
	"Ce lien de partage ne marche plus. Demandez-en un nouveau à la personne qui vous l'a envoyé.",
	"Votre lien de partage n'ouvre que le document « {0} ».",
	"Revenir au document",
}

var months = []string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}

// pageLanguages : langues de la console (web/i18n.js), le français d'abord.
var pageLanguages = []string{"fr", "en", "es", "pt", "it", "de", "nl", "tr", "zh", "ja", "ar", "fa"}

var tableFile = regexp.MustCompile(`(?s)piTranslations\("([a-z]{2})", (\{.*\})\);\s*$`)

var (
	pageTablesOnce sync.Once
	pageTables     map[string]map[string]string
)

func loadPageTables() {
	pageTables = map[string]map[string]string{}
	for _, code := range pageLanguages[1:] {
		data, err := fs.ReadFile(webFiles, "web/i18n/"+code+".js")
		if err != nil {
			log.Printf("traductions %s : %v", code, err)
			continue
		}
		match := tableFile.FindSubmatch([]byte(strings.ReplaceAll(string(data), "\r\n", "\n")))
		if match == nil {
			log.Printf("traductions %s : illisibles", code)
			continue
		}
		var table map[string]string
		if err := json.Unmarshal(match[2], &table); err != nil {
			log.Printf("traductions %s : %v", code, err)
			continue
		}
		pageTables[code] = table
	}
}

// pageLanguage : ?lang=, puis la première langue connue d'Accept-Language, sinon le français.
func pageLanguage(r *http.Request) string {
	if asked := r.URL.Query().Get("lang"); slices.Contains(pageLanguages, asked) {
		return asked
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		code := strings.ToLower(strings.SplitN(tag, "-", 2)[0])
		if slices.Contains(pageLanguages, code) {
			return code
		}
	}
	return "fr"
}

// pageT traduit une phrase de pageKeys ; {0}, {1}… sont remplacés par args.
func pageT(lang, key string, args ...string) string {
	pageTablesOnce.Do(loadPageTables)
	text := key
	if translated, ok := pageTables[lang][key]; ok {
		text = translated
	}
	for i, arg := range args {
		text = strings.ReplaceAll(text, "{"+strconv.Itoa(i)+"}", arg)
	}
	return text
}

// pageDate : date AAAA-MM-JJ (ou RFC 3339) en toutes lettres ; vide si illisible.
func pageDate(lang, date string) string {
	day, err := time.Parse("2006-01-02", date)
	if err != nil {
		if day, err = time.Parse(time.RFC3339, date); err != nil {
			return ""
		}
	}
	return pageT(lang, "{0} {1} {2}", strconv.Itoa(day.Day()), pageT(lang, months[day.Month()-1]), strconv.Itoa(day.Year()))
}

var releasePattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// ReleaseVersion : version publiée (0.1.4, v0.2.0-beta.1) ; sinon version de
// développement, que les pieds de page ne montrent pas (seulement le commit).
func ReleaseVersion(version string) bool {
	return releasePattern.MatchString(version)
}

// footerHTML : pied de page des pages servies par Go, comme celui de la console (app.js, showFooter).
func (c *console) footerHTML(lang string) string {
	var parts []string
	if ReleaseVersion(c.Version) {
		parts = append(parts, "pi-appliance "+html.EscapeString(strings.TrimPrefix(c.Version, "v")))
	}
	if c.Commit != "" {
		short := c.Commit
		if len(short) > 7 {
			short = short[:7]
		}
		commit := fmt.Sprintf(`%s <a href="%s/commit/%s" style="color: inherit"><code>%s</code></a>`, html.EscapeString(pageT(lang, "commit")), RepoURL, html.EscapeString(c.Commit), html.EscapeString(short))
		if date := pageDate(lang, c.Date); date != "" {
			commit += " " + html.EscapeString(pageT(lang, "du {0}", date))
		}
		parts = append(parts, commit)
	}
	parts = append(parts,
		fmt.Sprintf(`<a href="%s" style="color: inherit">%s</a>`, LicenseURL, html.EscapeString(pageT(lang, "Code public, licence CC BY-NC-ND 4.0"))),
		fmt.Sprintf(`<a href="%s" style="color: inherit">%s</a>`, RepoURL, html.EscapeString(pageT(lang, "Code source"))))
	return `<footer style="margin-top: 3rem; font-size: 0.85rem; color: #6b3d4e">` + strings.Join(parts, " · ") + `</footer>`
}

// pageLink : lien d'une page de message.
type pageLink struct {
	Href, Text string
}

// messagePage : courte page d'explication (titre, texte déjà traduits), avec ses liens et le pied de page.
func (c *console) messagePage(w http.ResponseWriter, r *http.Request, status int, title, text string, links ...pageLink) {
	lang := pageLanguage(r)
	dir := "ltr"
	if lang == "ar" || lang == "fa" {
		dir = "rtl"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(status)
	var body strings.Builder
	for _, link := range links {
		fmt.Fprintf(&body, `<p><a href="%s" style="color: #c71f5d">%s</a></p>`, html.EscapeString(link.Href), html.EscapeString(link.Text))
	}
	fmt.Fprintf(w, `<!doctype html><html lang="%s" dir="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title></head><body style="font-family: system-ui, sans-serif; max-width: 36rem; margin: 3rem auto; padding: 0 1rem; line-height: 1.5; color: #33141f; background: #fff5f7"><p>%s</p>%s%s</body></html>`,
		lang, dir, html.EscapeString(title), html.EscapeString(text), body.String(), c.footerHTML(lang))
}

// sharePage : page lisible par une personne sans compte, avec un lien de retour au document.
func (c *console) sharePage(w http.ResponseWriter, r *http.Request, status int, key string, args []string, back string) {
	lang := pageLanguage(r)
	var links []pageLink
	if back != "" {
		links = append(links, pageLink{back, pageT(lang, "Revenir au document")})
	}
	c.messagePage(w, r, status, "Etherpad", pageT(lang, key, args...), links...)
}

// errorPage : page d'erreur de Traefik (spécification 10.2) ; GET
// /pi-appliance/erreur/{code}, demandée par le routeur de dernier recours
// (404, dynamic/console.yml) et par le middleware pi-appliance-errors (502 à
// 504, services et Cockpit). Ouverte depuis internet (internetOpenPath) : elle
// ne dit rien du Pi, sauf le lien vers la console, montré seulement à la maison.
func (c *console) errorPage(w http.ResponseWriter, r *http.Request) {
	lang := pageLanguage(r)
	code, _ := strconv.Atoi(r.PathValue("code"))
	// Adresse nue (routeur pi-appliance-root) : la console, sauf sur l'adresse
	// d'un service dont le routeur manque (conteneur arrêté ou qui redémarre en
	// boucle) : la page « service indisponible », pas la connexion de la console.
	if r.PathValue("code") == "racine" {
		if c.Services == nil || c.serviceOf(r.Host, "/") == "" {
			http.Redirect(w, r, BasePath+"/", http.StatusFound)
			return
		}
		code = http.StatusServiceUnavailable
	}
	var links []pageLink
	title, text := pageT(lang, "Page introuvable"), pageT(lang, "Cette page n'existe pas, ou le service qui la donne est arrêté ou désinstallé.")
	switch code {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		title, text = pageT(lang, "Service indisponible"), pageT(lang, "Ce service ne répond pas pour l'instant : il est peut-être en train de démarrer. Réessayez dans une minute ; s'il ne répond toujours pas, regardez son état dans la console.")
		// Traefik sert cette page à l'adresse demandée : la recharger réessaie le service.
		links = append(links, pageLink{"", pageT(lang, "Réessayer")})
	default:
		code = http.StatusNotFound
		// Cockpit demandé depuis internet sans y être ouvert (TODO-3.4) :
		// replacePath (dynamic/console.yml) garde le chemin d'origine dans
		// X-Replaced-Path. Rien d'autre du Pi n'est dit.
		if original := r.Header.Get("X-Replaced-Path"); fromInternet(r) && (original == "/cockpit" || strings.HasPrefix(original, "/cockpit/")) {
			title, text = "Cockpit", pageT(lang, "Cockpit ne s'ouvre que depuis la maison. Un administrateur peut l'ouvrir aussi depuis internet, dans la carte Cockpit de la console.")
		}
	}
	// Adresse complète : sur l'adresse d'un service, /pi-appliance/ mène au service.
	if !fromInternet(r) && c.Services != nil && c.Services.CurrentDomain() != "" {
		links = append(links, pageLink{"https://" + c.Services.CurrentDomain() + BasePath + "/", pageT(lang, "Ouvrir la console")})
	}
	c.messagePage(w, r, code, title, text, links...)
}
