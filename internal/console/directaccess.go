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
	"errors"
	"log"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/internal/addresses"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/internal/upnp"
)

// Accès direct depuis internet (spécification 7.2, TODO-3.8) : ouverture
// automatique des ports de la box par UPnP, avec l'accord de l'administrateur,
// et test de l'accès. API réservée à l'administrateur :
//   GET  api/internet-access       état de l'ouverture automatique
//   PUT  api/internet-access       {"enabled"} : accord donné ou retiré (maison seulement)
//   POST api/internet-access/test  ce que le Pi peut vérifier lui-même

// DirectPorts : ports à ouvrir sur la box : 80 et 443 (Traefik, certificats),
// puis ceux des services sans page web (51820/UDP de WireGuard…).
func DirectPorts(manager *services.Manager) []upnp.Mapping {
	ports := []upnp.Mapping{{Port: 80, Protocol: "TCP"}, {Port: 443, Protocol: "TCP"}}
	if manager == nil {
		return ports
	}
	instances, err := manager.List()
	if err != nil {
		log.Printf("ports à ouvrir : %v", err)
		return ports
	}
	for _, instance := range instances {
		for _, port := range instance.HostPorts {
			mapping := upnp.Mapping{Port: port.Port, Protocol: strings.ToUpper(port.Protocol)}
			if !slices.Contains(ports, mapping) {
				ports = append(ports, mapping)
			}
		}
	}
	return ports
}

// AccessStatus : réponse de GET api/internet-access.
type AccessStatus struct {
	// Automatic : l'ouverture automatique existe sur cette console.
	Automatic bool         `json:"automatic"`
	UPnP      *upnp.Status `json:"upnp,omitempty"`
	// Ports : ports à rediriger vers le Pi, à la main ou automatiquement.
	Ports []upnp.Mapping `json:"ports"`
	// LocalIP : adresse du Pi sur le réseau de la box, quand elle est connue.
	LocalIP string   `json:"local_ip,omitempty"`
	Hosts   []string `json:"hosts"`
}

func (c *console) accessState() AccessStatus {
	status := AccessStatus{Automatic: c.PortMapper != nil, Ports: DirectPorts(c.Services), Hosts: c.internetHosts()}
	if status.Hosts == nil {
		status.Hosts = []string{}
	}
	if c.PortMapper != nil {
		upnpStatus := c.PortMapper.Status()
		status.UPnP = &upnpStatus
		status.LocalIP = upnpStatus.LocalIP
	}
	return status
}

func (c *console) accessStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, c.accessState())
}

func (c *console) accessSave(w http.ResponseWriter, r *http.Request) {
	if c.PortMapper == nil {
		writeError(w, http.StatusServiceUnavailable, "Cette console n'ouvre pas les ports de la box : ouvrez-les à la main en suivant le guide.")
		return
	}
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.Enabled == nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	account, _ := c.account(r)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var err error
	if *request.Enabled {
		_, err = c.PortMapper.Enable(ctx, account.Name)
	} else {
		_, err = c.PortMapper.Disable(ctx, account.Name)
	}
	if err != nil {
		log.Printf("ouverture des ports : %v", err)
		writeError(w, http.StatusInternalServerError, "Impossible d'enregistrer votre choix.")
		return
	}
	writeJSON(w, http.StatusOK, c.accessState())
}

func (c *console) wakePorts() {
	if c.PortMapper != nil {
		c.PortMapper.WakeUp()
	}
}

