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

package sdcard

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/updates"
)

const lsblkSample = `{"blockdevices": [
 {"name":"/dev/mmcblk0","type":"disk","tran":null,"rm":false,"size":31914983424,"model":null,"vendor":null,"uuid":null,"mountpoint":null,
  "children":[{"name":"/dev/mmcblk0p2","type":"part","tran":null,"rm":false,"size":31000000000,"uuid":"root","mountpoint":"/"}]},
 {"name":"/dev/sda","type":"disk","tran":"usb","rm":"0","size":"500107862016","model":"SSD 870","vendor":"Samsung ","uuid":null,"mountpoint":null,
  "children":[{"name":"/dev/sda1","type":"part","size":"536870912","uuid":"boot","mountpoint":"/boot/firmware"},{"name":"/dev/sda2","type":"part","size":"499000000000","uuid":"r2","mountpoint":"/"}]},
 {"name":"/dev/sdb","type":"disk","tran":"usb","rm":true,"size":64000000000,"model":"Backup","vendor":"WD","uuid":null,"mountpoint":null,
  "children":[{"name":"/dev/sdb1","type":"part","size":64000000000,"uuid":"bk-uuid","mountpoint":null}]},
 {"name":"/dev/sdc","type":"disk","tran":"usb","rm":true,"size":32000000000,"model":"Stick","vendor":"","uuid":null,"mountpoint":null,
  "children":[{"name":"/dev/sdc1","type":"part","size":32000000000,"uuid":"x","mountpoint":"/media/stick"}]},
 {"name":"/dev/sdd","type":"disk","tran":"usb","rm":false,"size":1000000000000,"model":"Disque dur","vendor":"","uuid":null,"mountpoint":null},
 {"name":"/dev/sde","type":"disk","tran":"usb","rm":true,"size":2000000000,"model":"Petite","vendor":"","uuid":null,"mountpoint":null},
 {"name":"/dev/sdf","type":"disk","tran":"usb","rm":"1","size":"15931539456","model":"SD/MMC","vendor":"Generic","uuid":null,"mountpoint":null,
  "children":[{"name":"/dev/sdf1","type":"part","size":"15000000000","uuid":"old","mountpoint":null}]},
 {"name":"/dev/sdg","type":"disk","tran":"usb","rm":true,"size":0,"model":"Lecteur","vendor":"","uuid":null,"mountpoint":null},
 {"name":"/dev/sdh","type":"disk","tran":"usb","rm":true,"size":64000000000,"model":"Data","vendor":"","uuid":null,"mountpoint":null,
  "children":[{"name":"/dev/sdh1","type":"part","size":64000000000,"uuid":"data-uuid","mountpoint":"/mnt/pi-appliance-data"}]},
 {"name":"/dev/nvme0n1","type":"disk","tran":"nvme","rm":false,"size":256000000000,"model":"NVMe","vendor":"","uuid":null,"mountpoint":null}
]}`

const fstabSample = `# fstab
PARTUUID=abc / ext4 defaults 0 1
UUID=bk-uuid /mnt/pi-appliance-backup ext4 defaults,noatime,nofail 0 2
UUID="data-uuid" /mnt/pi-appliance-data ext4 defaults,noatime,nofail 0 2
UUID=other /srv ext4 defaults 0 2
`

var testRoles = map[string]string{"/mnt/pi-appliance-backup": RefusedBackup, "/mnt/pi-appliance-data": RefusedData}

func TestCards(t *testing.T) {
	devices, err := parseLsblk([]byte(lsblkSample))
	if err != nil {
		t.Fatal(err)
	}
	used := usedUUIDs(strings.NewReader(fstabSample), testRoles)
	cards := Cards(devices, used, 3_000_000_000)
	want := map[string]string{
		"/dev/sda": RefusedSystem,
		"/dev/sdb": RefusedBackup,
		"/dev/sdc": RefusedMounted,
		"/dev/sdd": "",
		"/dev/sde": RefusedTooSmall,
		"/dev/sdf": "",
		"/dev/sdg": RefusedEmpty,
		"/dev/sdh": RefusedData,
	}
	if len(cards) != len(want) {
		t.Fatalf("%d cartes au lieu de %d : %+v", len(cards), len(want), cards)
	}
	for _, card := range cards {
		reason, ok := want[card.Device]
		if !ok {
			t.Errorf("%s ne devrait pas être listé", card.Device)
			continue
		}
		if card.Refused != reason {
			t.Errorf("%s : refus %q au lieu de %q", card.Device, card.Refused, reason)
		}
	}
	if cards[5].Model != "Generic SD/MMC" || cards[5].Size != 15931539456 {
		t.Errorf("carte : %+v", cards[5])
	}
	if got := Cards(devices, used, 0)[4].Refused; got != "" {
		t.Errorf("taille de l'image inconnue : refus %q", got)
	}
}

