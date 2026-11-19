// This file is part of pi-appliance <https://github.com/gmoulard/pi-appliance>.
// Copyright (c) 2026 Guillaume Moulard.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3.

(() => {
  const marketplaceRoot = document.getElementById("marketplace-catalog");
  const homeRoot = document.getElementById("applications");
  const catalogIndex = (marketplaceRoot || homeRoot)?.dataset.catalogIndex;
  if (!catalogIndex) return;

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
    return catalog.apps;
  }

  function populateMarketplace(apps) {
    if (!marketplaceRoot) return;
    const status = document.getElementById("marketplace-catalog-status");
    const table = document.getElementById("marketplace-catalog-table");
    const body = table.querySelector("tbody");
    body.replaceChildren(...apps.map((app) => {
      const row = document.createElement("tr");
      for (const value of [
        app.name,
        app.description,
        app.category,
        `${app.memory_mb} Mo`,
        `${app.disk_mb} Mo`,
      ]) {
        const cell = document.createElement("td");
        cell.textContent = value;
        row.append(cell);
      }
      return row;
    }));
    status.hidden = true;
    table.hidden = false;
  }

  function populateHome(apps) {
    if (!homeRoot) return;
    const list = homeRoot.querySelector(".apps");
    const placeholder = document.getElementById("planned-wireguard");
    const status = document.getElementById("home-catalog-status");
    apps.forEach((app, index) => {
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
      state.textContent = "Dans le prototype";
      card.append(tile, title, description, state);
      list.insertBefore(card, placeholder);
    });
    status.hidden = true;
  }

  loadCatalog()
    .then((apps) => {
      populateMarketplace(apps);
      populateHome(apps);
    })
    .catch((error) => {
      console.error("Chargement du catalogue impossible :", error);
      for (const id of ["marketplace-catalog-status", "home-catalog-status"]) {
        const status = document.getElementById(id);
        if (status) status.textContent = "Le catalogue est indisponible. Réessayez plus tard.";
      }
    });
})();
