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
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gmoulard/pi-appliance/internal/addresses"
)

// Sauvegarde croisée avec un autre pi-appliance (spécification 9.1).
//
// Le Pi qui accueille : son administrateur fixe un espace réservé et obtient
// un code (valable une heure, une seule fois, 5 essais) ; les sauvegardes
// reçues vont sur son disque de sauvegarde, sous pi-appliance-proches/<proche>/,
// déjà chiffrées par le Pi qui les envoie : il ne peut pas les lire, ni le nom
// des services (manifest chiffré, fichiers numérotés).
//
// Le Pi qui envoie : son administrateur saisit l'adresse internet de l'autre
// Pi et le code ; chaque demande est ensuite signée par la clé de l'appliance
// (Ed25519, internal/addresses), seule reconnue par l'autre Pi.
//
// API des proches, sans session (<prefix>/api/peer/…, demandes signées) :
// POST pair, GET snapshots, PUT files/<date>/<décalage>/<fichier>,
// GET files/<date>/<fichier>, POST commit/<date>, DELETE snapshots/<date>.

// PeerPath : chemin de l'API des proches sur l'autre Pi.
const PeerPath = "/pi-appliance/api/peer"

// PeerSigner signe une demande faite à un autre pi-appliance (*addresses.Key).
type PeerSigner interface {
	Sign(r *http.Request, body []byte, now time.Time)
	Public() string
}

// HostedTarget : dossier des sauvegardes accueillies, sur le disque de
// sauvegarde monté sur mount ; ErrNoDisk sans disque.
func HostedTarget(mount string) func() (string, error) {
	disk := DiskTarget(mount, "x")
	return func() (string, error) {
		if _, err := disk(); err != nil {
			return "", err
		}
		return filepath.Join(mount, "pi-appliance-proches"), nil
	}
}

var (
	// ErrPeerCode : code de jumelage inconnu, déjà servi ou périmé.
	ErrPeerCode = errors.New("code de jumelage incorrect ou périmé")
	// ErrPeerFull : l'espace réservé chez le proche est plein.
	ErrPeerFull = errors.New("l'espace réservé chez votre proche est plein")
	// ErrPeerUnknown : l'autre Pi ne connaît pas (plus) ce Pi.
	ErrPeerUnknown = errors.New("l'autre pi-appliance ne reconnaît plus ce Pi : refaites le jumelage")
)

// Hôte : l'accueil des proches.

type hostedGuest struct {
	ID       string    `json:"id"`
	Key      string    `json:"key"`
	Name     string    `json:"name"`
	QuotaMB  int64     `json:"quota_mb"`
	Paired   time.Time `json:"paired"`
	LastSeen time.Time `json:"last_seen"`
}

type pairingCode struct {
	Hash     string    `json:"hash"`
	Name     string    `json:"name"`
	QuotaMB  int64     `json:"quota_mb"`
	Expires  time.Time `json:"expires"`
	Failures int       `json:"failures"`
}

type hostingState struct {
	Pairing *pairingCode  `json:"pairing,omitempty"`
	Guests  []hostedGuest `json:"guests"`
}

// GuestPublic : un proche accueilli, vu par la console.
type GuestPublic struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	QuotaMB  int64     `json:"quota_mb"`
	UsedMB   int64     `json:"used_mb"`
	LastSeen time.Time `json:"last_seen"`
}

// HostingStatus : l'accueil des sauvegardes de proches.
type HostingStatus struct {
	// Available : un disque de sauvegarde est branché pour les recevoir.
	Available   bool          `json:"available"`
	CodeExpires *time.Time    `json:"code_expires,omitempty"`
	Guests      []GuestPublic `json:"guests"`
}

// PeerGuest : le proche qui garde nos sauvegardes.
type PeerGuest struct {
	Address string `json:"address"`
	QuotaMB int64  `json:"quota_mb"`
}

// PeerStatus : sauvegarde chez un proche et proches accueillis ici.
type PeerStatus struct {
	// Available : ce Pi a une clé pour signer ses demandes.
	Available bool          `json:"available"`
	Peer      *PeerGuest    `json:"peer"`
	Hosting   HostingStatus `json:"hosting"`
}

var hostingMu sync.Mutex

func (m *Manager) hostingPath() string { return filepath.Join(m.Dir, stateDir, "hosting.json") }
func (m *Manager) peerPath() string    { return filepath.Join(m.Dir, stateDir, "peer.json") }

