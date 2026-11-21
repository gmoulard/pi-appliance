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

package diagnostic

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

// Rescue : quand Traefik ne répond plus sur le port 443, la console sert
// elle-même, sur Addr (port 8443) et en HTTPS avec le certificat local du Pi,
// le Handler de secours (page de diagnostic et connexion seulement, réseau de
// la maison seulement : console.RescueHandler). Il se referme dès que Traefik
// répond de nouveau.
type Rescue struct {
	Addr     string
	Probe    string
	CertFile string
	KeyFile  string
	Handler  http.Handler
	// Interval entre deux vérifications ; Failures vérifications ratées de suite
	// avant d'ouvrir (Traefik démarre après la console).
	Interval time.Duration
	Failures int
	Logf     func(format string, args ...any)

	mu       sync.Mutex
	down     int
	server   *http.Server
	listener net.Listener
	failure  string
}

func (r *Rescue) logf(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
	}
}

// Run vérifie Traefik jusqu'à la fin de ctx, puis referme le secours.
func (r *Rescue) Run(ctx context.Context) {
	interval := r.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer r.stop()
	for {
		r.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Check : une vérification, qui ouvre ou referme le secours.
func (r *Rescue) Check(ctx context.Context) {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", r.Probe)
	if err == nil {
		conn.Close()
		r.mu.Lock()
		r.down = 0
		r.mu.Unlock()
		r.stop()
		return
	}
	r.mu.Lock()
	r.down++
	failures := r.Failures
	if failures <= 0 {
		failures = 3
	}
	open := r.down >= failures && r.server == nil
	r.mu.Unlock()
	if open {
		err := r.start()
		r.mu.Lock()
		repeated := err != nil && err.Error() == r.failure
		r.failure = ""
		if err != nil {
			r.failure = err.Error()
		}
		r.mu.Unlock()
		if err != nil && !repeated {
			r.logf("console de secours sur %s : %v", r.Addr, err)
		}
	}
}

// Address : adresse d'écoute du secours, vide quand il est fermé.
func (r *Rescue) Address() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listener == nil {
		return ""
	}
	return r.listener.Addr().String()
}

func (r *Rescue) start() error {
	// Certificat relu à chaque ouverture : secrets.sh le refait avant sa fin.
	certificate, err := tls.LoadX509KeyPair(r.CertFile, r.KeyFile)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", r.Addr)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           r.Handler,
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 10 * time.Second,
	}
	r.mu.Lock()
	r.server, r.listener = server, listener
	r.mu.Unlock()
	r.logf("Traefik ne répond plus sur %s : console de secours ouverte sur %s (diagnostic seulement)", r.Probe, listener.Addr())
	go func() {
		if err := server.ServeTLS(listener, "", ""); !errors.Is(err, http.ErrServerClosed) {
			r.logf("console de secours : %v", err)
		}
	}()
	return nil
}

func (r *Rescue) stop() {
	r.mu.Lock()
	server := r.server
	r.server, r.listener = nil, nil
	r.mu.Unlock()
	if server == nil {
		return
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(shutdown)
	r.logf("Traefik répond de nouveau : console de secours fermée")
}
