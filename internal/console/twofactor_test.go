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

package console

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/mail"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

// Double authentification proposée et contrôlée à chaque création de compte
// (TODO-3.14), code par e-mail (TODO-3.15) et réglage de l'envoi des e-mails
// (TODO-3.16).

// codeMailer : faux expéditeur qui sait aussi envoyer des codes (comme le SMTP du Pi).
type codeMailer struct {
	fakeMailer
	mu    sync.Mutex
	codes map[string]string
	fail  bool
}

func (m *codeMailer) SendCode(account auth.Public, purpose auth.CodePurpose, code, lang string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return mail.SMTPError{Advice: "Le serveur d'envoi ne répond pas."}
	}
	if m.codes == nil {
		m.codes = map[string]string{}
	}
	m.codes[string(purpose)+":"+account.Name] = code
	return nil
}

func (m *codeMailer) code(t *testing.T, purpose auth.CodePurpose, name string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	code, ok := m.codes[string(purpose)+":"+name]
	if !ok {
		t.Fatalf("aucun code %s envoyé à %s", purpose, name)
	}
	delete(m.codes, string(purpose)+":"+name)
	return code
}

type twoFactorFixture struct {
	fixture
	mailer *codeMailer
	smtp   *mail.SMTP
	dir    string
}

// newTwoFactorFixture : mailer nil, le Pi ne sait pas envoyer de code.
func newTwoFactorFixture(t *testing.T, withMailer bool) twoFactorFixture {
	t.Helper()
	previous := sendAsync
	sendAsync = func(send func()) { send() }
	t.Cleanup(func() { sendAsync = previous })
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	accounts, err := auth.Open(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	smtp, err := mail.OpenSMTP(filepath.Join(dir, "smtp.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := &services.Manager{Dir: dir, Domain: "maison.example.org", Runner: okRunner{}}
	config := Config{Version: "1.2.3", Catalog: catalog, Services: manager, Accounts: accounts, SMTP: smtp, CLIToken: "jeton",
		Hostname: func() (string, error) { return "pi-appliance-1234", nil }}
	f := twoFactorFixture{smtp: smtp, dir: dir}
	if withMailer {
		f.mailer = &codeMailer{}
		config.Mailer = f.mailer
	}
	f.fixture = fixture{handler: Handler(config), accounts: accounts, manager: manager}
	return f
}

func (f twoFactorFixture) session(t *testing.T, cookie *http.Cookie) SessionState {
	t.Helper()
	return decode[SessionState](t, f.do(http.MethodGet, "/pi-appliance/api/session", "", "", cookie))
}

// acceptInvite : la personne invitée choisit son mot de passe ; renvoie sa session.
func (f twoFactorFixture) acceptInvite(t *testing.T, token string) *http.Cookie {
	t.Helper()
	accept := f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+token+`", "password": "douze car ok"}`, nil)
	if accept.Code != http.StatusOK {
		t.Fatalf("invitation acceptée : %d %s", accept.Code, accept.Body.String())
	}
	return sessionCookie(t, accept)
}

// enableEmailCode : vérifie l'adresse du compte connecté et active le code par e-mail.
func (f twoFactorFixture) enableEmailCode(t *testing.T, cookie *http.Cookie, name string) []string {
	t.Helper()
	if send := f.do(http.MethodPost, "/pi-appliance/api/account/email/send", "application/json", `{"lang": "fr"}`, cookie); send.Code != http.StatusAccepted {
		t.Fatalf("envoi du code de vérification : %d %s", send.Code, send.Body.String())
	}
	code := f.mailer.code(t, auth.CodeVerify, name)
	confirm := f.do(http.MethodPost, "/pi-appliance/api/account/email/confirm", "application/json", `{"code": "`+code+`"}`, cookie)
	if confirm.Code != http.StatusOK {
		t.Fatalf("vérification : %d %s", confirm.Code, confirm.Body.String())
	}
	reply := decode[struct {
		RecoveryCodes []string    `json:"recovery_codes"`
		Account       auth.Public `json:"account"`
	}](t, confirm)
	if !reply.Account.EmailVerified || !reply.Account.EmailCode || !reply.Account.TwoFactor {
		t.Fatalf("compte après vérification : %+v", reply.Account)
	}
	return reply.RecoveryCodes
}

// restricted : ce qu'un compte dont la double authentification est exigée
// mais pas activée ne peut pas faire, et ce qu'il peut faire.
func (f twoFactorFixture) restricted(t *testing.T, cookie *http.Cookie, admin bool) {
	t.Helper()
	paths := [][2]string{
		{http.MethodGet, "/pi-appliance/api/services"},
		{http.MethodGet, "/pi-appliance/api/catalog"},
		{http.MethodPost, "http://maison.example.org/pi-appliance/api/auth/return"},
		{http.MethodGet, "/pi-appliance/api/services/cloud/shares"},
	}
	if admin {
		paths = append(paths, [2]string{http.MethodGet, "/pi-appliance/api/accounts"}, [2]string{http.MethodGet, "/pi-appliance/api/mail"},
			[2]string{http.MethodPost, "/pi-appliance/api/factory-reset"}, [2]string{http.MethodGet, "/pi-appliance/api/diagnostic"})
	}
	for _, request := range paths {
		body := `{"return": "http://cloud.maison.example.org/"}`
		if strings.HasSuffix(request[1], "factory-reset") {
			body = `{"confirm": true, "password": "douze car ok"}`
		}
		if response := f.do(request[0], request[1], "application/json", body, cookie); response.Code != http.StatusForbidden && response.Code != http.StatusNotFound {
			t.Errorf("%s %s avant l'activation exigée : code %d %s", request[0], request[1], response.Code, response.Body.String())
		} else if response.Code == http.StatusNotFound && !strings.Contains(request[1], "diagnostic") {
			t.Errorf("%s %s : 404", request[0], request[1])
		}
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/account/devices", "", "", cookie); response.Code != http.StatusOK {
		t.Errorf("« Mon compte » reste permis : %d", response.Code)
	}
	state := f.session(t, cookie)
	if state.Account == nil || !state.Account.TOTPRequired || state.Account.TwoFactor {
		t.Errorf("session : %+v", state.Account)
	}
}

// A. Premier administrateur (assistant de premier démarrage) : la double
// authentification est proposée, et « Plus tard » est retenu.
func TestSetupOffersTwoFactor(t *testing.T) {
	f := newTwoFactorFixture(t, true)
	admin := f.admin(t)
	state := f.session(t, admin)
	if state.Account == nil || !state.Account.TwoFactorOffer || !state.EmailCodes {
		t.Fatalf("après la création de l'administrateur : %+v %+v", state, state.Account)
	}
	later := f.do(http.MethodPost, "/pi-appliance/api/account/two-factor/later", "", "", admin)
	if later.Code != http.StatusOK || decode[auth.Public](t, later).TwoFactorOffer {
		t.Fatalf("« Plus tard » : %d %s", later.Code, later.Body.String())
	}
	if f.session(t, admin).Account.TwoFactorOffer {
		t.Fatal("proposée de nouveau après « Plus tard »")
	}
}

// Activer une méthode répond aussi à la proposition.
func TestOfferEndsWithEmailCode(t *testing.T) {
	f := newTwoFactorFixture(t, true)
	admin := f.admin(t)
	f.enableEmailCode(t, admin, "marie")
	if account := f.session(t, admin).Account; account.TwoFactorOffer || !account.TwoFactor {
		t.Fatalf("après l'activation : %+v", account)
	}
}

// A. Invitation acceptée par lien : proposée ; exigée : appliquée dès la
// première session, jusqu'à l'activation par l'une ou l'autre méthode.
func TestInvitationOffersAndEnforcesTwoFactor(t *testing.T) {
	f := newTwoFactorFixture(t, true)
	admin := f.admin(t)
	free := f.acceptInvite(t, f.invite2(t, admin, "paul"))
	if state := f.session(t, free); !state.Account.TwoFactorOffer || state.Account.TOTPRequired {
		t.Fatalf("invitation sans exigence : %+v", state.Account)
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", free); response.Code != http.StatusOK {
		t.Fatalf("facultative : %d", response.Code)
	}
	response := f.do(http.MethodPost, "http://maison.example.org/pi-appliance/api/accounts", "application/json", `{"name": "lea", "email": "lea@example.org", "role": "admin", "totp_required": true}`, admin)
	reply := decode[InvitationReply](t, response)
	if !reply.Account.TOTPRequired {
		t.Fatalf("exigence absente de la réponse : %+v", reply.Account)
	}
	// Les e-mails partent par le faux expéditeur : le lien est dans l'invitation.
	sent := f.mailer.take()
	if len(sent) != 1 || sent[0].kind != "invite" {
		t.Fatalf("invitation : %+v", sent)
	}
	token := sent[0].link[strings.Index(sent[0].link, "#invite=")+len("#invite="):]
	lea := f.acceptInvite(t, token)
	f.restricted(t, lea, true)
	if account := f.session(t, lea).Account; !account.TwoFactorOffer {
		t.Fatalf("proposition : %+v", account)
	}
	// « Plus tard » ne lève pas l'exigence.
	f.do(http.MethodPost, "/pi-appliance/api/account/two-factor/later", "", "", lea)
	f.restricted(t, lea, true)
	// Code par e-mail activé : l'exigence est satisfaite.
	f.enableEmailCode(t, lea, "lea")
	for _, path := range []string{"/pi-appliance/api/services", "/pi-appliance/api/accounts", "/pi-appliance/api/mail"} {
		if response := f.do(http.MethodGet, path, "", "", lea); response.Code != http.StatusOK {
			t.Errorf("%s après l'activation : %d %s", path, response.Code, response.Body.String())
		}
	}
	// Désactiver la dernière méthode exigée : refusé.
	if response := f.do(http.MethodPost, "/pi-appliance/api/account/email/disable", "application/json", `{"password": "douze car ok"}`, lea); response.Code != http.StatusConflict {
		t.Fatalf("dernière méthode retirée : %d %s", response.Code, response.Body.String())
	}
	// Exigée après coup, sur un compte déjà connecté : appliquée tout de suite.
	if response := f.do(http.MethodPut, "/pi-appliance/api/accounts/paul", "application/json", `{"role": "user", "totp_required": true}`, admin); response.Code != http.StatusOK {
		t.Fatalf("exiger pour paul : %d %s", response.Code, response.Body.String())
	}
	f.restricted(t, free, false)
}

// A. pia user add --require-2fa : même chose par la ligne de commande et le lien d'invitation.
func TestCLIUserAddRequiresTwoFactor(t *testing.T) {
	f := newTwoFactorFixture(t, false)
	f.admin(t)
	response := f.cli(http.MethodPost, "accounts", `{"name": "paul", "email": "paul@example.org", "role": "user", "totp_required": true}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("pia user add : %d %s", response.Code, response.Body.String())
	}
	reply := decode[InvitationReply](t, response)
	if !reply.Account.TOTPRequired || reply.Sent || !strings.Contains(reply.Link, "#invite=") {
		t.Fatalf("réponse : %+v", reply)
	}
	paul := f.acceptInvite(t, reply.Link[strings.Index(reply.Link, "#invite=")+len("#invite="):])
	f.restricted(t, paul, false)
	state := f.session(t, paul)
	if !state.Account.TwoFactorOffer || state.EmailCodes {
		t.Fatalf("sans envoi d'e-mail, seule l'application est proposée : %+v", state)
	}
	// Sans envoi d'e-mail, pas de code par e-mail : l'application reste possible.
	if response := f.do(http.MethodPost, "/pi-appliance/api/account/email/send", "application/json", `{}`, paul); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("code sans envoi possible : %d", response.Code)
	}
	enableTOTP(t, f.fixture, paul)
	if response := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", paul); response.Code != http.StatusOK {
		t.Fatalf("après l'application : %d", response.Code)
	}
}

// B. Connexion avec le code reçu par e-mail.
func TestLoginWithEmailCode(t *testing.T) {
	f := newTwoFactorFixture(t, true)
	admin := f.admin(t)
	recovery := f.enableEmailCode(t, admin, "marie")
	if len(recovery) != 10 {
		t.Fatalf("codes de secours : %v", recovery)
	}
	login := f.do(http.MethodPost, "/pi-appliance/api/login", "application/json", `{"name": "marie", "password": "`+adminPassword+`"}`, nil)
	if login.Code != http.StatusUnauthorized || !strings.Contains(login.Body.String(), `"methods":["email"]`) {
		t.Fatalf("connexion sans code : %d %s", login.Code, login.Body.String())
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/login/email-code", "application/json", `{"name": "marie", "password": "faux mot de passe"}`, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("code demandé sans le bon mot de passe : %d", response.Code)
	}
	send := f.do(http.MethodPost, "/pi-appliance/api/login/email-code", "application/json", `{"name": "marie", "password": "`+adminPassword+`", "lang": "fr"}`, nil)
	if send.Code != http.StatusAccepted || decode[EmailCodeReply](t, send).Email != "m•••@example.org" {
		t.Fatalf("envoi du code : %d %s", send.Code, send.Body.String())
	}
	// Nouvel envoi avant une minute : refusé, avec l'attente.
	if again := f.do(http.MethodPost, "/pi-appliance/api/login/email-code", "application/json", `{"name": "marie", "password": "`+adminPassword+`"}`, nil); again.Code != http.StatusTooManyRequests || again.Header().Get("Retry-After") == "" {
		t.Fatalf("nouvel envoi trop tôt : %d", again.Code)
	}
	code := f.mailer.code(t, auth.CodeLogin, "marie")
	login = f.do(http.MethodPost, "/pi-appliance/api/login", "application/json", `{"name": "marie", "password": "`+adminPassword+`", "code": "`+code+`", "lang": "en"}`, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("connexion avec le code : %d %s", login.Code, login.Body.String())
	}
	if account, _ := f.accounts.Get("marie"); account.Lang != "en" {
		t.Fatalf("langue gardée : %q", account.Lang)
	}
	// Envoi impossible : le code est oublié, le message dit quoi faire.
	f.mailer.fail = true
	f.accounts.Now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	failed := f.do(http.MethodPost, "/pi-appliance/api/login/email-code", "application/json", `{"name": "marie", "password": "`+adminPassword+`"}`, nil)
	if failed.Code != http.StatusBadGateway || !strings.Contains(failed.Body.String(), "ne répond pas") {
		t.Fatalf("envoi impossible : %d %s", failed.Code, failed.Body.String())
	}
}

// B. Invitation ou lien « mot de passe oublié » d'un compte protégé par le
// code par e-mail : le nouveau mot de passe n'ouvre pas de session sans code.
func TestNewPasswordKeepsEmailCode(t *testing.T) {
	f := newTwoFactorFixture(t, true)
	admin := f.admin(t)
	f.acceptInvite(t, f.invite2(t, admin, "paul"))
	paul := f.loginNoCode(t, "paul", "douze car ok")
	f.enableEmailCode(t, paul, "paul")
	f.do(http.MethodPost, "/pi-appliance/api/accounts/paul/invitation", "", "", admin)
	sent := f.mailer.take()
	link := sent[len(sent)-1].link
	token := link[strings.Index(link, "#invite=")+len("#invite="):]
	accept := f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+token+`", "password": "nouveau mot"}`, nil)
	if accept.Code != http.StatusOK || len(accept.Result().Cookies()) != 0 {
		t.Fatalf("nouveau lien d'un compte protégé : %d, cookies %v", accept.Code, accept.Result().Cookies())
	}
	f.forgot("pi-appliance-1234.local", `{"name": "paul"}`)
	sent = f.mailer.take()
	link = sent[len(sent)-1].link
	token = link[strings.Index(link, "#reset=")+len("#reset="):]
	reset := f.do(http.MethodPost, "/pi-appliance/api/password/reset", "application/json", `{"token": "`+token+`", "password": "encore un autre"}`, nil)
	if reset.Code != http.StatusOK || len(reset.Result().Cookies()) != 0 {
		t.Fatalf("mot de passe oublié d'un compte protégé : %d, cookies %v", reset.Code, reset.Result().Cookies())
	}
	if login := f.do(http.MethodPost, "/pi-appliance/api/login", "application/json", `{"name": "paul", "password": "encore un autre"}`, nil); login.Code != http.StatusUnauthorized {
		t.Fatalf("code toujours demandé : %d", login.Code)
	}
	// L'administrateur retire la double authentification (boîte perdue) : `pia user 2fa-off`.
	if response := f.cli(http.MethodDelete, "accounts/paul/totp", ""); response.Code != http.StatusOK || decode[auth.Public](t, response).TwoFactor {
		t.Fatalf("retrait par l'administrateur : %d %s", response.Code, response.Body.String())
	}
	if login := f.do(http.MethodPost, "/pi-appliance/api/login", "application/json", `{"name": "paul", "password": "encore un autre"}`, nil); login.Code != http.StatusOK {
		t.Fatalf("après le retrait : %d %s", login.Code, login.Body.String())
	}
}

func (f twoFactorFixture) invite2(t *testing.T, admin *http.Cookie, name string) string {
	t.Helper()
	f.do(http.MethodPost, "http://maison.example.org/pi-appliance/api/accounts", "application/json", `{"name": "`+name+`", "email": "`+name+`@example.org", "role": "user"}`, admin)
	sent := f.mailer.take()
	link := sent[len(sent)-1].link
	return link[strings.Index(link, "#invite=")+len("#invite="):]
}

func (f twoFactorFixture) loginNoCode(t *testing.T, name, password string) *http.Cookie {
	t.Helper()
	login := f.do(http.MethodPost, "/pi-appliance/api/login", "application/json", `{"name": "`+name+`", "password": "`+password+`"}`, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("connexion de %s : %d %s", name, login.Code, login.Body.String())
	}
	return sessionCookie(t, login)
}

// C. Réglage « Envoi des e-mails » : administrateur seulement, mot de passe
// jamais renvoyé, fichier en droits 600, effacé par la remise à zéro.
func TestMailSettings(t *testing.T) {
	f := newTwoFactorFixture(t, false)
	admin := f.admin(t)
	paul := f.acceptInvite(t, f.invite(t, admin, `{"name": "paul", "email": "paul@example.org", "role": "user"}`))
	if state := f.session(t, admin); state.EmailCodes || state.RecoveryEmail {
		t.Fatalf("sans réglage : %+v", state)
	}
	body := `{"host": "smtp.gmail.com", "port": 587, "security": "starttls", "username": "pi@gmail.com", "password": "abcd efgh ijkl mnop", "from": "pi@gmail.com"}`
	save := f.do(http.MethodPut, "/pi-appliance/api/mail", "application/json", body, admin)
	if save.Code != http.StatusOK || strings.Contains(save.Body.String(), "abcd") || !strings.Contains(save.Body.String(), `"password_set":true`) {
		t.Fatalf("réglage : %d %s", save.Code, save.Body.String())
	}
	if get := f.do(http.MethodGet, "/pi-appliance/api/mail", "", "", admin); strings.Contains(get.Body.String(), "abcd") || !strings.Contains(get.Body.String(), `"host":"smtp.gmail.com"`) {
		t.Fatalf("lecture : %s", get.Body.String())
	}
	info, err := os.Stat(filepath.Join(f.dir, "smtp.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("droits : %v %v", info, err)
	}
	if state := f.session(t, admin); !state.EmailCodes || !state.RecoveryEmail {
		t.Fatalf("avec réglage : %+v", state)
	}
	if bad := f.do(http.MethodPut, "/pi-appliance/api/mail", "application/json", `{"host": "smtp.gmail.com", "security": "aucune", "from": "pi@gmail.com"}`, admin); bad.Code != http.StatusBadRequest {
		t.Fatalf("sécurité « aucune » : %d", bad.Code)
	}
	// Utilisateur : refusé.
	if response := f.do(http.MethodGet, "/pi-appliance/api/mail", "", "", paul); response.Code != http.StatusForbidden {
		t.Fatalf("utilisateur : %d", response.Code)
	}
	if response := f.do(http.MethodDelete, "/pi-appliance/api/mail", "", "", admin); response.Code != http.StatusOK || f.smtp.Configured() {
		t.Fatalf("effacer : %d", response.Code)
	}
	f.do(http.MethodPut, "/pi-appliance/api/mail", "application/json", body, admin)
	reset := f.do(http.MethodPost, "/pi-appliance/api/factory-reset", "application/json", `{"confirm": true, "password": "`+adminPassword+`"}`, admin)
	if reset.Code != http.StatusOK || f.smtp.Configured() {
		t.Fatalf("remise à zéro : %d %s", reset.Code, reset.Body.String())
	}
}

// C. Depuis internet, le serveur d'envoi ne change pas (il reçoit tous les codes).
func TestMailSettingsHomeOnly(t *testing.T) {
	if !slicesContains(homeOnlyPaths, "PUT "+BasePath+"/api/mail") || !slicesContains(homeOnlyPaths, "DELETE "+BasePath+"/api/mail") {
		t.Fatal("réglage de l'envoi des e-mails ouvert depuis internet")
	}
}

func slicesContains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func TestMaskEmail(t *testing.T) {
	for in, want := range map[string]string{"marie@example.org": "m•••@example.org", "élise@exemple.fr": "é•••@exemple.fr", "bizarre": "•••"} {
		if got := maskEmail(in); got != want {
			t.Errorf("%q : %q", in, got)
		}
	}
}
