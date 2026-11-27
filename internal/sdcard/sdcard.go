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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gmoulard/pi-appliance/internal/updates"
)

const (
	StateWriting   = "writing"
	StateVerifying = "verifying"
	StateDone      = "done"
	StateFailed    = "failed"
)

const (
	ErrNoImage        = "no-image"
	ErrDownload       = "download-failed"
	ErrChecksum       = "checksum"
	ErrTooSmall       = "too-small"
	ErrWrite          = "write-failed"
	ErrVerify         = "verify-failed"
	ErrDeviceBusy     = "busy"
	ErrCancelled      = "cancelled"
	ErrCardChanged    = "card-changed"
	ErrAlreadyRunning = "running"
	ErrInterrupted    = "interrupted"
)

type Failure struct {
	Code    string
	Refused string
}

func (f Failure) Error() string {
	if f.Refused != "" {
		return f.Code + " : " + f.Refused
	}
	return f.Code
}

type Device interface {
	io.ReadWriteCloser
	Sync() error
}

type Job struct {
	State     string `json:"state"`
	Device    string `json:"device"`
	Model     string `json:"model"`
	Size      int64  `json:"size"`
	Version   string `json:"version"`
	Percent   int    `json:"percent"`
	Error     string `json:"error,omitempty"`
	StartedAt string `json:"startedAt"`
}

type Status struct {
	Image *Image `json:"image"`
	Cards []Card `json:"cards"`
	Job   *Job   `json:"job"`
}

type Manager struct {
	Releases   updates.ReleaseLister
	Client     *http.Client
	Lsblk      func(ctx context.Context) ([]byte, error)
	Fstab      string
	Roles      map[string]string
	Decompress func(ctx context.Context, compressed io.Reader) (io.ReadCloser, func() error, error)
	Open       func(path string, write bool) (Device, error)
	Channel    func() string
	Marker     string
	Logf       func(format string, args ...any)

	mu           sync.Mutex
	image        *Image
	imageAt      time.Time
	imageErr     error
	imageChannel string
	restored     bool
	job          *Job
	cancel       context.CancelFunc
	done         atomic.Int64
	total        atomic.Int64
	verifying    atomic.Bool
}

func XZ(ctx context.Context, compressed io.Reader) (io.ReadCloser, func() error, error) {
	cmd := exec.CommandContext(ctx, "xz", "-dc")
	cmd.Stdin = compressed
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return out, func() error {
		if err := cmd.Wait(); err != nil {
			return fmt.Errorf("xz : %w %s", err, bytes.TrimSpace(stderr.Bytes()))
		}
		return nil
	}, nil
}

func Lsblk(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "lsblk", LsblkArgs...).Output()
}

func (m *Manager) logf(format string, args ...any) {
	if m.Logf != nil {
		m.Logf(format, args...)
	}
}

func (m *Manager) client() *http.Client {
	if m.Client != nil {
		return m.Client
	}
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: time.Minute}}
}

func (m *Manager) channel() string {
	if m.Channel != nil && m.Channel() == updates.Beta {
		return updates.Beta
	}
	return updates.Stable
}

func (m *Manager) currentImage(ctx context.Context) (*Image, error) {
	channel := m.channel()
	m.mu.Lock()
	cached, at, cachedErr, cachedChannel := m.image, m.imageAt, m.imageErr, m.imageChannel
	m.mu.Unlock()
	if cachedChannel == channel && ((cached != nil && time.Since(at) < time.Hour) || (cachedErr != nil && time.Since(at) < 5*time.Minute)) {
		return cached, cachedErr
	}
	image, err := m.findImage(ctx, channel)
	m.mu.Lock()
	m.image, m.imageAt, m.imageErr, m.imageChannel = image, time.Now(), err, channel
	m.mu.Unlock()
	return image, err
}

func (m *Manager) findImage(ctx context.Context, channel string) (*Image, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	releases, err := m.Releases.List(ctx)
	if err != nil {
		return nil, err
	}
	image, ok := latestImage(releases, channel)
	if !ok {
		return nil, Failure{Code: ErrNoImage}
	}
	size, err := xzSize(ctx, m.client(), image.URL, image.CompressedSize)
	if err != nil {
		m.logf("carte SD : taille de l'image %s inconnue : %v", image.Name, err)
	}
	image.Size = size
	return &image, nil
}

func (m *Manager) cards(ctx context.Context, imageSize int64) ([]Card, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := m.Lsblk(ctx)
	if err != nil {
		return nil, fmt.Errorf("lsblk : %w", err)
	}
	devices, err := parseLsblk(out)
	if err != nil {
		return nil, err
	}
	used := map[string]string{}
	if file, err := os.Open(m.Fstab); err == nil {
		used = usedUUIDs(file, m.Roles)
		file.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return Cards(devices, used, imageSize), nil
}

func (m *Manager) Status(ctx context.Context) (Status, error) {
	var status Status
	image, err := m.currentImage(ctx)
	if err != nil {
		m.logf("carte SD : image introuvable : %v", err)
	}
	status.Image = image
	var size int64
	if image != nil {
		size = image.Size
	}
	if status.Cards, err = m.cards(ctx, size); err != nil {
		return Status{}, err
	}
	status.Job = m.jobCopy()
	return status, nil
}

func (m *Manager) jobCopy() *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.restoreLocked()
	if m.job == nil {
		return nil
	}
	job := *m.job
	if job.State == StateWriting || job.State == StateVerifying {
		if total := m.total.Load(); total > 0 {
			job.Percent = int(min(99, m.done.Load()*100/total))
		}
		if m.verifying.Load() {
			job.State = StateVerifying
		}
	}
	return &job
}

