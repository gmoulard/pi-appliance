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
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SSH du Pi (service « ssh » de Raspberry Pi OS). Les mots de passe se règlent
// dans un fichier à part, lu avant les autres par sshd (le premier réglage
// trouvé l'emporte) ; une clé publique est exigée avant de les couper, pour ne
// jamais fermer la porte à la personne connectée.

// SSHDropIn : réglage des mots de passe écrit par pi-appliance.
const SSHDropIn = "/etc/ssh/sshd_config.d/00-pi-appliance.conf"

// keyTypes : types de clés publiques acceptés (OpenSSH).
var keyTypes = []string{
	"ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521",
	"sk-ssh-ed25519@openssh.com", "sk-ecdsa-sha2-nistp256@openssh.com",
}

func sshCommand(env Env, args []string) error {
	if len(args) == 0 {
		return sshState(env, nil)
	}
	sub := strings.ToLower(args[0])
	switch sub {
	case "etat", "état", "status":
		return sshState(env, args[1:])
	case "activer", "on":
		return sshService(env, args[1:], true)
	case "desactiver", "désactiver", "off":
		return sshService(env, args[1:], false)
	case "cle", "clé", "cles", "clés", "key", "keys":
		return sshKeys(env, args[1:])
	case "mot-de-passe", "password":
		return sshPassword(env, args[1:])
	}
	return usageError{"Commande ssh inconnue : " + args[0]}
}

func run(env Env, name string, args ...string) (string, error) {
	output, err := env.Command(context.Background(), name, args...)
	return strings.TrimSpace(output), err
}

// passwordLogin : les mots de passe sont-ils acceptés ? D'après sshd -T (réglage
// effectif, root), sinon d'après le fichier de pi-appliance ; vide : inconnu.
func passwordLogin(env Env) string {
	if output, err := run(env, "sshd", "-T"); err == nil {
		for _, line := range strings.Split(output, "\n") {
			if value, ok := strings.CutPrefix(line, "passwordauthentication "); ok {
				return strings.TrimSpace(value)
			}
		}
	}
	data, err := os.ReadFile(env.Root + SSHDropIn)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.EqualFold(fields[0], "PasswordAuthentication") {
			return strings.ToLower(fields[1])
		}
	}
	return ""
}

func sshState(env Env, args []string) error {
	f, err := parseFlags(args, []string{"utilisateur"}, nil)
	if err != nil {
		return err
	}
	active, _ := run(env, "systemctl", "is-active", "ssh")
	enabled, _ := run(env, "systemctl", "is-enabled", "ssh")
	switch {
	case active == "active":
		fmt.Fprintln(env.Out, "SSH : en marche.")
	default:
		fmt.Fprintln(env.Out, "SSH : arrêté. Pour l'allumer : pia ssh activer")
	}
	if enabled == "enabled" {
		fmt.Fprintln(env.Out, "Au démarrage du Pi : allumé.")
	} else {
		fmt.Fprintln(env.Out, "Au démarrage du Pi : éteint.")
	}
	switch passwordLogin(env) {
	case "yes":
		fmt.Fprintln(env.Out, "Connexion par mot de passe : acceptée.")
	case "no":
		fmt.Fprintln(env.Out, "Connexion par mot de passe : refusée (clé SSH seulement).")
	default:
		fmt.Fprintln(env.Out, "Connexion par mot de passe : inconnue.")
	}
	if user, err := resolveUser(env, f.value("utilisateur")); err == nil {
		keys, _ := readKeys(user)
		fmt.Fprintf(env.Out, "Clés SSH de %s : %d.\n", user.name, len(keys.valid()))
		if host := readSystem(env, "/etc/hostname"); host != "" && active == "active" {
			fmt.Fprintf(env.Out, "Pour se connecter : ssh %s@%s.local\n", user.name, host)
		}
	}
	return nil
}

func sshService(env Env, args []string, on bool) error {
	f, err := parseFlags(args, nil, []string{"oui"})
	if err != nil {
		return err
	}
	if len(f.positional) > 0 {
		return usageError{"Option inconnue : " + f.positional[0]}
	}
	if env.Euid != 0 {
		if on {
			return errNeedRoot("ssh activer")
		}
		return errNeedRoot("ssh desactiver")
	}
	if on {
		if output, err := run(env, "systemctl", "enable", "--now", "ssh"); err != nil {
			return fmt.Errorf("SSH n'a pas pu être allumé : %s", output)
		}
		fmt.Fprintln(env.Out, "SSH est allumé, et le restera aux prochains démarrages.")
		return nil
	}
	question := "Éteindre SSH ? Vous ne pourrez plus vous connecter au Pi par SSH (la console reste disponible)."
	if env.Getenv("SSH_CONNECTION") != "" {
		question = "Vous êtes connecté par SSH : l'éteindre peut couper cette connexion, et vous ne pourrez plus vous reconnecter par SSH (la console reste disponible). Continuer ?"
	}
	if !f.set["oui"] && !confirm(env, question) {
		return errors.New("Rien n'a été fait.")
	}
	if output, err := run(env, "systemctl", "disable", "--now", "ssh"); err != nil {
		return fmt.Errorf("SSH n'a pas pu être éteint : %s", output)
	}
	fmt.Fprintln(env.Out, "SSH est éteint, et le restera aux prochains démarrages. Pour le rallumer : pia ssh activer (avec un écran et un clavier, ou depuis la console d'administration du système).")
	return nil
}

