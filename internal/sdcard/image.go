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
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gmoulard/pi-appliance/internal/updates"
)

type Image struct {
	Version        string `json:"version"`
	Name           string `json:"name"`
	URL            string `json:"-"`
	ChecksumURL    string `json:"-"`
	CompressedSize int64  `json:"compressedSize"`
	Size           int64  `json:"size"`
}

const imageSuffix = ".img.xz"

// NE PAS prendre la dernière version tout court : les versions « socle seulement » n'ont pas d'image (workflow Image, image: false).
func latestImage(releases []updates.Release) (Image, bool) {
	var best Image
	found := false
	for _, release := range releases {
		if !release.Allowed(updates.Stable) {
			continue
		}
		for _, asset := range release.Assets {
			if !strings.HasSuffix(asset.Name, imageSuffix) {
				continue
			}
			checksum, ok := release.Asset(asset.Name + ".sha256")
			if !ok {
				continue
			}
			if found && !updates.Newer(best.Version, release.Tag) {
				continue
			}
			best = Image{Version: release.Tag, Name: asset.Name, URL: asset.URL, ChecksumURL: checksum.URL, CompressedSize: asset.Size}
			found = true
		}
	}
	return best, found
}

func parseChecksum(data []byte, name string) (string, error) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		sum := strings.ToLower(fields[0])
		if len(fields) > 1 && strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		if decoded, err := hex.DecodeString(sum); err == nil && len(decoded) == 32 {
			return sum, nil
		}
	}
	return "", fmt.Errorf("empreinte de %s illisible", name)
}

func getRange(ctx context.Context, client *http.Client, url string, from, to int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, to))
	request.Header.Set("User-Agent", "pi-appliance")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	// NE PAS lire une réponse 200 : ce serait toute l'image (plus d'un gigaoctet).
	if response.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("lecture partielle refusée : réponse %d", response.StatusCode)
	}
	want := to - from + 1
	data, err := io.ReadAll(io.LimitReader(response.Body, want+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != want {
		return nil, fmt.Errorf("lecture partielle : %d octets au lieu de %d", len(data), want)
	}
	return data, nil
}

var errXZ = errors.New("index xz illisible")

func readVarint(r *bytes.Reader) (int64, error) {
	var value uint64
	for shift := uint(0); shift < 63; shift += 7 {
		b, err := r.ReadByte()
		if err != nil {
			return 0, errXZ
		}
		value |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return int64(value), nil
		}
	}
	return 0, errXZ
}

func xzIndexSize(index []byte) (int64, error) {
	r := bytes.NewReader(index)
	if b, err := r.ReadByte(); err != nil || b != 0 {
		return 0, errXZ
	}
	count, err := readVarint(r)
	if err != nil {
		return 0, err
	}
	var total int64
	for i := int64(0); i < count; i++ {
		if _, err := readVarint(r); err != nil {
			return 0, err
		}
		size, err := readVarint(r)
		if err != nil {
			return 0, err
		}
		total += size
	}
	return total, nil
}

func xzSize(ctx context.Context, client *http.Client, url string, fileSize int64) (int64, error) {
	if fileSize < 32 {
		return 0, errXZ
	}
	footer, err := getRange(ctx, client, url, fileSize-12, fileSize-1)
	if err != nil {
		return 0, err
	}
	if footer[10] != 'Y' || footer[11] != 'Z' {
		return 0, errXZ
	}
	backward := (int64(binary.LittleEndian.Uint32(footer[4:8])) + 1) * 4
	if backward > 1<<20 || backward >= fileSize-12 {
		return 0, errXZ
	}
	index, err := getRange(ctx, client, url, fileSize-12-backward, fileSize-13)
	if err != nil {
		return 0, err
	}
	return xzIndexSize(index)
}
