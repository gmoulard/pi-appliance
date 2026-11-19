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
  showServicesSummary(services);
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
      if (service.app === "site-web") {
        const upload = element("button", "Déposer un ZIP", "secondary");
        upload.addEventListener("click", () => chooseSiteArchive(service, upload));
        buttons.append(upload);
      }
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
    if (service.app === "site-web") {
      detail.append(element("p", `Nextcloud : dossier externe /srv/pi-appliance/services/${service.name}/www`, "meta"));
      detail.append(element("p", "FTP : compte pi sur le port 2121, sauf port personnalisé.", "meta"));
    }
    card.append(cardHead(service.app, `${service.name} (${service.app_name})`, detail), actions);
    return card;
  }));
}

// Jauges de la mémoire et du disque (spécification 6.2) : ce que le Pi a, ce
// qui est déjà pris, la part du nouveau service et ce qui resterait.
const piSection = document.getElementById("piSection");
const piGauges = document.getElementById("piGauges");
let resources = { memory: null, disk: null, usb_disk: null };
const GAUGE_TITLES = { memory: "Mémoire", disk: "Carte SD", usb: "Disque USB" };

function sizeText(mb) {
  if (Math.abs(mb) >= 1024) {
    return `${(mb / 1024).toLocaleString("fr-FR", { maximumFractionDigits: 1 })} Go`;
  }
  return `${Math.round(mb)} Mo`;
}

function gauge(kind, values, shareMB) {
  const memory = kind === "memory";
  const share = shareMB || 0;
  const left = values.free_mb - share;
  const color = left < 0 ? "red" : left < values.total_mb * 0.15 ? "orange" : "green";
  const box = element("div", undefined, `gauge ${color}`);
  const title = GAUGE_TITLES[kind];
  box.append(element("p", `${title} : ${sizeText(values.total_mb)} en tout, ${sizeText(values.used_mb)} déjà pris.`));
  const bar = element("div", undefined, "bar");
  bar.setAttribute("aria-hidden", "true");
  const used = element("span", undefined, "used");
  used.style.width = `${(100 * values.used_mb) / values.total_mb}%`;
  bar.append(used);
  if (share > 0) {
    const part = element("span", undefined, "share");
    part.style.width = `${(100 * Math.min(share, values.free_mb)) / values.total_mb}%`;
    bar.append(part);
  }
  box.append(bar);
  const what = memory ? "de mémoire" : kind === "usb" ? "de place sur le disque USB" : "de place sur la carte SD";
  if (share === 0) {
    box.append(element("p", `Il reste ${sizeText(values.free_mb)} ${what}.`, "meta"));
    // Alerte de remplissage (spécification 4) : la carte SD, ou le disque USB.
    if (!memory && color !== "green") {
      box.append(element("p", kind === "usb"
        ? "Le disque USB se remplit : désinstallez les services dont vous ne vous servez plus, ou utilisez un disque plus grand."
        : resources.usb_disk
          ? "La carte SD se remplit."
          : "La carte SD se remplit : branchez un disque USB, la console vous proposera d'y placer les données de vos services.", "warning"));
    }
  } else if (left >= 0) {
    box.append(element("p", `Ce service prendrait ${sizeText(share)} : il resterait ${sizeText(left)} ${what}.`, "meta"));
  } else {
    box.append(element("p", memory
      ? `Ce service demande ${sizeText(share)} de mémoire, il en manquerait ${sizeText(-left)} : il risque d'être lent. Arrêtez un autre service pour lui faire de la place.`
      : `Ce service demande ${sizeText(share)} sur le disque, il en manquerait ${sizeText(-left)} : il risque de ne pas s'installer. ${resources.usb_disk
        ? "Désinstallez un service dont vous ne vous servez plus."
        : "Branchez un disque USB pour avoir plus de place, ou désinstallez un service dont vous ne vous servez plus."}`, "warning"));
  }
  return box;
}

function gauges(memoryMB, diskMB) {
  const list = [];
  if (resources.memory) list.push(gauge("memory", resources.memory, memoryMB));
  // Les données des services vont sur le disque USB s'il y en a un.
  if (resources.usb_disk) {
    list.push(gauge("usb", resources.usb_disk, diskMB));
    if (resources.disk) list.push(gauge("disk", resources.disk, 0));
  } else if (resources.disk) {
    list.push(gauge("disk", resources.disk, diskMB));
  }
  return list;
}

