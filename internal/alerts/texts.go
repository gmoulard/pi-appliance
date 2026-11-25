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

package alerts

import (
	"strings"
)

// Textes des alertes, en français (qui fait foi) et en anglais pour les autres
// langues, comme les autres e-mails du Pi (mail.Language).
type texts struct {
	subject, intro, recoveredSubject, recovered string
	// line : une ligne par problème ; {key} et {value} remplacés.
	line func(p Problem) string
}

var alertTexts = map[string]map[Type]texts{
	"fr": {
		Disk: {
			subject:          "pi-appliance : disque presque plein",
			intro:            "Le disque de votre Pi est presque plein. Quand il n'y a plus de place, les services et les sauvegardes peuvent s'arrêter. Libérez de la place (fichiers, services inutiles) ou branchez un disque plus grand.",
			recoveredSubject: "pi-appliance : disque, retour à la normale",
			recovered:        "Le disque de votre Pi a de nouveau assez de place.",
			line: func(p Problem) string {
				if p.Key == "usb" {
					return "Disque USB : " + p.Value + " utilisés"
				}
				return "Carte SD : " + p.Value + " utilisés"
			},
		},
		Service: {
			subject:          "pi-appliance : service arrêté",
			intro:            "Un service de votre Pi est arrêté depuis plus de 10 minutes, sans que personne ne l'ait arrêté dans la console. Ouvrez la console pour le redémarrer ; s'il s'arrête encore, regardez le diagnostic du Pi.",
			recoveredSubject: "pi-appliance : services, retour à la normale",
			recovered:        "Les services signalés sont de nouveau en marche (ou ont été arrêtés exprès dans la console).",
			line:             func(p Problem) string { return "« " + p.Key + " » est arrêté" },
		},
		Power: {
			subject:          "pi-appliance : le Pi manque de courant",
			intro:            "Votre Pi a manqué de courant (sous-tension) depuis son dernier démarrage. Il peut ralentir, s'arrêter ou abîmer la carte SD. Utilisez l'alimentation officielle du Pi, ou une alimentation plus puissante, et un câble court.",
			recoveredSubject: "pi-appliance : alimentation, retour à la normale",
			recovered:        "Votre Pi a redémarré et n'a plus manqué de courant depuis.",
			line:             func(p Problem) string { return "" },
		},
		Backup: {
			subject:          "pi-appliance : la sauvegarde de la nuit a échoué",
			intro:            "La sauvegarde de la nuit ne s'est pas faite entièrement. Vérifiez que le disque de sauvegarde est branché et a de la place, puis lancez une sauvegarde depuis l'écran « Sauvegardes » de la console.",
			recoveredSubject: "pi-appliance : sauvegardes, retour à la normale",
			recovered:        "La dernière sauvegarde a réussi.",
			line:             func(p Problem) string { return "Sauvegarde du " + p.Value },
		},
		Updates: {
			subject:          "pi-appliance : correctifs de sécurité à installer",
			intro:            "Des correctifs de sécurité attendent d'être installés sur votre Pi. Installez-les depuis l'écran « Mises à jour » de la console : ils protègent votre Pi et vos données.",
			recoveredSubject: "pi-appliance : correctifs de sécurité installés",
			recovered:        "Les correctifs de sécurité signalés sont installés.",
			line:             func(p Problem) string { return p.Key },
		},
		Certificate: {
			subject:          "pi-appliance : certificat bientôt expiré",
			intro:            "Le certificat d'une adresse internet de votre Pi expire bientôt et n'a pas été renouvelé tout seul. Après cette date, les navigateurs afficheront une alerte. Vérifiez que les ports 80 et 443 de la box mènent toujours au Pi.",
			recoveredSubject: "pi-appliance : certificats, retour à la normale",
			recovered:        "Les certificats signalés ont été renouvelés.",
			line:             func(p Problem) string { return p.Key + " : expire le " + p.Value },
		},
	},
	"en": {
		Disk: {
			subject:          "pi-appliance: disk almost full",
			intro:            "Your Pi's disk is almost full. When there is no space left, services and backups may stop. Free some space (files, unused services) or plug in a larger disk.",
			recoveredSubject: "pi-appliance: disk back to normal",
			recovered:        "Your Pi's disk has enough space again.",
			line: func(p Problem) string {
				if p.Key == "usb" {
					return "USB disk: " + p.Value + " used"
				}
				return "SD card: " + p.Value + " used"
			},
		},
		Service: {
			subject:          "pi-appliance: service stopped",
			intro:            "A service on your Pi has been stopped for more than 10 minutes, without anyone stopping it in the console. Open the console to restart it; if it stops again, look at the Pi diagnostics.",
			recoveredSubject: "pi-appliance: services back to normal",
			recovered:        "The services reported are running again (or were stopped on purpose in the console).",
			line:             func(p Problem) string { return "\"" + p.Key + "\" is stopped" },
		},
		Power: {
			subject:          "pi-appliance: the Pi lacks power",
			intro:            "Your Pi has lacked power (under-voltage) since it last started. It may slow down, stop or damage the SD card. Use the official Pi power supply, or a more powerful one, and a short cable.",
			recoveredSubject: "pi-appliance: power back to normal",
			recovered:        "Your Pi has restarted and has not lacked power since.",
			line:             func(p Problem) string { return "" },
		},
		Backup: {
			subject:          "pi-appliance: last night's backup failed",
			intro:            "Last night's backup did not complete. Check that the backup disk is plugged in and has space, then start a backup from the \"Backups\" screen of the console.",
			recoveredSubject: "pi-appliance: backups back to normal",
			recovered:        "The last backup succeeded.",
			line:             func(p Problem) string { return "Backup of " + p.Value },
		},
		Updates: {
			subject:          "pi-appliance: security fixes to install",
			intro:            "Security fixes are waiting to be installed on your Pi. Install them from the \"Updates\" screen of the console: they protect your Pi and your data.",
			recoveredSubject: "pi-appliance: security fixes installed",
			recovered:        "The security fixes reported are installed.",
			line:             func(p Problem) string { return p.Key },
		},
		Certificate: {
			subject:          "pi-appliance: certificate expiring soon",
			intro:            "The certificate of one of your Pi's internet addresses expires soon and was not renewed on its own. After that date, browsers will show a warning. Check that ports 80 and 443 of your router still lead to the Pi.",
			recoveredSubject: "pi-appliance: certificates back to normal",
			recovered:        "The certificates reported have been renewed.",
			line:             func(p Problem) string { return p.Key + ": expires on " + p.Value },
		},
	},
}