func (m *Manager) hosting() hostingState {
	state := hostingState{Guests: []hostedGuest{}}
	if data, err := os.ReadFile(m.hostingPath()); err == nil {
		json.Unmarshal(data, &state)
	}
	if state.Guests == nil {
		state.Guests = []hostedGuest{}
	}
	return state
}

func (m *Manager) saveHosting(state hostingState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(m.hostingPath(), append(data, '\n'), 0o600)
}

// codeAlphabet : sans 0, O, 1, I ni L, qui se confondent à la lecture.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func normalizeCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		if 'a' <= r && r <= 'z' {
			return r - 'a' + 'A'
		}
		return r
	}, code)
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte("pi-appliance peer code " + normalizeCode(code)))
	return hex.EncodeToString(sum[:])
}

func guestID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

// HostingCode tire un code de jumelage pour accueillir un proche, avec un
// espace réservé de quotaMB ; il remplace le code précédent.
func (m *Manager) HostingCode(name string, quotaMB int64) (string, time.Time, error) {
	if m.Hosted == nil {
		return "", time.Time{}, ErrNoDisk
	}
	if _, err := m.Hosted(); err != nil {
		return "", time.Time{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 60 {
		return "", time.Time{}, fmt.Errorf("%w : donnez un nom à ce proche (60 caractères au plus)", ErrInvalid)
	}
	if quotaMB < 1024 || quotaMB > 100<<20 {
		return "", time.Time{}, fmt.Errorf("%w : l'espace réservé va de 1 Go à 100 To", ErrInvalid)
	}
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	var b strings.Builder
	for i, c := range raw {
		if i == 5 {
			b.WriteByte('-')
		}
		b.WriteByte(codeAlphabet[int(c)%len(codeAlphabet)])
	}
	code := b.String()
	expires := m.now().Add(time.Hour).UTC()
	hostingMu.Lock()
	defer hostingMu.Unlock()
	state := m.hosting()
	state.Pairing = &pairingCode{Hash: hashCode(code), Name: name, QuotaMB: quotaMB, Expires: expires}
	return code, expires, m.saveHosting(state)
}

// SetGuestQuota change l'espace réservé à un proche.
func (m *Manager) SetGuestQuota(id string, quotaMB int64) error {
	if quotaMB < 1024 || quotaMB > 100<<20 {
		return fmt.Errorf("%w : l'espace réservé va de 1 Go à 100 To", ErrInvalid)
	}
	hostingMu.Lock()
	defer hostingMu.Unlock()
	state := m.hosting()
	i := slices.IndexFunc(state.Guests, func(g hostedGuest) bool { return g.ID == id })
	if i < 0 {
		return ErrNotFound
	}
	state.Guests[i].QuotaMB = quotaMB
	return m.saveHosting(state)
}

// RemoveGuest arrête d'accueillir un proche et efface ses sauvegardes.
func (m *Manager) RemoveGuest(id string) error {
	hostingMu.Lock()
	defer hostingMu.Unlock()
	state := m.hosting()
	i := slices.IndexFunc(state.Guests, func(g hostedGuest) bool { return g.ID == id })
	if i < 0 {
		return ErrNotFound
	}
	if m.Hosted != nil {
		if root, err := m.Hosted(); err == nil {
			if err := os.RemoveAll(filepath.Join(root, id)); err != nil {
				return err
			}
		}
	}
	state.Guests = slices.Delete(state.Guests, i, i+1)
	return m.saveHosting(state)
}

func (m *Manager) hostingStatus() HostingStatus {
	status := HostingStatus{Guests: []GuestPublic{}}
	var root string
	if m.Hosted != nil {
		if dir, err := m.Hosted(); err == nil {
			status.Available, root = true, dir
		}
	}
	hostingMu.Lock()
	state := m.hosting()
	hostingMu.Unlock()
	if p := state.Pairing; p != nil && m.now().Before(p.Expires) {
		expires := p.Expires
		status.CodeExpires = &expires
	}
	for _, g := range state.Guests {
		public := GuestPublic{ID: g.ID, Name: g.Name, QuotaMB: g.QuotaMB, LastSeen: g.LastSeen}
		if root != "" {
			public.UsedMB = dirSize(filepath.Join(root, g.ID)) >> 20
		}
		status.Guests = append(status.Guests, public)
	}
	return status
}

func (m *Manager) peerStatus(context.Context) *PeerStatus {
	status := &PeerStatus{Available: m.PeerKey != nil, Hosting: m.hostingStatus()}
	if peer, ok := m.peerSettings(); ok {
		status.Peer = &PeerGuest{Address: peer.Address, QuotaMB: peer.QuotaMB}
	}
	return status
}

// maxPeerBody : un morceau envoyé, plus de quoi signer.
func maxPeerBody() int64 { return int64(onlinePartSize) + 1<<20 }

// peerRequest vérifie une demande d'un proche : signature, et proche connu
// (sauf pour le jumelage). Rend le corps, la clé et le proche.
func (m *Manager) peerRequest(w http.ResponseWriter, r *http.Request, pairing bool) ([]byte, string, *hostedGuest, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPeerBody()))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "Demande trop grande.")
		return nil, "", nil, false
	}
	key, err := addresses.Verify(r, body, m.now())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Demande non reconnue.")
		return nil, "", nil, false
	}
	if pairing {
		return body, key, nil, true
	}
	hostingMu.Lock()
	defer hostingMu.Unlock()
	state := m.hosting()
	i := slices.IndexFunc(state.Guests, func(g hostedGuest) bool { return g.Key == key })
	if i < 0 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Ce Pi n'est pas jumelé ici.", "code": "unknown"})
		return nil, "", nil, false
	}
	guest := state.Guests[i]
	if m.now().Sub(guest.LastSeen) > time.Hour {
		state.Guests[i].LastSeen = m.now().UTC()
		m.saveHosting(state)
	}
	return body, key, &guest, true
}

