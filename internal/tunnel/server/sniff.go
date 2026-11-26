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

package server

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// maxHello : taille lue au plus pour trouver le nom demandé.
const maxHello = 16 << 10

var errSniffed = errors.New("nom lu")

// sniffSNI lit le message ClientHello du visiteur sans répondre ni rien
// déchiffrer, et renvoie le nom demandé (SNI) et les octets lus, à rejouer.
func sniffSNI(conn net.Conn) (string, []byte, error) {
	var seen bytes.Buffer
	reader := io.TeeReader(io.LimitReader(conn, maxHello), &seen)
	var name string
	tls.Server(readOnly{reader}, &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		name = hello.ServerName
		return nil, errSniffed
	}}).Handshake()
	name = normalizeHost(name)
	if name == "" {
		return "", seen.Bytes(), errors.New("nom demandé absent")
	}
	return name, seen.Bytes(), nil
}

// sniffHost lit l'en-tête d'une demande HTTP et renvoie son nom (Host) et
// les octets lus, à rejouer.
func sniffHost(conn net.Conn) (string, []byte, error) {
	var seen bytes.Buffer
	reader := bufio.NewReader(io.TeeReader(io.LimitReader(conn, maxHello), &seen))
	request, err := http.ReadRequest(reader)
	if err != nil {
		return "", seen.Bytes(), err
	}
	host := request.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = normalizeHost(host)
	if host == "" {
		return "", seen.Bytes(), errors.New("nom demandé absent")
	}
	return host, seen.Bytes(), nil
}

func normalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

// readOnly : connexion pour sniffSNI, qui lit le visiteur et n'écrit rien.
type readOnly struct{ reader io.Reader }

func (c readOnly) Read(p []byte) (int, error)       { return c.reader.Read(p) }
func (c readOnly) Write(p []byte) (int, error)      { return len(p), nil }
func (c readOnly) Close() error                     { return nil }
func (c readOnly) LocalAddr() net.Addr              { return nil }
func (c readOnly) RemoteAddr() net.Addr             { return nil }
func (c readOnly) SetDeadline(time.Time) error      { return nil }
func (c readOnly) SetReadDeadline(time.Time) error  { return nil }
func (c readOnly) SetWriteDeadline(time.Time) error { return nil }

// replay : connexion qui rend d'abord les octets déjà lus.
type replay struct {
	net.Conn
	reader io.Reader
}

func newReplay(conn net.Conn, seen []byte) *replay {
	return &replay{Conn: conn, reader: io.MultiReader(bytes.NewReader(seen), conn)}
}

func (c *replay) Read(p []byte) (int, error) { return c.reader.Read(p) }
