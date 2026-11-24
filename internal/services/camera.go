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

package services

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
)

// « Caméra et timelapse » (marketplace/camera, spécification 6.3, TODO-4.9) :
// ses services lisent les caméras du Pi. Un fichier Compose ne peut pas
// donner une caméra qui n'est peut-être pas encore branchée (« devices » fait
// échouer le démarrage quand le périphérique manque, et une webcam change de
// numéro d'un démarrage à l'autre) : la console monte le dossier /dev du Pi
// et n'y autorise que les caméras, par leurs numéros de périphérique
// (device_cgroup_rules), lus dans /proc/devices du Pi. Code propre à cette
// application plutôt qu'une nouvelle clé de manifeste : rien à changer pour
// les consoles déjà installées (spécification 6.5), qui refusent de toute
// façon cette application (dossier du Pi monté, absente de leur catalogue).

// CameraApp : identifiant de l'application des caméras.
const CameraApp = "camera"

// cameraServices : services de l'application qui lisent les caméras :
// motionEye (webcams USB) et MediaMTX (module caméra, par libcamera).
var cameraServices = []string{"motioneye", "camera-pi"}

// videoMajor : numéro fixe des périphériques vidéo de Linux (/dev/video*,
// /dev/v4l-subdev*), compté même si /proc/devices est illisible.
const videoMajor = 81

// cameraDrivers : pilotes de /proc/devices dont libcamera a besoin, en plus
// de la vidéo : « media » (/dev/media*) et la mémoire partagée « dma_heap ».
var cameraDrivers = map[string]bool{"video4linux": true, "media": true, "dma_heap": true}

// procDevices : liste des pilotes du Pi ; une variable pour les essais.
var procDevices = "/proc/devices"

// cameraRisk : confirmation demandée avant d'installer les caméras (vie
// privée), sans réserver l'application aux administrateurs.
const cameraRisk = "camera"

// cameraRules : règles d'accès aux caméras, d'après les numéros des pilotes
// vidéo, media et dma_heap du Pi (périphériques en mode caractère).
func cameraRules() []string {
	majors := map[int]bool{videoMajor: true}
	if file, err := os.Open(procDevices); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		characters := false
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			switch {
			case line == "Character devices:":
				characters = true
				continue
			case strings.HasSuffix(line, "devices:"):
				characters = false
				continue
			}
			fields := strings.Fields(line)
			if !characters || len(fields) != 2 || !cameraDrivers[fields[1]] {
				continue
			}
			if major, err := strconv.Atoi(fields[0]); err == nil && major > 0 {
				majors[major] = true
			}
		}
	}
	rules := []string{}
	for _, major := range slices.Sorted(maps.Keys(majors)) {
		rules = append(rules, fmt.Sprintf("c %d:* rmw", major))
	}
	return rules
}

// addCameras donne les caméras du Pi aux services de l'application, dans
// les services de pi-appliance.override.json.
func addCameras(services map[string]any) {
	rules := cameraRules()
	for _, name := range cameraServices {
		service, ok := services[name].(map[string]any)
		if !ok {
			service = map[string]any{}
			services[name] = service
		}
		volumes, _ := service["volumes"].([]string)
		service["volumes"] = append(volumes, "/dev:/dev")
		service["device_cgroup_rules"] = rules
	}
}

// cameraRisks : confirmation de l'installation des caméras.
func cameraRisks() []Risk {
	return []Risk{{Service: "motioneye", Code: cameraRisk, Message: "« Caméra et timelapse » voit et enregistre les images des caméras branchées au Pi."}}
}
