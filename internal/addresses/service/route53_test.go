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
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exemple publié par AWS dans la documentation de Signature Version 4
// (GET ListUsers d'IAM, clés d'exemple AKIDEXAMPLE).
func TestSignV4Example(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, "https://iam.amazonaws.com/?Action=ListUsers&Version=2010-05-08", nil)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	credentials := Credentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}
	signV4(request, nil, credentials, "us-east-1", "iam", time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/iam/aws4_request, SignedHeaders=content-type;host;x-amz-date, Signature=5d672d79c15b13162d9279b0855cfba6789a8edb4c82c400e06b5924a6f2b5d7"
	if got := request.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization\n%s\nattendu\n%s", got, want)
	}
}

type recordDoer struct {
	request *http.Request
	body    string
	status  int
	answer  string
}

func (d *recordDoer) Do(r *http.Request) (*http.Response, error) {
	data, _ := io.ReadAll(r.Body)
	d.request, d.body = r, string(data)
	return &http.Response{StatusCode: d.status, Body: io.NopCloser(strings.NewReader(d.answer))}, nil
}

func TestRoute53Change(t *testing.T) {
	doer := &recordDoer{status: http.StatusOK, answer: "<ChangeResourceRecordSetsResponse/>"}
	r := &Route53{ZoneID: "ZTEST", Credentials: Credentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "secret"}, HTTP: doer}
	if err := r.Set(context.Background(), "maison.pi-appliance.com", "alice.duckdns.org"); err != nil {
		t.Fatal(err)
	}
	if doer.request.URL.String() != "https://route53.amazonaws.com/2013-04-01/hostedzone/ZTEST/rrset/" || doer.request.Method != http.MethodPost {
		t.Fatalf("requête %s %s", doer.request.Method, doer.request.URL)
	}
	for _, part := range []string{
		`<Action>UPSERT</Action><ResourceRecordSet><Name>maison.pi-appliance.com.</Name><Type>CNAME</Type><TTL>300</TTL><ResourceRecords><ResourceRecord><Value>alice.duckdns.org</Value>`,
		`<Name>*.maison.pi-appliance.com.</Name>`,
		`xmlns="https://route53.amazonaws.com/doc/2013-04-01/"`,
	} {
		if !strings.Contains(doer.body, part) {
			t.Errorf("corps sans %s :\n%s", part, doer.body)
		}
	}
	if auth := doer.request.Header.Get("Authorization"); !strings.Contains(auth, "/us-east-1/route53/aws4_request") || strings.Contains(auth, "secret") {
		t.Errorf("Authorization %s", auth)
	}

	// Alias déjà absents : la libération réussit quand même.
	doer.status = http.StatusBadRequest
	doer.answer = `<ErrorResponse><Error><Type>Sender</Type><Code>InvalidChangeBatch</Code><Message>Tried to delete resource record set [name='maison.pi-appliance.com.', type='CNAME'] but it was not found</Message></Error></ErrorResponse>`
	if err := r.Remove(context.Background(), "maison.pi-appliance.com", "alice.duckdns.org"); err != nil {
		t.Fatal(err)
	}
	doer.answer = `<ErrorResponse><Error><Code>AccessDenied</Code><Message>not allowed</Message></Error></ErrorResponse>`
	if err := r.Set(context.Background(), "maison.pi-appliance.com", "alice.duckdns.org"); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("refus : %v", err)
	}
}

func TestLoadCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	os.WriteFile(path, []byte("[default]\naws_access_key_id = A\naws_secret_access_key = B\n[adresses]\naws_access_key_id=C\naws_secret_access_key=D\n"), 0o600)
	if c, err := LoadCredentials(path, "adresses"); err != nil || c.AccessKeyID != "C" || c.SecretAccessKey != "D" {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := LoadCredentials(path, "absent"); err == nil {
		t.Fatal("profil absent accepté")
	}
}
