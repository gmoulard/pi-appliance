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

package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/internal/sigv4"
)

// Stockage en ligne compatible S3 (spécification 9.1) : AWS S3, OVH Object
// Storage (S3), Scaleway, Backblaze B2 (S3), MinIO… Adresse en style
// « chemin » (https://<adresse>/<seau>/<objet>), compris de tous ; requêtes
// signées en SigV4 (internal/sigv4). Les fichiers, déjà chiffrés, sont
// envoyés par morceaux de 16 Mio (envoi en plusieurs parties), sans copie
// sur la carte SD. Une sauvegarde est sous pi-appliance-backups/<nom du Pi>/<date>/.

// OnlineSettings : le stockage en ligne choisi. Les clés d'accès restent sur
// le Pi (backup/online.json, droits 600, jamais sauvegardé) et ne sont
// jamais renvoyées par l'API.
type OnlineSettings struct {
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

// OnlinePublic : le stockage en ligne vu par la console, sans les clés.
type OnlinePublic struct {
	Configured bool   `json:"configured"`
	Endpoint   string `json:"endpoint,omitempty"`
	Region     string `json:"region,omitempty"`
	Bucket     string `json:"bucket,omitempty"`
}

var (
	bucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	regionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)
)

// onlinePartSize : taille des morceaux envoyés (5 Mio au moins pour S3) ; réduite dans les tests.
var onlinePartSize = 16 << 20

// onlineRetryDelay : attente avant de réessayer une requête ; réduite dans les tests.
var onlineRetryDelay = 2 * time.Second

func (s *OnlineSettings) check() error {
	s.Endpoint = strings.TrimRight(strings.TrimSpace(s.Endpoint), "/")
	if s.Endpoint != "" && !strings.Contains(s.Endpoint, "://") {
		s.Endpoint = "https://" + s.Endpoint
	}
	s.Region = strings.ToLower(strings.TrimSpace(s.Region))
	s.Bucket = strings.TrimSpace(s.Bucket)
	s.AccessKeyID = strings.TrimSpace(s.AccessKeyID)
	s.SecretAccessKey = strings.TrimSpace(s.SecretAccessKey)
	if s.Region == "" {
		s.Region = "us-east-1"
	}
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		return fmt.Errorf("%w : l'adresse du stockage commence par https:// (par exemple https://s3.gra.io.cloud.ovh.net)", ErrInvalid)
	}
	if !regionPattern.MatchString(s.Region) {
		return fmt.Errorf("%w : région illisible (par exemple gra, fr-par ou eu-west-3)", ErrInvalid)
	}
	if !bucketPattern.MatchString(s.Bucket) {
		return fmt.Errorf("%w : nom de seau (bucket) illisible : 3 à 63 lettres minuscules, chiffres, points ou tirets", ErrInvalid)
	}
	if s.AccessKeyID == "" || s.SecretAccessKey == "" || strings.ContainsAny(s.AccessKeyID+s.SecretAccessKey, " \t\r\n") {
		return fmt.Errorf("%w : saisissez les deux clés d'accès", ErrInvalid)
	}
	return nil
}

func (m *Manager) onlinePath() string { return filepath.Join(m.Dir, stateDir, "online.json") }

// OnlineSettings rend le stockage en ligne enregistré, faux s'il n'y en a pas.
func (m *Manager) OnlineSettings() (OnlineSettings, bool) {
	var settings OnlineSettings
	data, err := os.ReadFile(m.onlinePath())
	if err != nil || json.Unmarshal(data, &settings) != nil || settings.check() != nil {
		return OnlineSettings{}, false
	}
	return settings, true
}

// SetOnline enregistre le stockage en ligne après avoir vérifié qu'il
// accepte d'écrire et d'effacer ; des clés vides gardent celles enregistrées.
func (m *Manager) SetOnline(ctx context.Context, settings OnlineSettings) error {
	if current, ok := m.OnlineSettings(); ok && settings.AccessKeyID == "" && settings.SecretAccessKey == "" {
		settings.AccessKeyID, settings.SecretAccessKey = current.AccessKeyID, current.SecretAccessKey
	}
	if err := settings.check(); err != nil {
		return err
	}
	if err := m.newOnline(settings).probe(ctx); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	defer m.forget()
	return writeFile(m.onlinePath(), append(data, '\n'), 0o600)
}

