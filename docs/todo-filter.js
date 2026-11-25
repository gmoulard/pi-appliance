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

// Filtres de la feuille de route (docs/TODO.md). Quatre états :
// « [ ] » et « [x] » deviennent des cases à cocher (Markdown de GitHub),
// « [~] » (en cours ou à vérifier) et « [!] » (bloqué) restent du texte au
// début de la ligne : ils sont remplacés ici par une pastille.
(() => {
  const filters = {
    done: document.getElementById("todo-show-completed"),
    progress: document.getElementById("todo-show-progress"),
    todo: document.getElementById("todo-show-pending"),
    blocked: document.getElementById("todo-show-blocked"),
  };
  if (!filters.done || !filters.todo) return;

  const markers = {
    "[~]": { state: "progress", label: "En cours ou à vérifier", symbol: "~" },
    "[!]": { state: "blocked", label: "Bloquée", symbol: "!" },
  };

  function stateOf(item) {
    const checkbox = Array.from(item.children).find(
      (child) => child instanceof HTMLInputElement && child.type === "checkbox",
    );
    if (checkbox) return checkbox.checked ? "done" : "todo";
    // Premier texte de la ligne (avant une éventuelle sous-liste).
    const first = item.firstChild;
    if (!first || first.nodeType !== Node.TEXT_NODE) return null;
    const text = first.textContent.replace(/^\s+/, "");
    const marker = markers[text.slice(0, 3)];
    if (!marker) return null;
    first.textContent = text.slice(3);
    const badge = document.createElement("span");
    badge.className = `todo-state todo-state-${marker.state}`;
    badge.textContent = marker.symbol;
    badge.title = marker.label;
    badge.setAttribute("aria-label", marker.label);
    badge.setAttribute("role", "img");
    item.insertBefore(badge, first);
    item.classList.add("task-list-item");
    return marker.state;
  }

  const items = Array.from(document.querySelectorAll(".prose li"))
    .map((item) => {
      const state = stateOf(item);
      return state ? { item, state } : null;
    })
    .filter(Boolean);

  // Nombre d'actions numérotées (TODO-x.y) par état, affiché dans chaque filtre.
  const counts = { done: 0, progress: 0, todo: 0, blocked: 0 };
  for (const { item, state } of items) {
    const strong = item.querySelector(":scope > strong");
    if (strong && /^TODO-\d+\.\d+$/.test(strong.textContent.trim())) counts[state] += 1;
  }
  for (const [state, input] of Object.entries(filters)) {
    if (!input || !input.parentElement) continue;
    const count = document.createElement("span");
    count.className = "todo-count";
    count.textContent = `(${counts[state]})`;
    input.parentElement.append(" ", count);
  }

  function shown(state) {
    const input = filters[state];
    return input ? input.checked : true;
  }

  function updateVisibility() {
    for (let index = items.length - 1; index >= 0; index -= 1) {
      const { item, state } = items[index];
      const visibleByStatus = shown(state);
      const hasVisibleDescendant = Array.from(item.querySelectorAll("li"))
        .some((child) => child.dataset.todoVisible === "true");
      const visible = visibleByStatus || hasVisibleDescendant;
      item.hidden = !visible;
      item.dataset.todoVisible = String(visible);
      item.classList.toggle("todo-context", !visibleByStatus && hasVisibleDescendant);
    }
  }

  for (const input of Object.values(filters)) {
    if (input) input.addEventListener("change", updateVisibility);
  }
  updateVisibility();
})();