async function loadResources() {
  resources = await api("system").catch(() => ({ memory: null, disk: null, usb_disk: null }));
  const list = gauges(0, 0);
  piGauges.replaceChildren(...list);
  const tiles = nowTiles();
  piNow.replaceChildren(...tiles);
  piSection.hidden = list.length + tiles.length === 0 || wizardStep !== null;
  if (!piSection.hidden) {
    await loadStorage();
    await loadHistory();
  }
}

// Tableau de bord (spécification 5.2) : charge du processeur et température
// en ce moment, état des services, graphiques des derniers jours.
const piNow = document.getElementById("piNow");
const piServices = document.getElementById("piServices");
const piHistory = document.getElementById("piHistory");
const piCharts = document.getElementById("piCharts");
const noHistory = document.getElementById("noHistory");
const periodButtons = [...document.querySelectorAll(".periods button")];
let historyHours = 24;
let dashboardTimer = null;

function number(value, digits = 0) {
  return value.toLocaleString("fr-FR", { maximumFractionDigits: digits });
}

function nowTile(title, value, advice, color) {
  const box = element("div", undefined, `tile-now ${color}`);
  box.append(element("p", title, "meta"), element("p", value, "value"));
  if (advice) {
    box.append(element("p", advice, color === "green" ? "meta" : "warning"));
  }
  return box;
}

function known(value) {
  return value !== null && value !== undefined;
}

function nowTiles() {
  const tiles = [];
  const cpu = resources.cpu_percent;
  if (known(cpu)) {
    const color = cpu >= 90 ? "orange" : "green";
    tiles.push(nowTile("Charge du processeur", `${number(cpu)} %`,
      color === "green" ? "Le Pi a de la marge." : "Le Pi travaille à plein : les services peuvent ralentir.", color));
  }
  const temperature = resources.temperature_c;
  if (known(temperature)) {
    const color = temperature >= 80 ? "red" : temperature >= 70 ? "orange" : "green";
    const advice = {
      green: "Température normale.",
      orange: "Le Pi chauffe : vérifiez qu'il est bien aéré, loin d'une source de chaleur.",
      red: "Le Pi est trop chaud : il ralentit pour se protéger. Mettez-le dans un endroit aéré ; un boîtier avec ventilateur aide.",
    }[color];
    tiles.push(nowTile("Température", `${number(temperature)} °C`, advice, color));
  }
  return tiles;
}

function plural(n, one, many) {
  return `${n} ${n > 1 ? many : one}`;
}

function showServicesSummary(services) {
  if (!currentAccount || currentAccount.role !== "admin" || services.length === 0) {
    piServices.hidden = true;
    return;
  }
  const count = (color) => services.filter((service) => service.status.color === color).length;
  const parts = [];
  if (count("green")) parts.push(plural(count("green"), "service en marche", "services en marche"));
  if (count("orange")) parts.push(plural(count("orange"), "service en difficulté", "services en difficulté"));
  if (count("red")) parts.push(plural(count("red"), "service arrêté", "services arrêtés"));
  const worst = count("red") ? "red" : count("orange") ? "orange" : "green";
  piServices.replaceChildren(element("span", undefined, `dot ${worst}`), element("span", `${parts.join(", ")}.`));
  piServices.hidden = false;
}

// Graphiques : une courbe par mesure, chacune avec sa propre échelle.
const CHARTS = [
  { title: "Charge du processeur", unit: "%", max: 100, series: [{ key: "cpu_percent" }] },
  { title: "Mémoire utilisée", unit: "%", max: 100, series: [{ key: "memory_percent" }] },
  { title: "Température, au plus haut de chaque heure", unit: "°C", series: [{ key: "temperature_c" }] },
  { title: "Disque utilisé", unit: "%", max: 100, series: [{ key: "disk_percent" }] },
  {
    title: "Réseau, par heure", unit: "Mo",
    series: [{ key: "received_mb", label: "reçus" }, { key: "sent_mb", label: "envoyés", dashed: true }],
  },
];
const CHART = { width: 600, height: 150, left: 44, right: 8, top: 10, bottom: 24 };
const HOUR = 3600 * 1000;

function svgNode(tag, attributes) {
  const node = document.createElementNS(SVG, tag);
  for (const [name, value] of Object.entries(attributes)) {
    node.setAttribute(name, value);
  }
  return node;
}

