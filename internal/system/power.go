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

package system

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Power : sous-tension et ralentissement vus par la puce du Pi, en ce moment
// et depuis le démarrage (get_throttled du micrologiciel, comme
// vcgencmd get_throttled).
type Power struct {
	UndervoltageNow       bool `json:"undervoltage_now"`
	UndervoltageSinceBoot bool `json:"undervoltage_since_boot"`
	ThrottledNow          bool `json:"throttled_now"`
	ThrottledSinceBoot    bool `json:"throttled_since_boot"`
}

func readPower() *Power {
	paths, _ := filepath.Glob(throttledGlob)
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		power := parseThrottled(file)
		file.Close()
		if power != nil {
			return power
		}
	}
	return nil
}

// parseThrottled lit le masque du micrologiciel, en hexadécimal (« 50005 »,
// ou « throttled=0x50005 » pour vcgencmd) : bit 0 sous-tension, bit 2
// ralentissement, bits 16 et 18 les mêmes depuis le démarrage.
func parseThrottled(r io.Reader) *Power {
	data, err := io.ReadAll(io.LimitReader(r, 64))
	if err != nil {
		return nil
	}
	text := strings.TrimSpace(string(data))
	text = strings.TrimPrefix(text, "throttled=")
	text = strings.TrimPrefix(strings.TrimPrefix(text, "0x"), "0X")
	mask, err := strconv.ParseUint(text, 16, 32)
	if err != nil {
		return nil
	}
	bit := func(n uint) bool { return mask&(1<<n) != 0 }
	return &Power{
		UndervoltageNow:       bit(0),
		UndervoltageSinceBoot: bit(16) || bit(0),
		ThrottledNow:          bit(2),
		ThrottledSinceBoot:    bit(18) || bit(2),
	}
}
