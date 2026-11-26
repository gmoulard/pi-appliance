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

// Package server : relais du projet (spécification 7.3, 10.2), sur le Pi du
// projet. Il écoute les ports 80 et 443, lit le nom demandé par le visiteur
// sans rien déchiffrer et fait passer la connexion par une des connexions
// ouvertes par l'appliance de ce nom. Le chiffrement va du visiteur au Pi.
//
// NE PAS journaliser l'adresse d'un visiteur ni les erreurs réseau des
// visites, qui la contiennent (spécification 10.2, données personnelles).
package server

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/gmoulard/pi-appliance/internal/addresses"
	"github.com/gmoulard/pi-appliance/internal/tunnel"
)

// Names : appliances connues du service d'adresses (nom et clé publique).
type Names interface {
	// Owner : clé de l'appliance qui a réservé name.
	Owner(name string) (key string, ok bool)
	// NameOf : nom réservé par l'appliance de clé key.
	NameOf(key string) (name string, ok bool)
}

// Server : le relais.
type Server struct {
	// Zone : zone des adresses relayées (pi-appliance.com).
	Zone string
	// RelayName : nom du relais lui-même (relais.pi-appliance.com), où les
	// appliances se connectent ; TLS : son certificat.
	RelayName string
	TLS       *tls.Config
	Names     Names
	Places    *Places
	// RateBits : débit par appliance et par sens, en bits par seconde.
	RateBits int64
	// PoolLimit : connexions en attente gardées par appliance ; MaxVisits :
	// visites en cours par appliance.
	PoolLimit, MaxVisits int
	// FallbackHTTPS, FallbackHTTP : où envoyer, tels quels, les autres noms
	// (adresses, envoi, stats… du reverse proxy du Pi du projet) ; vide : refusé.
	FallbackHTTPS, FallbackHTTP string
	// AnswerTimeout : nil-valeur remplacée par tunnel.AnswerTimeout.
	AnswerTimeout time.Duration
	Now           func() time.Time
	Logf          func(format string, args ...any)

	mu         sync.Mutex
	appliances map[string]*appliance // par clé publique
}

// appliance : connexions ouvertes par une appliance qui a une place.
type appliance struct {
	idle   []*idle
	ready  chan struct{}
	visits int
	up     *limiter // visiteur vers Pi
	down   *limiter // Pi vers visiteur
}

type idle struct {
	conn   net.Conn
	reader *bufio.Reader
	added  time.Time
}

// Run : vérifications périodiques (places muettes, écriture des compteurs)
// jusqu'à la fin de ctx, puis écriture finale.
func (s *Server) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			if err := s.Places.Save(); err != nil {
				s.logf("places : %v", err)
			}
			s.mu.Unlock()
			return
		case <-ticker.C:
		}
		s.mu.Lock()
		for _, key := range s.Places.Expire(s.now()) {
			s.dropLocked(key)
		}
		if err := s.Places.Save(); err != nil {
			s.logf("places : %v", err)
		}
		s.mu.Unlock()
	}
}

// ServeHTTPS accepte les connexions du port 443.
func (s *Server) ServeHTTPS(listener net.Listener) error {
	return serve(listener, s.handleHTTPS)
}

// ServeHTTP accepte les connexions du port 80.
func (s *Server) ServeHTTP(listener net.Listener) error {
	return serve(listener, s.handleHTTP)
}

func serve(listener net.Listener, handle func(net.Conn)) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go handle(conn)
	}
}

func (s *Server) handleHTTPS(conn net.Conn) {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	name, seen, err := sniffSNI(conn)
	if err != nil {
		conn.Close()
		return
	}
	switch {
	case name == s.RelayName:
		server := tls.Server(newReplay(conn, seen), s.TLS)
		if err := server.Handshake(); err != nil {
			server.Close()
			return
		}
		s.handleAppliance(server)
	default:
		if key, ok := s.route(name); ok {
			s.visit(conn, seen, 443, key)
			return
		}
		if s.isAppliance(name) {
			// Nom d'appliance sans place : rien à montrer sans son certificat.
			conn.Close()
			return
		}
		s.fallback(conn, seen, s.FallbackHTTPS)
	}
}

func (s *Server) handleHTTP(conn net.Conn) {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	name, seen, err := sniffHost(conn)
	if err != nil {
		conn.Close()
		return
	}
	if key, ok := s.route(name); ok {
		s.visit(conn, seen, 80, key)
		return
	}
	if s.isAppliance(name) {
		unavailable(conn)
		return
	}
	s.fallback(conn, seen, s.FallbackHTTP)
}

