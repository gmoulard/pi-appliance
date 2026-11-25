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

(() => {
  const marketplaceRoot = document.getElementById("marketplace-catalog");
  const homeRoot = document.getElementById("applications");
  const catalogIndex = (marketplaceRoot || homeRoot)?.dataset.catalogIndex;
  if (!catalogIndex) return;

  // Textes dans la langue de la page (TODO-5.11), écrits par le gabarit
  // depuis _data/textes/<langue>.yml ; le français à défaut. Les noms et
  // descriptions des applications restent ceux des manifestes, en français ;
  // seules les étiquettes et les catégories sont traduites (`noms`).
  const text = {
    prototype: "Dans le prototype",
    a_venir: "À venir",
    indisponible: "Le catalogue est indisponible. Réessayez plus tard.",
    mo: "Mo",
    deux_points: " : ",
    virgule: ", ",
    noms: {},
  };
  try {
    Object.assign(text, JSON.parse(document.getElementById("textes-catalogue")?.textContent || "{}"));
  } catch { /* textes français */ }
  const translated = (label) => (text.noms && text.noms[label]) || label;

  async function loadCatalog() {
    const indexResponse = await fetch(catalogIndex);
    if (!indexResponse.ok) throw new Error(`Index du catalogue : HTTP ${indexResponse.status}`);
    const index = await indexResponse.json();
    if (!/^pi-appliance-[a-f0-9]{32}\.json$/.test(index.file)) {
      throw new Error("Nom de fichier de catalogue invalide.");
    }

    const catalogURL = new URL(index.file, indexResponse.url);
    const response = await fetch(catalogURL);
    if (!response.ok) throw new Error(`Catalogue : HTTP ${response.status}`);
    const catalog = await response.json();
    if (!Array.isArray(catalog.apps)) throw new Error("Format du catalogue invalide.");
    return {
      apps: catalog.apps,
      planned: Array.isArray(catalog.planned) ? catalog.planned : [],
      tags: Array.isArray(catalog.tags) ? catalog.tags : [],
    };
  }

  function appCard(app, index, label) {
    const card = document.createElement("li");
    card.className = `app tint-${index % 6}`;
    const tile = document.createElement("span");
    tile.className = "tile";
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.setAttribute("width", "26");
    svg.setAttribute("height", "26");
    svg.setAttribute("aria-hidden", "true");
    const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
    path.setAttribute("d", app.icon);
    svg.append(path);
    tile.append(svg);
    const title = document.createElement("h3");
    title.textContent = app.name;
    const description = document.createElement("p");
    description.textContent = app.description;
    const state = document.createElement("span");
    state.className = "soon";
    state.textContent = label;
    card.append(tile, title, description, state);
    return card;
  }

  // Filtre par étiquettes (TODO-5.22), comme la console : plusieurs
  // étiquettes à la fois, une application est montrée si elle a l'une
  // d'elles ; aucune choisie : toutes. Le choix est gardé dans l'adresse
  // (#etiquette=fichiers,maison) pour pouvoir le partager.
  function chosenTags(known) {
    const match = window.location.hash.match(/^#etiquette=([a-z0-9,-]*)$/);
    if (!match) return new Set();
    return new Set(match[1].split(",").filter((id) => known.includes(id)));
  }

  function showTagFilters(apps, tags, body) {
    const box = document.getElementById("marketplace-tags");
    const used = tags.filter((tag) => apps.some((app) => (app.tags || []).includes(tag.id)));
    if (!box || used.length < 2) return;
    const known = used.map((tag) => tag.id);
    let chosen = chosenTags(known);
    const empty = document.getElementById("marketplace-tags-empty");
    const apply = () => {
      for (const button of box.querySelectorAll("button")) {
        button.setAttribute("aria-pressed", String(chosen.has(button.dataset.tag)));
      }
      let shown = 0;
      for (const row of body.children) {
        row.hidden = chosen.size > 0 && !row.dataset.tags.split(" ").some((id) => chosen.has(id));
        if (!row.hidden) shown += 1;
      }
      if (empty) empty.hidden = shown > 0;
    };
    box.replaceChildren(...used.map((tag) => {
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = translated(tag.label);
      button.dataset.tag = tag.id;
      button.addEventListener("click", () => {
        if (chosen.has(tag.id)) chosen.delete(tag.id);
        else chosen.add(tag.id);
        const hash = chosen.size > 0 ? `#etiquette=${known.filter((id) => chosen.has(id)).join(",")}` : "";
        window.history.replaceState(null, "", `${window.location.pathname}${window.location.search}${hash}`);
        apply();
      });
      return button;
    }));
    window.addEventListener("hashchange", () => {
      chosen = chosenTags(known);
      apply();
    });
    box.hidden = false;
    const help = document.getElementById("marketplace-tags-help");
    if (help) help.hidden = false;
    apply();
  }

  function populateMarketplace(apps, planned, tags) {
    if (!marketplaceRoot) return;
    const plannedList = document.getElementById("marketplace-planned");
    if (plannedList) {
      plannedList.replaceChildren(...planned.map((app) => {
        const item = document.createElement("li");
        const name = document.createElement("strong");
        name.textContent = app.name;
        item.append(name, `${text.deux_points}${app.description} (${app.todo})`);
        return item;
      }));
      plannedList.hidden = planned.length === 0;
    }
    const status = document.getElementById("marketplace-catalog-status");
    const table = document.getElementById("marketplace-catalog-table");
    const body = table.querySelector("tbody");
    body.replaceChildren(...apps.map((app) => {
      const row = document.createElement("tr");
      row.dataset.tags = (app.tags || []).join(" ");
      for (const value of [
        app.name,
        app.description,
        translated(app.category),
        (app.tags || []).map((id) => translated((tags.find((tag) => tag.id === id) || { label: id }).label)).join(text.virgule),
        `${app.memory_mb} ${text.mo}`,
        `${app.disk_mb} ${text.mo}`,
      ]) {
        const cell = document.createElement("td");
        cell.textContent = value;
        row.append(cell);
      }
      return row;
    }));
    status.hidden = true;
    table.hidden = false;
    showTagFilters(apps, tags, body);
  }

  function populateHome(apps, planned) {
    if (!homeRoot) return;
    const list = homeRoot.querySelector(".apps");
    const status = document.getElementById("home-catalog-status");
    list.replaceChildren(
      ...apps.map((app, index) => appCard(app, index, text.prototype)),
      ...planned.map((app, index) => appCard(app, apps.length + index, text.a_venir)),
    );
    status.hidden = true;
  }

  loadCatalog()
    .then(({ apps, planned, tags }) => {
      populateMarketplace(apps, planned, tags);
      populateHome(apps, planned);
    })
    .catch((error) => {
      console.error("Chargement du catalogue impossible :", error);
      for (const id of ["marketplace-catalog-status", "home-catalog-status"]) {
        const status = document.getElementById(id);
        if (status) status.textContent = text.indisponible;
      }
    });
})();
