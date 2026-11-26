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

//go:build linux

package sdcard

import (
	"os"
	"syscall"
)

const blkflsbuf = 0x1261

type blockFile struct {
	*os.File
}

func flushBuffers(file *os.File) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), blkflsbuf, 0); errno != 0 {
		return errno
	}
	return nil
}

func (b blockFile) Sync() error {
	if err := b.File.Sync(); err != nil {
		return err
	}
	return flushBuffers(b.File)
}

// NE PAS retirer O_EXCL : sur un périphérique bloc, il refuse un disque monté ou utilisé, même si la liste a changé depuis.
func OpenDevice(path string, write bool) (Device, error) {
	flag := os.O_RDONLY
	if write {
		flag = os.O_WRONLY
	}
	file, err := os.OpenFile(path, flag|syscall.O_EXCL|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if !write {
		// NE PAS relire sans vider le cache : la relecture comparerait la mémoire du Pi, pas la carte.
		if err := flushBuffers(file); err != nil {
			file.Close()
			return nil, err
		}
	}
	return blockFile{file}, nil
}
