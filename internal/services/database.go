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

package services

// PostgreSQL partagé entre les services (spécification 6.3, TODO-2.73) : le
// serveur du service PostgreSQL de la marketplace rejoint le réseau Docker
// pi-appliance-db, sous le nom pi-<nom>-db ; chaque service qui le demande
// a sa base et son compte, du même nom (pi_<nom>), avec un mot de passe tiré
// au hasard, et rejoint ce réseau. Personne d'autre ne s'y connecte : la base
// n'est ouverte ni à la maison ni à internet, et CONNECT est retiré à PUBLIC.
// Le service reçoit ses réglages par des variables du fichier .env, à reprendre
// dans son fichier Compose (${PI_DATABASE_URL}…). Les données sont dans le
// volume du service PostgreSQL, donc dans ses sauvegardes.
//
// NE PAS écrire le mot de passe dans instance.json : l'état part dans l'API
// (GET api/services). Il est dans database.json, droits 600.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// PostgresApp : application PostgreSQL de la marketplace ; postgresService :
	// son serveur dans le fichier Compose.
	PostgresApp     = "postgresql"
	postgresService = "db"
	// DatabaseNetwork : réseau Docker du serveur PostgreSQL et de ses clients.
	DatabaseNetwork = "pi-appliance-db"
	databaseFile    = "database.json"
)

// ErrNoPostgres : aucun service PostgreSQL installé.
var ErrNoPostgres = fmt.Errorf("%w : installez d'abord PostgreSQL depuis la marketplace", ErrInvalid)

// Database : la base d'un service, sans son mot de passe.
type Database struct {
	// Server : service PostgreSQL qui la porte.
	Server string `json:"server"`
	Name   string `json:"name"`
}

func databaseHost(server string) string { return "pi-" + server + "-db" }

// databaseName : identifiant PostgreSQL d'un nom de service (lettres
// minuscules, chiffres, tirets : marketplace.ValidLabel), sans guillemets.
func databaseName(service string) string {
	return "pi_" + strings.ReplaceAll(service, "-", "_")
}

func (d Database) env(password string) map[string]string {
	host := databaseHost(d.Server)
	return map[string]string{
		"PI_DATABASE_HOST":     host,
		"PI_DATABASE_PORT":     "5432",
		"PI_DATABASE_NAME":     d.Name,
		"PI_DATABASE_USER":     d.Name,
		"PI_DATABASE_PASSWORD": password,
		"PI_DATABASE_URL":      fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable", d.Name, password, host, d.Name),
	}
}

func (m *Manager) databasePassword(name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(m.serviceDir(name), databaseFile))
	if err != nil {
		return "", err
	}
	var stored struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(data, &stored); err != nil || stored.Password == "" {
		return "", fmt.Errorf("%s illisible", databaseFile)
	}
	return stored.Password, nil
}

// postgresServer : le premier service PostgreSQL installé.
func (m *Manager) postgresServer() (string, error) {
	instances, err := m.List()
	if err != nil {
		return "", err
	}
	for _, instance := range instances {
		if instance.App == PostgresApp {
			return instance.Name, nil
		}
	}
	return "", ErrNoPostgres
}

// psql lance une commande SQL dans le serveur PostgreSQL, en tant que postgres.
func (m *Manager) psql(ctx context.Context, server, sql string) (string, error) {
	output, err := m.compose(ctx, server, "exec", "-T", postgresService, "psql", "-U", "postgres", "-v", "ON_ERROR_STOP=1", "-tA", "-c", sql)
	if err != nil {
		return "", fmt.Errorf("PostgreSQL a refusé la demande : %s", trimOutput(output, err))
	}
	return strings.TrimSpace(string(output)), nil
}

