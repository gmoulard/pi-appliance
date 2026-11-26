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

package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DNS écrit l'adresse IP de <nom>.pi-appliance.com et *.<nom>.pi-appliance.com.
type DNS interface {
	// Set crée ou remplace les deux enregistrements A, vers ip.
	Set(ctx context.Context, hostname, ip string) error
	// Remove retire les deux enregistrements ; absents : pas d'erreur.
	Remove(ctx context.Context, hostname string) error
}

// Credentials : clés de l'API OVH du service, les mêmes que les secrets GitHub
// Actions OVH_Application_key, OVH_Application_secret et OVH_Consumer_Key
// (décision du 2026-11-26). NE PAS journaliser ni versionner : elles se
// trouvent sur le Pi du projet seulement, jamais sur les Pi des utilisateurs.
type Credentials struct {
	ApplicationKey    string
	ApplicationSecret string
	ConsumerKey       string
}

// LoadCredentials lit un fichier de lignes NOM=valeur, aux noms des secrets
// GitHub Actions (OVH_Application_key, OVH_Application_secret,
// OVH_Consumer_Key) ; lignes vides et commentaires « # » ignorés.
func LoadCredentials(path string) (Credentials, error) {
	file, err := os.Open(path)
	if err != nil {
		return Credentials{}, err
	}
	defer file.Close()
	var credentials Credentials
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "OVH_Application_key":
			credentials.ApplicationKey = value
		case "OVH_Application_secret":
			credentials.ApplicationSecret = value
		case "OVH_Consumer_Key":
			credentials.ConsumerKey = value
		}
	}
	if err := scanner.Err(); err != nil {
		return Credentials{}, err
	}
	if credentials.ApplicationKey == "" || credentials.ApplicationSecret == "" || credentials.ConsumerKey == "" {
		return Credentials{}, fmt.Errorf("%s : il faut OVH_Application_key, OVH_Application_secret et OVH_Consumer_Key", path)
	}
	return credentials, nil
}

// Doer envoie une requête HTTP ; *http.Client en est un, les tests le remplacent.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// DefaultOVHEndpoint : API OVH Europe.
const DefaultOVHEndpoint = "https://eu.api.ovh.com/1.0"

// OVH : enregistrements A écrits dans la zone pi-appliance.com chez OVH, par
// l'API /domain/zone/{zone}/record puis /refresh, en requêtes signées
// (bibliothèque standard seulement).
type OVH struct {
	Zone        string // pi-appliance.com
	Credentials Credentials
	TTL         int
	Endpoint    string // DefaultOVHEndpoint par défaut
	HTTP        Doer
	Now         func() time.Time

	mu    sync.Mutex
	delta time.Duration // heure d'OVH moins heure du Pi, lue par /auth/time
	timed bool
}

// OVHError : refus de l'API OVH.
type OVHError struct {
	Status  int
	Message string
}

func (e *OVHError) Error() string {
	return fmt.Sprintf("OVH : réponse %d : %s", e.Status, e.Message)
}

type ovhRecord struct {
	ID        int64  `json:"id,omitempty"`
	FieldType string `json:"fieldType,omitempty"`
	SubDomain string `json:"subDomain,omitempty"`
	Target    string `json:"target"`
	TTL       int    `json:"ttl"`
}

func (o *OVH) Set(ctx context.Context, hostname, ip string) error {
	sub, err := o.subDomain(hostname)
	if err != nil {
		return err
	}
	ttl := o.TTL
	if ttl == 0 {
		ttl = 300
	}
	for _, name := range []string{sub, "*." + sub} {
		// Un CNAME ne peut pas côtoyer un A : ceux des anciens alias vers
		// DuckDNS (Route 53) sont retirés.
		if err := o.removeType(ctx, name, "CNAME"); err != nil {
			return err
		}
		ids, err := o.records(ctx, name, "A")
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			if err := o.call(ctx, http.MethodPost, "/domain/zone/"+o.Zone+"/record", ovhRecord{FieldType: "A", SubDomain: name, Target: ip, TTL: ttl}, nil); err != nil {
				return err
			}
			continue
		}
		if err := o.call(ctx, http.MethodPut, o.recordPath(ids[0]), ovhRecord{Target: ip, TTL: ttl}, nil); err != nil {
			return err
		}
		for _, id := range ids[1:] {
			if err := o.call(ctx, http.MethodDelete, o.recordPath(id), nil, nil); err != nil {
				return err
			}
		}
	}
	return o.refresh(ctx)
}