func TestLatestImage(t *testing.T) {
	asset := func(name string) updates.Asset { return updates.Asset{Name: name, URL: "https://x/" + name, Size: 10} }
	releases := []updates.Release{
		{Tag: "v0.2.4", Assets: []updates.Asset{asset("pi-appliance-v0.2.4-arm64.tar.gz")}},
		{Tag: "v0.2.3-beta.1", Assets: []updates.Asset{asset("pi-appliance-v0.2.3-beta.1.img.xz"), asset("pi-appliance-v0.2.3-beta.1.img.xz.sha256")}},
		{Tag: "v0.2.2", Assets: []updates.Asset{asset("pi-appliance-v0.2.2.img.xz")}},
		{Tag: "v0.1.9", Assets: []updates.Asset{asset("pi-appliance-v0.1.9.img.xz"), asset("pi-appliance-v0.1.9.img.xz.sha256")}},
		{Tag: "v0.2.0", Assets: []updates.Asset{asset("pi-appliance-v0.2.0.img.xz"), asset("pi-appliance-v0.2.0.img.xz.sha256")}},
		{Tag: "v0.3.0", Draft: true, Assets: []updates.Asset{asset("pi-appliance-v0.3.0.img.xz"), asset("pi-appliance-v0.3.0.img.xz.sha256")}},
	}
	image, ok := latestImage(releases, updates.Stable)
	if !ok || image.Version != "v0.2.0" || image.ChecksumURL != "https://x/pi-appliance-v0.2.0.img.xz.sha256" {
		t.Fatalf("image : %+v %v", image, ok)
	}
	if image, ok := latestImage(releases, updates.Beta); !ok || image.Version != "v0.2.3-beta.1" {
		t.Fatalf("canal bêta : %+v %v", image, ok)
	}
	if _, ok := latestImage(releases[:1], updates.Beta); ok {
		t.Error("version sans image retenue")
	}
}

func TestParseChecksum(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	if got, err := parseChecksum([]byte(sum+"  pi.img.xz\n"), "pi.img.xz"); err != nil || got != sum {
		t.Errorf("%q %v", got, err)
	}
	if _, err := parseChecksum([]byte(sum+"  autre.img.xz\n"), "pi.img.xz"); err == nil {
		t.Error("empreinte d'un autre fichier acceptée")
	}
	if _, err := parseChecksum([]byte("zz  pi.img.xz\n"), "pi.img.xz"); err == nil {
		t.Error("empreinte illisible acceptée")
	}
}

type fakeReleases []updates.Release

func (f fakeReleases) List(context.Context) ([]updates.Release, error) { return f, nil }

type fixture struct {
	manager *Manager
	device  string
	image   []byte
	sum     string
}

