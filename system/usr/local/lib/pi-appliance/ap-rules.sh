# This file is part of pi-appliance <https://github.com/gmoulard/pi-appliance>.
# Copyright (c) 2026 Guillaume Moulard.
#
# Licensed under the Creative Commons Attribution-NonCommercial-NoDerivatives
# 4.0 International License (CC BY-NC-ND 4.0), with the additional permissions
# of the LICENSE file at the root of the project: this file may be shared and
# integrated into another project unmodified, keeping this notice and a link to
# the project; modified versions may only be published to propose them to the
# official repository. Commercial use requires a separate licence from the
# author: guillaume@moulard.org.
#
# See <https://creativecommons.org/licenses/by-nc-nd/4.0/> and the LICENSE file.

# Règles du point d'accès Wi-Fi « pi-appliance » (spécification, section 4),
# sans aucun effet de bord : ap.sh les applique, tests/ap-rules-test.sh les vérifie.
# Bibliothèque lue par « . » depuis un script POSIX sh.
#
# Le point d'accès est ouvert tant que le Pi n'est pas connecté, pour 24 heures
# au plus. « Connecté » : un réseau a été choisi dans le portail (Wi-Fi de la
# maison ou câble) et ce réseau est actif. Il se referme donc dès que le Pi est
# connecté, ou au bout de 24 heures ; un redémarrage remet le compteur à zéro
# (l'état vit dans /run, effacé au démarrage) ; il se rouvre pour 24 heures si
# le Pi perd le réseau plus de AP_LOST secondes.
#
# Les durées se comptent en secondes depuis le démarrage (/proc/uptime) : le
# Pi n'a pas d'horloge sauvegardée, l'heure n'est juste qu'une fois connecté.

# shellcheck shell=sh disable=SC2034
set -eu

AP_WINDOW=${AP_WINDOW:-86400}   # durée d'ouverture : 24 heures
AP_GRACE=${AP_GRACE:-120}       # au démarrage d'un Pi déjà configuré : attente du réseau
AP_LOST=${AP_LOST:-600}         # réseau perdu « durablement » : 10 minutes

# État, gardé entre deux appels (ap.sh le sauvegarde dans /run/pi-appliance).
AP_WINDOW_START=${AP_WINDOW_START:-}  # début de la fenêtre de 24 heures, vide sans fenêtre
AP_OFFLINE_SINCE=${AP_OFFLINE_SINCE:-} # début de la perte du réseau, vide si connecté
AP_EVER_CONNECTED=${AP_EVER_CONNECTED:-0}

# ap_step <maintenant> <configuré 0|1> <réseau actif 0|1>
# Met à jour l'état et pose AP_WANT (open ou closed), AP_REASON et AP_CLOSES_IN
# (secondes avant fermeture quand AP_WANT=open).
ap_step() {
	now=$1
	configured=$2
	uplink=$3
	AP_CLOSES_IN=0
	if [ "$configured" = 1 ] && [ "$uplink" = 1 ]; then
		AP_WINDOW_START=
		AP_OFFLINE_SINCE=
		AP_EVER_CONNECTED=1
		AP_WANT=closed
		AP_REASON=connected
		return 0
	fi
	if [ -z "$AP_OFFLINE_SINCE" ]; then
		AP_OFFLINE_SINCE=$now
	fi
	if [ -z "$AP_WINDOW_START" ]; then
		if [ "$configured" = 1 ]; then
			# Pi déjà configuré : laisser au réseau le temps de revenir.
			if [ "$AP_EVER_CONNECTED" = 1 ]; then
				wait=$AP_LOST
			else
				wait=$AP_GRACE
			fi
			if [ $((now - AP_OFFLINE_SINCE)) -lt "$wait" ]; then
				AP_WANT=closed
				AP_REASON=waiting-network
				return 0
			fi
		fi
		AP_WINDOW_START=$now
	fi
	elapsed=$((now - AP_WINDOW_START))
	if [ "$elapsed" -lt "$AP_WINDOW" ]; then
		AP_WANT=open
		AP_REASON=not-connected
		AP_CLOSES_IN=$((AP_WINDOW - elapsed))
	else
		AP_WANT=closed
		AP_REASON=timeout
	fi
	return 0
}