function dateText(time, hours) {
  const options = hours <= 24 ? { weekday: "short", hour: "2-digit", minute: "2-digit" } : { day: "numeric", month: "short" };
  return new Date(time).toLocaleString("fr-FR", options);
}

function chart(spec, points, start, end, hours) {
  const values = spec.series.flatMap((series) => points.map((point) => point[series.key]).filter(known));
  if (values.length === 0) {
    return null;
  }
  const top = spec.max || Math.max(1, ...values) * 1.15;
  const figure = element("figure", undefined, "chart");
  const caption = element("figcaption");
  caption.append(element("span", spec.title));
  if (spec.series.length > 1) {
    const legend = element("span", undefined, "legend");
    for (const series of spec.series) {
      legend.append(element("span", series.label, series.dashed ? "key dashed" : "key"));
    }
    caption.append(legend);
  }
  figure.append(caption);
  const { width, height, left, right, top: pad, bottom } = CHART;
  const x = (time) => left + ((time - start) / (end - start)) * (width - left - right);
  const y = (value) => height - bottom - (value / top) * (height - bottom - pad);
  const svg = svgNode("svg", { viewBox: `0 0 ${width} ${height}`, role: "img" });
  svg.setAttribute("aria-label", `${spec.title} : de ${number(Math.min(...values), 1)} à ${number(Math.max(...values), 1)} ${spec.unit}, dernière valeur ${number(values.at(-1), 1)} ${spec.unit}.`);
  for (const value of [0, top / 2, top]) {
    svg.append(svgNode("line", { x1: left, x2: width - right, y1: y(value), y2: y(value), class: "grid" }));
    const label = svgNode("text", { x: left - 6, y: y(value) + 4, class: "axis", "text-anchor": "end" });
    label.textContent = number(value, top < 10 ? 1 : 0);
    svg.append(label);
  }
  for (const [time, anchor] of [[start, "start"], [end, "end"]]) {
    const label = svgNode("text", { x: x(time), y: height - 6, class: "axis", "text-anchor": anchor });
    label.textContent = dateText(time, hours);
    svg.append(label);
  }
  for (const series of spec.series) {
    // Une heure sans mesure (Pi éteint) coupe la courbe.
    const segments = [];
    let previous = null;
    for (const point of points) {
      const value = point[series.key];
      const time = Date.parse(point.time);
      if (!known(value)) {
        previous = null;
        continue;
      }
      if (previous === null || time - previous > 1.5 * HOUR) {
        segments.push([]);
      }
      segments.at(-1).push([x(time).toFixed(1), y(value).toFixed(1)]);
      previous = time;
    }
    const className = series.dashed ? "line dashed" : "line";
    for (const segment of segments) {
      if (segment.length === 1) {
        // Une heure seule : un point, sinon rien ne se verrait.
        svg.append(svgNode("circle", { cx: segment[0][0], cy: segment[0][1], r: 4, class: `${className} dot-mark` }));
      } else {
        svg.append(svgNode("path", { d: segment.map(([px, py], i) => `${i ? "L" : "M"}${px},${py}`).join(""), class: className }));
      }
    }
  }
  const cross = svgNode("line", { y1: pad, y2: height - bottom, class: "cross", visibility: "hidden" });
  svg.append(cross);
  const tip = element("p", undefined, "tip");
  tip.textContent = "Passez le doigt ou la souris sur la courbe pour lire les valeurs.";
  svg.addEventListener("pointermove", (event) => {
    const box = svg.getBoundingClientRect();
    const at = start + (((event.clientX - box.left) * (width / box.width) - left) / (width - left - right)) * (end - start);
    let nearest = null;
    for (const point of points) {
      if (spec.series.some((series) => known(point[series.key]))
        && (!nearest || Math.abs(Date.parse(point.time) - at) < Math.abs(Date.parse(nearest.time) - at))) {
        nearest = point;
      }
    }
    if (!nearest) return;
    const px = x(Date.parse(nearest.time));
    cross.setAttribute("x1", px);
    cross.setAttribute("x2", px);
    cross.setAttribute("visibility", "visible");
    const parts = spec.series
      .filter((series) => known(nearest[series.key]))
      .map((series) => `${number(nearest[series.key], 1)} ${spec.unit}${series.label ? ` ${series.label}` : ""}`);
    const when = new Date(nearest.time).toLocaleString("fr-FR", { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
    tip.textContent = `${when} : ${parts.join(", ")}`;
  });
  svg.addEventListener("pointerleave", () => cross.setAttribute("visibility", "hidden"));
  figure.append(svg, tip);
  return figure;
}

async function loadHistory() {
  const history = await api("system/history").catch(() => null);
  if (!history) {
    piHistory.hidden = true;
    return;
  }
  piHistory.hidden = false;
  const end = Date.now();
  const start = end - historyHours * HOUR;
  const points = history.points.filter((point) => Date.parse(point.time) >= start - HOUR);
  const charts = CHARTS.map((spec) => chart(spec, points, start, end, historyHours)).filter(Boolean);
  piCharts.replaceChildren(...charts);
  noHistory.hidden = charts.length > 0;
}

for (const button of periodButtons) {
  button.addEventListener("click", async () => {
    historyHours = Number(button.dataset.hours);
    for (const other of periodButtons) {
      other.setAttribute("aria-pressed", String(other === button));
    }
    await loadHistory();
  });
}

function startDashboard() {
  clearInterval(dashboardTimer);
  dashboardTimer = setInterval(() => {
    if (!piSection.hidden) {
      loadResources().catch(() => null);
    }
  }, 60000);
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
    const fit = gauges(app.memory_mb, app.disk_mb);
    if (fit.length > 0) {
      const box = element("div");
      box.append(...fit);
      form.append(box);
    }
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
    await refreshResources();
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
}

async function refreshResources() {
  await loadResources();
  await loadCatalog();
}

function chooseSiteArchive(service, button) {
  const input = document.createElement("input");
  input.type = "file";
  input.accept = ".zip,application/zip";
  input.addEventListener("change", async () => {
    const file = input.files && input.files[0];
    if (!file) return;
    if (!window.confirm(`Remplacer tout le contenu du site ${service.name} par « ${file.name} » ?`)) return;
    button.disabled = true;
    showMessage([`Déploiement du ZIP sur ${service.name}…`], "info");
    const data = new FormData();
    data.append("archive", file);
    try {
      const result = await api(`services/${encodeURIComponent(service.name)}/files`, { method: "POST", body: data });
      showMessage([result.message], "success");
    } catch (error) {
      showMessage([error.message], "error");
    } finally {
      button.disabled = false;
    }
  });
  input.click();
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
    await refreshResources();
  } catch (error) {
    showMessage([error.message], "error");
    button.disabled = false;
  }
}

// Disque USB (spécification 4) : la console propose d'y placer les données des
// services. La copie arrête les services, et l'accès à la console avec eux.
const storageBox = document.getElementById("storageBox");
const storageState = document.getElementById("storageState");
const storageDisks = document.getElementById("storageDisks");
let storageTimer = null;

function diskName(disk) {
  const name = disk.model || disk.label || "Disque USB";
  return `${name} (${sizeText(disk.sizeMB)})`;
}

function watchStorage() {
  clearInterval(storageTimer);
  storageTimer = setInterval(async () => {
    const done = await loadStorage().catch(() => false);
    if (done) {
      clearInterval(storageTimer);
      storageTimer = null;
      await refreshResources().catch(() => null);
    }
  }, 10000);
}

async function startMove(path, body, disk) {
  try {
    await api(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    showMessage([
      `Copie des données sur ${diskName(disk)}… Vos services sont arrêtés pendant la copie, qui peut prendre de quelques minutes à une heure.`,
      "Cette page peut ne plus répondre pendant ce temps : revenez-y plus tard, elle se mettra à jour toute seule.",
    ], "info");
    watchStorage();
  } catch (error) {
    showMessage([error.message], "error");
  }
  await loadStorage().catch(() => null);
}

function useDisk(disk) {
  if (!window.confirm(`Placer les données de vos services sur ${diskName(disk)} ?\n\nVos services seront arrêtés pendant la copie. Ensuite, le disque devra rester branché : sans lui, les services ne démarrent pas.`)) {
    return;
  }
  startMove("storage/use", { partition: disk.partition }, disk);
}

function formatDisk(disk) {
  if (!window.confirm(`Tout le contenu de ${diskName(disk)} va être effacé, pour le préparer à recevoir les données de vos services. Continuer ?`)) {
    return;
  }
  if (!window.confirm(`Dernière vérification : les fichiers qui sont sur ${diskName(disk)} seront perdus définitivement. Effacer le disque ?`)) {
    return;
  }
  startMove("storage/format", { disk: disk.device, erase: true }, disk);
}

const MOVE_STATES = {
  starting: "Préparation de la copie des données sur le disque USB…",
  formatting: "Préparation du disque USB…",
  copying: "Copie des données sur le disque USB… Vos services sont arrêtés pendant la copie.",
};

// loadStorage rend vrai quand un déplacement vient de se terminer.
async function loadStorage() {
  let status;
  try {
    status = await api("storage");
  } catch {
    storageBox.hidden = true;
    return false;
  }
  storageBox.hidden = false;
  storageDisks.replaceChildren();
  const move = status.move;
  if (move && MOVE_STATES[move.state]) {
    storageState.textContent = MOVE_STATES[move.state];
    watchStorage();
    return false;
  }
  if (status.data) {
    storageState.textContent = "Les données de vos services sont sur le disque USB. Laissez-le branché : sans lui, les services ne démarrent pas.";
    if (move && move.state === "done" && storageTimer) {
      showMessage(["Les données de vos services sont maintenant sur le disque USB. Vos services ont redémarré."], "success");
      return true;
    }
    return false;
  }
  if (move && move.state === "failed" && storageTimer) {
    showMessage([move.message || "Le déplacement des données a échoué : elles restent sur la carte SD."], "error");
    clearInterval(storageTimer);
    storageTimer = null;
  }
  if (status.disks.length === 0) {
    storageState.textContent = "Les données de vos services sont sur la carte SD. Branchez un disque USB pour avoir plus de place : un disque dur ou un SSD est plus rapide et plus durable qu'une carte SD.";
    return false;
  }
  storageState.textContent = status.disks.length > 1
    ? "Les données de vos services sont sur la carte SD. Vous pouvez les placer sur l'un de ces disques USB :"
    : "Les données de vos services sont sur la carte SD. Vous pouvez les placer sur ce disque USB :";
  storageDisks.replaceChildren(...status.disks.map((disk) => {
    const item = element("li");
    const button = element("button");
    button.type = "button";
    const ready = disk.fstype === "ext4" && disk.partition;
    const detail = ready ? "prêt" : disk.fstype ? "à effacer avant usage" : "vide, à préparer";
    button.append(element("span", diskName(disk)), element("span", detail, "meta"));
    button.addEventListener("click", () => (ready ? useDisk(disk) : formatDisk(disk)));
    item.append(button);
    return item;
  }));
  return false;
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
const wifiCountry = document.getElementById("wifiCountry");
// Pays proposés : ceux des langues de l'interface (spécification 5.3) et les voisins
// de la France ; le code est celui de la norme ISO 3166.
const COUNTRIES = {
  DE: "Allemagne", SA: "Arabie saoudite", AT: "Autriche", BE: "Belgique", BR: "Brésil",
  CA: "Canada", CN: "Chine", DZ: "Algérie", EG: "Égypte", ES: "Espagne", US: "États-Unis",
  FR: "France", IN: "Inde", IR: "Iran", IT: "Italie", JP: "Japon", LU: "Luxembourg",
  MA: "Maroc", MC: "Monaco", NL: "Pays-Bas", PT: "Portugal", GB: "Royaume-Uni",
  RU: "Russie", SN: "Sénégal", CH: "Suisse", TN: "Tunisie",
};
wifiCountry.replaceChildren(...Object.entries(COUNTRIES)
  .sort((a, b) => a[1].localeCompare(b[1], "fr"))
  .map(([code, name]) => {
    const option = element("option", name);
    option.value = code;
    return option;
  }));
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
  if (state.country && !COUNTRIES[state.country]) {
    const option = element("option", state.country);
    option.value = state.country;
    wifiCountry.append(option);
  }
  wifiCountry.value = state.country || "FR";
  wifiCountry.dataset.current = wifiCountry.value;
}

wifiCountry.addEventListener("change", async () => {
  wifiCountry.disabled = true;
  try {
    const result = await api("network/country", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ country: wifiCountry.value }),
    });
    wifiCountry.dataset.current = result.country;
    showMessage([`Pays du Wi-Fi : ${COUNTRIES[result.country] || result.country}. Cherchez à nouveau les Wi-Fi si le vôtre n'apparaît pas.`], "success");
  } catch (error) {
    wifiCountry.value = wifiCountry.dataset.current;
    showMessage([error.message], "error");
  } finally {
    wifiCountry.disabled = false;
  }
});

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
    return null;
  }
  networkSection.hidden = false;
  showNetworkState(state);
  showWifiList(await api("network/wifi").catch(() => ({ networks: [] })));
  clearInterval(networkTimer);
  networkTimer = setInterval(showApCountdown, 30000);
  return state;
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
    const state = await loadNetwork();
    if (wizardStep === "network" && state && state.configured) {
      await wizardServices();
    }
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    wiredButton.disabled = false;
  }
});

