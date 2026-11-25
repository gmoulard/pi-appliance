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

// Package oidc : la console fournisseur OpenID Connect pour les services
// (spécification 8.1, TODO-4.1). Strict minimum, bibliothèque standard de Go
// seulement : flux « authorization code » avec PKCE (S256) obligatoire,
// découverte, clés publiques (JWKS), jeton d'identité signé ES256, userinfo.
// Pas de flux implicite, pas de jeton de rafraîchissement, pas
// d'enregistrement dynamique : chaque client est enregistré par la console
// pour un service (clients.go).
package oidc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
)

// Signer : clé ECDSA P-256 qui signe les jetons d'identité, tirée au premier
// démarrage et gardée dans un fichier en droits 600.
// NE PAS journaliser ni renvoyer la clé privée : elle permet de se faire passer
// pour n'importe quel compte auprès des services.
type Signer struct {
	key *ecdsa.PrivateKey
	// KeyID : empreinte RFC 7638 de la clé publique (« kid »).
	KeyID string
}

// LoadOrCreateSigner lit la clé (PEM, SEC 1) ou la crée.
func LoadOrCreateSigner(path string) (*Signer, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("clé OpenID Connect illisible : %s", path)
		}
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil || key.Curve != elliptic.P256() {
			return nil, fmt.Errorf("clé OpenID Connect illisible : %s", path)
		}
		return newSigner(key), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// O_EXCL : deux démarrages simultanés ne tirent pas deux clés.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return LoadOrCreateSigner(path)
		}
		return nil, err
	}
	if err := pem.Encode(file, &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}); err != nil {
		file.Close()
		os.Remove(path)
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return newSigner(key), nil
}

func newSigner(key *ecdsa.PrivateKey) *Signer {
	s := &Signer{key: key}
	jwk := s.publicJWK()
	canonical := fmt.Sprintf(`{"crv":"P-256","kty":"EC","x":"%s","y":"%s"}`, jwk["x"], jwk["y"])
	sum := sha256.Sum256([]byte(canonical))
	s.KeyID = b64(sum[:])
	return s
}

func b64(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

func fixed(n *big.Int) []byte {
	out := make([]byte, 32)
	n.FillBytes(out)
	return out
}

func (s *Signer) publicJWK() map[string]string {
	x, y := s.key.PublicKey.X, s.key.PublicKey.Y //nolint:staticcheck // coordonnées pour la JWK
	return map[string]string{"kty": "EC", "crv": "P-256", "x": b64(fixed(x)), "y": b64(fixed(y))}
}

// JWKS : clés publiques, au format JSON Web Key Set.
func (s *Signer) JWKS() map[string]any {
	jwk := s.publicJWK()
	key := map[string]string{"kty": jwk["kty"], "crv": jwk["crv"], "x": jwk["x"], "y": jwk["y"], "use": "sig", "alg": "ES256", "kid": s.KeyID}
	return map[string]any{"keys": []map[string]string{key}}
}

// Sign : jeton JWT signé ES256 (signature r‖s sur 64 octets, RFC 7518).
func (s *Signer) Sign(claims map[string]any) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "ES256", "typ": "JWT", "kid": s.KeyID})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	input := b64(header) + "." + b64(payload)
	digest := sha256.Sum256([]byte(input))
	r, sig, err := ecdsa.Sign(rand.Reader, s.key, digest[:])
	if err != nil {
		return "", err
	}
	return input + "." + b64(append(fixed(r), fixed(sig)...)), nil
}

// Public : clé publique (tests, vérification).
func (s *Signer) Public() *ecdsa.PublicKey { return &s.key.PublicKey }
