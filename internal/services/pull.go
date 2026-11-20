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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// DockerSocket : prise de l'API Docker du Pi.
const DockerSocket = "/var/run/docker.sock"

// DockerAPI télécharge les images par l'API Docker, dont le flux donne
// l'avancement de chaque couche (Compose ne le donne pas sous une forme lisible).
type DockerAPI struct {
	// Socket : prise de l'API ; vide : DockerSocket.
	Socket string
}

func (d DockerAPI) client() *http.Client {
	socket := d.Socket
	if socket == "" {
		socket = DockerSocket
	}
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socket)
	}}}
}

func (d DockerAPI) Present(ctx context.Context, image string) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/images/"+image+"/json", nil)
	if err != nil {
		return false, err
	}
	response, err := d.client().Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	switch response.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	}
	return false, fmt.Errorf("Docker répond %d", response.StatusCode)
}

func (d DockerAPI) Pull(ctx context.Context, image string, report func(PullEvent)) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker/images/create?fromImage="+url.QueryEscape(pullReference(image)), nil)
	if err != nil {
		return err
	}
	response, err := d.client().Do(request)
	if err != nil {
		return fmt.Errorf("Docker ne répond pas : %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&failure)
		if failure.Message == "" {
			failure.Message = fmt.Sprintf("Docker répond %d", response.StatusCode)
		}
		return errors.New(failure.Message)
	}
	decoder := json.NewDecoder(response.Body)
	for {
		var line struct {
			ID             string `json:"id"`
			Status         string `json:"status"`
			Error          string `json:"error"`
			ProgressDetail struct {
				Current int64 `json:"current"`
				Total   int64 `json:"total"`
			} `json:"progressDetail"`
		}
		if err := decoder.Decode(&line); err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("téléchargement interrompu : %w", err)
		}
		if line.Error != "" {
			return errors.New(line.Error)
		}
		report(PullEvent{ID: line.ID, Status: line.Status, Current: line.ProgressDetail.Current, Total: line.ProgressDetail.Total})
	}
}

// pullReference ajoute « latest » à une image sans version.
func pullReference(image string) string {
	// NE PAS envoyer une image sans version : l'API Docker en téléchargerait alors toutes les versions.
	if strings.Contains(image, "@") || strings.Contains(image[strings.LastIndex(image, "/")+1:], ":") {
		return image
	}
	return image + ":latest"
}