// Assistant de premier démarrage (spécification 5.1) : compte administrateur,
// réseau de la maison, premiers services. Les autres étapes de la
// spécification (langue, adresse sur internet, utilisateurs, statistiques)
// viendront avec leurs fonctions.
const wizardSection = document.getElementById("wizard");
const wizardHelp = document.getElementById("wizardHelp");
const wizardNext = document.getElementById("wizardNext");
const wizardSkip = document.getElementById("wizardSkip");
const stepNetwork = document.getElementById("stepNetwork");
const stepServices = document.getElementById("stepServices");
let wizardStep = null;
let wizardTimer = null;

function setStep(step) {
  wizardStep = step;
  stepNetwork.className = step === "network" ? "current" : "done";
  stepServices.className = step === "services" ? "current" : "";
  clearInterval(wizardTimer);
}

async function startWizard() {
  wizardSection.hidden = false;
  servicesSection.hidden = true;
  catalogSection.hidden = true;
  piSection.hidden = true;
  setStep("network");
  const state = await loadNetwork();
  if (!state || state.configured) {
    await wizardServices();
    return;
  }
  wizardHelp.textContent = "Reliez le Pi à votre réseau : choisissez le Wi-Fi de la maison ci-dessous, ou le câble s'il est branché sur votre box.";
  wizardNext.hidden = true;
  wizardSkip.hidden = false;
  // Le choix se fait parfois sans réponse de la page (connexion au Wi-Fi) :
  // l'étape avance dès que l'état du réseau le montre.
  wizardTimer = setInterval(async () => {
    const current = await api("network").catch(() => null);
    if (current && current.configured && wizardStep === "network") {
      await loadNetwork();
      await wizardServices();
    }
  }, 5000);
}