// unavailable : réponse au visiteur HTTP d'un Pi qui n'a pas de place.
func unavailable(conn net.Conn) {
	defer conn.Close()
	body := "Ce Pi n'est pas joignable par le relais pour l'instant.\n"
	fmt.Fprintf(conn, "HTTP/1.1 503 Service Unavailable\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
}

// applianceName : nom réservé d'un nom demandé, <nom>.<zone> ou
// <quelque chose>.<nom>.<zone>.
func (s *Server) applianceName(host string) (string, bool) {
	suffix := "." + s.Zone
	if !strings.HasSuffix(host, suffix) || host == s.RelayName {
		return "", false
	}
	rest := strings.TrimSuffix(host, suffix)
	name := rest[strings.LastIndex(rest, ".")+1:]
	if addresses.Check(name) != nil {
		return "", false
	}
	return name, true
}

func (s *Server) isAppliance(host string) bool {
	name, ok := s.applianceName(host)
	if !ok {
		return false
	}
	_, ok = s.Names.Owner(name)
	return ok
}

// route : clé de l'appliance relayée sous ce nom, si elle a une place.
func (s *Server) route(host string) (string, bool) {
	name, ok := s.applianceName(host)
	if !ok {
		return "", false
	}
	key, ok := s.Names.Owner(name)
	if !ok {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.appliances[key] == nil || !s.Places.Has(key, s.now()) {
		return "", false
	}
	return key, true
}

func (s *Server) fallback(conn net.Conn, seen []byte, target string) {
	defer conn.Close()
	if target == "" {
		return
	}
	conn.SetReadDeadline(time.Time{})
	backend, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return
	}
	defer backend.Close()
	pipe(newReplay(conn, seen), conn, backend, backend, nil, nil)
}

// handleAppliance : demande d'une appliance (POST ou DELETE /api/tunnel).
func (s *Server) handleAppliance(conn net.Conn) {
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	reader := bufio.NewReader(conn)
	request, err := http.ReadRequest(reader)
	if err != nil {
		conn.Close()
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 1024))
	if err != nil || request.URL.Path != tunnel.Path || (request.Method != http.MethodPost && request.Method != http.MethodDelete) {
		answer(conn, http.StatusNotFound, tunnel.Status{State: tunnel.StateRefused, Message: "Demande inconnue du relais."})
		conn.Close()
		return
	}
	key, err := addresses.Verify(request, body, s.now())
	if err != nil {
		answer(conn, http.StatusForbidden, tunnel.Status{State: tunnel.StateRefused, Message: "Demande non reconnue : vérifiez l'heure du Pi."})
		conn.Close()
		return
	}
	name, ok := s.Names.NameOf(key)
	if !ok {
		answer(conn, http.StatusForbidden, tunnel.Status{State: tunnel.StateRefused, Message: "Réservez d'abord une adresse pi-appliance.com dans la console."})
		conn.Close()
		return
	}
	s.mu.Lock()
	if request.Method == http.MethodDelete {
		s.Places.Release(key, s.now())
		s.dropLocked(key)
		s.mu.Unlock()
		answer(conn, http.StatusOK, tunnel.Status{State: tunnel.StateRefused, Message: "Place du relais rendue."})
		conn.Close()
		return
	}
	status := s.Places.Admit(key, name, s.now())
	if status.State != tunnel.StateReady {
		s.mu.Unlock()
		code := http.StatusConflict
		if status.State == tunnel.StateQuota {
			code = http.StatusTooManyRequests
		}
		answer(conn, code, status)
		conn.Close()
		return
	}
	a := s.appliances[key]
	if a == nil {
		a = &appliance{ready: make(chan struct{}, 1), up: newLimiter(s.RateBits), down: newLimiter(s.RateBits)}
		if s.appliances == nil {
			s.appliances = map[string]*appliance{}
		}
		s.appliances[key] = a
	}
	s.mu.Unlock()
	if err := answer(conn, http.StatusOK, status); err != nil {
		conn.Close()
		return
	}
	conn.SetDeadline(time.Time{})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.appliances[key] != a {
		conn.Close()
		return
	}
	a.idle = append(a.idle, &idle{conn: conn, reader: reader, added: s.now()})
	for len(a.idle) > max(s.PoolLimit, 1) {
		a.idle[0].conn.Close()
		a.idle = a.idle[1:]
	}
	select {
	case a.ready <- struct{}{}:
	default:
	}
}

// dropLocked ferme les connexions d'une appliance qui n'a plus de place.
func (s *Server) dropLocked(key string) {
	if a := s.appliances[key]; a != nil {
		for _, i := range a.idle {
			i.conn.Close()
		}
		delete(s.appliances, key)
	}
}

func answer(conn net.Conn, code int, status tunnel.Status) error {
	body, _ := json.Marshal(status)
	_, err := fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", code, http.StatusText(code), len(body), body)
	return err
}

