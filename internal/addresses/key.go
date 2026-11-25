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

package addresses

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// En-têtes des demandes signées par l'appliance (spécification 7.1, section 15).
const (
	HeaderKey       = "Pi-Appliance-Key"
	HeaderDate      = "Pi-Appliance-Date"
	HeaderSignature = "Pi-Appliance-Signature"
	// MaxClockSkew : écart toléré entre l'horloge du Pi et celle du service.
	MaxClockSkew = 5 * time.Minute
)

// ErrSignature : demande non signée, mal signée ou trop ancienne.
var ErrSignature = errors.New("demande non reconnue")

// Key : clé Ed25519 propre à l'appliance, tirée au premier démarrage. Le
// service d'adresses ne connaît que sa partie publique.
// NE PAS journaliser ni renvoyer la clé privée : elle donne la main sur l'adresse.
type Key struct {
	private ed25519.PrivateKey
}

// LoadOrCreateKey lit la clé du fichier (graine en base64, droits 600) ou la
// crée si le fichier n'existe pas encore.
func LoadOrCreateKey(path string) (*Key, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("clé de l'appliance illisible : %s", path)
		}
		return &Key{private: ed25519.NewKeyFromSeed(seed)}, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(private.Seed()) + "\n"
	// O_EXCL : deux démarrages simultanés ne tirent pas deux clés différentes.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return LoadOrCreateKey(path)
		}
		return nil, err
	}
	if _, err := file.WriteString(encoded); err != nil {
		file.Close()
		os.Remove(path)
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return &Key{private: private}, nil
}

// Public renvoie la clé publique en base64, telle que le service la garde.
func (k *Key) Public() string {
	return base64.StdEncoding.EncodeToString(k.private.Public().(ed25519.PublicKey))
}

// signedText : ce que signe l'appliance. Le chemin et la méthode empêchent de
// rejouer une signature sur une autre action, la date de la rejouer plus tard.
func signedText(method, path, date string, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(method + "\n" + path + "\n" + date + "\n" + hex.EncodeToString(sum[:]))
}

// Sign pose sur la demande la clé publique, la date et la signature de body,
// qui doit être exactement le corps envoyé.
func (k *Key) Sign(r *http.Request, body []byte, now time.Time) {
	date := now.UTC().Format(time.RFC3339)
	r.Header.Set(HeaderKey, k.Public())
	r.Header.Set(HeaderDate, date)
	r.Header.Set(HeaderSignature, base64.StdEncoding.EncodeToString(ed25519.Sign(k.private, signedText(r.Method, r.URL.Path, date, body))))
}

// Verify vérifie la signature d'une demande reçue et renvoie la clé publique
// de l'appliance qui l'a faite.
func Verify(r *http.Request, body []byte, now time.Time) (string, error) {
	public, err := base64.StdEncoding.DecodeString(r.Header.Get(HeaderKey))
	if err != nil || len(public) != ed25519.PublicKeySize {
		return "", ErrSignature
	}
	signature, err := base64.StdEncoding.DecodeString(r.Header.Get(HeaderSignature))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return "", ErrSignature
	}
	date := r.Header.Get(HeaderDate)
	signed, err := time.Parse(time.RFC3339, date)
	if err != nil || signed.Sub(now).Abs() > MaxClockSkew {
		return "", ErrSignature
	}
	if !ed25519.Verify(public, signedText(r.Method, r.URL.Path, date, body), signature) {
		return "", ErrSignature
	}
	return base64.StdEncoding.EncodeToString(public), nil
}
