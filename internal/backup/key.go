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

package backup

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Clé des sauvegardes (spécification 9.1) : l'appliance la garde (fichier en
// droits 600, jamais dans les sauvegardes) ; chaque sauvegarde en contient une
// copie chiffrée par le mot de passe administrateur (enveloppe). Changer le
// mot de passe rechiffre l'enveloppe, la clé reste la même.

// Iterations de PBKDF2-SHA256 pour l'enveloppe, comme le mot de passe
// (internal/auth) ; réduit dans les tests.
var Iterations = 600_000

const envelopeKDF = "pbkdf2-sha256"

var (
	// ErrNoKey : pas encore d'enveloppe, l'administrateur ne s'est pas
	// connecté depuis l'arrivée des sauvegardes.
	ErrNoKey = errors.New("les sauvegardes attendent votre mot de passe : déconnectez-vous puis reconnectez-vous à la console")
	// ErrPassword : le mot de passe n'ouvre pas l'enveloppe.
	ErrPassword = errors.New("mot de passe incorrect pour cette sauvegarde")
)

// Envelope : la clé des sauvegardes chiffrée par le mot de passe administrateur.
type Envelope struct {
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Key        string `json:"key"`
	// KeyID identifie la clé sans la révéler.
	KeyID string `json:"key_id"`
}

func keyID(key []byte) string {
	id, err := hkdf.Key(sha256.New, key, nil, "pi-appliance backup key id", 8)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(id)
}

func passwordCipher(password string, salt []byte, iterations int) (cipher.AEAD, error) {
	kek, err := pbkdf2.Key(sha256.New, password, salt, iterations, keySize)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal chiffre la clé par le mot de passe.
func seal(key []byte, password string) (Envelope, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return Envelope{}, err
	}
	aead, err := passwordCipher(password, salt, Iterations)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, err
	}
	id := keyID(key)
	encode := base64.StdEncoding.EncodeToString
	return Envelope{
		KDF:        envelopeKDF,
		Iterations: Iterations,
		Salt:       encode(salt),
		Nonce:      encode(nonce),
		Key:        encode(aead.Seal(nil, nonce, key, []byte(id))),
		KeyID:      id,
	}, nil
}

// Open rend la clé contenue dans l'enveloppe.
func (e Envelope) Open(password string) ([]byte, error) {
	decode := base64.StdEncoding.DecodeString
	salt, err1 := decode(e.Salt)
	nonce, err2 := decode(e.Nonce)
	sealed, err3 := decode(e.Key)
	if e.KDF != envelopeKDF || e.Iterations < 1 || errors.Join(err1, err2, err3) != nil {
		return nil, ErrCorrupt
	}
	aead, err := passwordCipher(password, salt, e.Iterations)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, ErrCorrupt
	}
	key, err := aead.Open(nil, nonce, sealed, []byte(e.KeyID))
	if err != nil || len(key) != keySize || keyID(key) != e.KeyID {
		return nil, ErrPassword
	}
	return key, nil
}

// keyring : la clé et son enveloppe, dans le dossier des sauvegardes.
type keyring struct {
	dir string
}

func (k keyring) keyPath() string      { return filepath.Join(k.dir, "key") }
func (k keyring) envelopePath() string { return filepath.Join(k.dir, "envelope.json") }

// key lit la clé, ou la crée.
func (k keyring) key() ([]byte, error) {
	data, err := os.ReadFile(k.keyPath())
	if errors.Is(err, os.ErrNotExist) {
		key := make([]byte, keySize)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := writeFile(k.keyPath(), []byte(base64.StdEncoding.EncodeToString(key)+"\n"), 0o600); err != nil {
			return nil, err
		}
		return key, nil
	}
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(data)))
	if err != nil || len(key) != keySize {
		return nil, fmt.Errorf("%s : clé des sauvegardes illisible", k.keyPath())
	}
	return key, nil
}

func (k keyring) envelope() (Envelope, error) {
	var envelope Envelope
	data, err := os.ReadFile(k.envelopePath())
	if errors.Is(err, os.ErrNotExist) {
		return envelope, ErrNoKey
	}
	if err != nil {
		return envelope, err
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return envelope, fmt.Errorf("%s : %w", k.envelopePath(), err)
	}
	return envelope, nil
}

func (k keyring) hasEnvelope() bool {
	_, err := os.Stat(k.envelopePath())
	return err == nil
}

// protect (re)chiffre la clé par le mot de passe administrateur en vigueur.
func (k keyring) protect(password string) error {
	key, err := k.key()
	if err != nil {
		return err
	}
	envelope, err := seal(key, password)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(k.envelopePath(), append(data, '\n'), 0o600)
}

// writeFile écrit un fichier d'un coup (fichier temporaire puis renommage).
func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
