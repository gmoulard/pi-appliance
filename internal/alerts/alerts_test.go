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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/backup"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/internal/system"
	"github.com/gmoulard/pi-appliance/internal/updates"
)

type sentMail struct{ to, lang, subject, body string }

type fakeSender struct {
	configured bool
	fail       bool
	sent       []sentMail
}

func (f *fakeSender) Configured() bool { return f.configured }

func (f *fakeSender) SendAlert(to, name, lang, subject, body string) error {
	if f.fail {
		return errors.New("serveur injoignable")
	}
	f.sent = append(f.sent, sentMail{to, lang, subject, body})
	return nil
}

func (f *fakeSender) take() []sentMail {
	sent := f.sent
	f.sent = nil
	return sent
}

type fixture struct {
	monitor  *Monitor
	sender   *fakeSender
	now      time.Time
	problems map[Type][]Problem
}

func newFixture(t *testing.T, path string) *fixture {
	f := &fixture{sender: &fakeSender{configured: true}, now: time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC), problems: map[Type][]Problem{}}
	checks := map[Type]Check{}
	for _, kind := range Types {
		checks[kind] = func(context.Context, time.Time) ([]Problem, bool) { return f.problems[kind], true }
	}
	f.monitor = &Monitor{
		Path:   path,
		Checks: checks,
		Recipients: func() []Recipient {
			return []Recipient{{Name: "marie", Email: "marie@example.org", Lang: "fr"}, {Name: "john", Email: "john@example.org", Lang: "en"}}
		},
		Sender: f.sender,
		Link:   func() string { return "https://maison.local/pi-appliance/" },
		Now:    func() time.Time { return f.now },
	}
	return f
}

func (f *fixture) check(after time.Duration) []sentMail {
	f.now = f.now.Add(after)
	f.monitor.Check(context.Background())
	return f.sender.take()
}

func TestAlertOncePerDayThenRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alerts.json")
	f := newFixture(t, path)
	f.problems[Disk] = []Problem{{Key: "sd", Value: "93 %"}}
	sent := f.check(0)
	if len(sent) != 2 || sent[0].to != "marie@example.org" || sent[0].subject != "pi-appliance : disque presque plein" || !strings.Contains(sent[0].body, "- Carte SD : 93 % utilisés") || !strings.Contains(sent[0].body, "https://maison.local/pi-appliance/") {
		t.Fatalf("première alerte : %+v", sent)
	}
	if sent[1].subject != "pi-appliance: disk almost full" || !strings.Contains(sent[1].body, "SD card: 93 % used") {
		t.Fatalf("alerte en anglais : %+v", sent[1])
	}
	if sent := f.check(Every); len(sent) != 0 {
		t.Fatalf("même problème : %+v", sent)
	}
	// Un nouveau problème du même type dans la journée attend le lendemain.
	f.problems[Disk] = append(f.problems[Disk], Problem{Key: "usb", Value: "95 %"})
	if sent := f.check(time.Hour); len(sent) != 0 {
		t.Fatalf("rafale : %+v", sent)
	}
	// Redémarrage de la console : l'état est relu, pas de nouvel envoi.
	g := newFixture(t, path)
	g.now, g.problems = f.now, f.problems
	if sent := g.check(Every); len(sent) != 0 {
		t.Fatalf("après un redémarrage : %+v", sent)
	}
	sent = g.check(24 * time.Hour)
	if len(sent) != 2 || !strings.Contains(sent[0].body, "Disque USB : 95 % utilisés") || !strings.Contains(sent[0].body, "Carte SD") {
		t.Fatalf("le lendemain : %+v", sent)
	}
	g.problems[Disk] = nil
	sent = g.check(Every)
	if len(sent) != 2 || sent[0].subject != "pi-appliance : disque, retour à la normale" {
		t.Fatalf("retour à la normale : %+v", sent)
	}
	if sent := g.check(Every); len(sent) != 0 {
		t.Fatalf("retour à la normale répété : %+v", sent)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("alerts.json : %v %v", info, err)
	}
}

