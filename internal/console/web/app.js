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

const status = document.getElementById("status");
const message = document.getElementById("message");
const servicesList = document.getElementById("services");
const noServices = document.getElementById("noServices");
const catalogList = document.getElementById("catalog");
const footer = document.getElementById("version");
const statusDot = document.getElementById("statusDot");
const TINTS = 6;

async function api(path, options = {}) {
  const response = await fetch(`api/${path}`, options);
  if (response.status === 204) {
    return null;
  }
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(body.error || `Erreur ${response.status}`);
  }
  return body;
}

function element(tag, text, className) {
  const node = document.createElement(tag);
  if (text !== undefined) {
    node.textContent = text;
  }
  if (className) {
    node.className = className;
  }
  return node;
}

function tint(card, appId) {
  let sum = 0;
  for (const char of appId) {
    sum = (sum * 31 + char.codePointAt(0)) % 9973;
  }
  const n = sum % TINTS;
  card.style.setProperty("--tint", `var(--tint-${n})`);
  card.style.setProperty("--pale", `var(--tint-${n}-pale)`);
}

function cardHead(appId, appName, title, detail) {
  const head = element("div", undefined, "card-head");
  const tile = element("span", Array.from(appName)[0].toUpperCase(), "tile");
  tile.setAttribute("aria-hidden", "true");
  const text = element("div");
  text.append(element("h3", title), detail);
  head.append(tile, text);
  return head;
}

function showMessage(lines, kind) {
  message.replaceChildren(...lines.map((line) => element("p", line)));
  message.className = kind;
  message.hidden = false;
}

async function loadServices() {
  const { services } = await api("services");
  noServices.hidden = services.length > 0;
  servicesList.replaceChildren(...services.map((service) => {
    const card = element("li", undefined, "card");
    tint(card, service.app);
    const link = element("a", `https://${service.address}`);
    link.href = `https://${service.address}`;
    link.target = "_blank";
    link.rel = "noopener";
    const address = element("p", undefined, "address");
    address.append(link);
    const remove = element("button", "Désinstaller", "secondary");
    remove.addEventListener("click", () => uninstall(service, remove));
    card.append(cardHead(service.app, service.app_name, `${service.name} (${service.app_name})`, address), remove);
    return card;
  }));
}

async function loadCatalog() {
  const { apps } = await api("catalog");
  catalogList.replaceChildren(...apps.map((app) => {
    const card = element("li", undefined, "card app");
    tint(card, app.id);
    card.append(cardHead(app.id, app.name, app.name, element("p", app.description)), element("p", `${app.category} · licence ${app.license}`, "meta"));
    const form = element("form");
    const nameLabel = element("label", "Nom du service");
    const name = element("input");
    name.name = "name";
    name.value = app.subdomain;
    name.pattern = "[a-z]([a-z0-9\\-]{0,30}[a-z0-9])?";
    name.required = true;
    nameLabel.append(name);
    form.append(nameLabel);
    const asked = app.parameters.filter((parameter) => !parameter.generate);
    for (const parameter of asked) {
      const label = element("label", parameter.label);
      const input = element("input");
      input.name = parameter.name;
      input.value = parameter.default || "";
      input.required = !parameter.default;
      label.append(input);
      form.append(label);
    }
    const button = element("button", "Installer");
    form.append(button);
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      install(app, form, button);
    });
    card.append(form);
    return card;
  }));
}

async function install(app, form, button) {
  const data = new FormData(form);
  const parameters = {};
  for (const parameter of app.parameters.filter((p) => !p.generate)) {
    parameters[parameter.name] = data.get(parameter.name);
  }
  button.disabled = true;
  showMessage([`Installation de ${app.name}… Le premier téléchargement peut prendre plusieurs minutes.`], "info");
  try {
    const result = await api("services", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ app: app.id, name: data.get("name"), parameters }),
    });
    const lines = [`${app.name} est prêt : https://${result.instance.address}`];
    for (const [label, value] of Object.entries(result.secrets || {})) {
      lines.push(`${label} : ${value} (notez-le, il ne sera plus affiché)`);
    }
    lines.push(...(result.warnings || []));
    showMessage(lines, "success");
    await loadServices();
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
}

async function uninstall(service, button) {
  if (!window.confirm(`Désinstaller ${service.name} ?`)) {
    return;
  }
  const keep = window.confirm(`Garder les données de ${service.name} pour une prochaine installation ?\n\nOK : les garder\nAnnuler : les supprimer`);
  if (!keep && !window.confirm(`Les données de ${service.name} vont être supprimées définitivement. Continuer ?`)) {
    return;
  }
  button.disabled = true;
  try {
    await api(`services/${encodeURIComponent(service.name)}?data=${keep ? "keep" : "delete"}`, { method: "DELETE" });
    showMessage([`${service.name} est désinstallé${keep ? ", ses données sont gardées" : ""}.`], "success");
    await loadServices();
  } catch (error) {
    showMessage([error.message], "error");
    button.disabled = false;
  }
}

api("health")
  .then((health) => {
    status.textContent = "Appliance en service.";
    statusDot.className = "dot ok";
    footer.textContent = `pi-appliance ${health.version}`;
    return Promise.all([loadServices(), loadCatalog()]);
  })
  .catch((error) => {
    status.textContent = `Appliance injoignable : ${error.message}`;
    statusDot.className = "dot down";
  });