// AccessTest : ce que le Pi vérifie lui-même. Il ne peut pas se joindre depuis
// internet : se connecter à sa propre adresse publique passe par la box
// (NAT loopback) et ne prouve rien. Le dernier mot revient à un essai depuis
// un téléphone en 4G (Wi-Fi coupé), que la console propose.
type AccessTest struct {
	Hosts []string `json:"hosts"`
	URL   string   `json:"url,omitempty"`
	// PublicIP : adresse publique vue d'internet (https://api.ipify.org).
	PublicIP      string `json:"public_ip,omitempty"`
	PublicIPError string `json:"public_ip_error,omitempty"`
	// Box : la box répond en UPnP, avec son nom et son adresse publique selon elle.
	Box           bool   `json:"box"`
	BoxName       string `json:"box_name,omitempty"`
	BoxExternalIP string `json:"box_external_ip,omitempty"`
	BoxError      string `json:"box_error,omitempty"`
	// Shared : la box n'a pas d'adresse publique à elle (adresse partagée par le
	// fournisseur, CGNAT, ou 4G) : l'accès direct est impossible.
	Shared bool `json:"shared"`
	// DoubleBox : la box a une adresse publique autre que celle vue d'internet :
	// un autre routeur est devant elle.
	DoubleBox bool       `json:"double_box"`
	DNS       []DNSCheck `json:"dns"`
	// Automatic : ouverture automatique en marche ; Ports : son état.
	Automatic bool             `json:"automatic"`
	Ports     []upnp.PortState `json:"ports"`
}

// DNSCheck : une adresse du Pi mène-t-elle à la box ?
type DNSCheck struct {
	Host      string   `json:"host"`
	Addresses []string `json:"addresses"`
	Matches   bool     `json:"matches"`
	Error     string   `json:"error,omitempty"`
}

func (c *console) accessTest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result := AccessTest{Hosts: c.internetHosts(), DNS: []DNSCheck{}, Ports: []upnp.PortState{}}
	if result.Hosts == nil {
		result.Hosts = []string{}
	}
	if len(result.Hosts) > 0 {
		result.URL = "https://" + result.Hosts[0] + BasePath + "/"
	}
	if c.PublicIP != nil {
		ip, err := c.PublicIP(ctx)
		if err != nil {
			result.PublicIPError = err.Error()
		}
		result.PublicIP = ip
	}
	if c.PortMapper != nil {
		gateway, external, err := c.PortMapper.Probe(ctx)
		switch {
		case gateway == nil:
			result.BoxError = upnp.Explain(err)
		default:
			result.Box = true
			result.BoxName = gateway.Name
			result.BoxExternalIP = external
		}
		status := c.PortMapper.Status()
		result.Automatic = status.Enabled
		if status.Enabled {
			result.Ports = status.Ports
		}
	}
	if ip := net.ParseIP(result.BoxExternalIP); ip != nil {
		result.Shared = !publicIP(ip)
		result.DoubleBox = !result.Shared && result.PublicIP != "" && result.PublicIP != ip.String()
	}
	lookup := c.LookupHost
	if lookup == nil {
		lookup = net.DefaultResolver.LookupHost
	}
	for _, host := range result.Hosts {
		check := DNSCheck{Host: host, Addresses: []string{}}
		found, err := lookup(ctx, host)
		if err != nil {
			check.Error = "Cette adresse n'existe pas encore sur internet."
		} else {
			check.Addresses = found
			check.Matches = result.PublicIP != "" && slices.Contains(found, result.PublicIP)
		}
		result.DNS = append(result.DNS, check)
	}
	writeJSON(w, http.StatusOK, result)
}

// publicIP : adresse joignable depuis internet (ni privée, ni partagée par le
// fournisseur en 100.64.0.0/10, ni réservée).
func publicIP(ip net.IP) bool {
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	_, shared, _ := net.ParseCIDR("100.64.0.0/10")
	return !shared.Contains(ip)
}

// addressService : GET api/address/service : le service d'adresses du projet
// répond-il ? L'assistant ne propose <nom>.pi-appliance.com qu'à cette condition.
func (c *console) addressService(w http.ResponseWriter, r *http.Request) {
	reachable := false
	if c.Address != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_, err := c.Address.Check(ctx, "essai")
		var refused *addresses.ServiceError
		reachable = err == nil || (errors.As(err, &refused) && refused.Status != http.StatusServiceUnavailable)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"reachable": reachable})
}