type account struct {
	name     string
	uid, gid int
	home     string
}

// resolveUser : compte dont on gère les clés : --utilisateur, sinon celui qui a
// lancé sudo, sinon le premier compte de personne (uid 1000 et plus).
func resolveUser(env Env, name string) (account, error) {
	data, err := os.ReadFile(env.Root + "/etc/passwd")
	if err != nil {
		return account{}, err
	}
	if name == "" {
		if sudo := env.Getenv("SUDO_USER"); sudo != "" && sudo != "root" {
			name = sudo
		}
	}
	var first *account
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 7 {
			continue
		}
		uid, err1 := strconv.Atoi(fields[2])
		gid, err2 := strconv.Atoi(fields[3])
		if err1 != nil || err2 != nil {
			continue
		}
		user := account{name: fields[0], uid: uid, gid: gid, home: env.Root + fields[5]}
		if name != "" && fields[0] == name {
			return user, nil
		}
		if name == "" && first == nil && uid >= 1000 && uid < 60000 {
			first = &user
		}
	}
	if name != "" {
		return account{}, fmt.Errorf("Compte inconnu sur le Pi : %s", name)
	}
	if first == nil {
		return account{}, errors.New("Aucun compte de personne sur le Pi : indiquez --utilisateur NOM.")
	}
	return *first, nil
}

// keyFile : lignes de authorized_keys ; key est vide pour une ligne qui n'est
// pas une clé (commentaire, ligne vide, clé avec options : gardées telles quelles).
type keyLine struct {
	text string
	key  publicKey
}

type keyFile []keyLine

type publicKey struct {
	kind    string
	blob    []byte
	comment string
}