// guestStore : les sauvegardes d'un proche sur le disque de ce Pi.
func (m *Manager) guestStore(w http.ResponseWriter, guest *hostedGuest) (*diskStore, bool) {
	if m.Hosted != nil {
		if root, err := m.Hosted(); err == nil {
			return &diskStore{root: filepath.Join(root, guest.ID), free: m.freeMB}, true
		}
	}
	writeError(w, http.StatusServiceUnavailable, "Le disque de sauvegarde de ce Pi n'est pas branché.")
	return nil, false
}

// PeerPairRequest : le code de jumelage saisi sur l'autre Pi.
type PeerPairRequest struct {
	Code string `json:"code"`
}

// PeerPairReply : l'espace réservé par le Pi qui accueille.
type PeerPairReply struct {
	QuotaMB int64 `json:"quota_mb"`
}

// PeerList : sauvegardes gardées pour un proche.
type PeerList struct {
	Complete []string `json:"complete"`
	Partial  []string `json:"partial"`
	UsedMB   int64    `json:"used_mb"`
	QuotaMB  int64    `json:"quota_mb"`
}

func (m *Manager) peerPair(w http.ResponseWriter, r *http.Request) {
	body, key, _, ok := m.peerRequest(w, r, true)
	if !ok {
		return
	}
	var request PeerPairRequest
	if json.Unmarshal(body, &request) != nil {
		writeError(w, http.StatusBadRequest, "Demande illisible.")
		return
	}
	hostingMu.Lock()
	defer hostingMu.Unlock()
	state := m.hosting()
	p := state.Pairing
	if p == nil || !m.now().Before(p.Expires) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Code de jumelage incorrect ou périmé.", "code": "code"})
		return
	}
	if subtle.ConstantTimeCompare([]byte(hashCode(request.Code)), []byte(p.Hash)) != 1 {
		p.Failures++
		if p.Failures >= 5 {
			state.Pairing = nil
		}
		m.saveHosting(state)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Code de jumelage incorrect ou périmé.", "code": "code"})
		return
	}
	state.Pairing = nil
	guest := hostedGuest{ID: guestID(key), Key: key, Name: p.Name, QuotaMB: p.QuotaMB, Paired: m.now().UTC(), LastSeen: m.now().UTC()}
	if i := slices.IndexFunc(state.Guests, func(g hostedGuest) bool { return g.Key == key }); i >= 0 {
		state.Guests[i] = guest
	} else {
		state.Guests = append(state.Guests, guest)
	}
	if err := m.saveHosting(state); err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, PeerPairReply{QuotaMB: guest.QuotaMB})
}

