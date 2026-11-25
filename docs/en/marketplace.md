---
layout: default
lang: en
ref: marketplace
traduit_le: 2026-11-25
---
# pi-appliance marketplace

The marketplace brings together the applications you can install and manage from the pi-appliance console. The catalogue is published on this site: consoles already installed read it again every 6 hours, and an accepted application appears there without a new version of pi-appliance.

> **Experimental version.** The image is validated on Raspberry Pi 3 B+ and Raspberry Pi 4; at every change, an automatic test installs each application and opens its page, at home and from the internet, but each one still has to be tried on a real Pi. Services also open from the internet, under the Pi's address (see the [guide, in French]({{ '/guide-utilisateur.html#services-sur-internet' | relative_url }})).

## The pi-appliance base

The **pi-appliance base** (Docker, Traefik, the system services and tools, the console and the `pia` command) comes with the image: it is not in the catalogue and cannot be uninstalled. Only an administrator, with the account created at first start-up or another administrator account, installs and manages the applications below from the console.

## Applications already in the catalogue

<div id="marketplace-catalog" data-catalog-index="{{ '/static/pi-appliance-catalog.json' | relative_url }}">
<p id="marketplace-catalog-status" role="status">Loading the catalogue…</p>
<p id="marketplace-tags-help" hidden>Tap one or more tags to see only the applications that have at least one of them; tap it again to remove it.</p>
<div id="marketplace-tags" class="todo-filters" role="group" aria-label="Filter by tag" hidden></div>
<p id="marketplace-tags-empty" hidden>No application has these tags.</p>
<table id="marketplace-catalog-table" hidden>
<thead><tr><th>Application</th><th>Description</th><th>Category</th><th>Tags</th><th>Memory</th><th>Disk</th></tr></thead>
<tbody></tbody>
</table>
</div>

The names and descriptions of the applications come from their manifests and are, for now, written in French only.

