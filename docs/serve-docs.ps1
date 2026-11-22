# This file is part of pi-appliance
# <https://github.com/gmoulard/pi-appliance>.
# Copyright (c) 2026 Guillaume Moulard.
#
# Licensed under the Creative Commons Attribution-NonCommercial-NoDerivatives
# 4.0 International License (CC BY-NC-ND 4.0), with the additional permissions
# of the LICENSE file at the root of the project: this file may be shared and
# integrated into another project unmodified, keeping this notice and a link to
# the project; modified versions may only be published to propose them to the
# official repository. Commercial use requires a separate licence from the
# author: guillaume@moulard.org.
#
# See <https://creativecommons.org/licenses/by-nc-nd/4.0/> and the LICENSE file.

param(
    [int]$Port = 4001,
    [switch]$NoBrowser,
    [switch]$Restart
)

$ErrorActionPreference = "Continue"
$name = "pi-appliance-docs"
$image = "ruby:3.3.6"
$docs = $PSScriptRoot

$docker = (Get-Command docker -ErrorAction SilentlyContinue).Source
if (-not $docker) {
    $docker = @(
        "$env:LOCALAPPDATA\Programs\DockerDesktop\resources\bin\docker.exe",
        "$env:ProgramFiles\Docker\Docker\resources\bin\docker.exe"
    ) | Where-Object { Test-Path $_ } | Select-Object -First 1
}
if (-not $docker) { throw "docker.exe introuvable : installez Docker Desktop." }

function Test-Engine {
    & $docker version --format "{{.Server.Version}}" *> $null
    return $LASTEXITCODE -eq 0
}

if (-not (Test-Engine)) {
    $desktop = @(
        "$env:LOCALAPPDATA\Programs\DockerDesktop\Docker Desktop.exe",
        "$env:ProgramFiles\Docker\Docker\Docker Desktop.exe"
    ) | Where-Object { Test-Path $_ } | Select-Object -First 1
    if (-not $desktop) { throw "Docker Desktop introuvable : démarrez le moteur Docker puis relancez." }
    Write-Host "Démarrage de Docker Desktop..."
    Start-Process $desktop
    $deadline = (Get-Date).AddMinutes(5)
    while (-not (Test-Engine)) {
        if ((Get-Date) -gt $deadline) { throw "Le moteur Docker n'a pas démarré en 5 minutes : ouvrez Docker Desktop, attendez « Engine running », puis relancez." }
        Start-Sleep -Seconds 3
    }
}

$running = & $docker inspect -f "{{.State.Running}}" $name 2>$null
if ($running -eq "true" -and -not $Restart) {
    $published = & $docker port $name 4000 2>$null | Select-Object -First 1
    Write-Host "Le site tourne déjà (autre lancement de ce script) : $published"
    Write-Host "Pour le remplacer : ./serve-docs.ps1 -Restart"
    exit 1
}
& $docker rm -f $name *> $null

$script = @'
set -e
cat > /tmp/Gemfile <<'GEMS'
source "https://rubygems.org"
gem "github-pages", group: :jekyll_plugins
gem "webrick"
gem "rake"
GEMS
export BUNDLE_GEMFILE=/tmp/Gemfile
cd /tmp
bundle install --quiet
exec bundle exec jekyll serve --source /site --destination /tmp/_site --host 0.0.0.0 --port 4000 --baseurl "" --livereload --force_polling
'@ -replace "`r", ""
$encoded = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($script))

$arguments = @(
    "run", "-d", "--name", $name,
    "-p", "${Port}:4000", "-p", "35729:35729",
    "-v", "${docs}:/site:ro",
    "-v", "pi-appliance-docs-gems:/usr/local/bundle",
    "-e", "JEKYLL_ENV=development", "-e", "PAGES_REPO_NWO=gmoulard/pi-appliance",
    "-e", "SERVE_SCRIPT=$encoded"
)
foreach ($variable in "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY") {
    $value = [Environment]::GetEnvironmentVariable($variable)
    if ($value) { $arguments += @("-e", "$variable=$value") }
}
$arguments += @($image, "sh", "-c", 'echo $SERVE_SCRIPT | base64 -d | bash')

$id = & $docker @arguments
if ($LASTEXITCODE -ne 0) { throw "Le conteneur n'a pas démarré." }
$id = "$id".Trim()

$url = "http://localhost:$Port/"
Write-Host "Construction du site (la première fois, installation des gems : quelques minutes)..."
try {
    $ready = $false
    while (-not $ready) {
        $state = & $docker inspect -f "{{.State.Running}}" $id 2>$null
        if ($state -ne "true") {
            & $docker logs $id
            throw "Le conteneur s'est arrêté."
        }
        try {
            Invoke-WebRequest $url -UseBasicParsing -TimeoutSec 3 -ErrorAction Stop | Out-Null
            $ready = $true
        } catch {
            Start-Sleep -Seconds 3
        }
    }
    Write-Host "Site prêt : $url (rechargé à chaque modification de docs/). Ctrl+C pour arrêter."
    if (-not $NoBrowser) { Start-Process $url }
    & $docker logs -f --since 1s $id
    & $docker inspect $id *> $null
    if ($LASTEXITCODE -ne 0) {
        $deadline = (Get-Date).AddSeconds(15)
        do {
            $current = & $docker inspect -f "{{.Id}}" $name 2>$null
            if ($current) { break }
            Start-Sleep -Seconds 1
        } while ((Get-Date) -lt $deadline)
        if ($current) {
            Write-Host "Conteneur remplacé par un autre lancement de ce script (-Restart) : le site tourne toujours sur $url, ce terminal ne suit plus ses messages."
        } else {
            Write-Host "Conteneur supprimé hors de ce script (docker rm ou Docker Desktop)."
        }
        $id = $null
    } elseif ((& $docker inspect -f "{{.State.Running}}" $id 2>$null) -ne "true") {
        $code = & $docker inspect -f "{{.State.ExitCode}}" $id 2>$null
        Write-Host "Jekyll s'est arrêté (code de sortie $code) : voir les messages ci-dessus."
    }
} finally {
    if ($id) {
        & $docker rm -f $id *> $null
        Write-Host "Conteneur arrêté."
    }
}
