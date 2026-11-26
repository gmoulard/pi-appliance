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
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	RefusedSystem   = "system"
	RefusedData     = "data"
	RefusedBackup   = "backup"
	RefusedMounted  = "mounted"
	RefusedFixed    = "fixed"
	RefusedEmpty    = "empty"
	RefusedTooSmall = "too-small"
)

type Card struct {
	Device  string `json:"device"`
	Model   string `json:"model"`
	Size    int64  `json:"size"`
	Refused string `json:"refused,omitempty"`
}

type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	text := strings.Trim(string(data), `"`)
	*b = flexBool(text == "true" || text == "1")
	return nil
}

type flexInt int64

func (n *flexInt) UnmarshalJSON(data []byte) error {
	text := strings.Trim(string(data), `"`)
	if text == "" || text == "null" {
		*n = 0
		return nil
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fmt.Errorf("taille illisible %q", text)
	}
	*n = flexInt(value)
	return nil
}

type blockDevice struct {
	Name       string        `json:"name"`
	Type       string        `json:"type"`
	Tran       string        `json:"tran"`
	RM         flexBool      `json:"rm"`
	Size       flexInt       `json:"size"`
	Model      string        `json:"model"`
	Vendor     string        `json:"vendor"`
	UUID       string        `json:"uuid"`
	Mountpoint string        `json:"mountpoint"`
	Children   []blockDevice `json:"children"`
}

var LsblkArgs = []string{"--json", "--bytes", "--paths", "--output", "NAME,TYPE,TRAN,RM,SIZE,MODEL,VENDOR,UUID,MOUNTPOINT"}

var systemMounts = map[string]bool{"/": true, "/boot": true, "/boot/firmware": true}

func parseLsblk(data []byte) ([]blockDevice, error) {
	var out struct {
		Devices []blockDevice `json:"blockdevices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("lsblk : réponse illisible : %w", err)
	}
	return out.Devices, nil
}

func walk(device blockDevice, visit func(blockDevice)) {
	visit(device)
	for _, child := range device.Children {
		walk(child, visit)
	}
}

func Cards(devices []blockDevice, used map[string]string, imageSize int64) []Card {
	cards := []Card{}
	for _, device := range devices {
		if device.Type != "disk" || device.Tran != "usb" {
			continue
		}
		card := Card{Device: device.Name, Model: strings.Join(strings.Fields(device.Vendor+" "+device.Model), " "), Size: int64(device.Size)}
		var system, mounted bool
		role := ""
		walk(device, func(d blockDevice) {
			if systemMounts[d.Mountpoint] {
				system = true
			}
			if d.Mountpoint != "" {
				mounted = true
			}
			if r := used[d.UUID]; d.UUID != "" && r != "" && role == "" {
				role = r
			}
		})
		switch {
		case system:
			card.Refused = RefusedSystem
		case role != "":
			card.Refused = role
		case mounted:
			card.Refused = RefusedMounted
		case !bool(device.RM):
			card.Refused = RefusedFixed
		case card.Size <= 0:
			card.Refused = RefusedEmpty
		case imageSize > 0 && card.Size < imageSize:
			card.Refused = RefusedTooSmall
		}
		cards = append(cards, card)
	}
	return cards
}

func usedUUIDs(fstab io.Reader, roles map[string]string) map[string]string {
	used := map[string]string{}
	scanner := bufio.NewScanner(fstab)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		uuid, ok := strings.CutPrefix(fields[0], "UUID=")
		if !ok {
			continue
		}
		if role := roles[fields[1]]; role != "" {
			used[strings.Trim(uuid, `"`)] = role
		}
	}
	return used
}
