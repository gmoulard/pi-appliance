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

package console

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/mail"
)

// Envoi des e-mails par le serveur SMTP réglé sur le Pi (spécification 10.2,
// TODO-3.16), double authentification par un code envoyé par e-mail et
// vérification de l'adresse e-mail (spécification 8.1, 5.1, TODO-3.15,
// TODO-2.19), proposition de la double authentification à la création d'un
// compte (TODO-3.14) :
//
//	GET    api/mail                 réglage sans le mot de passe (administrateur)
//	PUT    api/mail                 {host, port, security, username, password, from}
//	                                (+ account_password, code depuis internet)
//	DELETE api/mail                 efface le réglage ({account_password, code} depuis internet)
//	POST   api/mail/test            {to, lang, settings} e-mail d'essai (à soi par
//	                                défaut), avec le réglage tapé (settings) ou
//	                                celui enregistré ; parti, le réglage est
//	                                enregistré et noté vérifié
//	POST   api/login/email-code     {name, password, lang} code de connexion par e-mail
//	POST   api/account/email/send   {lang} code de vérification de son adresse
//	POST   api/account/email/confirm {code, enable} adresse vérifiée, code par e-mail activé
//	POST   api/account/email/disable {password} code par e-mail désactivé
//	POST   api/account/two-factor/later  « Plus tard » à la proposition

// MailStatus : réponse de GET api/mail.
type MailStatus struct {
	mail.SMTPStatus
	// EmailCodeAccounts : comptes qui reçoivent leur code de connexion par
	// e-mail : sans envoi réglé, ils passeront par l'application ou un code de secours.
	EmailCodeAccounts int `json:"email_code_accounts"`
	// ProjectService : sans réglage, les liens partent par le service du projet.
	ProjectService bool `json:"project_service"`
}

// MailTestRequest : POST api/mail/test ; Settings absent : le réglage enregistré.
type MailTestRequest struct {
	To       string             `json:"to"`
	Lang     string             `json:"lang"`
	Settings *mail.SMTPSettings `json:"settings"`
	MailConfirm
}

// MailConfirm : depuis internet, une action sensible (envoi des e-mails,
// confirmPaths…) demande le mot de passe du compte et son code de double
// authentification, dans la demande ou dans les en-têtes X-Pi-Confirm-Password
// et X-Pi-Confirm-Code (fenêtre de confirmation de la console).
type MailConfirm struct {
	AccountPassword string `json:"account_password"`
	Code            string `json:"code"`
}

// MailSaveRequest : PUT api/mail.
type MailSaveRequest struct {
	mail.SMTPSettings
	MailConfirm
}

// NE PAS relâcher : depuis internet, une session volée ne doit pas pouvoir
// détourner les codes de connexion, ouvrir la box ou effacer le Pi. Il faut un
// compte avec double authentification, son mot de passe et un code frais.
// NE PAS journaliser les en-têtes de confirmation.
func (c *console) confirmFromInternet(w http.ResponseWriter, r *http.Request, confirm MailConfirm) bool {
	if !fromInternet(r) {
		return true
	}
	if confirm.AccountPassword == "" {
		confirm = MailConfirm{AccountPassword: r.Header.Get("X-Pi-Confirm-Password"), Code: r.Header.Get("X-Pi-Confirm-Code")}
	}
	account, ok := c.account(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Connectez-vous.")
		return false
	}
	if !account.HasTwoFactor() {
		writeError(w, http.StatusForbidden, "Depuis internet, cette action demande la double authentification : activez-la d'abord dans « Mon compte » (application d'authentification), ou faites-le depuis la maison.")
		return false
	}
	if confirm.AccountPassword == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Depuis internet, confirmez avec le mot de passe de votre compte et votre code de double authentification.", "confirm_needed": true})
		return false
	}
	token, _, err := c.Accounts.Login(account.Name, confirm.AccountPassword, confirm.Code, clientIP(r))
	if err != nil {
		writeAuthError(w, err)
		return false
	}
	c.Accounts.Logout(token)
	log.Printf("action confirmée depuis internet par %s : %s %s", account.Name, r.Method, r.URL.Path)
	return true
}