func (k publicKey) fingerprint() string {
	sum := sha256.Sum256(k.blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func (k publicKey) line() string {
	line := k.kind + " " + base64.StdEncoding.EncodeToString(k.blob)
	if k.comment != "" {
		line += " " + k.comment
	}
	return line
}

// parseKey lit une clé publique OpenSSH « type base64 [commentaire] » et vérifie
// que le contenu annonce le même type.
func parseKey(text string) (publicKey, bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 2 || !contains(keyTypes, fields[0]) {
		return publicKey{}, false
	}
	blob, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil || len(blob) < 4 {
		return publicKey{}, false
	}
	size := binary.BigEndian.Uint32(blob[:4])
	if uint64(size) > uint64(len(blob)-4) || string(blob[4:4+size]) != fields[0] {
		return publicKey{}, false
	}
	comment := strings.Join(fields[2:], " ")
	if strings.ContainsAny(comment, "\r\n") {
		return publicKey{}, false
	}
	return publicKey{kind: fields[0], blob: blob, comment: comment}, true
}

func (u account) keysPath() string {
	return filepath.Join(u.home, ".ssh", "authorized_keys")
}

func readKeys(user account) (keyFile, error) {
	data, err := os.ReadFile(user.keysPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file keyFile
	for _, text := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		key, _ := parseKey(text)
		file = append(file, keyLine{text: text, key: key})
	}
	return file, nil
}

func (f keyFile) valid() []publicKey {
	var keys []publicKey
	for _, line := range f {
		if line.key.kind != "" {
			keys = append(keys, line.key)
		}
	}
	return keys
}

// writeKeys écrit authorized_keys (dossier 700, fichier 600) au compte.
func writeKeys(env Env, user account, file keyFile) error {
	dir := filepath.Dir(user.keysPath())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := env.Chown(dir, user.uid, user.gid); err != nil {
		return err
	}
	var b strings.Builder
	for _, line := range file {
		b.WriteString(line.text + "\n")
	}
	temporary := user.keysPath() + ".tmp"
	if err := os.WriteFile(temporary, []byte(b.String()), 0o600); err != nil {
		return err
	}
	if err := env.Chown(temporary, user.uid, user.gid); err != nil {
		os.Remove(temporary)
		return err
	}
	return os.Rename(temporary, user.keysPath())
}

func sshKeys(env Env, args []string) error {
	if len(args) == 0 {
		return usageError{"pia ssh cle liste | ajouter | retirer"}
	}
	f, err := parseFlags(args[1:], []string{"utilisateur", "fichier"}, nil)
	if err != nil {
		return err
	}
	user, err := resolveUser(env, f.value("utilisateur"))
	if err != nil {
		return err
	}
	switch strings.ToLower(args[0]) {
	case "liste", "list":
		file, err := readKeys(user)
		if errors.Is(err, fs.ErrPermission) {
			return errNeedRoot("ssh cle liste")
		}
		if err != nil {
			return err
		}
		keys := file.valid()
		if len(keys) == 0 {
			fmt.Fprintf(env.Out, "Aucune clé SSH pour %s. Pour en ajouter une : pia ssh cle ajouter \"<clé publique>\"\n", user.name)
			return nil
		}
		for i, key := range keys {
			fmt.Fprintf(env.Out, "%d. %s %s %s\n", i+1, key.kind, key.fingerprint(), key.comment)
		}
		return nil
	case "ajouter", "add":
		if env.Euid != 0 {
			return errNeedRoot("ssh cle ajouter …")
		}
		text := strings.Join(f.positional, " ")
		if path := f.value("fichier"); path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text = string(data)
		}
		key, ok := parseKey(text)
		if !ok {
			return errors.New("Ce n'est pas une clé publique SSH (elle commence par ssh-ed25519, ssh-rsa ou ecdsa-…, et se trouve dans le fichier .pub). Ne donnez jamais la clé privée.")
		}
		file, err := readKeys(user)
		if err != nil {
			return err
		}
		for _, existing := range file.valid() {
			if existing.fingerprint() == key.fingerprint() {
				fmt.Fprintf(env.Out, "Cette clé est déjà là pour %s.\n", user.name)
				return nil
			}
		}
		file = append(file, keyLine{text: key.line(), key: key})
		if err := writeKeys(env, user, file); err != nil {
			return err
		}
		fmt.Fprintf(env.Out, "Clé ajoutée pour %s (%s).\n", user.name, key.fingerprint())
		return nil
	case "retirer", "remove":
		if env.Euid != 0 {
			return errNeedRoot("ssh cle retirer …")
		}
		if len(f.positional) != 1 {
			return usageError{"Indiquez le numéro de la clé (voir pia ssh cle liste)."}
		}
		number, err := strconv.Atoi(f.positional[0])
		file, readErr := readKeys(user)
		if readErr != nil {
			return readErr
		}
		keys := file.valid()
		if err != nil || number < 1 || number > len(keys) {
			return fmt.Errorf("Numéro inconnu : %s (voir pia ssh cle liste).", f.positional[0])
		}
		removed := keys[number-1]
		if len(keys) == 1 && passwordLogin(env) == "no" {
			return errors.New("C'est la dernière clé et les mots de passe sont refusés : vous ne pourriez plus vous connecter. Activez d'abord les mots de passe : pia ssh mot-de-passe activer")
		}
		var kept keyFile
		for _, line := range file {
			if line.key.kind != "" && line.key.fingerprint() == removed.fingerprint() {
				continue
			}
			kept = append(kept, line)
		}
		if err := writeKeys(env, user, kept); err != nil {
			return err
		}
		fmt.Fprintf(env.Out, "Clé retirée pour %s (%s).\n", user.name, removed.fingerprint())
		return nil
	}
	return usageError{"Commande inconnue : ssh cle " + args[0]}
}

func sshPassword(env Env, args []string) error {
	f, err := parseFlags(args, []string{"utilisateur"}, nil)
	if err != nil {
		return err
	}
	if len(f.positional) != 1 {
		return usageError{"pia ssh mot-de-passe activer | desactiver"}
	}
	var on bool
	switch strings.ToLower(f.positional[0]) {
	case "activer", "on":
		on = true
	case "desactiver", "désactiver", "off":
	default:
		return usageError{"pia ssh mot-de-passe activer | desactiver"}
	}
	if env.Euid != 0 {
		return errNeedRoot("ssh mot-de-passe " + f.positional[0])
	}
	if !on {
		user, err := resolveUser(env, f.value("utilisateur"))
		if err != nil {
			return err
		}
		file, err := readKeys(user)
		if err != nil {
			return err
		}
		if len(file.valid()) == 0 {
			return fmt.Errorf("%s n'a aucune clé SSH : sans mot de passe, plus personne ne pourrait se connecter. Ajoutez d'abord une clé : pia ssh cle ajouter \"<clé publique>\"", user.name)
		}
	}
	value := "no"
	if on {
		value = "yes"
	}
	content := "# Écrit par pi-appliance (pia ssh mot-de-passe) : ne pas modifier à la main.\n" +
		"PasswordAuthentication " + value + "\nKbdInteractiveAuthentication " + value + "\n"
	path := env.Root + SSHDropIn
	previous, readErr := os.ReadFile(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	if output, err := run(env, "sshd", "-t"); err != nil {
		if readErr == nil {
			_ = os.WriteFile(path, previous, 0o644)
		} else {
			_ = os.Remove(path)
		}
		return fmt.Errorf("La configuration de SSH est refusée, rien n'a été changé : %s", output)
	}
	if output, err := run(env, "systemctl", "try-reload-or-restart", "ssh"); err != nil {
		return fmt.Errorf("Réglage écrit, mais SSH n'a pas pu le relire : %s", output)
	}
	if on {
		fmt.Fprintln(env.Out, "Connexion SSH par mot de passe : acceptée.")
	} else {
		fmt.Fprintln(env.Out, "Connexion SSH par mot de passe : refusée, clé SSH seulement. Gardez cette session ouverte et vérifiez dans un autre terminal que votre clé marche.")
	}
	return nil
}