// CreateDatabase donne au service sa base dans le PostgreSQL partagé (ou
// redonne les réglages de celle qu'il a), puis le redémarre.
func (m *Manager) CreateDatabase(ctx context.Context, name string) (Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	instance, err := m.instance(name)
	if err != nil {
		return Instance{}, err
	}
	if instance.NoWebPage || instance.App == PostgresApp {
		return Instance{}, fmt.Errorf("%w : %s ne peut pas recevoir de base", ErrInvalid, name)
	}
	server, err := m.postgresServer()
	if err != nil {
		return Instance{}, err
	}
	// Serveur PostgreSQL installé avant cette version : il rejoint le réseau.
	if err := m.ensureNetwork(ctx, m.serviceDir(server), DatabaseNetwork); err != nil {
		return Instance{}, err
	}
	if err := m.rewriteOverride(server); err != nil {
		return Instance{}, err
	}
	if output, err := m.compose(ctx, server, "up", "--detach", "--wait"); err != nil {
		return Instance{}, fmt.Errorf("PostgreSQL n'a pas redémarré : %s", trimOutput(output, err))
	}
	database := Database{Server: server, Name: databaseName(name)}
	password, err := m.databasePassword(name)
	if err != nil {
		password = randomSecret()
	}
	// Le mot de passe (lettres et chiffres, randomSecret) et le nom (pi_ + [a-z0-9_]) ne demandent pas de guillemets.
	exists, err := m.psql(ctx, server, fmt.Sprintf("SELECT 1 FROM pg_roles WHERE rolname = '%s'", database.Name))
	if err != nil {
		return Instance{}, err
	}
	if exists == "1" {
		_, err = m.psql(ctx, server, fmt.Sprintf("ALTER ROLE %s WITH LOGIN PASSWORD '%s'", database.Name, password))
	} else {
		_, err = m.psql(ctx, server, fmt.Sprintf("CREATE ROLE %s WITH LOGIN PASSWORD '%s'", database.Name, password))
	}
	if err != nil {
		return Instance{}, err
	}
	exists, err = m.psql(ctx, server, fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname = '%s'", database.Name))
	if err != nil {
		return Instance{}, err
	}
	if exists != "1" {
		if _, err := m.psql(ctx, server, fmt.Sprintf("CREATE DATABASE %s OWNER %s", database.Name, database.Name)); err != nil {
			return Instance{}, err
		}
	}
	if _, err := m.psql(ctx, server, fmt.Sprintf("REVOKE ALL ON DATABASE %s FROM PUBLIC", database.Name)); err != nil {
		return Instance{}, err
	}
	// La base « postgres » (celle d'Adminer) n'est pas non plus pour les services.
	if _, err := m.psql(ctx, server, "REVOKE CONNECT ON DATABASE postgres FROM PUBLIC"); err != nil {
		return Instance{}, err
	}
	if err := writeJSON(filepath.Join(m.serviceDir(name), databaseFile), map[string]string{"password": password}, 0o600); err != nil {
		return Instance{}, err
	}
	instance, err = m.changeInstance(name, func(instance *Instance) error {
		instance.Database = &database
		return nil
	})
	if err != nil {
		return Instance{}, err
	}
	return instance, m.applyHosting(ctx, name)
}

// dropDatabase efface la base et le compte d'un service désinstallé sans
// garder ses données. Appelé sous m.mu.
func (m *Manager) dropDatabase(ctx context.Context, instance Instance) error {
	if instance.Database == nil {
		return nil
	}
	if err := m.installed(instance.Database.Server); err != nil {
		return nil // serveur déjà retiré
	}
	name := instance.Database.Name
	if _, err := m.psql(ctx, instance.Database.Server, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", name)); err != nil {
		return err
	}
	_, err := m.psql(ctx, instance.Database.Server, fmt.Sprintf("DROP ROLE IF EXISTS %s", name))
	return err
}

// databaseClients : services qui ont une base dans ce serveur PostgreSQL.
func (m *Manager) databaseClients(server string) []string {
	instances, err := m.List()
	if err != nil {
		return nil
	}
	var clients []string
	for _, instance := range instances {
		if instance.Database != nil && instance.Database.Server == server {
			clients = append(clients, instance.Name)
		}
	}
	return clients
}
