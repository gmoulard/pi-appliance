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

package sigv4

import (
	"net/http"
	"testing"
	"time"
)

// Exemple publié par AWS dans la documentation de Signature Version 4
// (GET ListUsers d'IAM, clés d'exemple AKIDEXAMPLE).
func TestSignExample(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, "https://iam.amazonaws.com/?Action=ListUsers&Version=2010-05-08", nil)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	credentials := Credentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}
	Sign(request, EmptyHash, credentials, "us-east-1", "iam", time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/iam/aws4_request, SignedHeaders=content-type;host;x-amz-date, Signature=5d672d79c15b13162d9279b0855cfba6789a8edb4c82c400e06b5924a6f2b5d7"
	if got := request.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization\n%s\nattendu\n%s", got, want)
	}
}

// Exemple « GET Bucket (List Objects) » de la documentation S3 (Signature
// Calculations for the Authorization Header), avec x-amz-content-sha256.
func TestSignS3Example(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/?max-keys=2&prefix=J", nil)
	request.Header.Set("X-Amz-Content-Sha256", EmptyHash)
	credentials := Credentials{AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}
	Sign(request, EmptyHash, credentials, "us-east-1", "s3", time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7"
	if got := request.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization\n%s\nattendu\n%s", got, want)
	}
}
