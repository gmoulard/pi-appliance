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

package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"
)

// pia user : comptes de la console (spécification 8.1), par l'API de la
// console comme l'écran « Comptes ». Le mot de passe n'est jamais tapé dans
// le terminal : la personne reçoit un lien pour le choisir.

type userAccount struct {
	Name          string     `json:"name"`
	Email         string     `json:"email"`
	Role          string     `json:"role"`
	ExpiresAt     *time.Time `json:"expires_at"`
	Invited       bool       `json:"invited"`
	TOTP          bool       `json:"totp"`
	TOTPRequired  bool       `json:"totp_required"`
	RecoveryCodes int        `json:"recovery_codes"`
	// EmailCode : code de connexion envoyé par e-mail (double authentification).
	EmailCode bool `json:"email_code"`
}

// twoFactor : au moins une méthode de double authentification active.
func (a userAccount) twoFactor() bool { return a.TOTP || a.EmailCode }

type userDevice struct {
	Name     string    `json:"name"`
	PairedAt time.Time `json:"paired_at"`
	LastUsed time.Time `json:"last_used"`
}

type userShare struct {
	Service   string    `json:"service"`
	Pad       string    `json:"pad"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type userInvitation struct {
	Account userAccount `json:"account"`
	Link    string      `json:"link"`
	Sent    bool        `json:"sent"`
}

// userRequest : corps de POST et PUT api/accounts (tous les champs, même inchangés).
type userRequest struct {
	Name         string     `json:"name,omitempty"`
	Email        string     `json:"email,omitempty"`
	Role         string     `json:"role"`
	Expires      *time.Time `json:"expires"`
	TOTPRequired bool       `json:"totp_required"`
}

const userHelp = `pia user : comptes de la console.

  pia user list                   comptes, rôle, état, double authentification
  pia user show <compte>          détail d'un compte, ses téléphones et ses liens de partage
  pia user add <compte> <e-mail> [--role admin|user|guest] [--until AAAA-MM-JJ]
                  [--require-2fa]   la personne reçoit un lien pour choisir son mot de passe
  pia user edit <compte> [--role admin|user|guest] [--until AAAA-MM-JJ]
                  [--require-2fa yes|no]
  pia user reset <compte>         nouveau lien pour choisir un mot de passe (oublié ou invitation expirée)
  pia user 2fa-off <compte> [--yes]
                                  retirer la double authentification (code par e-mail et
                                  application ; téléphone ou boîte e-mail et codes perdus)
  pia user delete <compte> [--yes]

Rôles : admin (administrateur), user (utilisateur), guest (invité, avec --until :
dernier jour d'accès).
`

func userCommand(env Env, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(env.Out, userHelp)
		return nil
	}
	commands := map[string]func(Env, []string) error{
		"list": userList, "ls": userList,
		"show": userShow,
		"add":  userAdd, "create": userAdd,
		"edit": userEdit, "modify": userEdit,
		"reset": userReset, "password": userReset,
		"2fa-off": userTOTPOff,
		"delete":  userDelete, "remove": userDelete, "rm": userDelete,
	}
	run, ok := commands[strings.ToLower(args[0])]
	if !ok {
		return userUsage("Commande inconnue : pia user " + args[0])
	}
	return run(env, args[1:])
}

// userUsageError : mauvaise utilisation de pia user ; son aide suit le message.
type userUsageError struct{ message string }

func (e userUsageError) Error() string { return e.message + "\n\n" + userHelp }

func userUsage(message string) error { return userUsageError{message} }

func userList(env Env, args []string) error {
	if len(args) > 0 {
		return userUsage("pia user list ne prend pas d'argument.")
	}
	var reply struct {
		Accounts []userAccount `json:"accounts"`
	}
	if err := call(context.Background(), env, http.MethodGet, "accounts", nil, &reply); err != nil {
		return err
	}
	if len(reply.Accounts) == 0 {
		fmt.Fprintln(env.Out, "Aucun compte : créez le compte administrateur dans la console.")
		return nil
	}
	table := tabwriter.NewWriter(env.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "COMPTE\tRÔLE\tE-MAIL\tÉTAT\tDOUBLE AUTHENTIFICATION")
	for _, account := range reply.Accounts {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", account.Name, roleName(account.Role), account.Email, accountState(account), totpState(account))
	}
	return table.Flush()
}

func userShow(env Env, args []string) error {
	if len(args) != 1 {
		return userUsage("Indiquez un compte : pia user show <compte> (voir pia user list).")
	}
	var reply struct {
		Account userAccount  `json:"account"`
		Devices []userDevice `json:"devices"`
		Shares  []userShare  `json:"shares"`
	}
	if err := call(context.Background(), env, http.MethodGet, "accounts/"+url.PathEscape(strings.ToLower(args[0])), nil, &reply); err != nil {
		return err
	}
	account := reply.Account
	table := tabwriter.NewWriter(env.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(table, "Compte\t%s\n", account.Name)
	fmt.Fprintf(table, "E-mail\t%s\n", account.Email)
	fmt.Fprintf(table, "Rôle\t%s\n", roleName(account.Role))
	fmt.Fprintf(table, "État\t%s\n", accountState(account))
	fmt.Fprintf(table, "Double authentification\t%s\n", totpState(account))
	if account.twoFactor() {
		fmt.Fprintf(table, "Codes de secours restants\t%d\n", account.RecoveryCodes)
	}
	if err := table.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(env.Out)
	if len(reply.Devices) == 0 {
		fmt.Fprintln(env.Out, "Téléphones appairés : aucun.")
	} else {
		fmt.Fprintln(env.Out, "Téléphones appairés :")
		for _, device := range reply.Devices {
			last := "jamais"
			if !device.LastUsed.IsZero() {
				last = dayAndTime(device.LastUsed)
			}
			fmt.Fprintf(env.Out, "  %s : appairé le %s, dernière connexion : %s\n", device.Name, day(device.PairedAt), last)
		}
	}
	if len(reply.Shares) == 0 {
		fmt.Fprintln(env.Out, "Liens de partage : aucun.")
	} else {
		fmt.Fprintln(env.Out, "Liens de partage :")
		for _, share := range reply.Shares {
			fmt.Fprintf(env.Out, "  %s, « %s » : créé le %s, valable jusqu'au %s\n", share.Service, share.Pad, day(share.CreatedAt), day(share.ExpiresAt))
		}
	}
	return nil
}

func userAdd(env Env, args []string) error {
	f, err := parseFlags(args, []string{"role", "until"}, []string{"require-2fa"})
	if err != nil {
		return err
	}
	if len(f.positional) != 2 {
		return userUsage("Indiquez le compte et son adresse e-mail : pia user add <compte> <e-mail>.")
	}
	role, err := parseRole(f.value("role"), "user")
	if err != nil {
		return err
	}
	expires, err := parseUntil(f.value("until"))
	if err != nil {
		return err
	}
	if role == "guest" && expires == nil {
		return userUsage("Un invité a besoin d'un dernier jour d'accès : --until AAAA-MM-JJ.")
	}
	if role != "guest" && expires != nil {
		return userUsage("--until ne vaut que pour un invité (--role guest).")
	}
	request := userRequest{Name: f.positional[0], Email: f.positional[1], Role: role, Expires: expires, TOTPRequired: f.set["require-2fa"]}
	var reply userInvitation
	if err := call(context.Background(), env, http.MethodPost, "accounts", request, &reply); err != nil {
		return err
	}
	fmt.Fprintf(env.Out, "Compte %s créé (%s).\n", reply.Account.Name, roleName(reply.Account.Role))
	printLink(env, reply, "choisir son mot de passe", "7 jours")
	if reply.Account.TOTPRequired {
		fmt.Fprintln(env.Out, "La double authentification est exigée : dès sa première connexion, la personne devra l'activer (code par e-mail ou application) avant d'ouvrir quoi que ce soit.")
	} else {
		fmt.Fprintln(env.Out, "La double authentification lui sera proposée à sa première connexion (facultative ; --require-2fa pour l'exiger).")
	}
	return nil
}

func userEdit(env Env, args []string) error {
	f, err := parseFlags(args, []string{"role", "until", "require-2fa"}, nil)
	if err != nil {
		return err
	}
	if len(f.positional) != 1 {
		return userUsage("Indiquez un compte : pia user edit <compte> [--role …] [--until …] [--require-2fa yes|no].")
	}
	if len(f.values) == 0 {
		return userUsage("Rien à changer : indiquez --role, --until ou --require-2fa.")
	}
	account, err := getAccount(env, f.positional[0])
	if err != nil {
		return err
	}
	request := userRequest{Role: account.Role, Expires: account.ExpiresAt, TOTPRequired: account.TOTPRequired}
	if value := f.value("role"); value != "" {
		if request.Role, err = parseRole(value, ""); err != nil {
			return err
		}
		if request.Role != "guest" {
			request.Expires = nil
		}
	}
	if value := f.value("until"); value != "" {
		if request.Expires, err = parseUntil(value); err != nil {
			return err
		}
	}
	if request.Role == "guest" && request.Expires == nil {
		return userUsage("Un invité a besoin d'un dernier jour d'accès : --until AAAA-MM-JJ.")
	}
	if request.Role != "guest" && f.value("until") != "" {
		return userUsage("--until ne vaut que pour un invité (--role guest).")
	}
	if value := f.value("require-2fa"); value != "" {
		switch strings.ToLower(value) {
		case "yes", "oui", "true", "on":
			request.TOTPRequired = true
		case "no", "non", "false", "off":
			request.TOTPRequired = false
		default:
			return userUsage("--require-2fa attend yes ou no.")
		}
	}
	var updated userAccount
	if err := call(context.Background(), env, http.MethodPut, "accounts/"+url.PathEscape(account.Name), request, &updated); err != nil {
		return err
	}
	fmt.Fprintf(env.Out, "Compte %s modifié : %s, %s, double authentification : %s.\n", updated.Name, roleName(updated.Role), accountState(updated), totpState(updated))
	return nil
}

func userReset(env Env, args []string) error {
	if len(args) != 1 {
		return userUsage("Indiquez un compte : pia user reset <compte>.")
	}
	var reply userInvitation
	if err := call(context.Background(), env, http.MethodPost, "accounts/"+url.PathEscape(strings.ToLower(args[0]))+"/invitation", nil, &reply); err != nil {
		return err
	}
	if !reply.Account.Invited {
		fmt.Fprintln(env.Out, "Le mot de passe actuel reste valable jusqu'à ce que le nouveau soit choisi.")
	}
	printLink(env, reply, "choisir un nouveau mot de passe", "7 jours")
	return nil
}

func userTOTPOff(env Env, args []string) error {
	f, err := parseFlags(args, nil, []string{"yes", "oui"})
	if err != nil {
		return err
	}
	if len(f.positional) != 1 {
		return userUsage("Indiquez un compte : pia user 2fa-off <compte>.")
	}
	name := strings.ToLower(f.positional[0])
	current, err := getAccount(env, name)
	if err != nil {
		return err
	}
	if !current.twoFactor() {
		fmt.Fprintf(env.Out, "%s n'a pas activé la double authentification : rien à retirer.\n", current.Name)
		return nil
	}
	if !f.set["yes"] && !f.set["oui"] && !confirm(env, fmt.Sprintf("Retirer la double authentification de %s (code par e-mail et application) ? À faire seulement si son téléphone ou sa boîte e-mail, et ses codes de secours, sont perdus.", name)) {
		return errors.New("Rien n'a été fait.")
	}
	var account userAccount
	if err := call(context.Background(), env, http.MethodDelete, "accounts/"+url.PathEscape(name)+"/totp", nil, &account); err != nil {
		return err
	}
	fmt.Fprintf(env.Out, "Double authentification retirée pour %s : la personne pourra la réactiver dans « Mon compte ».\n", account.Name)
	if account.TOTPRequired {
		fmt.Fprintln(env.Out, "Elle reste exigée pour ce compte : il faudra la réactiver à la prochaine connexion.")
	}
	return nil
}

func userDelete(env Env, args []string) error {
	f, err := parseFlags(args, nil, []string{"yes", "oui"})
	if err != nil {
		return err
	}
	if len(f.positional) != 1 {
		return userUsage("Indiquez un compte : pia user delete <compte>.")
	}
	name := strings.ToLower(f.positional[0])
	if !f.set["yes"] && !f.set["oui"] && !confirm(env, fmt.Sprintf("Supprimer le compte %s ? Ses sessions, ses téléphones appairés et ses liens de partage ne marcheront plus.", name)) {
		return errors.New("Rien n'a été fait.")
	}
	if err := call(context.Background(), env, http.MethodDelete, "accounts/"+url.PathEscape(name), nil, nil); err != nil {
		return err
	}
	fmt.Fprintf(env.Out, "Compte %s supprimé.\n", name)
	return nil
}

func getAccount(env Env, name string) (userAccount, error) {
	var reply struct {
		Account userAccount `json:"account"`
	}
	err := call(context.Background(), env, http.MethodGet, "accounts/"+url.PathEscape(strings.ToLower(name)), nil, &reply)
	return reply.Account, err
}

// printLink : le lien est parti par e-mail, ou il est à transmettre à la personne.
func printLink(env Env, reply userInvitation, what, lifetime string) {
	if reply.Sent {
		fmt.Fprintf(env.Out, "Un e-mail est parti à %s avec un lien pour %s (valable %s).\n", reply.Account.Email, what, lifetime)
		return
	}
	fmt.Fprintf(env.Out, "Envoyez ce lien à la personne (à %s) pour %s ; il marche %s et une seule fois :\n\n  %s\n", reply.Account.Email, what, lifetime, reply.Link)
}

func parseRole(value, fallback string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		if fallback == "" {
			return "", userUsage("Rôle manquant : admin, user ou guest.")
		}
		return fallback, nil
	case "admin", "administrateur":
		return "admin", nil
	case "user", "utilisateur":
		return "user", nil
	case "guest", "invité", "invite":
		return "guest", nil
	}
	return "", userUsage("Rôle inconnu : " + value + " (admin, user ou guest).")
}

// parseUntil : dernier jour d'accès d'un invité ; la console garde le début
// du jour suivant, à l'heure du Pi (comme l'écran « Comptes »).
func parseUntil(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	last, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return nil, userUsage("Date illisible : " + value + " (attendu AAAA-MM-JJ, par exemple 2026-12-31).")
	}
	end := last.AddDate(0, 0, 1)
	return &end, nil
}

func roleName(role string) string {
	switch role {
	case "admin":
		return "administrateur"
	case "user":
		return "utilisateur"
	case "guest":
		return "invité"
	}
	return role
}

func accountState(account userAccount) string {
	switch {
	case account.ExpiresAt != nil && !time.Now().Before(*account.ExpiresAt):
		return "accès terminé depuis le " + day(*account.ExpiresAt)
	case account.Invited:
		return "invitation en attente"
	case account.ExpiresAt != nil:
		return "actif jusqu'au " + day(account.ExpiresAt.Add(-time.Nanosecond)) + " inclus"
	}
	return "actif"
}

func totpState(account userAccount) string {
	var methods []string
	if account.EmailCode {
		methods = append(methods, "code par e-mail")
	}
	if account.TOTP {
		methods = append(methods, "application")
	}
	switch {
	case len(methods) > 0 && account.TOTPRequired:
		return "activée (" + strings.Join(methods, " et ") + ", exigée)"
	case len(methods) > 0:
		return "activée (" + strings.Join(methods, " et ") + ")"
	case account.TOTPRequired:
		return "exigée, pas encore activée"
	}
	return "non"
}

func day(t time.Time) string { return t.Local().Format("02/01/2006") }

func dayAndTime(t time.Time) string { return t.Local().Format("02/01/2006 15:04") }
