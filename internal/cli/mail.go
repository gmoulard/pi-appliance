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

package cli

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// pia mail : l'envoi des e-mails du Pi (carte « Envoi des e-mails » de la
// console, spécification 10.2) : voir, régler, essayer, effacer. NE PAS
// prendre le mot de passe sur la ligne de commande (historique, liste des
// programmes) : il est demandé, sans s'afficher, ou lu sur l'entrée standard.

const mailHelp = `pia mail : envoi des e-mails du Pi (invitations, mot de passe oublié,
codes de connexion, alertes), par une boîte e-mail que vous avez déjà.

  pia mail                        réglage actuel (sans le mot de passe)
  pia mail regler --serveur NOM [--securite starttls|tls] [--port N]
                  [--identifiant ID] [--expediteur ADRESSE] [--essai ADRESSE]
                                  le mot de passe est demandé (il ne s'affiche pas) ;
                                  touche Entrée seule : garder celui enregistré.
                                  --essai : envoie d'abord un e-mail d'essai à
                                  ADRESSE et n'enregistre que s'il part
  pia mail essai <adresse>        envoie un e-mail d'essai à cette adresse
  pia mail effacer [--oui]        le Pi n'envoie plus d'e-mails par ce serveur

Exemples : Gmail : --serveur smtp.gmail.com --identifiant vous@gmail.com
(mot de passe d'application) ; Free : --serveur smtp.free.fr --securite tls
--identifiant jean.dupont --expediteur jean.dupont@free.fr.
`

// mailStatus : réponse de GET api/mail (sans le mot de passe).
type mailStatus struct {
	Configured        bool      `json:"configured"`
	Host              string    `json:"host"`
	Port              int       `json:"port"`
	Security          string    `json:"security"`
	Username          string    `json:"username"`
	From              string    `json:"from"`
	PasswordSet       bool      `json:"password_set"`
	UpdatedAt         time.Time `json:"updated_at"`
	UpdatedBy         string    `json:"updated_by"`
	Verified          bool      `json:"verified"`
	VerifiedAt        time.Time `json:"verified_at"`
	EmailCodeAccounts int       `json:"email_code_accounts"`
}

type mailSettings struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
}

type mailTestRequest struct {
	To       string        `json:"to"`
	Lang     string        `json:"lang"`
	Settings *mailSettings `json:"settings,omitempty"`
}

type mailTestReply struct {
	Email  string     `json:"email"`
	Status mailStatus `json:"status"`
}

func mailCommand(env Env, args []string) error {
	if len(args) == 0 {
		return mailShow(env)
	}
	switch strings.ToLower(args[0]) {
	case "help", "aide", "-h", "--help":
		fmt.Fprint(env.Out, mailHelp)
		return nil
	case "etat", "état", "status", "show":
		return mailShow(env)
	case "regler", "régler", "set", "setup":
		return mailSet(env, args[1:])
	case "essai", "test":
		return mailTest(env, args[1:])
	case "effacer", "delete", "off":
		return mailForget(env, args[1:])
	}
	return mailUsage("Commande inconnue : pia mail " + args[0])
}

type mailUsageError struct{ message string }

func (e mailUsageError) Error() string { return e.message + "\n\n" + mailHelp }

func mailUsage(message string) error { return mailUsageError{message} }

func mailShow(env Env) error {
	var status mailStatus
	if err := call(context.Background(), env, http.MethodGet, "mail", nil, &status); err != nil {
		return err
	}
	printMail(env, status)
	return nil
}

func printMail(env Env, status mailStatus) {
	if !status.Configured {
		fmt.Fprintln(env.Out, "Envoi des e-mails : pas réglé (pia mail regler, ou la carte « Envoi des e-mails » de la console).")
		return
	}
	security := "STARTTLS"
	if status.Security == "tls" {
		security = "TLS"
	}
	fmt.Fprintf(env.Out, "Serveur d'envoi : %s, port %d, %s\n", status.Host, status.Port, security)
	identifier := status.Username
	if identifier == "" {
		identifier = "(aucun)"
	}
	password := "non"
	if status.PasswordSet {
		password = "enregistré"
	}
	fmt.Fprintf(env.Out, "Identifiant : %s ; mot de passe : %s\n", identifier, password)
	fmt.Fprintf(env.Out, "Adresse d'expédition : %s\n", status.From)
	if status.Verified {
		fmt.Fprintf(env.Out, "Vérifié le %s : un e-mail d'essai est parti avec ce réglage.\n", dayAndTime(status.VerifiedAt))
	} else {
		fmt.Fprintln(env.Out, "Pas encore vérifié : pia mail essai <adresse>.")
	}
	if !status.UpdatedAt.IsZero() {
		fmt.Fprintf(env.Out, "Changé le %s par %s.\n", dayAndTime(status.UpdatedAt), status.UpdatedBy)
	}
}