// MailTestReply : où l'e-mail d'essai est parti, et le réglage tel qu'il est
// désormais (enregistré et vérifié).
type MailTestReply struct {
	Sent   bool       `json:"sent"`
	Email  string     `json:"email"`
	Status MailStatus `json:"status"`
}

// EmailCodeRequest : POST api/login/email-code.
type EmailCodeRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Lang     string `json:"lang"`
}

type LangRequest struct {
	Lang string `json:"lang"`
}

// EmailConfirmRequest : Enable absent ou vrai : le code par e-mail devient la
// double authentification du compte (choix par défaut).
type EmailConfirmRequest struct {
	Code   string `json:"code"`
	Enable *bool  `json:"enable"`
}

// EmailCodeReply : où le code est parti, l'adresse à moitié masquée.
type EmailCodeReply struct {
	Sent  bool   `json:"sent"`
	Email string `json:"email"`
}

func (c *console) mailRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+BasePath+"/api/mail", c.adminOnly(c.mailStatus))
	mux.HandleFunc("PUT "+BasePath+"/api/mail", c.adminOnly(c.mailSave))
	mux.HandleFunc("DELETE "+BasePath+"/api/mail", c.adminOnly(c.mailForget))
	mux.HandleFunc("POST "+BasePath+"/api/mail/test", c.adminOnly(c.mailTest))
	mux.HandleFunc("POST "+BasePath+"/api/login/email-code", c.loginEmailCode)
	mux.HandleFunc("POST "+BasePath+"/api/account/email/send", c.ownAccount(c.verifyEmailSend))
	mux.HandleFunc("POST "+BasePath+"/api/account/email/confirm", c.ownAccount(c.verifyEmailConfirm))
	mux.HandleFunc("POST "+BasePath+"/api/account/email/disable", c.ownAccount(c.emailCodeDisable))
	mux.HandleFunc("POST "+BasePath+"/api/account/two-factor/later", c.ownAccount(c.twoFactorLater))
}

// mailer : l'envoi des liens (invitations, mot de passe oublié) : le serveur
// SMTP réglé sur le Pi, sinon le service du projet ; nil : pas d'e-mail.
func (c *console) mailer() auth.Mailer {
	if c.SMTP.Configured() {
		return c.SMTP
	}
	return c.Mailer
}

// codeMailer : l'envoi des codes à 6 chiffres, seulement par le SMTP du Pi (ou
// un expéditeur qui sait envoyer des codes) ; nil : pas de code par e-mail.
func (c *console) codeMailer() auth.CodeMailer {
	if c.SMTP.Configured() {
		return c.SMTP
	}
	if codes, ok := c.Mailer.(auth.CodeMailer); ok {
		return codes
	}
	return nil
}

func (c *console) mailStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, c.mailState())
}

func (c *console) mailState() MailStatus {
	status := MailStatus{ProjectService: c.Mailer != nil}
	if c.SMTP != nil {
		status.SMTPStatus = c.SMTP.Status()
	}
	for _, account := range c.Accounts.List() {
		if account.EmailCode {
			status.EmailCodeAccounts++
		}
	}
	return status
}

// NE PAS renvoyer ni journaliser le mot de passe du compte e-mail.
func (c *console) mailSave(w http.ResponseWriter, r *http.Request) {
	if c.SMTP == nil {
		writeError(w, http.StatusNotImplemented, "L'envoi des e-mails ne se règle pas ici.")
		return
	}
	var request MailSaveRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if !c.confirmFromInternet(w, r, request.MailConfirm) {
		return
	}
	account, _ := c.account(r)
	if err := c.SMTP.Save(request.SMTPSettings, account.Name); err != nil {
		if errors.Is(err, mail.ErrInvalid) {
			writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), mail.ErrInvalid.Error()+" : "))
			return
		}
		log.Printf("envoi des e-mails : réglage non enregistré : %v", err)
		writeError(w, http.StatusInternalServerError, "Le réglage n'a pas pu être enregistré.")
		return
	}
	log.Printf("envoi des e-mails : réglage changé par %s", account.Name)
	writeJSON(w, http.StatusOK, c.mailState())
}

