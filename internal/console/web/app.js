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
    const button = element("button", "Installer");
    form.append(button);
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      install(app, form, button);
    });
    card.append(form, element("p", `${app.category} · licence ${app.license}`, "meta"));
    return card;
  }));
}

async function install(app, form, button) {
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
    await loadServices();
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
    await loadServices().catch(() => null);
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

// Réseau de la maison (spécification, section 4) : choix du Wi-Fi ou du câble,
// depuis le point d'accès « pi-appliance » du premier démarrage ou plus tard.
const networkSection = document.getElementById("networkSection");
const networkState = document.getElementById("networkState");
const networkAp = document.getElementById("networkAp");
const networkCert = document.getElementById("networkCert");
const wifiList = document.getElementById("wifiList");
const noWifi = document.getElementById("noWifi");
const wifiForm = document.getElementById("wifiForm");
const wifiPasswordField = document.getElementById("wifiPasswordField");
const wifiScanButton = document.getElementById("wifiScan");
const wifiOtherButton = document.getElementById("wifiOther");
const wiredButton = document.getElementById("wired");
const LAST_ERRORS = {
  "wrong-password": "La dernière connexion a échoué : le Wi-Fi a refusé le mot de passe.",
  "not-found": "La dernière connexion a échoué : ce Wi-Fi est introuvable. Est-il allumé et assez proche ?",
  timeout: "La dernière connexion a échoué : le Wi-Fi ne répond pas.",
  failed: "La dernière connexion a échoué.",
};
const SECURITY = { open: "ouvert", wpa: "protégé", wpa3: "protégé", enterprise: "entreprise" };
let apClosesAt = 0;
let networkTimer = null;

function signalText(signal) {
  if (signal >= 70) return "très bon signal";
  if (signal >= 45) return "bon signal";
  if (signal >= 20) return "signal faible";
  return "signal très faible";
}

function minutesText(seconds) {
  const minutes = Math.max(1, Math.round(seconds / 60));
  if (minutes >= 120) return `${Math.round(minutes / 60)} heures`;
  return minutes === 1 ? "une minute" : `${minutes} minutes`;
}

function showApCountdown() {
  const seconds = Math.round((apClosesAt - Date.now()) / 1000);
  if (seconds > 0) {
    networkAp.textContent = `Le Wi-Fi de configuration « pi-appliance » se fermera dans ${minutesText(seconds)}. Après, redémarrez le Pi pour le rouvrir.`;
  }
}

function showNetworkState(state) {
  let line;
  if (state.accessPoint.open) {
    line = "Le Pi n'est pas encore relié à votre réseau : choisissez le Wi-Fi de la maison, ou branchez un câble.";
  } else if (state.wifi.connected) {
    line = `Le Pi est relié au Wi-Fi « ${state.wifi.ssid} ».`;
  } else if (state.wired.connected) {
    line = "Le Pi est relié à votre box par le câble réseau.";
  } else {
    line = "Le Pi n'est relié à aucun réseau.";
  }
  if (state.addresses.length > 0 && !state.accessPoint.open) {
    line += ` Adresse : ${state.addresses.join(", ")}.`;
  }
  const dot = element("span", undefined, `dot ${state.accessPoint.open ? "orange" : (state.wifi.connected || state.wired.connected) ? "green" : "red"}`);
  networkState.replaceChildren(dot, element("span", line));
  if (state.lastError) {
    showMessage([LAST_ERRORS[state.lastError] || LAST_ERRORS.failed], "error");
  }
  networkCert.hidden = !state.accessPoint.open;
  networkAp.hidden = !state.accessPoint.open;
  apClosesAt = state.accessPoint.open ? Date.now() + state.accessPoint.closesInSeconds * 1000 : 0;
  showApCountdown();
  wiredButton.hidden = !state.wired.connected || state.configured === "wired";
}

function chooseWifi(network, button) {
  for (const other of wifiList.querySelectorAll("button")) {
    other.setAttribute("aria-pressed", String(other === button));
  }
  wifiForm.elements.ssid.value = network ? network.ssid : "";
  wifiForm.elements.hidden.checked = !network;
  wifiForm.elements.password.value = "";
  const open = network && network.security === "open";
  wifiPasswordField.hidden = open;
  wifiForm.elements.password.required = !open;
  wifiForm.hidden = false;
  (network ? wifiForm.elements.password : wifiForm.elements.ssid).focus();
}

function showWifiList({ networks }) {
  noWifi.hidden = networks.length > 0;
  wifiList.replaceChildren(...networks.map((network) => {
    const item = element("li");
    const button = element("button");
    button.type = "button";
    button.setAttribute("aria-pressed", "false");
    button.append(element("span", network.ssid), element("span", `${SECURITY[network.security] || ""} · ${signalText(network.signal)}`, "meta"));
    if (network.security === "enterprise") {
      button.disabled = true;
      button.title = "Les Wi-Fi d'entreprise ne sont pas encore pris en charge.";
    } else {
      button.addEventListener("click", () => chooseWifi(network, button));
    }
    item.append(button);
    return item;
  }));
}

async function loadNetwork() {
  let state;
  try {
    state = await api("network");
  } catch {
    networkSection.hidden = true;
    return;
  }
  networkSection.hidden = false;
  showNetworkState(state);
  showWifiList(await api("network/wifi").catch(() => ({ networks: [] })));
  clearInterval(networkTimer);
  networkTimer = setInterval(showApCountdown, 30000);
}

wifiScanButton.addEventListener("click", async () => {
  if (apClosesAt && !window.confirm("Pour chercher les Wi-Fi, le Wi-Fi « pi-appliance » se coupe quelques secondes. Votre téléphone s'y reconnectera tout seul. Continuer ?")) {
    return;
  }
  wifiScanButton.disabled = true;
  showMessage(["Recherche des Wi-Fi…"], "info");
  try {
    showWifiList(await api("network/wifi/scan", { method: "POST" }));
    message.hidden = true;
  } catch (error) {
    showMessage([error.message, "Si la page ne répond plus, reconnectez-vous au Wi-Fi « pi-appliance »."], "error");
  } finally {
    wifiScanButton.disabled = false;
  }
});

wifiOtherButton.addEventListener("click", () => chooseWifi(null, null));

wifiForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const data = new FormData(wifiForm);
  const ssid = data.get("ssid");
  const button = wifiForm.querySelector("button");
  button.disabled = true;
  try {
    await api("network/wifi", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ssid, password: wifiPasswordField.hidden ? "" : data.get("password"), hidden: data.get("hidden") === "on" }),
    });
    wifiForm.reset();
    wifiForm.hidden = true;
    showMessage(apClosesAt ? [
      `Le Pi se connecte au Wi-Fi « ${ssid} ». Le Wi-Fi « pi-appliance » va se fermer.`,
      `Reconnectez votre téléphone ou votre ordinateur au Wi-Fi « ${ssid} », puis ouvrez https://pi-appliance.local/pi-appliance`,
      "Si le Wi-Fi « pi-appliance » réapparaît dans une minute, la connexion a échoué : reconnectez-vous-y pour réessayer.",
    ] : [`Le Pi se connecte au Wi-Fi « ${ssid} ». Si cette page ne répond plus, ouvrez https://pi-appliance.local/pi-appliance`], "info");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
});

wiredButton.addEventListener("click", async () => {
  wiredButton.disabled = true;
  try {
    await api("network/wired", { method: "POST" });
    showMessage(["C'est noté : le Pi utilise le câble réseau. Le Wi-Fi « pi-appliance » va se fermer : ouvrez ensuite https://pi-appliance.local/pi-appliance depuis votre réseau."], "success");
    await loadNetwork();
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    wiredButton.disabled = false;
  }
});

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
  logoutButton.hidden = true;
  emailReminder.hidden = true;
  networkSection.hidden = true;
  clearInterval(networkTimer);
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
  if (account.role === "admin") {
    await loadNetwork();
  }
  await loadCatalog();
  await loadServices();
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
