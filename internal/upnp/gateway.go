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

// Package upnp ouvre les ports du Pi sur la box par UPnP-IGD (accès direct,
// spécification 7.2) : découverte de la box en SSDP (multidiffusion), lecture
// de sa description XML, puis AddPortMapping, DeletePortMapping et
// GetExternalIPAddress en SOAP. Bibliothèque standard seulement.
package upnp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SSDPAddress : adresse de multidiffusion de la découverte UPnP.
const SSDPAddress = "239.255.255.250:1900"

// Services de la box qui savent ouvrir un port, du plus récent au plus ancien.
var serviceTypes = []string{
	"urn:schemas-upnp-org:service:WANIPConnection:2",
	"urn:schemas-upnp-org:service:WANIPConnection:1",
	"urn:schemas-upnp-org:service:WANPPPConnection:1",
}

// Ce que la découverte demande : une box (IGD 1 ou 2).
var searchTargets = []string{
	"urn:schemas-upnp-org:device:InternetGatewayDevice:1",
	"urn:schemas-upnp-org:device:InternetGatewayDevice:2",
}

// ErrNoGateway : aucune box n'a répondu en UPnP (UPnP coupé sur la box, ou
// box qui ne le connaît pas).
var ErrNoGateway = errors.New("aucune box ne répond en UPnP")

// Mapping : un port du Pi ouvert sur la box, même numéro des deux côtés.
type Mapping struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"` // TCP ou UDP
}

func (m Mapping) String() string { return fmt.Sprintf("%d/%s", m.Port, m.Protocol) }

// Error : refus de la box, avec son code UPnP.
type Error struct {
	Code        int
	Description string
}

func (e *Error) Error() string {
	return fmt.Sprintf("la box refuse (code UPnP %d %s)", e.Code, e.Description)
}

// Codes UPnP utiles (UPnP-gw-WANIPConnection).
const (
	CodeNotAuthorized = 606
	CodeNoSuchEntry   = 714
	CodeConflict      = 718
	CodeOnlyPermanent = 725
)

// Gateway : la box trouvée, et l'adresse du Pi vue de la box.
type Gateway struct {
	// Name : nom donné par la box (friendlyName), pour l'écran.
	Name        string `json:"name"`
	ControlURL  string `json:"-"`
	ServiceType string `json:"-"`
	// LocalIP : adresse du Pi sur le réseau de la box, vers qui les ports sont ouverts.
	LocalIP string       `json:"local_ip"`
	Client  *http.Client `json:"-"`
}

// Discoverer cherche la box. Address vide : multidiffusion SSDP ; les tests
// donnent l'adresse d'un faux SSDP.
type Discoverer struct {
	Address string
	Timeout time.Duration
	Client  *http.Client
}

// Discover envoie la recherche SSDP et garde la première box qui décrit un
// service capable d'ouvrir des ports.
func (d Discoverer) Discover(ctx context.Context) (*Gateway, error) {
	address := d.Address
	if address == "" {
		address = SSDPAddress
	}
	timeout := d.Timeout
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	target, err := net.ResolveUDPAddr("udp4", address)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, fmt.Errorf("recherche de la box impossible : %w", err)
	}
	defer conn.Close()
	for _, st := range searchTargets {
		request := "M-SEARCH * HTTP/1.1\r\n" +
			"HOST: " + SSDPAddress + "\r\n" +
			"MAN: \"ssdp:discover\"\r\n" +
			"MX: 2\r\n" +
			"ST: " + st + "\r\n\r\n"
		if _, err := conn.WriteTo([]byte(request), target); err != nil {
			return nil, fmt.Errorf("recherche de la box impossible : %w", err)
		}
	}
	deadline := time.Now().Add(timeout)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	conn.SetReadDeadline(deadline)
	seen := map[string]bool{}
	buffer := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFromUDP(buffer)
		if err != nil {
			return nil, ErrNoGateway
		}
		location := ssdpLocation(buffer[:n])
		if location == "" || seen[location] || !sameHost(location, from.IP) {
			continue
		}
		seen[location] = true
		gateway, err := d.describe(ctx, location)
		if err == nil {
			return gateway, nil
		}
	}
}

// ssdpLocation lit l'en-tête LOCATION d'une réponse SSDP.
func ssdpLocation(data []byte) string {
	reader := textproto.NewReader(bufio.NewReader(bytes.NewReader(data)))
	status, err := reader.ReadLine()
	if err != nil || !strings.HasPrefix(status, "HTTP/1.") || !strings.Contains(status, " 200") {
		return ""
	}
	header, err := reader.ReadMIMEHeader()
	if err != nil && len(header) == 0 {
		return ""
	}
	return strings.TrimSpace(header.Get("Location"))
}