func mailSet(env Env, args []string) error {
	f, err := parseFlags(args, []string{"serveur", "server", "host", "port", "securite", "sécurité", "security", "identifiant", "username", "expediteur", "expéditeur", "from", "essai", "test"}, nil)
	if err != nil {
		return err
	}
	if len(f.positional) != 0 {
		return mailUsage("Les réglages se donnent par options : pia mail regler --serveur NOM …")
	}
	pick := func(names ...string) string {
		for _, name := range names {
			if value := f.value(name); value != "" {
				return value
			}
		}
		return ""
	}
	var current mailStatus
	if err := call(context.Background(), env, http.MethodGet, "mail", nil, &current); err != nil {
		return err
	}
	settings := mailSettings{Host: pick("serveur", "server", "host"), Security: strings.ToLower(pick("securite", "sécurité", "security")),
		Username: pick("identifiant", "username"), From: pick("expediteur", "expéditeur", "from")}
	// Options absentes : le réglage enregistré, s'il y en a un.
	if current.Configured {
		if settings.Host == "" {
			settings.Host = current.Host
		}
		if settings.Security == "" {
			settings.Security = current.Security
		}
		if f.value("identifiant") == "" && f.value("username") == "" {
			settings.Username = current.Username
		}
		if settings.From == "" && settings.Username == current.Username {
			settings.From = current.From
		}
	}
	if settings.Host == "" {
		return mailUsage("Indiquez le serveur d'envoi : --serveur smtp.gmail.com, par exemple.")
	}
	if settings.Security == "" {
		settings.Security = "starttls"
	}
	if settings.Security != "starttls" && settings.Security != "tls" {
		return mailUsage("--securite attend starttls (port 587 le plus souvent) ou tls (port 465).")
	}
	if value := pick("port"); value != "" {
		if settings.Port, err = strconv.Atoi(value); err != nil {
			return mailUsage("--port attend un nombre (587 ou 465 le plus souvent).")
		}
	} else if current.Configured && current.Host == settings.Host && current.Security == settings.Security {
		settings.Port = current.Port
	}
	if settings.Username != "" {
		keep := current.Configured && current.PasswordSet && current.Host == settings.Host && current.Username == settings.Username
		prompt := "Mot de passe du compte e-mail (il ne s'affiche pas) : "
		if keep {
			prompt = "Mot de passe du compte e-mail (Entrée seule : garder celui enregistré) : "
		}
		if settings.Password, err = readSecret(env, prompt); err != nil {
			return err
		}
	}
	if to := pick("essai", "test"); to != "" {
		var reply mailTestReply
		if err := call(context.Background(), env, http.MethodPost, "mail/test", mailTestRequest{To: to, Lang: "fr", Settings: &settings}, &reply); err != nil {
			return err
		}
		fmt.Fprintf(env.Out, "E-mail d'essai envoyé à %s : réglage vérifié et enregistré. Regardez s'il est arrivé (et dans les courriers indésirables).\n", reply.Email)
		printMail(env, reply.Status)
		return nil
	}
	var status mailStatus
	if err := call(context.Background(), env, http.MethodPut, "mail", settings, &status); err != nil {
		return err
	}
	fmt.Fprintln(env.Out, "Réglage enregistré. Envoyez-vous un e-mail d'essai : pia mail essai <adresse>.")
	printMail(env, status)
	return nil
}

func mailTest(env Env, args []string) error {
	if len(args) != 1 {
		return mailUsage("Indiquez l'adresse qui doit recevoir l'e-mail d'essai : pia mail essai <adresse>.")
	}
	var reply mailTestReply
	if err := call(context.Background(), env, http.MethodPost, "mail/test", mailTestRequest{To: args[0], Lang: "fr"}, &reply); err != nil {
		return err
	}
	fmt.Fprintf(env.Out, "E-mail d'essai envoyé à %s : regardez s'il est arrivé (et dans les courriers indésirables).\n", reply.Email)
	return nil
}

func mailForget(env Env, args []string) error {
	f, err := parseFlags(args, nil, []string{"oui", "yes"})
	if err != nil {
		return err
	}
	var status mailStatus
	if err := call(context.Background(), env, http.MethodGet, "mail", nil, &status); err != nil {
		return err
	}
	if !status.Configured {
		fmt.Fprintln(env.Out, "L'envoi des e-mails n'est pas réglé : rien à effacer.")
		return nil
	}
	question := "Ne plus envoyer d'e-mails depuis le Pi ?"
	if status.EmailCodeAccounts > 0 {
		question = fmt.Sprintf("Ne plus envoyer d'e-mails ? %d compte(s) reçoivent leur code de connexion par e-mail : ils devront utiliser leur application d'authentification ou un code de secours.", status.EmailCodeAccounts)
	}
	if !f.set["oui"] && !f.set["yes"] && !confirm(env, question) {
		fmt.Fprintln(env.Out, "Rien n'a changé.")
		return nil
	}
	var after mailStatus
	if err := call(context.Background(), env, http.MethodDelete, "mail", nil, &after); err != nil {
		return err
	}
	fmt.Fprintln(env.Out, "Le Pi n'envoie plus d'e-mails par ce serveur.")
	return nil
}

// readSecret lit un secret sur l'entrée standard, sans l'afficher sur un
// terminal (stty -echo sur /dev/tty, s'il y en a un).
func readSecret(env Env, prompt string) (string, error) {
	fmt.Fprint(env.Out, prompt)
	hidden := false
	if env.Command != nil {
		if _, err := env.Command(context.Background(), "stty", "-F", "/dev/tty", "-echo"); err == nil {
			hidden = true
		}
	}
	line, err := bufio.NewReader(env.In).ReadString('\n')
	if hidden {
		env.Command(context.Background(), "stty", "-F", "/dev/tty", "echo")
	}
	// Saisie masquée (Entrée pas affichée) ou lue dans un tube : à la ligne.
	fmt.Fprintln(env.Out)
	if err != nil && line == "" {
		return "", nil
	}
	return strings.TrimRight(line, "\r\n"), nil
}