async function wizardServices() {
  setStep("services");
  networkSection.hidden = true;
  wizardHelp.textContent = "Choisissez vos premiers services dans la marketplace ci-dessous. Vous pourrez en ajouter d'autres plus tard.";
  wizardSkip.hidden = true;
  wizardNext.textContent = "Terminer la mise en route";
  wizardNext.hidden = false;
  servicesSection.hidden = false;
  catalogSection.hidden = false;
  await loadResources();
  await loadCatalog();
  await loadServices();
}

wizardSkip.addEventListener("click", async () => {
  if (!window.confirm("Passer le choix du réseau ? Le Wi-Fi « pi-appliance » restera ouvert, au plus 24 heures, pour le faire plus tard.")) {
    return;
  }
  await wizardServices();
});

wizardNext.addEventListener("click", async () => {
  wizardNext.disabled = true;
  try {
    await api("wizard", { method: "POST" });
    wizardStep = null;
    wizardSection.hidden = true;
    showMessage(["Votre appliance est prête. Vous retrouverez tout ici : vos services, la marketplace et le réseau."], "success");
    await showConsole(currentAccount, false);
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    wizardNext.disabled = false;
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
  piSection.hidden = true;
  wizardSection.hidden = true;
  wizardStep = null;
  clearInterval(networkTimer);
  clearInterval(wizardTimer);
  clearInterval(dashboardTimer);
  clearInterval(storageTimer);
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
    const session = await api("session");
    await showConsole(account, session.wizard);
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

async function showConsole(account, wizard) {
  currentAccount = account;
  authSection.hidden = true;
  logoutButton.hidden = false;
  servicesSection.hidden = false;
  catalogSection.hidden = account.role !== "admin";
  emailReminder.hidden = account.email_verified;
  emailReminder.textContent = `Votre adresse ${account.email} sera vérifiée par un code envoyé par e-mail dès que l'appliance pourra en envoyer.`;
  if (account.role === "admin" && wizard) {
    await startWizard();
    return;
  }
  if (account.role === "admin") {
    await loadNetwork();
    await loadResources();
    startDashboard();
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
      await showConsole(session.account, session.wizard);
    }
  })
  .catch((error) => {
    status.textContent = `Appliance injoignable : ${error.message}`;
    statusDot.className = "dot down";
  });