func (m *Manager) Start(ctx context.Context, device, model string, size int64) error {
	m.mu.Lock()
	m.restoreLocked()
	running := m.job != nil && (m.job.State == StateWriting || m.job.State == StateVerifying)
	m.mu.Unlock()
	if running {
		return Failure{Code: ErrAlreadyRunning}
	}
	image, err := m.currentImage(ctx)
	if err != nil {
		var failure Failure
		if errors.As(err, &failure) {
			return failure
		}
		return Failure{Code: ErrDownload}
	}
	cards, err := m.cards(ctx, image.Size)
	if err != nil {
		return err
	}
	var card *Card
	for i := range cards {
		if cards[i].Device == device && cards[i].Model == model && cards[i].Size == size {
			card = &cards[i]
		}
	}
	if card == nil {
		return Failure{Code: ErrCardChanged}
	}
	if card.Refused != "" {
		return Failure{Code: "refused", Refused: card.Refused}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job != nil && (m.job.State == StateWriting || m.job.State == StateVerifying) {
		return Failure{Code: ErrAlreadyRunning}
	}
	jobCtx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.done.Store(0)
	m.total.Store(0)
	m.verifying.Store(false)
	m.job = &Job{State: StateWriting, Device: card.Device, Model: card.Model, Size: card.Size, Version: image.Version, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	m.mark(*m.job)
	go m.run(jobCtx, *card, *image)
	return nil
}

func (m *Manager) Cancel() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel == nil || m.job == nil || (m.job.State != StateWriting && m.job.State != StateVerifying) {
		return false
	}
	m.cancel()
	return true
}

func (m *Manager) run(ctx context.Context, card Card, image Image) {
	err := m.write(ctx, card, image)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancel = nil
	m.unmark()
	if err == nil {
		m.job.State, m.job.Percent = StateDone, 100
		m.logf("carte SD : %s (%s) prête avec pi-appliance %s", card.Device, card.Model, image.Version)
		return
	}
	code := ErrWrite
	var failure Failure
	if errors.As(err, &failure) {
		code = failure.Code
	}
	if ctx.Err() != nil {
		code = ErrCancelled
	}
	m.job.State, m.job.Error = StateFailed, code
	m.logf("carte SD : copie sur %s échouée : %v", card.Device, err)
}

// NE PAS se passer de la marque : une copie coupée par un redémarrage de la console laisse une carte à moitié écrite sans que personne le sache.
func (m *Manager) mark(job Job) {
	if m.Marker == "" {
		return
	}
	data, err := json.Marshal(job)
	if err == nil {
		err = os.WriteFile(m.Marker, data, 0o600)
	}
	if err != nil {
		m.logf("carte SD : copie en cours non enregistrée : %v", err)
	}
}

func (m *Manager) unmark() {
	if m.Marker == "" {
		return
	}
	if err := os.Remove(m.Marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		m.logf("carte SD : marque de la copie non effacée : %v", err)
	}
}

func (m *Manager) restoreLocked() {
	if m.restored || m.Marker == "" {
		return
	}
	m.restored = true
	data, err := os.ReadFile(m.Marker)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		m.logf("carte SD : marque de la copie illisible : %v", err)
		return
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		m.logf("carte SD : marque de la copie illisible : %v", err)
		job = Job{}
	}
	job.State, job.Percent, job.Error = StateFailed, 0, ErrInterrupted
	m.job = &job
	m.logf("carte SD : la copie sur %s a été interrompue par un redémarrage de la console", job.Device)
	m.unmark()
}

type counter struct {
	n *atomic.Int64
}

func (c counter) Write(p []byte) (int, error) {
	c.n.Add(int64(len(p)))
	return len(p), nil
}

func (m *Manager) get(ctx context.Context, url string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "pi-appliance")
	response, err := m.client().Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("réponse %d", response.StatusCode)
	}
	return response, nil
}

func (m *Manager) expectedChecksum(ctx context.Context, image Image) (string, error) {
	response, err := m.get(ctx, image.ChecksumURL)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return "", err
	}
	return parseChecksum(data, image.Name)
}

type limitedWriter struct {
	w      io.Writer
	left   int64
	tooBig bool
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.left {
		l.tooBig = true
		return 0, Failure{Code: ErrTooSmall}
	}
	n, err := l.w.Write(p)
	l.left -= int64(n)
	return n, err
}