func (o *OVH) Remove(ctx context.Context, hostname string) error {
	sub, err := o.subDomain(hostname)
	if err != nil {
		return err
	}
	for _, name := range []string{sub, "*." + sub} {
		for _, fieldType := range []string{"A", "CNAME"} {
			if err := o.removeType(ctx, name, fieldType); err != nil {
				return err
			}
		}
	}
	return o.refresh(ctx)
}

// subDomain : <nom> de <nom>.<zone>.
func (o *OVH) subDomain(hostname string) (string, error) {
	sub, found := strings.CutSuffix(strings.TrimSuffix(hostname, "."), "."+o.Zone)
	if !found || sub == "" || strings.Contains(sub, ".") {
		return "", fmt.Errorf("%s n'est pas un nom de la zone %s", hostname, o.Zone)
	}
	return sub, nil
}

func (o *OVH) recordPath(id int64) string {
	return "/domain/zone/" + o.Zone + "/record/" + strconv.FormatInt(id, 10)
}

func (o *OVH) records(ctx context.Context, sub, fieldType string) ([]int64, error) {
	query := url.Values{"fieldType": {fieldType}, "subDomain": {sub}}
	var ids []int64
	err := o.call(ctx, http.MethodGet, "/domain/zone/"+o.Zone+"/record?"+query.Encode(), nil, &ids)
	return ids, err
}

func (o *OVH) removeType(ctx context.Context, sub, fieldType string) error {
	ids, err := o.records(ctx, sub, fieldType)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := o.call(ctx, http.MethodDelete, o.recordPath(id), nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func (o *OVH) refresh(ctx context.Context) error {
	return o.call(ctx, http.MethodPost, "/domain/zone/"+o.Zone+"/refresh", nil, nil)
}

func (o *OVH) endpoint() string {
	if o.Endpoint != "" {
		return strings.TrimSuffix(o.Endpoint, "/")
	}
	return DefaultOVHEndpoint
}

func (o *OVH) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o *OVH) doer() Doer {
	if o.HTTP != nil {
		return o.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// timestamp : heure d'OVH, lue une fois par /auth/time (non signé), puis
// déduite de l'horloge du Pi.
func (o *OVH) timestamp(ctx context.Context) (int64, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.timed {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, o.endpoint()+"/auth/time", nil)
		if err != nil {
			return 0, err
		}
		response, err := o.doer().Do(request)
		if err != nil {
			return 0, err
		}
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<10))
		response.Body.Close()
		server, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if response.StatusCode != http.StatusOK || err != nil {
			return 0, fmt.Errorf("OVH : heure illisible (réponse %d)", response.StatusCode)
		}
		o.delta = time.Unix(server, 0).Sub(o.now())
		o.timed = true
	}
	return o.now().Add(o.delta).Unix(), nil
}

// Signature : "$1$" + SHA-1 en hexadécimal de
// secret+clé client+méthode+URL+corps+horodatage, séparés par « + ».
func ovhSignature(secret, consumer, method, url, body string, timestamp int64) string {
	sum := sha1.Sum([]byte(strings.Join([]string{secret, consumer, method, url, body, strconv.FormatInt(timestamp, 10)}, "+")))
	return "$1$" + hex.EncodeToString(sum[:])
}

// call envoie une requête signée et décode la réponse dans result (nil : ignorée).
func (o *OVH) call(ctx context.Context, method, path string, body, result any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	timestamp, err := o.timestamp(ctx)
	if err != nil {
		return err
	}
	address := o.endpoint() + path
	request, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Ovh-Application", o.Credentials.ApplicationKey)
	request.Header.Set("X-Ovh-Consumer", o.Credentials.ConsumerKey)
	request.Header.Set("X-Ovh-Timestamp", strconv.FormatInt(timestamp, 10))
	request.Header.Set("X-Ovh-Signature", ovhSignature(o.Credentials.ApplicationSecret, o.Credentials.ConsumerKey, method, address, string(payload), timestamp))
	response, err := o.doer().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if response.StatusCode >= 300 {
		var refused struct {
			Message string `json:"message"`
		}
		json.Unmarshal(data, &refused)
		if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			// Horloge peut-être décalée : l'heure d'OVH sera relue.
			o.mu.Lock()
			o.timed = false
			o.mu.Unlock()
		}
		return &OVHError{Status: response.StatusCode, Message: refused.Message}
	}
	if result == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, result)
}
