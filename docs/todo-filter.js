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
  const showCompleted = document.getElementById("todo-show-completed");
  const showPending = document.getElementById("todo-show-pending");
  if (!showCompleted || !showPending) return;

  const items = Array.from(document.querySelectorAll(".prose li"))
    .map((item) => {
      const checkbox = Array.from(item.children).find(
        (child) => child instanceof HTMLInputElement && child.type === "checkbox",
      );
      return checkbox ? { item, checkbox } : null;
    })
    .filter(Boolean);

  function updateVisibility() {
    for (let index = items.length - 1; index >= 0; index -= 1) {
      const { item, checkbox } = items[index];
      const visibleByStatus = checkbox.checked ? showCompleted.checked : showPending.checked;
      const hasVisibleDescendant = Array.from(item.querySelectorAll("li"))
        .some((child) => child.dataset.todoVisible === "true");
      const visible = visibleByStatus || hasVisibleDescendant;
      item.hidden = !visible;
      item.dataset.todoVisible = String(visible);
      item.classList.toggle("todo-context", !visibleByStatus && hasVisibleDescendant);
    }
  }

  showCompleted.addEventListener("change", updateVisibility);
  showPending.addEventListener("change", updateVisibility);
  updateVisibility();
})();
