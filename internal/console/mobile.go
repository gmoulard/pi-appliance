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

// Application mobile (spécification 5.2, TODO-5.16) : la demande api/hello,
// sans connexion, et le lien d'ajout du Pi avec son code QR, pour
// l'administrateur. Ni l'une ni l'autre ne porte de secret : nom du Pi,
// version, empreinte de la clé publique, adresses des services.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

const (
	// MobileApp : adresse de l'application mobile (dépôt gmoulard/pi-appliance-pwa).
	MobileApp = "https://app.pi-appliance.com"
	// mobileAdd : début du lien qui ajoute ce Pi à l'application.
	mobileAdd = MobileApp + "/#add?"
)

// Hello : réponse de GET api/hello, lue par l'application mobile. Rien d'autre
// n'y entre : ni état du système, ni comptes.
type Hello struct {
	Name     string         `json:"name"`
	Version  string         `json:"version"`
	ID       string         `json:"id"`
	Services []HelloService `json:"services"`
}

// HelloService : un service qui a une page web, et son adresse complète.
type HelloService struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// MobileLink : lien d'ajout du Pi à l'application et son code QR (PNG en adresse data:).
type MobileLink struct {
	Link string `json:"link"`
	QR   string `json:"qr"`
}

// DeviceID : identifiant stable et non secret du Pi, les 16 premiers
// caractères hexadécimaux du SHA-256 des 32 octets de sa clé publique Ed25519
// (device.pub, PEM « PUBLIC KEY », créée par secrets.sh avec device.key).
// Vide si la clé manque ou est illisible.
func DeviceID(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return ""
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return ""
	}
	public, ok := key.(ed25519.PublicKey)
	if !ok {
		return ""
	}
	sum := sha256.Sum256(public)
	return hex.EncodeToString(sum[:])[:16]
}

// piName : nom du Pi sur le réseau (<nom>.local), celui que name.sh donne au système.
func (c *console) piName() string {
	hostname := c.Hostname
	if hostname == nil {
		hostname = os.Hostname
	}
	name, err := hostname()
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), ".local"))
	if err != nil || name == "" || name == "localhost" {
		return "pi-appliance"
	}
	return name
}

// internetHost : adresse du Pi sur internet, s'il en a une : l'adresse
// personnelle de la console, sinon <nom>.pi-appliance.com réservée, sinon le
// nom DuckDNS, sinon le domaine personnel des services.
func (c *console) internetHost() string {
	if domain := c.internetSettings().Domain; domain != "" {
		return domain
	}
	if c.Address != nil {
		if host := c.Address.Status().Hostname; host != "" {
			return host
		}
	}
	if c.DuckDNS != nil {
		if host := c.DuckDNS.Status().Hostname; host != "" {
			return host
		}
	}
	if c.Services != nil {
		domain := strings.TrimSuffix(strings.ToLower(c.Services.CurrentDomain()), ".")
		if domain != "" && !strings.HasSuffix(domain, ".local") && strings.Contains(domain, ".") {
			return domain
		}
	}
	return ""
}

// helloServices : services qui ont une page web, par ordre alphabétique.
func (c *console) helloServices() []HelloService {
	list := []HelloService{}
	instances, err := c.Services.List()
	if err != nil {
		log.Printf("liste des services : %v", err)
		return list
	}
	for _, instance := range instances {
		if instance.NoWebPage || instance.Address == "" {
			continue
		}
		list = append(list, HelloService{Name: instance.Name, Address: "https://" + strings.ToLower(instance.Address)})
	}
	slices.SortFunc(list, func(a, b HelloService) int { return strings.Compare(a.Name, b.Name) })
	return list
}

func (c *console) hello() Hello {
	return Hello{Name: c.piName(), Version: c.Version, ID: DeviceID(c.DevicePublicKey), Services: c.helloServices()}
}

// NE PAS élargir : api/hello est ouverte à tous, sans connexion, et sur
// internet (Traefik, console.yml) ; elle ne dit que le nom, la version,
// l'empreinte de la clé publique et les adresses des services. CORS pour
// l'application mobile seulement, sans cookie.
func helloHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", MobileApp)
	w.Header().Set("Cache-Control", "no-store")
}

func (c *console) helloGet(w http.ResponseWriter, _ *http.Request) {
	helloHeaders(w)
	writeJSON(w, http.StatusOK, c.hello())
}

// helloPreflight : demande préalable du navigateur ; Access-Control-Allow-Private-Network
// pour Chrome, qui la demande avant qu'une page d'internet joigne le réseau de la maison.
func (c *console) helloPreflight(w http.ResponseWriter, _ *http.Request) {
	helloHeaders(w)
	w.Header().Set("Access-Control-Allow-Methods", "GET")
	w.Header().Set("Access-Control-Allow-Private-Network", "true")
	w.Header().Set("Access-Control-Max-Age", "600")
	w.WriteHeader(http.StatusNoContent)
}

// mobileLink : https://app.pi-appliance.com/#add?id=…&l=…&n=…&s=…&s=…&w=…
// (spécification 5.2) ; un paramètre s par service, « <nom>,<adresse> ».
func (c *console) mobileLink() string {
	hello := c.hello()
	values := url.Values{}
	values.Set("n", hello.Name)
	values.Set("l", "https://"+hello.Name+".local"+BasePath)
	if host := c.internetHost(); host != "" {
		values.Set("w", "https://"+host+BasePath)
	}
	if hello.ID != "" {
		values.Set("id", hello.ID)
	}
	for _, service := range hello.Services {
		values.Add("s", service.Name+","+service.Address)
	}
	return mobileAdd + values.Encode()
}

// mobile : le lien à partager, sans code d'appairage, et le code QR, qui
// ajoute le code d'appairage c de l'administrateur connecté (spécification 8.1).
func (c *console) mobile(w http.ResponseWriter, r *http.Request) {
	link := c.mobileLink()
	scanned := link
	if account, ok := c.account(r); ok && DeviceID(c.DevicePublicKey) != "" {
		code, _ := c.Accounts.PairingCode(account.Name)
		scanned += "&c=" + url.QueryEscape(code)
	}
	png, err := qrcode.Encode(scanned, qrcode.Medium, 512)
	if err != nil {
		log.Printf("code QR de l'application mobile : %v", err)
		writeError(w, http.StatusInternalServerError, "Le code à scanner n'a pas pu être dessiné.")
		return
	}
	writeJSON(w, http.StatusOK, MobileLink{Link: link, QR: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)})
}