func (m *Manager) peerList(w http.ResponseWriter, r *http.Request) {
	_, _, guest, ok := m.peerRequest(w, r, false)
	if !ok {
		return
	}
	st, ok := m.guestStore(w, guest)
	if !ok {
		return
	}
	complete, partial, err := st.list(r.Context())
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, PeerList{Complete: nonNil(complete), Partial: nonNil(partial), UsedMB: dirSize(st.root) >> 20, QuotaMB: guest.QuotaMB})
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func (m *Manager) peerPut(w http.ResponseWriter, r *http.Request) {
	body, _, guest, ok := m.peerRequest(w, r, false)
	if !ok {
		return
	}
	st, ok := m.guestStore(w, guest)
	if !ok {
		return
	}
	id, file := r.PathValue("id"), r.PathValue("file")
	offset, err := strconv.ParseInt(r.PathValue("offset"), 10, 64)
	if err != nil || offset < 0 || !idPattern.MatchString(id) || !validFile(file) || file == manifestName {
		writeError(w, http.StatusBadRequest, "Demande illisible.")
		return
	}
	if dirSize(st.root)+int64(len(body)) > guest.QuotaMB<<20 {
		writeJSON(w, http.StatusInsufficientStorage, map[string]string{"error": "L'espace réservé est plein.", "code": "full"})
		return
	}
	path := filepath.Join(st.root, id+partialSuffix, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		writeFailure(w, err)
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		writeFailure(w, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeFailure(w, err)
		return
	}
	if info.Size() != offset {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Morceau inattendu.", "code": "offset"})
		return
	}
	if _, err := f.WriteAt(body, offset); err != nil {
		writeFailure(w, err)
		return
	}
	if err := f.Sync(); err != nil {
		writeFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) peerGet(w http.ResponseWriter, r *http.Request) {
	_, _, guest, ok := m.peerRequest(w, r, false)
	if !ok {
		return
	}
	st, ok := m.guestStore(w, guest)
	if !ok {
		return
	}
	f, err := st.open(r.Context(), r.PathValue("id"), r.PathValue("file"))
	if err != nil {
		writeFailure(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	io.Copy(w, f)
}

func (m *Manager) peerCommit(w http.ResponseWriter, r *http.Request) {
	body, _, guest, ok := m.peerRequest(w, r, false)
	if !ok {
		return
	}
	st, ok := m.guestStore(w, guest)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !idPattern.MatchString(id) || !exists(filepath.Join(st.root, id+partialSuffix)) {
		writeError(w, http.StatusBadRequest, "Demande illisible.")
		return
	}
	if err := st.commit(r.Context(), id, body); err != nil {
		writeFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) peerDelete(w http.ResponseWriter, r *http.Request) {
	_, _, guest, ok := m.peerRequest(w, r, false)
	if !ok {
		return
	}
	st, ok := m.guestStore(w, guest)
	if !ok {
		return
	}
	if err := st.remove(r.Context(), r.PathValue("id")); err != nil {
		writeFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Proche : l'envoi de nos sauvegardes chez un autre Pi.

type peerSettings struct {
	Address string `json:"address"`
	QuotaMB int64  `json:"quota_mb"`
}

func (m *Manager) peerSettings() (peerSettings, bool) {
	var settings peerSettings
	data, err := os.ReadFile(m.peerPath())
	if err != nil || json.Unmarshal(data, &settings) != nil || settings.Address == "" {
		return peerSettings{}, false
	}
	return settings, true
}

// peerAddress : l'adresse du Pi du proche, https://<nom> sans chemin.
func peerAddress(address string) (string, error) {
	address = strings.TrimRight(strings.TrimSpace(address), "/")
	if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		return "", fmt.Errorf("%w : saisissez l'adresse internet du Pi de votre proche, par exemple paul.pi-appliance.com", ErrInvalid)
	}
	return "https://" + u.Host, nil
}

// PairPeer jumelle ce Pi avec celui d'un proche, par le code affiché chez lui.
func (m *Manager) PairPeer(ctx context.Context, address, code string) error {
	if m.PeerKey == nil {
		return fmt.Errorf("%w : ce Pi n'a pas de clé pour signer ses demandes", ErrInvalid)
	}
	address, err := peerAddress(address)
	if err != nil {
		return err
	}
	if strings.TrimSpace(code) == "" {
		return fmt.Errorf("%w : saisissez le code affiché sur le Pi de votre proche", ErrInvalid)
	}
	peer := m.newPeer(address)
	body, _ := json.Marshal(PeerPairRequest{Code: code})
	data, err := peer.send(ctx, http.MethodPost, "/pair", body, http.StatusOK)
	if err != nil {
		return err
	}
	var reply PeerPairReply
	if err := json.Unmarshal(data, &reply); err != nil {
		return &ErrPeer{Detail: "réponse illisible"}
	}
	settings, _ := json.MarshalIndent(peerSettings{Address: address, QuotaMB: reply.QuotaMB}, "", "  ")
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	defer m.forget()
	return writeFile(m.peerPath(), append(settings, '\n'), 0o600)
}

// RemovePeer arrête l'envoi chez le proche ; ce qui y est gardé y reste
// jusqu'à ce que son administrateur l'efface.
func (m *Manager) RemovePeer() error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	defer m.forget()
	err := os.Remove(m.peerPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (m *Manager) peerStore() store {
	settings, ok := m.peerSettings()
	if !ok || m.PeerKey == nil {
		return nil
	}
	return m.newPeer(settings.Address)
}

func (m *Manager) newPeer(address string) *peerStore {
	client := m.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	return &peerStore{base: address + PeerPath, key: m.PeerKey, http: client, now: m.now}
}

// ErrPeer : l'autre pi-appliance refuse ou ne répond pas.
type ErrPeer struct {
	Status int
	Code   string
	Detail string
}

func (e *ErrPeer) Error() string {
	switch {
	case e.Code == "code":
		return ErrPeerCode.Error()
	case e.Code == "full":
		return ErrPeerFull.Error()
	case e.Code == "unknown":
		return ErrPeerUnknown.Error()
	case e.Status == 0:
		return "le pi-appliance de votre proche ne répond pas : " + e.Detail
	}
	return fmt.Sprintf("le pi-appliance de votre proche a répondu %d", e.Status)
}

type peerStore struct {
	base string
	key  PeerSigner
	http *http.Client
	now  func() time.Time
}

func (p *peerStore) place() string         { return PlacePeer }
func (p *peerStore) sealed() bool          { return true }
func (p *peerStore) freeMB() (int64, bool) { return 0, false }

func (p *peerStore) do(ctx context.Context, method, path string, body []byte, want int) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, p.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Pi-Appliance", "1")
	request.Header.Set("Content-Type", "application/octet-stream")
	p.key.Sign(request, body, p.now())
	response, err := p.http.Do(request)
	if err != nil {
		return nil, &ErrPeer{Detail: err.Error()}
	}
	if response.StatusCode != want {
		defer response.Body.Close()
		var refused struct {
			Code string `json:"code"`
		}
		json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&refused)
		return nil, &ErrPeer{Status: response.StatusCode, Code: refused.Code}
	}
	return response, nil
}

func (p *peerStore) send(ctx context.Context, method, path string, body []byte, want int) ([]byte, error) {
	response, err := p.do(ctx, method, path, body, want)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return io.ReadAll(io.LimitReader(response.Body, 16<<20))
}

func (p *peerStore) list(ctx context.Context) (complete, partial []string, err error) {
	data, err := p.send(ctx, http.MethodGet, "/snapshots", nil, http.StatusOK)
	if err != nil {
		return nil, nil, err
	}
	var list PeerList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, nil, &ErrPeer{Detail: "réponse illisible"}
	}
	return list.Complete, list.Partial, nil
}

func (p *peerStore) create(ctx context.Context, id, file string) (fileWriter, error) {
	if !idPattern.MatchString(id) || !validFile(file) || file == manifestName {
		return nil, ErrInvalid
	}
	return &peerWriter{ctx: ctx, p: p, id: id, file: file, buf: make([]byte, 0, onlinePartSize)}, nil
}

// peerWriter envoie un fichier par morceaux, chacun signé, à son décalage.
type peerWriter struct {
	ctx     context.Context
	p       *peerStore
	id      string
	file    string
	buf     []byte
	offset  int64
	started bool
	err     error
}

func (w *peerWriter) Write(b []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	written := 0
	for len(b) > 0 {
		if len(w.buf) == cap(w.buf) {
			if w.err = w.flush(); w.err != nil {
				return written, w.err
			}
		}
		n := copy(w.buf[len(w.buf):cap(w.buf)], b)
		w.buf = w.buf[:len(w.buf)+n]
		b = b[n:]
		written += n
	}
	return written, nil
}

func (w *peerWriter) flush() error {
	path := fmt.Sprintf("/files/%s/%d/%s", w.id, w.offset, w.file)
	if _, err := w.p.send(w.ctx, http.MethodPut, path, w.buf, http.StatusNoContent); err != nil {
		return err
	}
	w.offset += int64(len(w.buf))
	w.buf = w.buf[:0]
	w.started = true
	return nil
}

func (w *peerWriter) Close() error {
	if w.err != nil {
		return w.err
	}
	if len(w.buf) > 0 || !w.started {
		return w.flush()
	}
	return nil
}

// Abort : le brouillon est effacé chez le proche avec la sauvegarde (remove).
func (w *peerWriter) Abort() {}

func (p *peerStore) commit(ctx context.Context, id string, manifest []byte) error {
	_, err := p.send(ctx, http.MethodPost, "/commit/"+id, manifest, http.StatusNoContent)
	return err
}

func (p *peerStore) open(ctx context.Context, id, file string) (io.ReadCloser, error) {
	if !idPattern.MatchString(id) || !validFile(file) {
		return nil, ErrNotFound
	}
	response, err := p.do(ctx, http.MethodGet, "/files/"+id+"/"+file, nil, http.StatusOK)
	var refused *ErrPeer
	if errors.As(err, &refused) && refused.Status == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

func (p *peerStore) remove(ctx context.Context, id string) error {
	if !idPattern.MatchString(id) {
		return ErrInvalid
	}
	_, err := p.send(ctx, http.MethodDelete, "/snapshots/"+id, nil, http.StatusNoContent)
	return err
}

// API de la console (administrateur) et des proches (demandes signées).

// HostingCodeRequest : accueillir un proche, avec cet espace réservé.
type HostingCodeRequest struct {
	Name    string `json:"name"`
	QuotaMB int64  `json:"quota_mb"`
}

// HostingCodeReply : le code à saisir sur le Pi du proche.
type HostingCodeReply struct {
	Code    string    `json:"code"`
	Expires time.Time `json:"expires"`
}

// PeerRequest : jumeler ce Pi avec celui d'un proche.
type PeerRequest struct {
	Address string `json:"address"`
	Code    string `json:"code"`
}

func (m *Manager) peerRoutes(mux *http.ServeMux, prefix string, admin func(http.HandlerFunc) http.HandlerFunc) {
	base := prefix + "/api/backups"
	mux.HandleFunc("POST "+base+"/hosting/code", admin(func(w http.ResponseWriter, r *http.Request) {
		var request HostingCodeRequest
		if !decode(w, r, &request) {
			return
		}
		code, expires, err := m.HostingCode(request.Name, request.QuotaMB)
		if err != nil {
			writeFailure(w, err)
			return
		}
		writeJSON(w, http.StatusOK, HostingCodeReply{Code: code, Expires: expires})
	}))
	mux.HandleFunc("PUT "+base+"/hosting/{guest}", admin(func(w http.ResponseWriter, r *http.Request) {
		var request HostingCodeRequest
		if !decode(w, r, &request) {
			return
		}
		if err := m.SetGuestQuota(r.PathValue("guest"), request.QuotaMB); err != nil {
			writeFailure(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("DELETE "+base+"/hosting/{guest}", admin(func(w http.ResponseWriter, r *http.Request) {
		if err := m.RemoveGuest(r.PathValue("guest")); err != nil {
			writeFailure(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("PUT "+base+"/peer", admin(func(w http.ResponseWriter, r *http.Request) {
		var request PeerRequest
		if !decode(w, r, &request) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		if err := m.PairPeer(ctx, request.Address, request.Code); err != nil {
			writeFailure(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("DELETE "+base+"/peer", admin(func(w http.ResponseWriter, r *http.Request) {
		if err := m.RemovePeer(); err != nil {
			writeFailure(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	peer := prefix + "/api/peer"
	mux.HandleFunc("POST "+peer+"/pair", m.peerPair)
	mux.HandleFunc("GET "+peer+"/snapshots", m.peerList)
	mux.HandleFunc("PUT "+peer+"/files/{id}/{offset}/{file...}", m.peerPut)
	mux.HandleFunc("GET "+peer+"/files/{id}/{file...}", m.peerGet)
	mux.HandleFunc("POST "+peer+"/commit/{id}", m.peerCommit)
	mux.HandleFunc("DELETE "+peer+"/snapshots/{id}", m.peerDelete)
}
