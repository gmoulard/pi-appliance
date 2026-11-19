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
const authSection = document.getElementById("auth");
const authTitle = document.getElementById("authTitle");
const authHelp = document.getElementById("authHelp");
const authForm = document.getElementById("authForm");
const authButton = document.getElementById("authButton");
const logoutButton = document.getElementById("logout");
const emailReminder = document.getElementById("emailReminder");
const servicesSection = document.getElementById("servicesSection");
const catalogSection = document.getElementById("catalogSection");
const systemSection = document.getElementById("systemSection");
const gaugesBox = document.getElementById("gauges");
const TINTS = 6;
const ADMIN_PASSWORD_MIN = 12;
let currentAccount = null;
const CONTROLS = [
  { action: "start", label: "Démarrer", done: "démarré", when: (color) => color === "red" },
  { action: "stop", label: "Arrêter", done: "arrêté", when: (color) => color !== "red" },
  { action: "restart", label: "Redémarrer", done: "redémarré", when: (color) => color !== "red" },
  { action: "update", label: "Mettre à jour", done: "à jour", when: () => true },
];

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

const icons = {};
const SVG = "http://www.w3.org/2000/svg";

function tile(appId) {
  const box = element("span", undefined, "tile");
  box.setAttribute("aria-hidden", "true");
  const svg = document.createElementNS(SVG, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  const path = document.createElementNS(SVG, "path");
  path.setAttribute("d", icons[appId] || "");
  svg.append(path);
  box.append(svg);
  return box;
}

function cardHead(appId, title, detail) {
  const head = element("div", undefined, "card-head");
  const text = element("div");
  text.append(element("h3", title), detail);
  head.append(tile(appId), text);
  return head;
}

function showMessage(lines, kind) {
  message.replaceChildren(...lines.map((line) => element("p", line)));
  message.className = kind;
  message.hidden = false;
}

// Taille en Mo écrite comme on la dit, comme Size côté serveur : « 120 Mo », « 1,5 Go ».
function size(mb) {
  if (mb < 1024) {
    return `${mb} Mo`;
  }
  const tenths = Math.floor((mb * 10 + 512) / 1024);
  if (tenths % 10 === 0 || tenths >= 100) {
    return `${Math.floor((tenths + 5) / 10)} Go`;
  }
  return `${Math.floor(tenths / 10)},${tenths % 10} Go`;
}

function percent(part, total) {
  return `${Math.max(0, Math.min(100, (part / total) * 100))}%`;
}

// Jauge de mémoire ou de disque (spécification 6.2) : déjà utilisé, part du nouveau service
// (appName) et ce qui reste ; verte, orange ou rouge selon ce qui resterait.
function gaugeElement(gauge, appName) {
  const box = element("div", undefined, `gauge ${gauge.color}`);
  const head = element("div", undefined, "gauge-head");
  head.append(element("h3", gauge.title), element("span", `${size(gauge.total_mb)} en tout`, "meta"));
  const bar = element("div", undefined, "bar");
  bar.setAttribute("role", "img");
  bar.setAttribute("aria-label", gauge.summary);
  const used = element("span", undefined, "used");
  used.style.width = percent(gauge.used_mb, gauge.total_mb);
  bar.append(used);
  const legend = element("ul", undefined, "legend");
  const item = (className, text) => {
    const line = element("li");
    line.append(element("span", undefined, `swatch ${className}`), element("span", text));
    return line;
  };
  legend.append(item("used", `Déjà utilisé par le système et les services\u00a0: ${size(gauge.used_mb)}`));
  if (gauge.need_mb > 0) {
    const need = element("span", undefined, "need");
    need.style.width = percent(Math.min(gauge.need_mb, gauge.total_mb - gauge.used_mb), gauge.total_mb);
    bar.append(need);
    legend.append(item("need", `${appName}\u00a0: ${size(gauge.need_mb)}`));
  }
  legend.append(item("free", `Libre${gauge.need_mb > 0 ? " après l'installation" : ""}\u00a0: ${size(Math.max(gauge.left_mb, 0))}`));
  box.append(head, bar, legend, element("p", gauge.summary, "summary"));
  if (gauge.advice) {
    box.append(element("p", gauge.advice, "advice"));
  }
  return box;
}

async function loadSystem() {
  const { gauges } = await api("system");
  gaugesBox.replaceChildren(...gauges.map((gauge) => gaugeElement(gauge)));
  systemSection.hidden = gauges.length === 0 || !currentAccount || currentAccount.role !== "admin";
}

async function refresh() {
  await loadServices();
  if (currentAccount && currentAccount.role === "admin") {
    await loadSystem().catch(() => null);
  }
}

async function loadServices() {
  const { services } = await api("services");
  noServices.hidden = services.length > 0;
  servicesList.replaceChildren(...services.map((service) => {
    const card = element("li", undefined, "card");
    tint(card, service.app);
    const open = element("a", "Ouvrir", "button-outline");
    open.href = `https://${service.address}`;
    open.target = "_blank";
    open.rel = "noopener";
    const actions = element("div", undefined, "actions");
    actions.append(open);
    if (currentAccount && currentAccount.role === "admin") {
      const buttons = element("div", undefined, "controls");
      for (const control of CONTROLS.filter((c) => c.when(service.status.color))) {
        const button = element("button", control.label, "secondary");
        button.addEventListener("click", () => act(service, control, button));
        buttons.append(button);
      }
      const remove = element("button", "Désinstaller", "secondary");
      remove.addEventListener("click", () => uninstall(service, remove));
      buttons.append(remove);
      actions.append(buttons);
    }
    const state = element("p", undefined, "state");
    state.append(element("span", undefined, `dot ${service.status.color}`), element("span", service.status.message));
    const detail = element("div");
    detail.append(element("p", service.address, "address"), state);
    if (service.update_available) {
      detail.append(element("p", "Nouvelle version disponible.", "meta"));
    }
    card.append(cardHead(service.app, `${service.name} (${service.app_name})`, detail), actions);
    return card;
  }));
}

async function loadCatalog() {
  const { apps } = await api("catalog");
  for (const app of apps) {
    icons[app.id] = app.icon;
  }
  catalogList.replaceChildren(...apps.map((app) => {
    const card = element("li", undefined, "card app");
    tint(card, app.id);
    card.append(cardHead(app.id, app.name, element("p", app.description)));
    card.append(element("p", `Il demande ${size(app.memory_mb)} de mémoire et ${size(app.disk_mb)} de place sur le disque.`, "meta"));
    const sheet = element("div", undefined, "sheet");
    sheet.hidden = true;
    const gauges = element("div", undefined, "gauges");
    const form = element("form");
    const nameLabel = element("label", "Nom du service");
    const name = element("input");
    name.name = "name";
    name.value = app.subdomain;
    name.pattern = "[a-z]([a-z0-9\\-]{0,30}[a-z0-9])?";
    name.required = true;
    nameLabel.append(name);
    form.append(nameLabel);
    const asked = (app.parameters || []).filter((parameter) => !parameter.generate);
    for (const parameter of asked) {
      const label = element("label", parameter.label);
      const input = element("input");
      input.name = parameter.name;
      input.value = parameter.default || "";
      input.required = !parameter.default;
      label.append(input);
      form.append(label);
    }
    const buttons = element("div", undefined, "controls");
    const button = element("button", "Installer");
    const cancel = element("button", "Annuler", "secondary");
    cancel.type = "button";
    buttons.append(button, cancel);
    form.append(buttons);
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      install(app, form, button, close);
    });
    sheet.append(gauges, form);
    const open = element("button", "Installer…");
    open.type = "button";
    function close() {
      sheet.hidden = true;
      open.hidden = false;
    }
    cancel.addEventListener("click", close);
    open.addEventListener("click", async () => {
      open.disabled = true;
      try {
        const system = await api(`system?app=${encodeURIComponent(app.id)}`);
        gauges.replaceChildren(...system.gauges.map((gauge) => gaugeElement(gauge, app.name)));
      } catch (error) {
        gauges.replaceChildren(element("p", `Mémoire et disque inconnus : ${error.message}`, "meta"));
      } finally {
        open.disabled = false;
      }
      open.hidden = true;
      sheet.hidden = false;
      button.focus();
    });
    card.append(open, sheet, element("p", `${app.category} · licence ${app.license}`, "meta"));
    return card;
  }));
}