func TestServiceDelayAndDisabledType(t *testing.T) {
	f := newFixture(t, filepath.Join(t.TempDir(), "alerts.json"))
	f.problems[Service] = []Problem{{Key: "nextcloud"}}
	if sent := f.check(0); len(sent) != 0 {
		t.Fatalf("service rouge depuis peu : %+v", sent)
	}
	if sent := f.check(5 * time.Minute); len(sent) != 0 {
		t.Fatalf("service rouge depuis 5 minutes : %+v", sent)
	}
	sent := f.check(5 * time.Minute)
	if len(sent) != 2 || !strings.Contains(sent[0].body, "« nextcloud » est arrêté") {
		t.Fatalf("service rouge depuis 10 minutes : %+v", sent)
	}
	if err := f.monitor.SetEnabled(Power, false); err != nil {
		t.Fatal(err)
	}
	f.problems[Power] = []Problem{{Key: "démarrage-1"}}
	if sent := f.check(Every); len(sent) != 0 {
		t.Fatalf("type coupé : %+v", sent)
	}
	if err := f.monitor.SetEnabled("inconnu", false); !errors.Is(err, ErrUnknownType) {
		t.Fatalf("type inconnu : %v", err)
	}
	for _, s := range f.monitor.Settings() {
		if s.Enabled == (s.Type == Power) {
			t.Fatalf("réglages : %+v", f.monitor.Settings())
		}
	}
	// Réactivé : le problème toujours là est signalé.
	f.monitor.SetEnabled(Power, true)
	if sent := f.check(Every); len(sent) != 2 || !strings.Contains(sent[0].subject, "manque de courant") {
		t.Fatalf("type réactivé : %+v", sent)
	}
}

func TestNotSentWithoutMail(t *testing.T) {
	f := newFixture(t, filepath.Join(t.TempDir(), "alerts.json"))
	f.sender.configured = false
	f.problems[Backup] = []Problem{{Key: "2026-11-24T02:00:00Z", Value: "2026-11-24"}}
	if sent := f.check(0); len(sent) != 0 {
		t.Fatalf("envoi pas réglé : %+v", sent)
	}
	f.sender.configured, f.sender.fail = true, true
	f.check(Every)
	// Échec d'envoi : réessayé à la vérification suivante.
	f.sender.fail = false
	if sent := f.check(Every); len(sent) != 2 || !strings.Contains(sent[0].body, "Sauvegarde du 2026-11-24") {
		t.Fatalf("après réglage : %+v", sent)
	}
	f.monitor.Recipients = func() []Recipient { return nil }
	f.problems[Updates] = []Problem{{Key: "openssl"}}
	if sent := f.check(Every); len(sent) != 0 {
		t.Fatalf("sans administrateur vérifié : %+v", sent)
	}
}

func TestComposeCleansLines(t *testing.T) {
	_, body := Compose(Service, false, []Problem{{Key: "a{link}\nb"}}, "marie", "https://maison.local/pi-appliance/", "de")
	if strings.Contains(body, "{link}") || strings.Contains(body, "a\nb") || !strings.Contains(body, "\"alinkb\" is stopped") {
		t.Fatalf("texte : %q", body)
	}
	_, body = Compose(Power, false, []Problem{{Key: "x"}}, "marie", "", "fr")
	if strings.Contains(body, "\n- ") {
		t.Fatalf("ligne vide : %q", body)
	}
}

type fakeServices struct{ colors map[string]services.Color }

func (f fakeServices) List() ([]services.Instance, error) {
	var list []services.Instance
	for _, name := range []string{"nextcloud", "site", "wiki"} {
		list = append(list, services.Instance{Name: name})
	}
	return list, nil
}

func (f fakeServices) Status(_ context.Context, name string) (services.Status, error) {
	return services.Status{Color: f.colors[name]}, nil
}