var common = map[string][4]string{
	"fr": {"Bonjour,", "Ouvrir la console : ", "Vous recevez cet e-mail parce que vous êtes administrateur (« {name} ») de ce Pi. Pour ne plus recevoir ce type d'alerte : console, « Mon Pi », « Alertes par e-mail ».", "-- \npi-appliance\nMessage envoyé automatiquement, merci de ne pas y répondre.\n"},
	"en": {"Hello,", "Open the console: ", "You receive this email because you are an administrator (\"{name}\") of this Pi. To stop this type of alert: console, \"My Pi\", \"Email alerts\".", "-- \npi-appliance\nAutomatic message, please do not reply.\n"},
}

// maxLines : lignes de problèmes au plus dans un e-mail (correctifs nombreux).
const maxLines = 30

// Compose écrit le sujet et le texte d'une alerte ou d'un retour à la normale.
func Compose(t Type, recovered bool, problems []Problem, name, link, lang string) (string, string) {
	if strings.ToLower(strings.TrimSpace(lang)) != "fr" {
		lang = "en"
	} else {
		lang = "fr"
	}
	text := alertTexts[lang][t]
	words := common[lang]
	var b strings.Builder
	b.WriteString(words[0] + "\n\n")
	subject := text.subject
	if recovered {
		subject = text.recoveredSubject
		b.WriteString(text.recovered + "\n")
	} else {
		b.WriteString(text.intro + "\n")
		lines := 0
		for _, p := range problems {
			line := clean(text.line(p))
			if line == "" {
				continue
			}
			if lines == maxLines {
				b.WriteString("- …\n")
				break
			}
			if lines == 0 {
				b.WriteString("\n")
			}
			b.WriteString("- " + line + "\n")
			lines++
		}
	}
	if link != "" {
		b.WriteString("\n" + words[1] + clean(link) + "\n")
	}
	b.WriteString("\n" + strings.ReplaceAll(words[2], "{name}", clean(name)) + "\n\n")
	b.WriteString(words[3])
	return subject, b.String()
}

// clean : une ligne d'un e-mail, sans caractère de contrôle ni accolade
// (mail.Compose remplace {name}, {link}, {code}), 200 caractères au plus.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '{' || r == '}' {
			return -1
		}
		return r
	}, s)
	if runes := []rune(s); len(runes) > 200 {
		s = string(runes[:200]) + "…"
	}
	return s
}
