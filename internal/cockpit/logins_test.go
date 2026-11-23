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

package cockpit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// syncScript tient lieu de cockpit.sh pour la synchronisation : chaque
// administrateur reçoit son nom comme compte système, sauf « pi », déjà pris.
type syncScript struct {
	installed bool
	accounts  []string
	syncs     []string
}

func (s *syncScript) Run(_ context.Context, stdin []byte, args ...string) ([]byte, error) {
	switch args[0] {
	case "status":
		list := `[]`
		if len(s.accounts) > 0 {
			list = `["` + strings.Join(s.accounts, `","`) + `"]`
		}
		installed := "false"
		if s.installed {
			installed = "true"
		}
		return []byte(`{"installed": ` + installed + `, "active": true, "accounts": ` + list + `}`), nil
	case "sync":
		lines := strings.Split(strings.TrimSpace(string(stdin)), "\n")
		sort.Strings(lines)
		s.syncs = append(s.syncs, strings.Join(lines, "|"))
		s.accounts = nil
		var pairs, conflicts []string
		for _, line := range lines {
			name, password, _ := strings.Cut(line, " ")
			if len(password) != passwordLength {
				return []byte(`{"ok": false, "error": "invalid", "message": "Mot de passe refusé."}`), errors.New("exit status 1")
			}
			if name == "pi" {
				conflicts = append(conflicts, `"pi"`)
				continue
			}
			s.accounts = append(s.accounts, name)
			pairs = append(pairs, `"`+name+`": "`+name+`"`)
		}
		return []byte(`{"ok": true, "accounts": {` + strings.Join(pairs, ",") + `}, "conflicts": [` + strings.Join(conflicts, ",") + `]}`), nil
	}
	return []byte(`{"ok": true}`), nil
}

func TestLoginsCredentials(t *testing.T) {
	script := &syncScript{installed: true}
	admins := []string{"marie", "pi"}
	file := filepath.Join(t.TempDir(), "system", "cockpit-accounts.json")
	logins := &Logins{Client: &Client{Runner: script}, File: file, Admins: func() []string { return admins }}
	ctx := context.Background()

	user, password, err := logins.Credentials(ctx, "marie")
	if err != nil || user != "marie" || len(password) != passwordLength {
		t.Fatalf("marie : %q %q %v", user, password, err)
	}
	if _, _, err := logins.Credentials(ctx, "pi"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("nom pris sur le Pi : %v", err)
	}
	if _, _, err := logins.Credentials(ctx, "paul"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("pas administrateur : %v", err)
	}
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("fichier des mots de passe : %v %v", info, err)
	}
	// Même mot de passe d'une fois sur l'autre, même après relecture du fichier.
	again := &Logins{Client: &Client{Runner: script}, File: file, Admins: func() []string { return admins }}
	if _, second, _ := again.Credentials(ctx, "marie"); second != password {
		t.Fatal("mot de passe changé")
	}

	// marie n'est plus administratrice : son compte système part dans la minute.
	admins = []string{"pi"}
	before := len(script.syncs)
	logins.tick(ctx)
	if len(script.syncs) != before+1 || strings.Contains(script.syncs[len(script.syncs)-1], "marie") || len(script.accounts) != 0 {
		t.Fatalf("synchronisation : %q, comptes %q", script.syncs, script.accounts)
	}
	logins.tick(ctx)
	if len(script.syncs) != before+1 {
		t.Fatal("synchronisation sans changement")
	}

	// Cockpit retiré : tout est oublié.
	script.installed = false
	logins.tick(ctx)
	if logins.User("marie") != "" || len(logins.state.Passwords) != 0 {
		t.Fatalf("état après retrait : %+v", logins.state)
	}
}
