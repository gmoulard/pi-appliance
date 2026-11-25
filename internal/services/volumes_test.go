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

package services

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// noBackupRunner : deux volumes, dont un marqué pour ne jamais être sauvegardé.
type noBackupRunner struct {
	fakeRunner
	root string
}

func (r *noBackupRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	r.fakeRunner.Run(ctx, dir, args...)
	call := strings.Join(args, " ")
	switch {
	case strings.HasPrefix(call, "volume ls") && strings.Contains(call, "label="+NoBackupLabel+"=false"):
		return []byte("pi-cloud_jeton\n"), nil
	case strings.HasPrefix(call, "volume ls"):
		return []byte("pi-cloud_data\npi-cloud_jeton\n"), nil
	case strings.HasPrefix(call, "volume inspect"):
		return []byte(filepath.Join(r.root, args[len(args)-1]) + "\n"), nil
	}
	return nil, nil
}

// Un volume marqué pi-appliance.backup=false n'est pas rendu aux sauvegardes.
func TestVolumesSkipsNoBackup(t *testing.T) {
	runner := &noBackupRunner{root: t.TempDir()}
	m := installed(t, runner)
	volumes, err := m.Volumes(context.Background(), "cloud")
	if err != nil {
		t.Fatal(err)
	}
	if len(volumes) != 1 || volumes[0].Name != "pi-cloud_data" || volumes[0].Path != filepath.Join(runner.root, "pi-cloud_data") {
		t.Fatalf("volumes %+v", volumes)
	}
	if !runner.called("volume ls --quiet --filter label=com.docker.compose.project=pi-cloud --filter label=pi-appliance.backup=false") {
		t.Fatalf("volumes exclus non demandés : %v", runner.calls)
	}
}
