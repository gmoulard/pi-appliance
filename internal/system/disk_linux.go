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

import "syscall"

// disk : place libre pour un utilisateur ordinaire (Bavail), comme df.
func disk(dir string) *Gauge {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil || stat.Blocks == 0 {
		return nil
	}
	size := int64(stat.Bsize)
	return newGauge(int64(stat.Blocks)*size>>20, int64(stat.Bavail)*size>>20)
}