// RemoveOnline arrête les envois en ligne ; les sauvegardes déjà envoyées y restent.
func (m *Manager) RemoveOnline() error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	defer m.forget()
	err := os.Remove(m.onlinePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (m *Manager) onlineStore() store {
	settings, ok := m.OnlineSettings()
	if !ok {
		return nil
	}
	return m.newOnline(settings)
}

func (m *Manager) newOnline(settings OnlineSettings) *onlineStore {
	client := m.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	return &onlineStore{settings: settings, prefix: "pi-appliance-backups/" + m.Host + "/", http: client, now: m.now}
}

// ErrOnline : le stockage en ligne refuse ou ne répond pas.
type ErrOnline struct {
	Status int
	Code   string
	Detail string
}

func (e *ErrOnline) Error() string {
	switch {
	case e.Code == "NoSuchBucket":
		return "le stockage en ligne ne connaît pas ce seau (bucket)"
	case e.Code == "InvalidAccessKeyId" || e.Code == "SignatureDoesNotMatch" || e.Status == http.StatusForbidden || e.Status == http.StatusUnauthorized:
		return "le stockage en ligne refuse les clés d'accès"
	case e.Status == 0:
		return "le stockage en ligne ne répond pas : " + e.Detail
	}
	return fmt.Sprintf("le stockage en ligne a répondu %d %s", e.Status, e.Code)
}

type onlineStore struct {
	settings OnlineSettings
	prefix   string
	http     *http.Client
	now      func() time.Time
}

func (o *onlineStore) place() string { return PlaceOnline }
func (o *onlineStore) sealed() bool  { return true }

func (o *onlineStore) freeMB() (int64, bool) { return 0, false }

func (o *onlineStore) objectURL(key string, query url.Values) string {
	u := o.settings.Endpoint + "/" + o.settings.Bucket + "/"
	if key != "" {
		parts := strings.Split(key, "/")
		for i, part := range parts {
			parts[i] = sigv4.Escape(part)
		}
		u += strings.Join(parts, "/")
	}
	if len(query) > 0 {
		u += "?" + strings.ReplaceAll(query.Encode(), "+", "%20")
	}
	return u
}

// do envoie une requête signée ; body tient en mémoire (un morceau au plus).
// Les erreurs passagères (réseau, 5xx) sont réessayées deux fois.
func (o *onlineStore) do(ctx context.Context, method, key string, query url.Values, body []byte, want int) (*http.Response, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * onlineRetryDelay):
			}
		}
		request, err := http.NewRequestWithContext(ctx, method, o.objectURL(key, query), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		request.ContentLength = int64(len(body))
		hash := sigv4.Hash(body)
		request.Header.Set("X-Amz-Content-Sha256", hash)
		sigv4.Sign(request, hash, sigv4.Credentials{AccessKeyID: o.settings.AccessKeyID, SecretAccessKey: o.settings.SecretAccessKey}, o.settings.Region, "s3", o.now())
		response, err := o.http.Do(request)
		if err != nil {
			last = &ErrOnline{Detail: err.Error()}
			continue
		}
		deleted := method == http.MethodDelete && (response.StatusCode == http.StatusOK || response.StatusCode == http.StatusNoContent)
		if response.StatusCode == want || deleted {
			return response, nil
		}
		data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		var refused struct {
			Code string `xml:"Code"`
		}
		xml.Unmarshal(data, &refused)
		last = &ErrOnline{Status: response.StatusCode, Code: refused.Code}
		if response.StatusCode < 500 {
			return nil, last
		}
	}
	return nil, last
}

func (o *onlineStore) send(ctx context.Context, method, key string, query url.Values, body []byte, want int) ([]byte, http.Header, error) {
	response, err := o.do(ctx, method, key, query, body, want)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	return data, response.Header, err
}

// probe vérifie les réglages : écrire, lire puis effacer un petit fichier.
func (o *onlineStore) probe(ctx context.Context) error {
	key := o.prefix + "essai-pi-appliance"
	if _, _, err := o.send(ctx, http.MethodPut, key, nil, []byte("pi-appliance"), http.StatusOK); err != nil {
		return err
	}
	_, _, err := o.send(ctx, http.MethodDelete, key, nil, nil, http.StatusNoContent)
	return err
}

type listResult struct {
	Contents []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
}

// keys rend tous les objets sous prefix (ListObjectsV2, page par page).
func (o *onlineStore) keys(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	token := ""
	for page := 0; page < 1000; page++ {
		query := url.Values{"list-type": {"2"}, "prefix": {prefix}}
		if token != "" {
			query.Set("continuation-token", token)
		}
		data, _, err := o.send(ctx, http.MethodGet, "", query, nil, http.StatusOK)
		if err != nil {
			return nil, err
		}
		var result listResult
		if err := xml.Unmarshal(data, &result); err != nil {
			return nil, &ErrOnline{Status: http.StatusOK, Code: "réponse illisible"}
		}
		for _, c := range result.Contents {
			keys = append(keys, c.Key)
		}
		if !result.IsTruncated || result.NextContinuationToken == "" {
			return keys, nil
		}
		token = result.NextContinuationToken
	}
	return keys, nil
}

func (o *onlineStore) list(ctx context.Context) (complete, partial []string, err error) {
	keys, err := o.keys(ctx, o.prefix)
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	for _, key := range keys {
		rest := strings.TrimPrefix(key, o.prefix)
		id, file, ok := strings.Cut(rest, "/")
		if !ok || !idPattern.MatchString(id) {
			continue
		}
		if _, known := seen[id]; !known {
			seen[id] = false
		}
		if file == manifestName {
			seen[id] = true
		}
	}
	for id, done := range seen {
		if done {
			complete = append(complete, id)
		} else {
			partial = append(partial, id)
		}
	}
	slices.Sort(complete)
	slices.Sort(partial)
	return complete, partial, nil
}