func newFixture(t *testing.T, cardSize int64, checksum func(real string) string) *fixture {
	t.Helper()
	if _, err := exec.LookPath("xz"); err != nil {
		t.Skip("xz absent")
	}
	dir := t.TempDir()
	image := make([]byte, 3<<20+12345)
	if _, err := rand.Read(image[:1<<20]); err != nil {
		t.Fatal(err)
	}
	raw := filepath.Join(dir, "pi.img")
	if err := os.WriteFile(raw, image, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("xz", "-k", "-1", raw).CombinedOutput(); err != nil {
		t.Fatalf("xz : %v %s", err, out)
	}
	compressed, err := os.ReadFile(raw + ".xz")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(compressed)
	real := hex.EncodeToString(sum[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pi-appliance-v0.2.0.img.xz":
			http.ServeContent(w, r, "x", time.Time{}, bytes.NewReader(compressed))
		case "/pi-appliance-v0.2.0.img.xz.sha256":
			io.WriteString(w, checksum(real)+"  pi-appliance-v0.2.0.img.xz\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	device := filepath.Join(dir, "card")
	if err := os.WriteFile(device, bytes.Repeat([]byte{0xff}, 2<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	lsblk := `{"blockdevices":[{"name":"/dev/sdz","type":"disk","tran":"usb","rm":true,"size":` + strconv.FormatInt(cardSize, 10) + `,"model":"Carte","vendor":"Generic"}]}`
	manager := &Manager{
		Releases: fakeReleases{{Tag: "v0.2.0", Assets: []updates.Asset{
			{Name: "pi-appliance-v0.2.0.img.xz", URL: server.URL + "/pi-appliance-v0.2.0.img.xz", Size: int64(len(compressed))},
			{Name: "pi-appliance-v0.2.0.img.xz.sha256", URL: server.URL + "/pi-appliance-v0.2.0.img.xz.sha256"},
		}}},
		Client:     server.Client(),
		Lsblk:      func(context.Context) ([]byte, error) { return []byte(lsblk), nil },
		Fstab:      filepath.Join(dir, "fstab"),
		Decompress: XZ,
		Open: func(path string, write bool) (Device, error) {
			if path != "/dev/sdz" {
				return nil, errors.New("périphérique inattendu " + path)
			}
			if write {
				return os.OpenFile(device, os.O_WRONLY, 0)
			}
			return os.Open(device)
		},
	}
	return &fixture{manager: manager, device: device, image: image, sum: real}
}

func waitJob(t *testing.T, manager *Manager) Job {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if job := manager.jobCopy(); job != nil && job.State != StateWriting && job.State != StateVerifying {
			return *job
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("copie trop longue")
	return Job{}
}

func TestWriteAndVerify(t *testing.T) {
	f := newFixture(t, 64<<20, func(real string) string { return real })
	status, err := f.manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Image == nil || status.Image.Size != int64(len(f.image)) || status.Image.Version != "v0.2.0" {
		t.Fatalf("image : %+v", status.Image)
	}
	if len(status.Cards) != 1 || status.Cards[0].Refused != "" {
		t.Fatalf("cartes : %+v", status.Cards)
	}
	if err := f.manager.Start(context.Background(), "/dev/sdz", "Generic Carte", 64<<20); err != nil {
		t.Fatal(err)
	}
	job := waitJob(t, f.manager)
	if job.State != StateDone || job.Percent != 100 {
		t.Fatalf("copie : %+v", job)
	}
	written, err := os.ReadFile(f.device)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written[:len(f.image)], f.image) {
		t.Error("carte différente de l'image")
	}
}

func TestTooSmallRefused(t *testing.T) {
	f := newFixture(t, 1<<20, func(real string) string { return real })
	err := f.manager.Start(context.Background(), "/dev/sdz", "Generic Carte", 1<<20)
	var failure Failure
	if !errors.As(err, &failure) || failure.Refused != RefusedTooSmall {
		t.Fatalf("carte trop petite : %v", err)
	}
	if err := f.manager.Start(context.Background(), "/dev/sdz", "Autre carte", 1<<20); !errors.As(err, &failure) || failure.Code != ErrCardChanged {
		t.Fatalf("carte changée : %v", err)
	}
}

func TestWrongChecksumRefused(t *testing.T) {
	f := newFixture(t, 64<<20, func(string) string { return strings.Repeat("0", 64) })
	if err := f.manager.Start(context.Background(), "/dev/sdz", "Generic Carte", 64<<20); err != nil {
		t.Fatal(err)
	}
	job := waitJob(t, f.manager)
	if job.State != StateFailed || job.Error != ErrChecksum {
		t.Fatalf("empreinte fausse : %+v", job)
	}
	written, _ := os.ReadFile(f.device)
	if !bytes.Equal(written[:1<<20], make([]byte, 1<<20)) {
		t.Error("début de la carte non effacé après l'échec")
	}
}

type corrupted struct {
	*os.File
}

func (c corrupted) Read(p []byte) (int, error) {
	n, err := c.File.Read(p)
	if n > 0 {
		p[n/2] ^= 0x01
	}
	return n, err
}

func TestVerifyMismatchFails(t *testing.T) {
	f := newFixture(t, 64<<20, func(real string) string { return real })
	open := f.manager.Open
	f.manager.Open = func(path string, write bool) (Device, error) {
		device, err := open(path, write)
		if err != nil || write {
			return device, err
		}
		return corrupted{device.(*os.File)}, nil
	}
	if err := f.manager.Start(context.Background(), "/dev/sdz", "Generic Carte", 64<<20); err != nil {
		t.Fatal(err)
	}
	job := waitJob(t, f.manager)
	if job.State != StateFailed || job.Error != ErrVerify {
		t.Fatalf("relecture différente : %+v", job)
	}
}

func TestStartRefusesSystemDisk(t *testing.T) {
	f := newFixture(t, 64<<20, func(real string) string { return real })
	f.manager.Lsblk = func(context.Context) ([]byte, error) { return []byte(lsblkSample), nil }
	f.manager.Roles = testRoles
	if err := os.WriteFile(f.manager.Fstab, []byte(fstabSample), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, device := range []struct {
		path, model string
		size        int64
		reason      string
	}{
		{"/dev/sda", "Samsung SSD 870", 500107862016, RefusedSystem},
		{"/dev/sdc", "Stick", 32000000000, RefusedMounted},
		{"/dev/sdb", "WD Backup", 64000000000, RefusedBackup},
		{"/dev/sdh", "Data", 64000000000, RefusedData},
	} {
		err := f.manager.Start(context.Background(), device.path, device.model, device.size)
		var failure Failure
		if !errors.As(err, &failure) || failure.Refused != device.reason {
			t.Errorf("%s : %v", device.path, err)
		}
	}
	if err := f.manager.Start(context.Background(), "/dev/mmcblk0", "", 31914983424); err == nil {
		t.Error("carte du Pi acceptée")
	}
}

type blocking struct{ ctx context.Context }

func (b blocking) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b blocking) Close() error { return nil }

func TestCancel(t *testing.T) {
	f := newFixture(t, 64<<20, func(real string) string { return real })
	f.manager.Decompress = func(ctx context.Context, _ io.Reader) (io.ReadCloser, func() error, error) {
		return blocking{ctx}, func() error { return nil }, nil
	}
	if err := f.manager.Start(context.Background(), "/dev/sdz", "Generic Carte", 64<<20); err != nil {
		t.Fatal(err)
	}
	var failure Failure
	if err := f.manager.Start(context.Background(), "/dev/sdz", "Generic Carte", 64<<20); !errors.As(err, &failure) || failure.Code != ErrAlreadyRunning {
		t.Fatalf("deuxième copie : %v", err)
	}
	if !f.manager.Cancel() {
		t.Fatal("annulation refusée")
	}
	job := waitJob(t, f.manager)
	if job.State != StateFailed || job.Error != ErrCancelled {
		t.Fatalf("annulation : %+v", job)
	}
}

func TestFixedUSBDiskAccepted(t *testing.T) {
	f := newFixture(t, 64<<20, func(real string) string { return real })
	lsblk := `{"blockdevices":[{"name":"/dev/sdz","type":"disk","tran":"usb","rm":false,"size":67108864,"model":"SSD","vendor":"Generic"}]}`
	f.manager.Lsblk = func(context.Context) ([]byte, error) { return []byte(lsblk), nil }
	if err := f.manager.Start(context.Background(), "/dev/sdz", "Generic SSD", 64<<20); err != nil {
		t.Fatal(err)
	}
	if job := waitJob(t, f.manager); job.State != StateDone {
		t.Fatalf("SSD USB : %+v", job)
	}
}

func TestImageFollowsChannel(t *testing.T) {
	f := newFixture(t, 64<<20, func(real string) string { return real })
	releases := f.manager.Releases.(fakeReleases)
	beta := updates.Release{Tag: "v0.3.0-beta.1", Prerelease: true, Assets: []updates.Asset{
		{Name: "pi-appliance-v0.3.0-beta.1.img.xz", URL: releases[0].Assets[0].URL, Size: releases[0].Assets[0].Size},
		{Name: "pi-appliance-v0.3.0-beta.1.img.xz.sha256", URL: releases[0].Assets[1].URL},
	}}
	f.manager.Releases = append(fakeReleases{beta}, releases...)
	channel := updates.Stable
	f.manager.Channel = func() string { return channel }
	status, err := f.manager.Status(context.Background())
	if err != nil || status.Image == nil || status.Image.Version != "v0.2.0" {
		t.Fatalf("canal stable : %+v %v", status.Image, err)
	}
	channel = updates.Beta
	status, err = f.manager.Status(context.Background())
	if err != nil || status.Image == nil || status.Image.Version != "v0.3.0-beta.1" {
		t.Fatalf("canal bêta : %+v %v", status.Image, err)
	}
}

func TestInterruptedCopyReported(t *testing.T) {
	f := newFixture(t, 64<<20, func(real string) string { return real })
	f.manager.Marker = filepath.Join(t.TempDir(), "sdcard-copy.json")
	f.manager.Decompress = func(ctx context.Context, _ io.Reader) (io.ReadCloser, func() error, error) {
		return blocking{ctx}, func() error { return nil }, nil
	}
	if err := f.manager.Start(context.Background(), "/dev/sdz", "Generic Carte", 64<<20); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(f.manager.Marker)
	if err != nil {
		t.Fatalf("copie en cours non marquée : %v", err)
	}
	restarted := &Manager{Marker: filepath.Join(t.TempDir(), "sdcard-copy.json")}
	if err := os.WriteFile(restarted.Marker, saved, 0o600); err != nil {
		t.Fatal(err)
	}
	job := restarted.jobCopy()
	if job == nil || job.State != StateFailed || job.Error != ErrInterrupted || job.Device != "/dev/sdz" || job.Version != "v0.2.0" {
		t.Fatalf("copie interrompue : %+v", job)
	}
	if _, err := os.Stat(restarted.Marker); !os.IsNotExist(err) {
		t.Fatalf("marque gardée après le signalement : %v", err)
	}
	if !f.manager.Cancel() {
		t.Fatal("annulation refusée")
	}
	waitJob(t, f.manager)
	if _, err := os.Stat(f.manager.Marker); !os.IsNotExist(err) {
		t.Fatalf("marque gardée à la fin de la copie : %v", err)
	}
	if job := f.manager.jobCopy(); job.Error != ErrCancelled {
		t.Fatalf("fin de copie : %+v", job)
	}
}
