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
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Chiffrement des archives (spécification 9.1), avec la bibliothèque standard
// de Go seulement : AES-256-GCM par blocs de 64 Kio, selon la construction
// STREAM (Hoang, Reyhanitabar, Rogaway et Vizár, 2015), celle du format age.
//
//	en-tête : "piabkp01" (8 octets), sel aléatoire (16 octets)
//	clé du fichier : HKDF-SHA256(clé des sauvegardes, sel, "pi-appliance backup v1")
//	bloc n : AES-GCM, nonce = n sur 11 octets gros-boutiste + 1 octet « dernier bloc »
//
// Chaque fichier a sa propre clé, ce qui permet un compteur comme nonce ;
// l'octet « dernier bloc » fait échouer la lecture d'une archive tronquée, le
// compteur celle de blocs déplacés.

const (
	streamMagic = "piabkp01"
	saltSize    = 16
	chunkSize   = 64 << 10
	keySize     = 32
	streamInfo  = "pi-appliance backup v1"
)

// ErrCorrupt : archive modifiée, tronquée ou chiffrée avec une autre clé.
var ErrCorrupt = errors.New("sauvegarde abîmée ou chiffrée avec une autre clé")

func streamCipher(key, salt []byte) (cipher.AEAD, error) {
	if len(key) != keySize {
		return nil, errors.New("clé des sauvegardes invalide")
	}
	fileKey, err := hkdf.Key(sha256.New, key, salt, streamInfo, keySize)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(fileKey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func chunkNonce(counter uint64, last bool) []byte {
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[3:11], counter)
	if last {
		nonce[11] = 1
	}
	return nonce
}

// encryptWriter chiffre ce qu'on lui écrit ; Close écrit le dernier bloc.
type encryptWriter struct {
	w       io.Writer
	aead    cipher.AEAD
	buf     []byte
	counter uint64
	closed  bool
}

// encrypt rend un écrivain qui chiffre vers w ; Close est obligatoire.
func encrypt(w io.Writer, key []byte) (io.WriteCloser, error) {
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	aead, err := streamCipher(key, salt)
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(w, streamMagic); err != nil {
		return nil, err
	}
	if _, err := w.Write(salt); err != nil {
		return nil, err
	}
	return &encryptWriter{w: w, aead: aead, buf: make([]byte, 0, chunkSize)}, nil
}

func (e *encryptWriter) Write(p []byte) (int, error) {
	if e.closed {
		return 0, errors.New("archive déjà fermée")
	}
	written := 0
	for len(p) > 0 {
		// Un bloc plein n'est écrit qu'à l'arrivée de la suite : c'est
		// peut-être le dernier.
		if len(e.buf) == chunkSize {
			if err := e.flush(false); err != nil {
				return written, err
			}
		}
		n := copy(e.buf[len(e.buf):chunkSize], p)
		e.buf = e.buf[:len(e.buf)+n]
		p = p[n:]
		written += n
	}
	return written, nil
}

func (e *encryptWriter) flush(last bool) error {
	if e.counter == 1<<63 {
		return errors.New("archive trop grande")
	}
	sealed := e.aead.Seal(nil, chunkNonce(e.counter, last), e.buf, nil)
	e.counter++
	e.buf = e.buf[:0]
	_, err := e.w.Write(sealed)
	return err
}

func (e *encryptWriter) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	return e.flush(true)
}

// decryptReader déchiffre une archive bloc par bloc ; une erreur ErrCorrupt
// arrive au plus tard à la fin, si l'archive est tronquée.
type decryptReader struct {
	r       *bufio.Reader
	aead    cipher.AEAD
	chunk   []byte
	plain   []byte
	counter uint64
	done    bool
	err     error
}

func decrypt(r io.Reader, key []byte) (io.Reader, error) {
	header := make([]byte, len(streamMagic)+saltSize)
	if _, err := io.ReadFull(r, header); err != nil || string(header[:len(streamMagic)]) != streamMagic {
		return nil, ErrCorrupt
	}
	aead, err := streamCipher(key, header[len(streamMagic):])
	if err != nil {
		return nil, err
	}
	return &decryptReader{r: bufio.NewReaderSize(r, chunkSize+64), aead: aead, chunk: make([]byte, chunkSize+aead.Overhead())}, nil
}

func (d *decryptReader) Read(p []byte) (int, error) {
	for len(d.plain) == 0 {
		if d.err != nil {
			return 0, d.err
		}
		if d.done {
			return 0, io.EOF
		}
		d.next()
	}
	n := copy(p, d.plain)
	d.plain = d.plain[n:]
	return n, nil
}

func (d *decryptReader) next() {
	n, err := io.ReadFull(d.r, d.chunk)
	last := false
	switch {
	case errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF):
		last = true
	case err != nil:
		d.err = fmt.Errorf("lecture de la sauvegarde : %w", err)
		return
	default:
		if _, err := d.r.Peek(1); errors.Is(err, io.EOF) {
			last = true
		}
	}
	plain, err := d.aead.Open(d.chunk[:0], chunkNonce(d.counter, last), d.chunk[:n], nil)
	if err != nil {
		d.err = ErrCorrupt
		return
	}
	d.counter++
	d.plain = plain
	d.done = last
}
