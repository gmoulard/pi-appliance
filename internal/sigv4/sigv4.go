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

// Package sigv4 signe les requêtes HTTP selon AWS Signature Version 4, avec
// la bibliothèque standard seulement : Route 53 (service d'adresses) et les
// stockages en ligne compatibles S3 (sauvegardes, spécification 9.1).
package sigv4

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Credentials : clés d'accès.
// NE PAS journaliser ni versionner ni renvoyer par une API.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
}

// EmptyHash : SHA-256 d'un corps vide.
var EmptyHash = Hash(nil)

// Hash rend le SHA-256 de data en hexadécimal, comme l'attend SigV4.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Sign signe la requête : en-têtes host, content-type (s'il y est) et
// x-amz-* (x-amz-date posé ici) ; payloadHash est le SHA-256 du corps en
// hexadécimal (S3 le demande aussi dans x-amz-content-sha256, à poser avant).
func Sign(r *http.Request, payloadHash string, credentials Credentials, region, service string, now time.Time) {
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	r.Header.Set("X-Amz-Date", amzDate)
	host := r.Host
	if host == "" {
		host = r.URL.Host
	}
	headers := map[string]string{"host": host}
	for name, values := range r.Header {
		lower := strings.ToLower(name)
		if lower == "content-type" || strings.HasPrefix(lower, "x-amz-") {
			headers[lower] = strings.Join(values, ",")
		}
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
	canonical := strings.Join([]string{r.Method, path, canonicalQuery(r), canonicalHeaders.String(), signedHeaders, payloadHash}, "\n")
	scope := day + "/" + region + "/" + service + "/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + Hash([]byte(canonical))
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
			parts = append(parts, Escape(key)+"="+Escape(value))
		}
	}
	return strings.Join(parts, "&")
}

// Escape encode s comme le demande SigV4 : tout sauf A-Z a-z 0-9 - _ . ~.
func Escape(s string) string {
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

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