func (c *console) mailForget(w http.ResponseWriter, r *http.Request) {
	if c.SMTP == nil {
		writeError(w, http.StatusNotImplemented, "L'envoi des e-mails ne se règle pas ici.")
		return
	}
	var confirm MailConfirm
	if r.ContentLength != 0 && !decodeJSON(w, r, &confirm) {
		return
	}
	if !c.confirmFromInternet(w, r, confirm) {
		return
	}
	if err := c.SMTP.Forget(); err != nil {
		log.Printf("envoi des e-mails : réglage non effacé : %v", err)
		writeError(w, http.StatusInternalServerError, "Le réglage n'a pas pu être effacé.")
		return
	}
	account, _ := c.account(r)
	log.Printf("envoi des e-mails : réglage effacé par %s", account.Name)
	writeJSON(w, http.StatusOK, c.mailState())
}

// NE PAS renvoyer ni journaliser le mot de passe du compte e-mail.
func (c *console) mailTest(w http.ResponseWriter, r *http.Request) {
	var request MailTestRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if c.SMTP == nil {
		writeError(w, http.StatusNotImplemented, "L'envoi des e-mails ne se règle pas ici.")
		return
	}
	// Un réglage tapé est enregistré s'il marche : même confirmation que PUT.
	if request.Settings != nil && !c.confirmFromInternet(w, r, request.MailConfirm) {
		return
	}
	if request.Settings == nil && !c.SMTP.Configured() {
		writeError(w, http.StatusConflict, "Entrez d'abord les réglages de votre boîte e-mail.")
		return
	}
	account, _ := c.account(r)
	to := strings.TrimSpace(request.To)
	if to == "" {
		to = account.Email
	}
	if to == "" {
		writeError(w, http.StatusBadRequest, "Entrez l'adresse qui doit recevoir l'e-mail d'essai.")
		return
	}
	name := account.Name
	if name == CLIAccount {
		name = "pia"
	}
	lang := requestLanguage(r, request.Lang)
	var err error
	if request.Settings != nil {
		err = c.SMTP.TestSettings(*request.Settings, to, name, lang, account.Name)
	} else {
		err = c.SMTP.Test(to, name, lang)
	}
	if err != nil {
		var invalid mail.SettingsError
		if errors.As(err, &invalid) {
			writeError(w, http.StatusBadRequest, invalid.Error())
			return
		}
		writeSendError(w, err)
		return
	}
	log.Printf("envoi des e-mails : e-mail d'essai parti, réglage vérifié par %s", account.Name)
	writeJSON(w, http.StatusOK, MailTestReply{Sent: true, Email: to, Status: c.mailState()})
}

// writeSendError : un e-mail qui n'est pas parti, avec ce qu'il faut vérifier.
func writeSendError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, mail.ErrTooMany):
		status = http.StatusTooManyRequests
	case errors.Is(err, mail.ErrInvalid):
		status = http.StatusBadRequest
	}
	writeError(w, status, sendErrorText(err))
}

// sendErrorText : pourquoi un e-mail n'est pas parti, en langage courant
// (journalisé sans le contenu du message).
func sendErrorText(err error) string {
	var refused mail.SMTPError
	switch {
	case errors.As(err, &refused):
		log.Printf("envoi d'un e-mail : %s (%v)", refused.Advice, refused.Err)
		return "L'e-mail n'est pas parti. " + refused.Advice
	case errors.Is(err, mail.ErrTooMany):
		return "Le Pi a envoyé beaucoup d'e-mails dans l'heure : réessayez plus tard."
	case errors.Is(err, mail.ErrInvalid):
		return "Cette adresse e-mail ne semble pas valide."
	default:
		log.Printf("envoi d'un e-mail : %v", err)
		return "L'e-mail n'est pas parti : vérifiez le réglage de l'envoi des e-mails."
	}
}

