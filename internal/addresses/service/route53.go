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

package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// DNS écrit les alias <nom>.pi-appliance.com et *.<nom>.pi-appliance.com.
type DNS interface {
	// Set crée ou remplace les deux alias vers target.
	Set(ctx context.Context, hostname, target string) error
	// Remove retire les deux alias, qui pointaient vers target ; absents : pas d'erreur.
	Remove(ctx context.Context, hostname, target string) error
}

// Credentials : clés d'accès AWS du service, limitées à la zone pi-appliance.com.
// NE PAS journaliser ni versionner : elles se trouvent sur le Pi du projet seulement.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
}

// LoadCredentials lit un fichier au format de la CLI AWS (~/.aws/credentials), section profile.
func LoadCredentials(path, profile string) (Credentials, error) {
	file, err := os.Open(path)
	if err != nil {
		return Credentials{}, err
	}
	defer file.Close()
	var credentials Credentials
	section := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || section != profile {
			continue
		}
		switch strings.TrimSpace(key) {
		case "aws_access_key_id":
			credentials.AccessKeyID = strings.TrimSpace(value)
		case "aws_secret_access_key":
			credentials.SecretAccessKey = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return Credentials{}, err
	}
	if credentials.AccessKeyID == "" || credentials.SecretAccessKey == "" {
		return Credentials{}, fmt.Errorf("%s : profil %q sans aws_access_key_id ni aws_secret_access_key", path, profile)
	}
	return credentials, nil
}

// Doer envoie une requête HTTP ; *http.Client en est un, les tests le remplacent.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Route53 : alias écrits dans une zone hébergée d'AWS Route 53, par l'API
// ChangeResourceRecordSets signée en SigV4 (bibliothèque standard seulement).
type Route53 struct {
	ZoneID      string
	Credentials Credentials
	TTL         int
	Endpoint    string // https://route53.amazonaws.com par défaut
	HTTP        Doer
	Now         func() time.Time
}

const route53Namespace = "https://route53.amazonaws.com/doc/2013-04-01/"

type changeRequest struct {
	XMLName xml.Name `xml:"ChangeResourceRecordSetsRequest"`
	Xmlns   string   `xml:"xmlns,attr"`
	Comment string   `xml:"ChangeBatch>Comment"`
	Changes []change `xml:"ChangeBatch>Changes>Change"`
}

type change struct {
	Action string `xml:"Action"`
	Name   string `xml:"ResourceRecordSet>Name"`
	Type   string `xml:"ResourceRecordSet>Type"`
	TTL    int    `xml:"ResourceRecordSet>TTL"`
	Value  string `xml:"ResourceRecordSet>ResourceRecords>ResourceRecord>Value"`
}

type route53Error struct {
	Code    string `xml:"Error>Code"`
	Message string `xml:"Error>Message"`
}

func (r *Route53) Set(ctx context.Context, hostname, target string) error {
	return r.change(ctx, "UPSERT", hostname, target)
}

func (r *Route53) Remove(ctx context.Context, hostname, target string) error {
	err := r.change(ctx, "DELETE", hostname, target)
	var refused *route53Error
	// Alias déjà absents (retirés à la main, ou cible différente) : rien à retirer.
	if errors.As(err, &refused) && refused.Code == "InvalidChangeBatch" && strings.Contains(refused.Message, "not found") {
		return nil
	}
	return err
}

func (e *route53Error) Error() string { return "Route 53 : " + e.Code + " : " + e.Message }

func (r *Route53) change(ctx context.Context, action, hostname, target string) error {
	ttl := r.TTL
	if ttl == 0 {
		ttl = 300
	}
	request := changeRequest{Xmlns: route53Namespace, Comment: "pi-appliance " + action + " " + hostname}
	for _, name := range []string{hostname + ".", "*." + hostname + "."} {
		request.Changes = append(request.Changes, change{Action: action, Name: name, Type: "CNAME", TTL: ttl, Value: target})
	}
	body, err := xml.Marshal(request)
	if err != nil {
		return err
	}
	body = append([]byte(xml.Header), body...)
	endpoint := r.Endpoint
	if endpoint == "" {
		endpoint = "https://route53.amazonaws.com"
	}
	path := "/2013-04-01/hostedzone/" + r.ZoneID + "/rrset/"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpRequest.Header.Set("Content-Type", "application/xml")
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	signV4(httpRequest, body, r.Credentials, "us-east-1", "route53", now())
	doer := r.HTTP
	if doer == nil {
		doer = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := doer.Do(httpRequest)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if response.StatusCode == http.StatusOK {
		return nil
	}
	var refused route53Error
	if xml.Unmarshal(data, &refused) == nil && refused.Code != "" {
		return &refused
	}
	return fmt.Errorf("Route 53 : réponse %d", response.StatusCode)
}

// signV4 signe la requête selon AWS Signature Version 4 : en-têtes host,
// x-amz-date et content-type (s'il y est), corps haché en SHA-256.
func signV4(r *http.Request, body []byte, credentials Credentials, region, service string, now time.Time) {
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	r.Header.Set("X-Amz-Date", amzDate)
	host := r.Host
	if host == "" {
		host = r.URL.Host
	}
	headers := map[string]string{"host": host, "x-amz-date": amzDate}
	if value := r.Header.Get("Content-Type"); value != "" {
		headers["content-type"] = value
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name + ":" + strings.TrimSpace(headers[name]) + "\n")
	}
	signedHeaders := strings.Join(names, ";")
	path := r.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	canonical := strings.Join([]string{r.Method, path, canonicalQuery(r), canonicalHeaders.String(), signedHeaders, hashHex(body)}, "\n")
	scope := day + "/" + region + "/" + service + "/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hashHex([]byte(canonical))
	key := hmacSHA256([]byte("AWS4"+credentials.SecretAccessKey), day)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, service)
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, toSign))
	r.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+credentials.AccessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

// canonicalQuery : paramètres triés, encodés comme le demande SigV4 (espace en %20).
func canonicalQuery(r *http.Request) string {
	query := r.URL.Query()
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		values := query[key]
		sort.Strings(values)
		for _, value := range values {
			parts = append(parts, awsEscape(key)+"="+awsEscape(value))
		}
	}
	return strings.Join(parts, "&")
}

func awsEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