func (o *onlineStore) key(id, file string) string { return o.prefix + id + "/" + file }

func (o *onlineStore) create(ctx context.Context, id, file string) (fileWriter, error) {
	if !idPattern.MatchString(id) || !validFile(file) || file == manifestName {
		return nil, ErrInvalid
	}
	return &onlineWriter{ctx: ctx, o: o, key: o.key(id, file), buf: make([]byte, 0, onlinePartSize)}, nil
}

// onlineWriter envoie un fichier : d'un coup s'il tient dans un morceau,
// sinon en plusieurs parties (CreateMultipartUpload, UploadPart, Complete).
type onlineWriter struct {
	ctx      context.Context
	o        *onlineStore
	key      string
	buf      []byte
	uploadID string
	parts    []completedPart
	err      error
}

type completedPart struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

func (w *onlineWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	written := 0
	for len(p) > 0 {
		if len(w.buf) == cap(w.buf) {
			if w.err = w.flush(); w.err != nil {
				return written, w.err
			}
		}
		n := copy(w.buf[len(w.buf):cap(w.buf)], p)
		w.buf = w.buf[:len(w.buf)+n]
		p = p[n:]
		written += n
	}
	return written, nil
}

func (w *onlineWriter) flush() error {
	if w.uploadID == "" {
		data, _, err := w.o.send(w.ctx, http.MethodPost, w.key, url.Values{"uploads": {""}}, nil, http.StatusOK)
		if err != nil {
			return err
		}
		var created struct {
			UploadID string `xml:"UploadId"`
		}
		if xml.Unmarshal(data, &created) != nil || created.UploadID == "" {
			return &ErrOnline{Status: http.StatusOK, Code: "envoi en plusieurs parties refusé"}
		}
		w.uploadID = created.UploadID
	}
	number := len(w.parts) + 1
	query := url.Values{"partNumber": {strconv.Itoa(number)}, "uploadId": {w.uploadID}}
	_, header, err := w.o.send(w.ctx, http.MethodPut, w.key, query, w.buf, http.StatusOK)
	if err != nil {
		return err
	}
	w.parts = append(w.parts, completedPart{PartNumber: number, ETag: header.Get("ETag")})
	w.buf = w.buf[:0]
	return nil
}

func (w *onlineWriter) Close() error {
	if w.err != nil {
		w.Abort()
		return w.err
	}
	if w.uploadID == "" {
		_, _, err := w.o.send(w.ctx, http.MethodPut, w.key, nil, w.buf, http.StatusOK)
		return err
	}
	if len(w.buf) > 0 {
		if err := w.flush(); err != nil {
			w.Abort()
			return err
		}
	}
	body, err := xml.Marshal(struct {
		XMLName xml.Name        `xml:"CompleteMultipartUpload"`
		Parts   []completedPart `xml:"Part"`
	}{Parts: w.parts})
	if err != nil {
		return err
	}
	data, _, err := w.o.send(w.ctx, http.MethodPost, w.key, url.Values{"uploadId": {w.uploadID}}, body, http.StatusOK)
	if err != nil {
		w.Abort()
		return err
	}
	// S3 peut répondre 200 avec une erreur dans le corps.
	if bytes.Contains(data, []byte("<Error>")) {
		w.Abort()
		return &ErrOnline{Status: http.StatusOK, Code: "envoi refusé à la fin"}
	}
	return nil
}

func (w *onlineWriter) Abort() {
	if w.uploadID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(w.ctx), time.Minute)
	defer cancel()
	w.o.send(ctx, http.MethodDelete, w.key, url.Values{"uploadId": {w.uploadID}}, nil, http.StatusNoContent)
	w.uploadID = ""
}

func (o *onlineStore) commit(ctx context.Context, id string, manifest []byte) error {
	_, _, err := o.send(ctx, http.MethodPut, o.key(id, manifestName), nil, manifest, http.StatusOK)
	return err
}

func (o *onlineStore) open(ctx context.Context, id, file string) (io.ReadCloser, error) {
	if !idPattern.MatchString(id) || !validFile(file) {
		return nil, ErrNotFound
	}
	response, err := o.do(ctx, http.MethodGet, o.key(id, file), nil, nil, http.StatusOK)
	var refused *ErrOnline
	if errors.As(err, &refused) && (refused.Status == http.StatusNotFound || refused.Code == "NoSuchKey") {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

func (o *onlineStore) remove(ctx context.Context, id string) error {
	if !idPattern.MatchString(id) {
		return ErrInvalid
	}
	keys, err := o.keys(ctx, o.prefix+id+"/")
	if err != nil {
		return err
	}
	// Le manifest d'abord : une sauvegarde à moitié effacée n'est plus prise pour une vraie.
	slices.SortFunc(keys, func(a, b string) int {
		return boolInt(!strings.HasSuffix(a, "/"+manifestName)) - boolInt(!strings.HasSuffix(b, "/"+manifestName))
	})
	var errs []error
	for _, key := range keys {
		if _, _, err := o.send(ctx, http.MethodDelete, key, nil, nil, http.StatusNoContent); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