// loginEmailCode : à la connexion, mot de passe juste, envoie le code par e-mail.
func (c *console) loginEmailCode(w http.ResponseWriter, r *http.Request) {
	var request EmailCodeRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	mailer := c.codeMailer()
	if mailer == nil {
		writeError(w, http.StatusServiceUnavailable, "Le Pi ne peut pas envoyer d'e-mail en ce moment : entrez le code de votre application d'authentification ou un code de secours.")
		return
	}
	account, code, err := c.Accounts.LoginEmailCode(request.Name, request.Password, clientIP(r))
	if err != nil {
		writeCodeError(w, err)
		return
	}
	lang := requestLanguage(r, firstNonEmpty(request.Lang, account.Lang))
	if err := mailer.SendCode(account.Public(), auth.CodeLogin, code, lang); err != nil {
		c.Accounts.ForgetEmailCode(auth.CodeLogin, account.Name)
		writeSendError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, EmailCodeReply{Sent: true, Email: maskEmail(account.Email)})
}

func (c *console) verifyEmailSend(w http.ResponseWriter, r *http.Request, account auth.Account) {
	var request LangRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	mailer := c.codeMailer()
	if mailer == nil {
		writeError(w, http.StatusServiceUnavailable, "Le Pi ne sait pas encore envoyer d'e-mail : un administrateur doit d'abord régler « Envoi des e-mails ».")
		return
	}
	account, code, err := c.Accounts.VerifyEmailCode(account.Name, clientIP(r))
	if err != nil {
		writeCodeError(w, err)
		return
	}
	if err := mailer.SendCode(account.Public(), auth.CodeVerify, code, requestLanguage(r, firstNonEmpty(request.Lang, account.Lang))); err != nil {
		c.Accounts.ForgetEmailCode(auth.CodeVerify, account.Name)
		writeSendError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, EmailCodeReply{Sent: true, Email: account.Email})
}

func (c *console) verifyEmailConfirm(w http.ResponseWriter, r *http.Request, account auth.Account) {
	var request EmailConfirmRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	enable := request.Enable == nil || *request.Enable
	codes, account, err := c.Accounts.ConfirmEmail(account.Name, request.Code, enable)
	if err != nil {
		writeCodeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes, "account": account.Public()})
}

func (c *console) emailCodeDisable(w http.ResponseWriter, r *http.Request, account auth.Account) {
	var request PasswordRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	account, err := c.Accounts.DisableEmailCode(account.Name, request.Password)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, account.Public())
}

func (c *console) twoFactorLater(w http.ResponseWriter, r *http.Request, account auth.Account) {
	account, err := c.Accounts.DismissTwoFactorOffer(account.Name)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, account.Public())
}

func writeCodeError(w http.ResponseWriter, err error) {
	var resend auth.ResendError
	switch {
	case errors.As(err, &resend):
		seconds := int((resend.Wait + time.Second - 1) / time.Second)
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "Un code vient d'être envoyé : regardez dans votre boîte e-mail (et les courriers indésirables). Vous pourrez en demander un autre dans " + waitText(resend.Wait) + ".", "wait": seconds})
	case errors.Is(err, auth.ErrNoEmailCode):
		writeError(w, http.StatusBadRequest, "Ce compte ne reçoit pas de code par e-mail : entrez le code de votre application d'authentification ou un code de secours.")
	case errors.Is(err, auth.ErrEmailCode):
		writeError(w, http.StatusUnauthorized, "Ce code ne marche pas : il est faux, a plus de 10 minutes ou a déjà servi. Demandez-en un nouveau.")
	default:
		writeAuthError(w, err)
	}
}

// maskEmail : marie.dupont@example.org → m•••@example.org, pour dire où le
// code est parti sans montrer l'adresse à qui ne connaît que le mot de passe.
func maskEmail(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" {
		return "•••"
	}
	return string([]rune(local)[0]) + "•••@" + domain
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
