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

// Services sur internet (spécification 7.1, 8.2 ; TODO-3.17, choix de
// l'utilisateur du 2026-11-22) : un service s'ouvre depuis internet sous un
// chemin de chaque adresse internet du Pi, https://<adresse>/<service>/ (par
// exemple https://vdl.moulard.org/hugo/), sans entrée DNS à créer. La console
// écrit services-internet.yml (configuration dynamique de Traefik) : un
// routeur par service publié, avec les middlewares de son routeur Docker.
//   - « Tout le monde, sur internet » (public) : toujours publié ;
//   - « Personnes connectées » (users) : seulement console ouverte sur
//     internet, la connexion passant par elle ;
//   - « Maison seulement » (home) et administrateurs (admins) : jamais.
// Le service partage alors l'adresse (l'origine) de la console : risque
// accepté par l'utilisateur, écrit dans la spécification (8.3).

import (
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

// Pourquoi un service ne s'ouvre pas depuis internet (internet_reason de
// GET api/services) ; vide : il s'ouvre à internet_address.
const (
	reasonHome      = "home"
	reasonConsole   = "console"
	reasonNoAddress = "no_address"
)

// servicePublication : adresse internet d'un service, ou pourquoi il n'en a pas.
type servicePublication struct {
	App     string
	Access  string
	Address string
	Reason  string
}

// publications : pour chaque service installé avec une page web, son
// adresse sur internet ou la raison de son absence.
func (c *console) publications() map[string]servicePublication {
	result := map[string]servicePublication{}
	if c.Services == nil {
		return result
	}
	instances, err := c.Services.List()
	if err != nil {
		log.Printf("services sur internet : %v", err)
		return result
	}
	enabled, hosts := c.internetEnabled(), c.internetHosts()
	for _, instance := range instances {
		if instance.NoWebPage {
			continue
		}
		publication := servicePublication{App: instance.App, Access: instance.Access}
		switch {
		case instance.Access != services.AccessPublic && instance.Access != services.AccessUsers:
			publication.Reason = reasonHome
		case instance.Access == services.AccessUsers && !enabled:
			publication.Reason = reasonConsole
		case len(hosts) == 0:
			publication.Reason = reasonNoAddress
		default:
			publication.Reason = c.Services.InternetPath(instance)
		}
		if publication.Reason == "" {
			publication.Address = "https://" + hosts[0] + "/" + instance.Name + "/"
		}
		result[instance.Name] = publication
	}
	return result
}

// traefikServices : configuration dynamique de Traefik des services publiés
// sous les adresses internet du Pi ; vide sans service publié.
//
// Règle (Path(`/<nom>`) || PathPrefix(`/<nom>/`)) : jamais /<nom>2 ; aucun
// nom ne vaut pi-appliance ni cockpit (services.ReservedNames), la console,
// api/hello, l'authentification déléguée et les pages d'erreur restent à la
// console. Priorité 10 : au-dessus de la console (2), d'api/hello (3) et du
// dernier recours (1). Middlewares : /<nom> mène à /<nom>/, page d'erreur et
// niveau d'accès comme sur le routeur Docker, chemin retiré en dernier (le
// forward auth voit donc l'adresse complète, X-Forwarded-Uri en /<nom>/…),
// avec l'en-tête X-Forwarded-Prefix posé par stripPrefix.
func traefikServices(hosts []string, published map[string]servicePublication) string {
	names := make([]string, 0, len(published))
	for name, publication := range published {
		if publication.Address != "" && !slices.Contains(services.ReservedNames, name) && marketplace.ValidLabel(name) {
			names = append(names, name)
		}
	}
	if len(hosts) == 0 || len(names) == 0 {
		return ""
	}
	slices.Sort(names)
	rules := make([]string, len(hosts))
	for i, host := range hosts {
		rules[i] = "Host(`" + host + "`)"
	}
	hostRule := strings.Join(rules, " || ")
	var routers, middlewares strings.Builder
	for _, name := range names {
		publication := published[name]
		id := "pi-" + name + "-internet"
		chain := []string{id + "-slash@file"}
		chain = append(chain, services.RouterMiddlewares(publication.App, publication.Access)...)
		chain = append(chain, id+"-strip@file")
		fmt.Fprintf(&routers, "    %s:\n", id)
		routers.WriteString("      entryPoints: [websecure]\n")
		fmt.Fprintf(&routers, "      rule: \"(%s) && (Path(`/%s`) || PathPrefix(`/%s/`))\"\n", hostRule, name, name)
		routers.WriteString("      priority: 10\n")
		fmt.Fprintf(&routers, "      middlewares: [%s]\n", strings.Join(chain, ", "))
		fmt.Fprintf(&routers, "      service: pi-%s@docker\n", name)
		routers.WriteString("      tls:\n        certResolver: letsencrypt\n")
		fmt.Fprintf(&middlewares, "    %s-slash:\n      redirectRegex:\n", id)
		fmt.Fprintf(&middlewares, "        regex: '^(https?://[^/]+/%s)(\\?.*)?$'\n", name)
		middlewares.WriteString("        replacement: '${1}/${2}'\n")
		fmt.Fprintf(&middlewares, "    %s-strip:\n      stripPrefix:\n        prefixes: [\"/%s\"]\n", id, name)
	}
	var b strings.Builder
	b.WriteString("# Écrit par la console pi-appliance (services sur internet, spécification 8.2).\n")
	b.WriteString("# Ne pas modifier à la main : la console le réécrit à chaque changement.\n")
	b.WriteString("http:\n  routers:\n")
	b.WriteString(routers.String())
	b.WriteString("  middlewares:\n")
	b.WriteString(middlewares.String())
	return b.String()
}

// forwardTarget : clé de la session déléguée d'une adresse demandée et chemin
// de son cookie. Sous une adresse internet du Pi, un service publié a sa
// propre clé, <adresse>/<nom>, et ses cookies sous /<nom> : sa session ne
// vaut ni pour la console ni pour un autre service de la même adresse.
// service : l'adresse est celle d'un service publié ; protected : il demande
// une connexion.
func (c *console) forwardTarget(host, uri string) (key, cookiePath string, service, protected bool) {
	host = requestHostOf(host)
	if host == "" || !slices.Contains(c.internetHosts(), host) {
		return host, "/", false, false
	}
	path, _, _ := strings.Cut(uri, "?")
	first, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	publication, ok := c.publications()[first]
	if !ok || publication.Address == "" {
		return host, "/", false, false
	}
	protected = publication.Access == services.AccessUsers || publication.Access == services.AccessAdmins
	return host + "/" + first, "/" + first, true, protected
}
