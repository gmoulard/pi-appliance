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
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/sigv4"
)

// fakeS3 : un stockage S3 en mémoire, qui vérifie la signature SigV4 de
// chaque requête comme le ferait le vrai.
type fakeS3 struct {
	t       *testing.T
	mu      sync.Mutex
	bucket  string
	creds   sigv4.Credentials
	objects map[string][]byte
	uploads map[string]map[int][]byte
	next    int
	// pageSize : objets par page de liste (pour essayer la suite des pages).
	pageSize int
	requests int
}

func newFakeS3(t *testing.T) (*fakeS3, *httptest.Server) {
	delay := onlineRetryDelay
	onlineRetryDelay = time.Millisecond
	t.Cleanup(func() { onlineRetryDelay = delay })
	f := &fakeS3{t: t, bucket: "mes-sauvegardes", creds: sigv4.Credentials{AccessKeyID: "AKID", SecretAccessKey: "secret"},
		objects: map[string][]byte{}, uploads: map[string]map[int][]byte{}, pageSize: 3}
	server := httptest.NewTLSServer(f)
	t.Cleanup(server.Close)
	return f, server
}

func s3Error(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	fmt.Fprintf(w, "<Error><Code>%s</Code></Error>", code)
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	// Signature : recalculée avec les clés connues, à la date annoncée.
	if r.Header.Get("X-Amz-Content-Sha256") != sigv4.Hash(body) {
		s3Error(w, http.StatusBadRequest, "XAmzContentSHA256Mismatch")
		return
	}
	date, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
	if err != nil {
		s3Error(w, http.StatusForbidden, "AccessDenied")
		return
	}
	check, _ := http.NewRequest(r.Method, "https://"+r.Host+r.URL.RequestURI(), nil)
	check.Header.Set("X-Amz-Content-Sha256", r.Header.Get("X-Amz-Content-Sha256"))
	sigv4.Sign(check, sigv4.Hash(body), f.creds, "gra", "s3", date)
	if check.Header.Get("Authorization") != r.Header.Get("Authorization") {
		s3Error(w, http.StatusForbidden, "SignatureDoesNotMatch")
		return
	}
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if bucket != f.bucket {
		s3Error(w, http.StatusNotFound, "NoSuchBucket")
		return
	}
	query := r.URL.Query()
	switch {
	case r.Method == http.MethodGet && key == "":
		f.list(w, query)
	case r.Method == http.MethodPost && query.Has("uploads"):
		f.next++
		id := fmt.Sprintf("upload-%d", f.next)
		f.uploads[id] = map[int][]byte{}
		fmt.Fprintf(w, "<InitiateMultipartUploadResult><UploadId>%s</UploadId></InitiateMultipartUploadResult>", id)
	case r.Method == http.MethodPut && query.Has("uploadId"):
		parts, ok := f.uploads[query.Get("uploadId")]
		if !ok {
			s3Error(w, http.StatusNotFound, "NoSuchUpload")
			return
		}
		var n int
		fmt.Sscan(query.Get("partNumber"), &n)
		parts[n] = body
		w.Header().Set("ETag", fmt.Sprintf(`"etag-%d"`, n))
	case r.Method == http.MethodPost && query.Has("uploadId"):
		parts, ok := f.uploads[query.Get("uploadId")]
		if !ok {
			s3Error(w, http.StatusNotFound, "NoSuchUpload")
			return
		}
		var complete struct {
			Parts []completedPart `xml:"Part"`
		}
		xml.Unmarshal(body, &complete)
		var data []byte
		for i, p := range complete.Parts {
			if p.PartNumber != i+1 || p.ETag != fmt.Sprintf(`"etag-%d"`, i+1) {
				s3Error(w, http.StatusBadRequest, "InvalidPart")
				return
			}
			data = append(data, parts[p.PartNumber]...)
		}
		f.objects[key] = data
		delete(f.uploads, query.Get("uploadId"))
		fmt.Fprint(w, "<CompleteMultipartUploadResult/>")
	case r.Method == http.MethodDelete && query.Has("uploadId"):
		delete(f.uploads, query.Get("uploadId"))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut:
		f.objects[key] = body
	case r.Method == http.MethodGet:
		data, ok := f.objects[key]
		if !ok {
			s3Error(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		w.Write(data)
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		s3Error(w, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func (f *fakeS3) list(w http.ResponseWriter, query map[string][]string) {
	prefix := ""
	if v := query["prefix"]; len(v) > 0 {
		prefix = v[0]
	}
	var keys []string
	for key := range f.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	start := 0
	if v := query["continuation-token"]; len(v) > 0 {
		fmt.Sscan(v[0], &start)
	}
	end := min(start+f.pageSize, len(keys))
	fmt.Fprint(w, "<ListBucketResult>")
	for _, key := range keys[start:end] {
		fmt.Fprintf(w, "<Contents><Key>%s</Key></Contents>", key)
	}
	if end < len(keys) {
		fmt.Fprintf(w, "<IsTruncated>true</IsTruncated><NextContinuationToken>%d</NextContinuationToken>", end)
	}
	fmt.Fprint(w, "</ListBucketResult>")
}

func (f *fakeS3) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for key := range f.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// withOnline règle le stockage en ligne du fixture vers le faux S3.
func withOnline(t *testing.T, f *fixture) (*fakeS3, OnlineSettings) {
	t.Helper()
	fake, server := newFakeS3(t)
	f.HTTP = server.Client()
	settings := OnlineSettings{Endpoint: server.URL, Region: "gra", Bucket: fake.bucket, AccessKeyID: "AKID", SecretAccessKey: "secret"}
	if err := f.SetOnline(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	if len(fake.keys()) != 0 {
		t.Fatalf("l'essai doit être effacé : %v", fake.keys())
	}
	return fake, settings
}

func TestOnlineBackupAndRestore(t *testing.T) {
	defer func(size int) { onlinePartSize = size }(onlinePartSize)
	onlinePartSize = 1024 // plusieurs parties
	f := newFixture(t)
	f.install(t, "blog", "index.html", strings.Repeat("blog ", 2000))
	fake, _ := withOnline(t, f)
	ctx := context.Background()
	snapshot, err := f.Backup(ctx, Manual)
	if err != nil {
		t.Fatal(err)
	}
	if last := f.Last(); last == nil || !last.OK {
		t.Fatalf("résultat : %+v", last)
	}
	keys := fake.keys()
	prefix := "pi-appliance-backups/pi/" + snapshot.ID + "/"
	want := []string{prefix + "config.enc", prefix + "manifest.json", prefix + "services/1.enc", prefix + "services/2.enc"}
	if strings.Join(keys, " ") != strings.Join(want, " ") {
		t.Fatalf("objets en ligne :\n%v\nattendu :\n%v", keys, want)
	}
	// Rien de lisible hors de la maison : ni données, ni noms des services.
	for _, key := range keys {
		data := fake.objects[key]
		for _, secret := range []string{"photo v1", "SECRET=v1", "cloud", "blog"} {
			if bytes.Contains(data, []byte(secret)) || strings.Contains(key, secret) {
				t.Errorf("%s contient %q en clair", key, secret)
			}
		}
	}
	// L'enveloppe de la clé reste lisible.
	var manifest Snapshot
	if err := json.Unmarshal(fake.objects[prefix+"manifest.json"], &manifest); err != nil || manifest.Envelope.KeyID == "" || manifest.Sealed == "" || len(manifest.Services) != 0 {
		t.Fatalf("manifest en ligne : %+v %v", manifest, err)
	}
	// Disque débranché : la sauvegarde se retrouve et se restaure depuis le stockage en ligne.
	f.Target = func() (string, error) { return "", ErrNoDisk }
	f.forget()
	snapshots, places, err := f.snapshotsByPlace(ctx)
	if err != nil || len(snapshots) != 1 || len(snapshots[0].Services) != 2 || places[snapshot.ID][0] != PlaceOnline {
		t.Fatalf("sauvegardes en ligne : %+v %v %v", snapshots, places, err)
	}
	write(t, filepath.Join(f.volume("cloud"), "data", "photo.jpg"), "photo v2")
	if err := f.Restore(ctx, snapshot.ID, "cloud", ""); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(f.volume("cloud"), "data", "photo.jpg")); got != "photo v1" {
		t.Fatalf("données restaurées : %q", got)
	}
	if len(fake.uploads) != 0 {
		t.Fatalf("envois en plusieurs parties restés ouverts : %v", fake.uploads)
	}
}

func TestOnlineOnlyAndAnotherCard(t *testing.T) {
	f := newFixture(t)
	f.Target = func() (string, error) { return "", ErrNoDisk }
	fake, settings := withOnline(t, f)
	ctx := context.Background()
	snapshot, err := f.Backup(ctx, Nightly)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.objects["pi-appliance-backups/pi/"+snapshot.ID+"/services/1.enc"]; !ok {
		t.Fatalf("sauvegarde écrite directement en ligne : %v", fake.keys())
	}
	// Nouvelle carte SD : nouvelle clé, nouveau mot de passe, même stockage en ligne.
	os.RemoveAll(filepath.Join(f.fake.dir, stateDir))
	if err := f.Protect("nouveau mot de passe"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetOnline(ctx, settings); err != nil {
		t.Fatal(err)
	}
	snapshots, err := f.Snapshots()
	if err != nil || len(snapshots) != 1 || !snapshots[0].Locked || len(snapshots[0].Services) != 0 {
		t.Fatalf("sauvegarde d'une autre clé : %+v %v", snapshots, err)
	}
	if err := f.Restore(ctx, snapshot.ID, "cloud", "nouveau mot de passe"); !errors.Is(err, ErrPassword) {
		t.Fatalf("mot de passe d'aujourd'hui : %v", err)
	}
	write(t, filepath.Join(f.volume("cloud"), "data", "photo.jpg"), "photo v2")
	if err := f.Restore(ctx, snapshot.ID, "cloud", "mot de passe administrateur"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(f.volume("cloud"), "data", "photo.jpg")); got != "photo v1" {
		t.Fatalf("données : %q", got)
	}
	// Ouverte une fois, la sauvegarde se lit sans redemander le mot de passe.
	snapshots, _ = f.Snapshots()
	for _, s := range snapshots {
		if s.ID == snapshot.ID && (s.Locked || len(s.Services) != 1) {
			t.Fatalf("sauvegarde ouverte : %+v", s)
		}
	}
}

func TestOnlineSettingsAndRotation(t *testing.T) {
	f := newFixture(t)
	fake, server := newFakeS3(t)
	f.HTTP = server.Client()
	ctx := context.Background()
	good := OnlineSettings{Endpoint: server.URL, Region: "gra", Bucket: fake.bucket, AccessKeyID: "AKID", SecretAccessKey: "secret"}
	for _, bad := range []OnlineSettings{
		{Endpoint: "http://" + strings.TrimPrefix(server.URL, "https://"), Region: "gra", Bucket: fake.bucket, AccessKeyID: "AKID", SecretAccessKey: "secret"},
		{Endpoint: server.URL, Region: "gra", Bucket: "A", AccessKeyID: "AKID", SecretAccessKey: "secret"},
		{Endpoint: server.URL, Region: "gra", Bucket: fake.bucket},
	} {
		if err := f.SetOnline(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v : %v", bad, err)
		}
	}
	wrong := good
	wrong.SecretAccessKey = "faux"
	var online *ErrOnline
	if err := f.SetOnline(ctx, wrong); !errors.As(err, &online) || !strings.Contains(err.Error(), "clés") {
		t.Fatalf("mauvaises clés : %v", err)
	}
	other := good
	other.Bucket = "autre-seau"
	if err := f.SetOnline(ctx, other); !errors.As(err, &online) || online.Code != "NoSuchBucket" {
		t.Fatalf("seau inconnu : %v", err)
	}
	if _, ok := f.OnlineSettings(); ok {
		t.Fatal("rien ne doit être enregistré après un refus")
	}
	if err := f.SetOnline(ctx, good); err != nil {
		t.Fatal(err)
	}
	// Changer la région sans ressaisir les clés : elles sont gardées.
	if err := f.SetOnline(ctx, OnlineSettings{Endpoint: server.URL, Region: "gra", Bucket: fake.bucket}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(f.onlinePath()); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("droits des clés : %v %v", info, err)
	}
	// Une sauvegarde interrompue en ligne est effacée à la suivante ; rotation comme sur le disque.
	fake.objects["pi-appliance-backups/pi/20261101-030000/config.enc"] = []byte("x")
	if err := f.SetSettings(Settings{Enabled: true, Hour: 3, Daily: 2}); err != nil {
		t.Fatal(err)
	}
	for day := 0; day < 3; day++ {
		if _, err := f.Backup(ctx, Nightly); err != nil {
			t.Fatal(err)
		}
		f.now = f.now.AddDate(0, 0, 1)
	}
	ids := map[string]bool{}
	for _, key := range fake.keys() {
		ids[strings.Split(key, "/")[2]] = true
	}
	if len(ids) != 2 || !ids["20261122-030000"] || !ids["20261121-030000"] {
		t.Fatalf("en ligne après rotation : %v", fake.keys())
	}
	// Stockage en ligne injoignable : la sauvegarde du disque est faite quand même, et c'est dit.
	server.Close()
	f.forget()
	_, err := f.Backup(ctx, Manual)
	var replica *ReplicaError
	if !errors.As(err, &replica) || replica.Places[0] != PlaceOnline {
		t.Fatalf("stockage injoignable : %v", err)
	}
	if last := f.Last(); last == nil || last.OK || !strings.Contains(last.Message, "en ligne") {
		t.Fatalf("résultat : %+v", last)
	}
	if err := f.RemoveOnline(); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.OnlineSettings(); ok {
		t.Fatal("stockage en ligne retiré")
	}
}