// visit fait passer un visiteur par une connexion de l'appliance.
func (s *Server) visit(conn net.Conn, seen []byte, port int, key string) {
	defer conn.Close()
	from, err := netip.ParseAddrPort(conn.RemoteAddr().String())
	if err != nil {
		return
	}
	s.mu.Lock()
	a := s.appliances[key]
	if a == nil || a.visits >= max(s.MaxVisits, 1) {
		s.mu.Unlock()
		return
	}
	a.visits++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		a.visits--
		s.mu.Unlock()
	}()
	pi := s.take(a, tunnel.Visit{Port: port, From: from})
	if pi == nil {
		return
	}
	defer pi.conn.Close()
	conn.SetReadDeadline(time.Time{})
	count := func(n int) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.Places.Count(key, int64(n), s.now())
	}
	pipe(newReplay(conn, seen), conn, pi.reader, pi.conn, &meter{limiter: a.up, count: count}, &meter{limiter: a.down, count: count})
}

// take : une connexion de l'appliance qui a répondu « OK » à la visite ;
// attend AnswerTimeout au plus qu'une connexion se libère.
func (s *Server) take(a *appliance, v tunnel.Visit) *idle {
	deadline := time.Now().Add(s.answerTimeout())
	for time.Now().Before(deadline) {
		s.mu.Lock()
		var pi *idle
		for len(a.idle) > 0 && pi == nil {
			// La plus récente d'abord : les anciennes ont pu être fermées par le Pi.
			pi = a.idle[len(a.idle)-1]
			a.idle = a.idle[:len(a.idle)-1]
			if s.now().Sub(pi.added) > tunnel.IdleTimeout+time.Minute {
				pi.conn.Close()
				pi = nil
			}
		}
		s.mu.Unlock()
		if pi == nil {
			select {
			case <-a.ready:
			case <-time.After(time.Until(deadline)):
			}
			continue
		}
		pi.conn.SetDeadline(time.Now().Add(s.answerTimeout()))
		if _, err := io.WriteString(pi.conn, v.Line()); err == nil {
			if line, err := pi.reader.ReadString('\n'); err == nil && strings.TrimSpace(line) == "OK" {
				pi.conn.SetDeadline(time.Time{})
				return pi
			}
		}
		pi.conn.Close()
	}
	return nil
}

// pipe copie dans les deux sens jusqu'à la fin des deux.
func pipe(fromVisitor io.Reader, visitor net.Conn, fromPi io.Reader, pi net.Conn, up, down *meter) {
	done := make(chan struct{}, 2)
	go func() {
		copyMetered(pi, fromVisitor, up)
		closeWrite(pi)
		done <- struct{}{}
	}()
	go func() {
		copyMetered(visitor, fromPi, down)
		closeWrite(visitor)
		done <- struct{}{}
	}()
	<-done
	// Un sens fini ou coupé (quota) : l'autre a une minute pour finir.
	visitor.SetDeadline(time.Now().Add(time.Minute))
	pi.SetDeadline(time.Now().Add(time.Minute))
	<-done
}

// meter : débit et quota d'un sens.
type meter struct {
	limiter *limiter
	count   func(n int) bool
}

func copyMetered(dst io.Writer, src io.Reader, m *meter) {
	buffer := make([]byte, 16<<10)
	for {
		n, err := src.Read(buffer)
		if n > 0 {
			if m != nil {
				if !m.count(n) {
					return
				}
				m.limiter.wait(n)
			}
			if _, werr := dst.Write(buffer[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func closeWrite(conn net.Conn) {
	if half, ok := conn.(interface{ CloseWrite() error }); ok {
		half.CloseWrite()
	}
}

// limiter : seau à jetons, en octets.
type limiter struct {
	mu     sync.Mutex
	rate   float64 // octets par seconde
	tokens float64
	last   time.Time
}

func newLimiter(bits int64) *limiter {
	rate := float64(bits) / 8
	return &limiter{rate: rate, tokens: rate, last: time.Now()}
}

func (l *limiter) wait(n int) {
	if l == nil || l.rate <= 0 {
		return
	}
	l.mu.Lock()
	now := time.Now()
	l.tokens = min(l.rate, l.tokens+now.Sub(l.last).Seconds()*l.rate)
	l.last = now
	l.tokens -= float64(n)
	debt := l.tokens
	l.mu.Unlock()
	if debt < 0 {
		time.Sleep(time.Duration(-debt / l.rate * float64(time.Second)))
	}
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) answerTimeout() time.Duration {
	if s.AnswerTimeout > 0 {
		return s.AnswerTimeout
	}
	return tunnel.AnswerTimeout
}

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}
