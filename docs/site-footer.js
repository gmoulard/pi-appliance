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

// Pied de page (docs/_includes/version.html) : dates en français à l'heure
// de Paris, date exacte du commit et dernière étiquette de version lues dans
// l'API GitHub. Sans réseau, le texte construit par GitHub Pages reste.
(() => {
  const box = document.getElementById("build-info");
  if (!box) return;
  const repo = box.dataset.repo;
  const sha = box.dataset.commit;
  const api = `https://api.github.com/repos/${repo}`;
  const field = (name) => box.querySelector(`[data-field="${name}"]`);

  const dateTime = new Intl.DateTimeFormat("fr-FR", {
    dateStyle: "long", timeStyle: "short", timeZone: "Europe/Paris",
  });
  const dateOnly = new Intl.DateTimeFormat("fr-FR", {
    dateStyle: "long", timeZone: "Europe/Paris",
  });
  const show = (el, iso, withTime) => {
    const date = new Date(iso);
    if (!el || Number.isNaN(date.getTime())) return;
    el.setAttribute("datetime", date.toISOString());
    el.title = date.toISOString();
    el.textContent = withTime
      ? `${dateTime.format(date)} (heure de Paris)`
      : dateOnly.format(date);
  };

  // Réponses gardées le temps de la visite : l'API GitHub limite à
  // 60 requêtes par heure sans compte.
  const get = async (path) => {
    const key = `pi-appliance:${path}`;
    try {
      const kept = sessionStorage.getItem(key);
      if (kept) return JSON.parse(kept);
    } catch { /* stockage indisponible */ }
    const response = await fetch(api + path, { headers: { Accept: "application/vnd.github+json" } });
    if (!response.ok) throw new Error(`${response.status} ${path}`);
    const data = await response.json();
    try { sessionStorage.setItem(key, JSON.stringify(data)); } catch { /* idem */ }
    return data;
  };
  const commitDate = async (ref) => (await get(`/commits/${ref}`)).commit.committer.date;

  show(field("commit-date"), field("commit-date")?.getAttribute("datetime"), true);
  show(field("version-date"), field("version-date")?.getAttribute("datetime"), false);

  if (sha) {
    commitDate(sha).then((iso) => show(field("commit-date"), iso, true)).catch(() => {});
  }

  // Pas de version publiée (GitHub Release) : dernière étiquette v… du dépôt.
  if (field("version-date")?.tagName !== "TIME") {
    get("/tags?per_page=100").then(async (tags) => {
      const tag = tags.find((t) => /^v\d/.test(t.name));
      if (!tag) return;
      const iso = await commitDate(tag.commit.sha);
      const version = field("version");
      version.textContent = tag.name;
      const time = document.createElement("time");
      time.dataset.field = "version-date";
      field("version-date").replaceWith(", du ", time);
      show(time, iso, false);
    }).catch(() => {});
  }
})();
