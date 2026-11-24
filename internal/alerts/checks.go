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

package alerts

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/internal/backup"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/internal/system"
	"github.com/gmoulard/pi-appliance/internal/updates"
)

// DiskPercent : part utilisée d'un disque à partir de laquelle il est
// « presque plein ».
const DiskPercent = 90

// CertificateDays : un certificat qui expire dans moins de jours n'a pas été
// renouvelé (Traefik le renouvelle 30 jours avant).
const CertificateDays = 14

// DiskCheck : carte SD et disque USB des données remplis à DiskPercent % ou plus.
func DiskCheck(resources func() system.Resources) Check {
	return func(context.Context, time.Time) ([]Problem, bool) {
		r := resources()
		if r.Disk == nil && r.USBDisk == nil {
			return nil, false
		}
		var problems []Problem
		for _, d := range []struct {
			key   string
			gauge *system.Gauge
		}{{"sd", r.Disk}, {"usb", r.USBDisk}} {
			if d.gauge == nil || d.gauge.TotalMB <= 0 {
				continue
			}
			percent := d.gauge.UsedMB * 100 / d.gauge.TotalMB
			if percent >= DiskPercent {
				problems = append(problems, Problem{Key: d.key, Value: fmt.Sprintf("%d %%", percent)})
			}
		}
		return problems, true
	}
}

// ServiceManager : ce que la vérification des services lit (*services.Manager).
type ServiceManager interface {
	List() ([]services.Instance, error)
	Status(ctx context.Context, name string) (services.Status, error)
}

// ServiceCheck : services arrêtés (rouges), sauf ceux arrêtés exprès dans la
// console (stopped). Un état inconnu (Docker qui ne répond pas, orange) n'alerte pas.
func ServiceCheck(manager ServiceManager, stopped func(string) bool) Check {
	return func(ctx context.Context, _ time.Time) ([]Problem, bool) {
		instances, err := manager.List()
		if err != nil {
			return nil, false
		}
		var problems []Problem
		for _, instance := range instances {
			if stopped != nil && stopped(instance.Name) {
				continue
			}
			status, err := manager.Status(ctx, instance.Name)
			if err == nil && status.Color == services.Red {
				problems = append(problems, Problem{Key: instance.Name})
			}
		}
		return problems, true
	}
}

// bootIDPath : identifiant du démarrage en cours, donné par le noyau.
var bootIDPath = "/proc/sys/kernel/random/boot_id"

// PowerCheck : sous-tension depuis le démarrage ; une alerte par démarrage.
func PowerCheck(resources func() system.Resources) Check {
	return func(context.Context, time.Time) ([]Problem, bool) {
		power := resources().Power
		if power == nil {
			return nil, false
		}
		if !power.UndervoltageSinceBoot {
			return nil, true
		}
		boot := "boot"
		if data, err := os.ReadFile(bootIDPath); err == nil && strings.TrimSpace(string(data)) != "" {
			boot = strings.TrimSpace(string(data))
		}
		return []Problem{{Key: boot}}, true
	}
}

// BackupState : ce que la vérification des sauvegardes lit (*backup.Manager).
type BackupState interface {
	Settings() backup.Settings
	Last() *backup.Outcome
}

// BackupCheck : dernière sauvegarde de la nuit en échec ; une sauvegarde
// réussie, de la nuit ou à la demande, est le retour à la normale.
func BackupCheck(backups BackupState) Check {
	return func(context.Context, time.Time) ([]Problem, bool) {
		if !backups.Settings().Enabled {
			return nil, true
		}
		last := backups.Last()
		switch {
		case last == nil:
			return nil, true
		case last.Action != "backup":
			// Une restauration : la dernière sauvegarde n'est plus connue ici.
			return nil, false
		case last.OK:
			return nil, true
		case last.Kind == backup.Nightly:
			return []Problem{{Key: last.At.UTC().Format(time.RFC3339), Value: last.At.Local().Format("2006-01-02")}}, true
		default:
			// Échec d'une sauvegarde à la demande : vu dans la console.
			return nil, false
		}
	}
}

// UpdatesState : ce que la vérification des correctifs lit (*updates.Checker).
type UpdatesState interface {
	Status(ctx context.Context) updates.Status
}

// UpdatesCheck : correctifs de sécurité du système en attente, un problème par
// paquet (un nouveau correctif est un nouveau problème).
func UpdatesCheck(checker UpdatesState) Check {
	return func(ctx context.Context, _ time.Time) ([]Problem, bool) {
		status := checker.Status(ctx)
		if status.System == nil {
			return nil, false
		}
		var problems []Problem
		for _, p := range status.System.Packages {
			if p.Security {
				problems = append(problems, Problem{Key: p.Name, Value: p.To})
			}
		}
		return problems, true
	}
}

// acmeFile : ce qui est lu du fichier des certificats Let's Encrypt de
// Traefik (acme.json) ; les clés privées ne sont pas lues.
type acmeFile map[string]*struct {
	Certificates []struct {
		Domain struct {
			Main string   `json:"main"`
			SANs []string `json:"sans"`
		} `json:"domain"`
		Certificate string `json:"certificate"`
	} `json:"Certificates"`
}

// CertificateCheck : certificats Let's Encrypt des adresses internet du Pi
// (hosts) qui expirent dans moins de CertificateDays jours. Les certificats
// d'adresses que le Pi n'a plus ne comptent pas : Traefik ne les renouvelle pas.
func CertificateCheck(path string, hosts func() []string) Check {
	return func(_ context.Context, now time.Time) ([]Problem, bool) {
		wanted := hosts()
		if len(wanted) == 0 {
			return nil, true
		}
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, true
		}
		if err != nil {
			return nil, false
		}
		var file acmeFile
		if err := json.Unmarshal(data, &file); err != nil {
			return nil, false
		}
		var problems []Problem
		for _, resolver := range file {
			if resolver == nil {
				continue
			}
			for _, c := range resolver.Certificates {
				domain := strings.ToLower(c.Domain.Main)
				if !slices.Contains(wanted, domain) || slices.ContainsFunc(problems, func(p Problem) bool { return p.Key == domain }) {
					continue
				}
				expires, ok := certificateEnd(c.Certificate)
				if ok && expires.Sub(now) < CertificateDays*24*time.Hour {
					problems = append(problems, Problem{Key: domain, Value: expires.Local().Format("2006-01-02")})
				}
			}
		}
		return problems, true
	}
}

// certificateEnd : fin de validité du premier certificat (celui du domaine)
// d'une chaîne PEM en base64, comme Traefik l'écrit.
func certificateEnd(encoded string) (time.Time, bool) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return time.Time{}, false
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return time.Time{}, false
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, false
	}
	return certificate.NotAfter, true
}