func TestChecks(t *testing.T) {
	ctx, now := context.Background(), time.Now()
	resources := system.Resources{Disk: &system.Gauge{TotalMB: 1000, UsedMB: 905}, USBDisk: &system.Gauge{TotalMB: 1000, UsedMB: 100}, Power: &system.Power{UndervoltageSinceBoot: true}}
	problems, ok := DiskCheck(func() system.Resources { return resources })(ctx, now)
	if !ok || len(problems) != 1 || problems[0] != (Problem{Key: "sd", Value: "90 %"}) {
		t.Fatalf("disques : %v %v", problems, ok)
	}
	if _, ok := DiskCheck(func() system.Resources { return system.Resources{} })(ctx, now); ok {
		t.Fatal("disques inconnus")
	}
	bootIDPath = filepath.Join(t.TempDir(), "boot_id")
	os.WriteFile(bootIDPath, []byte("abc\n"), 0o600)
	if problems, ok := PowerCheck(func() system.Resources { return resources })(ctx, now); !ok || len(problems) != 1 || problems[0].Key != "abc" {
		t.Fatalf("alimentation : %v", problems)
	}
	manager := fakeServices{colors: map[string]services.Color{"nextcloud": services.Red, "site": services.Green, "wiki": services.Red}}
	problems, _ = ServiceCheck(manager, func(name string) bool { return name == "wiki" })(ctx, now)
	if len(problems) != 1 || problems[0].Key != "nextcloud" {
		t.Fatalf("services : %v", problems)
	}
	status := updates.Status{System: &updates.System{Packages: []updates.Package{{Name: "openssl", Security: true}, {Name: "vim"}}}}
	if problems, ok := UpdatesCheck(fakeUpdates{status})(ctx, now); !ok || len(problems) != 1 || problems[0].Key != "openssl" {
		t.Fatalf("correctifs : %v", problems)
	}
	if _, ok := UpdatesCheck(fakeUpdates{})(ctx, now); ok {
		t.Fatal("correctifs inconnus")
	}
}

type fakeUpdates struct{ status updates.Status }

func (f fakeUpdates) Status(context.Context) updates.Status { return f.status }

type fakeBackups struct {
	enabled bool
	last    *backup.Outcome
}

func (f fakeBackups) Settings() backup.Settings { return backup.Settings{Enabled: f.enabled} }
func (f fakeBackups) Last() *backup.Outcome     { return f.last }

func TestBackupCheck(t *testing.T) {
	at := time.Date(2026, 11, 24, 2, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name     string
		state    fakeBackups
		problems int
		ok       bool
	}{
		{"coupées", fakeBackups{false, &backup.Outcome{Action: "backup", Kind: backup.Nightly, At: at}}, 0, true},
		{"jamais faite", fakeBackups{true, nil}, 0, true},
		{"nuit en échec", fakeBackups{true, &backup.Outcome{Action: "backup", Kind: backup.Nightly, At: at}}, 1, true},
		{"réussie", fakeBackups{true, &backup.Outcome{Action: "backup", Kind: backup.Manual, OK: true, At: at}}, 0, true},
		{"à la demande en échec", fakeBackups{true, &backup.Outcome{Action: "backup", Kind: backup.Manual, At: at}}, 0, false},
		{"restauration", fakeBackups{true, &backup.Outcome{Action: "restore", At: at}}, 0, false},
	} {
		problems, ok := BackupCheck(c.state)(context.Background(), at)
		if len(problems) != c.problems || ok != c.ok {
			t.Errorf("%s : %v %v", c.name, problems, ok)
		}
	}
}

func TestCertificateCheck(t *testing.T) {
	now := time.Now()
	certificate := func(days int) string {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "x"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Duration(days) * 24 * time.Hour)}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	}
	file := map[string]any{"letsencrypt": map[string]any{
		"Account": map[string]any{"Email": "a@example.org"},
		"Certificates": []map[string]any{
			{"domain": map[string]any{"main": "maison.example.org"}, "certificate": certificate(5), "key": "secret"},
			{"domain": map[string]any{"main": "pi.duckdns.org"}, "certificate": certificate(60), "key": "secret"},
			{"domain": map[string]any{"main": "ancienne.example.org"}, "certificate": certificate(1), "key": "secret"},
		},
	}}
	data, _ := json.Marshal(file)
	path := filepath.Join(t.TempDir(), "acme.json")
	os.WriteFile(path, data, 0o600)
	hosts := func() []string { return []string{"maison.example.org", "pi.duckdns.org"} }
	problems, ok := CertificateCheck(path, hosts)(context.Background(), now)
	if !ok || len(problems) != 1 || problems[0].Key != "maison.example.org" {
		t.Fatalf("certificats : %v %v", problems, ok)
	}
	if problems, ok := CertificateCheck(filepath.Join(t.TempDir(), "absent"), hosts)(context.Background(), now); !ok || len(problems) != 0 {
		t.Fatalf("sans fichier : %v %v", problems, ok)
	}
}
