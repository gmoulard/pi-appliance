// This file is part of pi-appliance <https://github.com/gmoulard/pi-appliance>.
// Copyright (c) 2026 Guillaume Moulard.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3.

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
