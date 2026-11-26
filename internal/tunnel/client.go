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

package tunnel

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gmoulard/pi-appliance/internal/addresses"
)

// Client : côté appliance. Il garde Pool connexions ouvertes vers le relais,
// chacune prête pour un visiteur, et les refait à mesure.
type Client struct {
	// Relay : adresse du relais, hôte:port (DefaultRelay).
	Relay string
	// Key : clé de l'appliance, celle de son adresse pi-appliance.com.
	// NE PAS la remplacer par une autre clé : le relais reconnaît l'appliance
	// par la clé liée à son adresse dans le service d'adresses.
	Key *addresses.Key
	// TLS : réglage TLS vers le relais (nil : certificats du système, nom
	// tiré de Relay).
	TLS *tls.Config
	// Pool : connexions prêtes en même temps (1 au moins).
	Pool int
	// Target ouvre, pour un visiteur, la connexion locale vers Traefik (port
	// v.Port), avec l'en-tête ProxyHeader déjà écrit.
	Target func(ctx context.Context, v Visit) (net.Conn, error)
	// Now : horloge (nil : time.Now), pour les essais.
	Now func() time.Time
	// IdleTimeout, Retry : nil-valeurs remplacées par les constantes du paquet.
	IdleTimeout, Retry time.Duration
	// Logf : journal (nil : rien).
	Logf func(format string, args ...any)

	mu     sync.Mutex
	status Status
}

// Status : dernier état connu auprès du relais.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *Client) setStatus(s Status) {
	c.mu.Lock()
	changed := c.status != s
	c.status = s
	c.mu.Unlock()
	if changed {
		c.logf("relais : %s %s", s.State, s.Message)
	}
}

// Run garde les connexions jusqu'à la fin de ctx.
func (c *Client) Run(ctx context.Context) {
	pool := c.Pool
	if pool < 1 {
		pool = 1
	}
	var wait sync.WaitGroup
	for i := 0; i < pool; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			c.worker(ctx)
		}()
	}
	wait.Wait()
}

func (c *Client) worker(ctx context.Context) {
	for ctx.Err() == nil {
		status, err := c.serveOne(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logf("relais : %v", err)
			c.setStatus(Status{State: StateOffline, Message: "Le relais ne répond pas ; nouvel essai dans quelques minutes."})
		} else if status.State != StateReady {
			c.setStatus(status)
		}
		if err != nil || status.State != StateReady {
			select {
			case <-ctx.Done():
				return
			case <-time.After(c.retry()):
			}
		}
	}
}

// serveOne ouvre une connexion, demande la place et attend un visiteur ; la
// visite continue dans sa propre goroutine.
func (c *Client) serveOne(ctx context.Context) (Status, error) {
	conn, reader, response, err := c.request(ctx, http.MethodPost)
	if err != nil {
		return Status{}, err
	}
	status, err := readStatus(response)
	if err != nil || status.State != StateReady {
		conn.Close()
		return status, err
	}
	c.setStatus(status)
	// Attente d'un visiteur, refaite à IdleTimeout.
	conn.SetReadDeadline(time.Now().Add(c.idleTimeout()))
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	line, err := reader.ReadString('\n')
	stop()
	if err != nil {
		conn.Close()
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return status, nil
		}
		if ctx.Err() != nil {
			return status, ctx.Err()
		}
		return status, nil
	}
	conn.SetReadDeadline(time.Time{})
	visit, err := ParseVisit(strings.TrimSpace(line))
	if err != nil {
		conn.Close()
		return status, err
	}
	go c.visit(ctx, conn, reader, visit)
	return status, nil
}

func (c *Client) visit(ctx context.Context, conn net.Conn, reader *bufio.Reader, v Visit) {
	defer conn.Close()
	local, err := c.Target(ctx, v)
	if err != nil {
		c.logf("relais : service local injoignable : %v", err)
		io.WriteString(conn, "KO\n")
		return
	}
	defer local.Close()
	if _, err := io.WriteString(conn, "OK\n"); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(local, reader)
		closeWrite(local)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(conn, local)
		closeWrite(conn)
		done <- struct{}{}
	}()
	<-done
	<-done
}

// Release rend la place au relais (retour à l'accès direct).
func (c *Client) Release(ctx context.Context) error {
	conn, _, response, err := c.request(ctx, http.MethodDelete)
	if err != nil {
		return err
	}
	defer conn.Close()
	if response.StatusCode != http.StatusOK {
		status, _ := readStatus(response)
		return errors.New(status.Message)
	}
	return nil
}

// request ouvre une connexion TLS vers le relais et y envoie une demande
// signée ; la connexion reste ouverte après la réponse.
func (c *Client) request(ctx context.Context, method string) (net.Conn, *bufio.Reader, *http.Response, error) {
	config := c.TLS
	if config == nil {
		host, _, err := net.SplitHostPort(c.Relay)
		if err != nil {
			return nil, nil, nil, err
		}
		config = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	}
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}, Config: config}
	conn, err := dialer.DialContext(ctx, "tcp", c.Relay)
	if err != nil {
		return nil, nil, nil, err
	}
	request, err := http.NewRequest(method, "https://"+c.Relay+Path, nil)
	if err != nil {
		conn.Close()
		return nil, nil, nil, err
	}
	c.Key.Sign(request, nil, c.now())
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	if err := request.Write(conn); err != nil {
		conn.Close()
		return nil, nil, nil, err
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		conn.Close()
		return nil, nil, nil, err
	}
	conn.SetDeadline(time.Time{})
	return conn, reader, response, nil
}

// readStatus lit le corps JSON d'une réponse du relais (1 Kio au plus).
func readStatus(response *http.Response) (Status, error) {
	var status Status
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024))
	if err != nil {
		return status, err
	}
	if err := json.Unmarshal(data, &status); err != nil {
		return status, errors.New("réponse du relais illisible")
	}
	if response.StatusCode == http.StatusOK && status.State != StateReady {
		return status, errors.New("réponse du relais illisible")
	}
	return status, nil
}

func closeWrite(conn net.Conn) {
	if half, ok := conn.(interface{ CloseWrite() error }); ok {
		half.CloseWrite()
	}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) idleTimeout() time.Duration {
	if c.IdleTimeout > 0 {
		return c.IdleTimeout
	}
	return IdleTimeout
}

func (c *Client) retry() time.Duration {
	if c.Retry > 0 {
		return c.Retry
	}
	return Retry
}

func (c *Client) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}