// sameHost : la description est lue chez l'appareil qui a répondu, sur le
// réseau de la maison.
// NE PAS relâcher : sans cela, n'importe quel appareil de la maison ferait
// lire au Pi une adresse de son choix, sur internet comprise.
func sameHost(location string, from net.IP) bool {
	parsed, err := url.Parse(location)
	if err != nil || parsed.Scheme != "http" {
		return false
	}
	ip := net.ParseIP(parsed.Hostname())
	if ip == nil || !ip.Equal(from) {
		return false
	}
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

func (d Discoverer) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// Description XML de la box : arbre d'appareils et leurs services.
type device struct {
	FriendlyName string    `xml:"friendlyName"`
	Services     []service `xml:"serviceList>service"`
	Devices      []device  `xml:"deviceList>device"`
}

type service struct {
	ServiceType string `xml:"serviceType"`
	ControlURL  string `xml:"controlURL"`
}

type root struct {
	URLBase string `xml:"URLBase"`
	Device  device `xml:"device"`
}

func (d Discoverer) describe(ctx context.Context, location string) (*Gateway, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, err
	}
	response, err := d.client().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("description : code HTTP %d", response.StatusCode)
	}
	var description root
	if err := xml.NewDecoder(io.LimitReader(response.Body, 256<<10)).Decode(&description); err != nil {
		return nil, fmt.Errorf("description illisible : %w", err)
	}
	base, _ := url.Parse(location)
	if description.URLBase != "" {
		if parsed, err := url.Parse(description.URLBase); err == nil && parsed.Host == base.Host {
			base = parsed
		}
	}
	for _, wanted := range serviceTypes {
		if found, ok := findService(description.Device, wanted); ok {
			control, err := base.Parse(found.ControlURL)
			// Même appareil que la description : pas d'envoi ailleurs.
			if err != nil || control.Host != base.Host || control.Scheme != "http" {
				continue
			}
			local, err := localIP(base.Host)
			if err != nil {
				return nil, err
			}
			name := strings.TrimSpace(description.Device.FriendlyName)
			if name == "" {
				name = base.Hostname()
			}
			return &Gateway{Name: name, ControlURL: control.String(), ServiceType: wanted, LocalIP: local, Client: d.client()}, nil
		}
	}
	return nil, errors.New("la box ne propose pas d'ouvrir des ports")
}

func findService(d device, wanted string) (service, bool) {
	for _, s := range d.Services {
		if s.ServiceType == wanted {
			return s, true
		}
	}
	for _, child := range d.Devices {
		if s, ok := findService(child, wanted); ok {
			return s, true
		}
	}
	return service{}, false
}

// localIP : adresse du Pi par laquelle il joint la box (rien n'est envoyé :
// un « connect » UDP choisit seulement l'interface).
func localIP(host string) (string, error) {
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "80")
	}
	conn, err := net.Dial("udp4", host)
	if err != nil {
		return "", fmt.Errorf("adresse du Pi introuvable : %w", err)
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}

// ExternalIP : adresse publique de la box, d'après elle.
func (g *Gateway) ExternalIP(ctx context.Context) (string, error) {
	values, err := g.call(ctx, "GetExternalIPAddress", nil, "NewExternalIPAddress")
	if err != nil {
		return "", err
	}
	return values["NewExternalIPAddress"], nil
}

// Add ouvre le port m de la box vers le Pi ; lease 0 : sans fin.
func (g *Gateway) Add(ctx context.Context, m Mapping, lease time.Duration, description string) error {
	_, err := g.call(ctx, "AddPortMapping", [][2]string{
		{"NewRemoteHost", ""},
		{"NewExternalPort", strconv.Itoa(m.Port)},
		{"NewProtocol", m.Protocol},
		{"NewInternalPort", strconv.Itoa(m.Port)},
		{"NewInternalClient", g.LocalIP},
		{"NewEnabled", "1"},
		{"NewPortMappingDescription", description},
		{"NewLeaseDuration", strconv.Itoa(int(lease / time.Second))},
	})
	return err
}

// Delete referme le port m de la box.
func (g *Gateway) Delete(ctx context.Context, m Mapping) error {
	_, err := g.call(ctx, "DeletePortMapping", [][2]string{
		{"NewRemoteHost", ""},
		{"NewExternalPort", strconv.Itoa(m.Port)},
		{"NewProtocol", m.Protocol},
	})
	return err
}

// Owner : adresse de l'appareil vers qui la box fait suivre le port m ; vide
// si le port n'est pas ouvert.
func (g *Gateway) Owner(ctx context.Context, m Mapping) (string, error) {
	values, err := g.call(ctx, "GetSpecificPortMappingEntry", [][2]string{
		{"NewRemoteHost", ""},
		{"NewExternalPort", strconv.Itoa(m.Port)},
		{"NewProtocol", m.Protocol},
	}, "NewInternalClient")
	var refused *Error
	if errors.As(err, &refused) && refused.Code == CodeNoSuchEntry {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return values["NewInternalClient"], nil
}

// call envoie une action SOAP et lit les valeurs demandées de la réponse.
func (g *Gateway) call(ctx context.Context, action string, arguments [][2]string, wanted ...string) (map[string]string, error) {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0"?>` + "\r\n" +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&body, `<u:%s xmlns:u="%s">`, action, g.ServiceType)
	for _, argument := range arguments {
		body.WriteString("<" + argument[0] + ">")
		xml.EscapeText(&body, []byte(argument[1]))
		body.WriteString("</" + argument[0] + ">")
	}
	fmt.Fprintf(&body, `</u:%s></s:Body></s:Envelope>`, action)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, g.ControlURL, strings.NewReader(body.String()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	request.Header.Set("SOAPAction", `"`+g.ServiceType+"#"+action+`"`)
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("la box ne répond pas")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return nil, errors.New("réponse de la box interrompue")
	}
	if response.StatusCode != http.StatusOK {
		values := xmlValues(data, "errorCode", "errorDescription")
		if code, err := strconv.Atoi(values["errorCode"]); err == nil {
			return nil, &Error{Code: code, Description: values["errorDescription"]}
		}
		return nil, fmt.Errorf("la box refuse (code HTTP %d)", response.StatusCode)
	}
	return xmlValues(data, wanted...), nil
}

// xmlValues : texte des éléments nommés (nom local, sans espace de noms).
func xmlValues(data []byte, names ...string) map[string]string {
	values := map[string]string{}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	current := ""
	for {
		token, err := decoder.Token()
		if err != nil {
			return values
		}
		switch t := token.(type) {
		case xml.StartElement:
			current = t.Name.Local
		case xml.EndElement:
			current = ""
		case xml.CharData:
			for _, name := range names {
				if current == name {
					values[name] += strings.TrimSpace(string(t))
				}
			}
		}
	}
}
