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
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

func init() {
	Iterations = 1000
}

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func sealBytes(t *testing.T, key, plain []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w, err := encrypt(&out, key)
	if err != nil {
		t.Fatal(err)
	}
	// Écritures de tailles variées, pour passer les bords des blocs.
	for rest := plain; len(rest) > 0; {
		n := min(len(rest), 1+len(rest)%7777)
		if _, err := w.Write(rest[:n]); err != nil {
			t.Fatal(err)
		}
		rest = rest[n:]
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func openBytes(key, sealed []byte) ([]byte, error) {
	r, err := decrypt(bytes.NewReader(sealed), key)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func TestStreamRoundTrip(t *testing.T) {
	key := testKey(t)
	for _, size := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 3 * chunkSize, 3*chunkSize + 17} {
		plain := make([]byte, size)
		rand.Read(plain)
		got, err := openBytes(key, sealBytes(t, key, plain))
		if err != nil {
			t.Fatalf("%d octets : %v", size, err)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("%d octets : contenu différent", size)
		}
	}
}

func TestStreamRejectsTampering(t *testing.T) {
	key := testKey(t)
	plain := make([]byte, 2*chunkSize+100)
	sealed := sealBytes(t, key, plain)
	blockSize := chunkSize + 16
	header := len(streamMagic) + saltSize
	cases := map[string][]byte{
		"octet modifié":         flip(sealed, len(sealed)/2),
		"tronquée au bloc":      sealed[:header+blockSize],
		"tronquée au milieu":    sealed[:len(sealed)-5],
		"dernier bloc en moins": sealed[:header+2*blockSize],
		"blocs inversés":        swapBlocks(sealed, header, blockSize),
		"données en plus":       append(bytes.Clone(sealed), 0),
		"en-tête seul":          sealed[:header],
	}
	for name, data := range cases {
		if _, err := openBytes(key, data); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s : %v, ErrCorrupt attendu", name, err)
		}
	}
	if _, err := openBytes(testKey(t), sealed); !errors.Is(err, ErrCorrupt) {
		t.Errorf("autre clé : %v", err)
	}
}

func flip(data []byte, i int) []byte {
	out := bytes.Clone(data)
	out[i] ^= 1
	return out
}

func swapBlocks(data []byte, header, size int) []byte {
	out := bytes.Clone(data)
	copy(out[header:], data[header+size:header+2*size])
	copy(out[header+size:], data[header:header+size])
	return out
}

func TestEnvelope(t *testing.T) {
	dir := t.TempDir()
	keys := keyring{dir: dir}
	if _, err := keys.envelope(); !errors.Is(err, ErrNoKey) {
		t.Fatalf("enveloppe absente : %v", err)
	}
	if err := keys.protect("ancien mot de passe"); err != nil {
		t.Fatal(err)
	}
	key, err := keys.key()
	if err != nil {
		t.Fatal(err)
	}
	old, err := keys.envelope()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := old.Open("ancien mot de passe"); err != nil || !bytes.Equal(got, key) {
		t.Fatalf("ouverture : %v", err)
	}
	if _, err := old.Open("mauvais"); !errors.Is(err, ErrPassword) {
		t.Fatalf("mauvais mot de passe : %v", err)
	}
	// Nouveau mot de passe : même clé, l'ancienne enveloppe reste valable
	// pour les sauvegardes déjà faites.
	if err := keys.protect("nouveau mot de passe"); err != nil {
		t.Fatal(err)
	}
	renewed, _ := keys.envelope()
	if got, err := renewed.Open("nouveau mot de passe"); err != nil || !bytes.Equal(got, key) {
		t.Fatalf("après changement : %v", err)
	}
	if renewed.KeyID != old.KeyID {
		t.Fatal("la clé a changé avec le mot de passe")
	}
	if _, err := renewed.Open("ancien mot de passe"); !errors.Is(err, ErrPassword) {
		t.Fatalf("ancien mot de passe sur la nouvelle enveloppe : %v", err)
	}
	if got, err := old.Open("ancien mot de passe"); err != nil || !bytes.Equal(got, key) {
		t.Fatalf("ancienne enveloppe : %v", err)
	}
}