async function install(app, form, button, close) {
  const data = new FormData(form);
  const parameters = {};
  for (const parameter of (app.parameters || []).filter((p) => !p.generate)) {
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
    close();
    await refresh();
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
}

async function act(service, control, button) {
  if (control.action === "update" && !window.confirm(`Mettre à jour ${service.name} ? Le service sera coupé quelques minutes ; s'il ne redémarre pas, il reviendra tout seul à la version actuelle.`)) {
    return;
  }
  button.disabled = true;
  showMessage([`${control.label} ${service.name}…`], "info");
  try {
    const result = await api(`services/${encodeURIComponent(service.name)}/${control.action}`, { method: "POST" });
    if (control.action === "update") {
      const lines = [`${service.name} est à jour.`];
      for (const [label, value] of Object.entries(result.secrets || {})) {
        lines.push(`${label} : ${value} (notez-le, il ne sera plus affiché)`);
      }
      showMessage(lines, "success");
      return;
    }
    showMessage([`${service.name} est ${control.done} : ${result.message}`], result.color === "red" && control.action !== "stop" ? "error" : "success");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    await refresh().catch(() => null);
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
    await refresh();
  } catch (error) {
    showMessage([error.message], "error");
    button.disabled = false;
  }
}

// Écrans de compte : création du compte administrateur au premier démarrage,
// réinitialisation après dépôt du fichier reset, ou connexion (spécification 5.1, 8.5).
const AUTH_MODES = {
  setup: {
    title: "Bienvenue ! Créez votre compte administrateur",
    help: `C'est le compte qui gère l'appliance. Choisissez un mot de passe d'au moins ${ADMIN_PASSWORD_MIN} caractères : une phrase de plusieurs mots est plus facile à retenir. L'adresse e-mail servira à retrouver l'accès et à recevoir les alertes.`,
    button: "Créer le compte",
    fields: ["name", "email", "confirm"],
    path: "setup",
  },
  reset: {
    title: "Nouveau mot de passe administrateur",
    help: `Le fichier reset a été trouvé sur la carte SD. Choisissez un nouveau mot de passe d'au moins ${ADMIN_PASSWORD_MIN} caractères : vos services et vos données ne changent pas.`,
    button: "Changer le mot de passe",
    fields: ["confirm"],
    path: "reset",
  },
  login: {
    title: "Connexion",
    help: "Entrez votre identifiant et votre mot de passe.",
    button: "Se connecter",
    fields: ["name"],
    path: "login",
  },
};

let authMode = "login";

function showAuth(mode) {
  authMode = mode;
  const config = AUTH_MODES[mode];
  authTitle.textContent = config.title;
  authHelp.textContent = config.help;
  authButton.textContent = config.button;
  for (const field of ["name", "email", "confirm"]) {
    const shown = config.fields.includes(field);
    document.getElementById(`${field}Field`).hidden = !shown;
    authForm.elements[field].required = shown;
  }
  const password = authForm.elements.password;
  password.autocomplete = mode === "login" ? "current-password" : "new-password";
  password.minLength = mode === "login" ? 0 : ADMIN_PASSWORD_MIN;
  authForm.reset();
  authSection.hidden = false;
  servicesSection.hidden = true;
  catalogSection.hidden = true;
  systemSection.hidden = true;
  logoutButton.hidden = true;
  emailReminder.hidden = true;
}

authForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const config = AUTH_MODES[authMode];
  const data = new FormData(authForm);
  if (config.fields.includes("confirm") && data.get("password") !== data.get("confirm")) {
    showMessage(["Les deux mots de passe ne sont pas identiques."], "error");
    return;
  }
  const body = { password: data.get("password") };
  if (config.fields.includes("name")) {
    body.name = data.get("name");
  }
  if (config.fields.includes("email")) {
    body.email = data.get("email");
  }
  authButton.disabled = true;
  try {
    const account = await api(config.path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    message.hidden = true;
    await showConsole(account);
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    authButton.disabled = false;
  }
});

logoutButton.addEventListener("click", async () => {
  await api("logout", { method: "POST" }).catch(() => null);
  message.hidden = true;
  showAuth("login");
});

async function showConsole(account) {
  currentAccount = account;
  authSection.hidden = true;
  logoutButton.hidden = false;
  servicesSection.hidden = false;
  catalogSection.hidden = account.role !== "admin";
  emailReminder.hidden = account.email_verified;
  emailReminder.textContent = `Votre adresse ${account.email} sera vérifiée par un code envoyé par e-mail dès que l'appliance pourra en envoyer.`;
  await loadCatalog();
  await refresh();
}

api("health")
  .then(async (health) => {
    status.textContent = "Appliance en service.";
    statusDot.className = "dot ok";
    footer.textContent = `pi-appliance ${health.version}`;
    const session = await api("session");
    if (session.reset) {
      showAuth("reset");
    } else if (session.setup) {
      showAuth("setup");
    } else if (!session.account) {
      showAuth("login");
    } else {
      await showConsole(session.account);
    }
  })
  .catch((error) => {
    status.textContent = `Appliance injoignable : ${error.message}`;
    statusDot.className = "dot down";
  });