func (m *Manager) write(ctx context.Context, card Card, image Image) error {
	expected, err := m.expectedChecksum(ctx, image)
	if err != nil {
		return fmt.Errorf("%w : %v", Failure{Code: ErrDownload}, err)
	}
	response, err := m.get(ctx, image.URL)
	if err != nil {
		return fmt.Errorf("%w : %v", Failure{Code: ErrDownload}, err)
	}
	defer response.Body.Close()
	device, err := m.Open(card.Device, true)
	if err != nil {
		return fmt.Errorf("%w : %v", Failure{Code: ErrDeviceBusy}, err)
	}
	written, imageSum, err := m.copyImage(ctx, response.Body, device, card, image, expected)
	if err == nil {
		err = device.Sync()
		if err != nil {
			err = fmt.Errorf("%w : %v", Failure{Code: ErrWrite}, err)
		}
	}
	if closeErr := device.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("%w : %v", Failure{Code: ErrWrite}, closeErr)
	}
	if err == nil {
		err = m.verify(ctx, card.Device, written, imageSum)
	}
	if err != nil {
		m.wipe(card.Device)
	}
	return err
}

func (m *Manager) copyImage(ctx context.Context, body io.Reader, device Device, card Card, image Image, expected string) (int64, []byte, error) {
	compressedSum := sha256.New()
	var downloaded atomic.Int64
	source := io.TeeReader(body, io.MultiWriter(compressedSum, counter{&downloaded}))
	decompressCtx, stop := context.WithCancel(ctx)
	defer stop()
	out, wait, err := m.Decompress(decompressCtx, source)
	if err != nil {
		return 0, nil, fmt.Errorf("%w : %v", Failure{Code: ErrWrite}, err)
	}
	imageSum := sha256.New()
	var written atomic.Int64
	if image.Size > 0 {
		m.total.Store(image.Size)
	} else {
		m.total.Store(image.CompressedSize)
	}
	progress := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-progress:
				return
			case <-ticker.C:
				if image.Size > 0 {
					m.done.Store(written.Load())
				} else {
					m.done.Store(downloaded.Load())
				}
			}
		}
	}()
	target := &limitedWriter{w: io.MultiWriter(device, imageSum, counter{&written}), left: card.Size}
	_, copyErr := io.CopyBuffer(target, out, make([]byte, 4<<20))
	close(progress)
	if copyErr != nil {
		stop()
	}
	out.Close()
	waitErr := wait()
	switch {
	case ctx.Err() != nil:
		return 0, nil, Failure{Code: ErrCancelled}
	case target.tooBig:
		return 0, nil, Failure{Code: ErrTooSmall}
	case copyErr != nil:
		return 0, nil, fmt.Errorf("%w : %v", Failure{Code: ErrWrite}, copyErr)
	}
	if _, err := io.Copy(io.Discard, source); err != nil {
		return 0, nil, fmt.Errorf("%w : %v", Failure{Code: ErrDownload}, err)
	}
	// NE PAS se fier à xz seul : sans comparer l'empreinte publiée, un fichier tronqué ou remplacé passerait.
	if got := hex.EncodeToString(compressedSum.Sum(nil)); got != expected {
		return 0, nil, fmt.Errorf("%w : %s au lieu de %s", Failure{Code: ErrChecksum}, got, expected)
	}
	if waitErr != nil {
		return 0, nil, fmt.Errorf("%w : %v", Failure{Code: ErrChecksum}, waitErr)
	}
	return written.Load(), imageSum.Sum(nil), nil
}

func (m *Manager) verify(ctx context.Context, path string, written int64, want []byte) error {
	m.verifying.Store(true)
	m.done.Store(0)
	m.total.Store(written)
	device, err := m.Open(path, false)
	if err != nil {
		return fmt.Errorf("%w : %v", Failure{Code: ErrVerify}, err)
	}
	defer device.Close()
	sum := sha256.New()
	buffer := make([]byte, 4<<20)
	var read int64
	for read < written {
		if ctx.Err() != nil {
			return Failure{Code: ErrCancelled}
		}
		n, err := device.Read(buffer[:min(int64(len(buffer)), written-read)])
		sum.Write(buffer[:n])
		read += int64(n)
		m.done.Store(read)
		if err == io.EOF && read < written {
			return fmt.Errorf("%w : carte plus courte que l'image", Failure{Code: ErrVerify})
		}
		if err != nil && err != io.EOF {
			return fmt.Errorf("%w : %v", Failure{Code: ErrVerify}, err)
		}
	}
	if !bytes.Equal(sum.Sum(nil), want) {
		return Failure{Code: ErrVerify}
	}
	return nil
}

func (m *Manager) wipe(path string) {
	device, err := m.Open(path, true)
	if err != nil {
		m.logf("carte SD : %s non effacée après l'échec : %v", path, err)
		return
	}
	defer device.Close()
	if _, err := device.Write(make([]byte, 1<<20)); err != nil {
		m.logf("carte SD : %s non effacée après l'échec : %v", path, err)
		return
	}
	if err := device.Sync(); err != nil {
		m.logf("carte SD : %s non effacée après l'échec : %v", path, err)
	}
}