This list is loaded from the JSON catalogue generated from the manifests in [`marketplace/`](https://github.com/gmoulard/pi-appliance/tree/master/marketplace). The consoles read this same file in the GitHub repository: an accepted application appears on the Pis without a new version of pi-appliance. After any change to a manifest or to `marketplace/planned.yaml`, regenerate the file with `go run ./cmd/marketplace-json` from the root of the repository; otherwise the tests (`go test ./...`) fail. If you forget, a GitHub action regenerates it on `master` after acceptance. The file name contains the MD5 of its content; the index `docs/static/pi-appliance-catalog.json` gives the current version.

The console manages several instances, starting and stopping them, updates with rollback, and keeping or deleting data on uninstallation.

## What is planned or under study

These applications cannot be installed from the marketplace yet. The list comes from the same JSON catalogue (`marketplace/planned.yaml`):

<ul id="marketplace-planned" hidden></ul>

**Built into the console, outside the catalogue**: [Cockpit](https://cockpit-project.org/), administration of the Pi's system (logs, programs, network, disks and a terminal in the browser, for experienced users). It is not a container but system software: the console's Marketplace offers it to administrators, at home only, and you sign in with your pi-appliance account, without another password (see the [specification]({{ '/specification.html' | relative_url }}) (in French), section 6.4).

**Outside the catalogue, free Docker Compose**: an administrator can also install an application that is not in the catalogue, from its Docker Compose file, pasted or read from a web address (a GitHub page, for example). Such a service is neither checked nor supported by the project (see the [guide, in French]({{ '/guide-utilisateur.html#docker-compose-libre' | relative_url }})).

Databases: PostgreSQL is available, with the Adminer web interface. MongoDB (Pi 5 only), Redis, MSSQL (no image for the Pi) and SQLite (a simple file) are not offered on their own: see the [specification]({{ '/specification.html' | relative_url }}) (in French), section 6.3.

Public sharing links for Etherpad are also a planned improvement. Check the [roadmap]({{ '/TODO.html' | relative_url }}) (in French) to follow their progress.

## Adding an application: everyone can contribute

Contributions are open. You can propose a free application that is useful at home (the only exception for now: n8n, under the *fair-code* "Sustainable Use License", chosen by the project and flagged on its page), improve an existing manifest, or report a problem. A proposal goes through a pull request; it will be reviewed and tested before being merged.

### 1. Create a Docker Compose manifest

Fork the [pi-appliance repository](https://github.com/gmoulard/pi-appliance), then create `marketplace/<identifier>/compose.yaml`. The identifier must be in lower case, start with a letter and contain only letters, digits and hyphens.

The file is a standard Docker Compose manifest with an `x-pi-appliance` section describing the application. Example structure (replace the values and the image with those of your application):

```yaml
x-pi-appliance:
  name: My application
  description: A clear description for users.
  icon: M4 5h16v14H4z
  category: Outils
  tags: [administration]
  license: AGPL-3.0
  subdomain: outil
  service: app
  port: 8080
  memory_mb: 256
  disk_mb: 512
  login: application
  access: users
  parameters:
    - name: ADMIN_PASSWORD
      label: Administrator password
      generate: true

services:
  app:
    image: editeur/image:1.2.3
    restart: unless-stopped
    environment:
      ADMIN_PASSWORD: ${ADMIN_PASSWORD}
    volumes:
      - data:/var/lib/application

volumes:
  data:
```

### 2. Follow the catalogue rules

- Choose a Docker image with a **precise version** (not `latest` or `stable`) that is compatible with `linux/arm64`, the architecture of the targeted Raspberry Pis.
- Declare the published service in `service`, its internal port in `port`, and realistic estimates in MB for `memory_mb` and `disk_mb`.
- The `icon` field is an SVG path (`d`) on a 24 × 24 grid. The `login` field accepts `application` or `console`; `access` accepts `public`, `users`, `home` or `admins` (administrators, at home: reserved for applications that control the whole Pi, such as Portainer or Claude Code).
- Declare parameters in upper case in `parameters`. For secrets, use `generate: true` rather than putting a password in the manifest or in an image.
- Store persistent data in named Docker volumes. Put no secrets, no personal data and no user-specific configuration in the repository. A volume that must never be backed up (an account token) carries the label `pi-appliance.backup: "false"` (the volume's `labels`, like Claude Code).
- State the application's licence and check that redistributing it is compatible with the project.
- Use only the documented keys in `x-pi-appliance`: an unknown key causes the manifest to be rejected.
- Avoid options that give control over the whole Pi (privileged container, access to Docker, a folder of the Pi, host network, added capabilities): a new application that has them cannot be installed from the online catalogue, only after a new version of pi-appliance. An application that has access to Docker or is a privileged container must also declare `access: admins`, and the console asks for confirmation before installing it.
- An application that is planned but not ready yet goes in `marketplace/planned.yaml` (name, description, icon, category and roadmap line): the site shows it as "Coming soon".

The full definition of the fields and the validation rules are in the [specification, section 6.5]({{ '/specification.html' | relative_url }}) (in French).

### 3. Check and propose

From the root of the repository, run:

```shell
go run ./cmd/marketplace-json
go test ./...
```

The tests check in particular that the manifests are valid, that the images have a fixed version and that the Compose variables are declared. If possible, also try installing, stopping, restarting and uninstalling your application with Docker on a test machine.

Then open a pull request to `master`, with a description of the application, the resources it needs, the test steps and any known limitations. Once the pull request is accepted on `master`, the appliances read the new online catalogue within 6 hours, without a new version of pi-appliance.

## Useful links

- [Source code of the marketplace](https://github.com/gmoulard/pi-appliance/tree/master/marketplace)
- [Specification of the manifests]({{ '/specification.html' | relative_url }}) (in French)
- [Roadmap]({{ '/TODO.html' | relative_url }}) (in French)
- [pi-appliance repository](https://github.com/gmoulard/pi-appliance): open a pull request from your fork to propose your contribution.
