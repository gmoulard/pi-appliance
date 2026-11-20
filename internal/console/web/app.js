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
  { action: "start", label: t("Démarrer"), done: (name, state) => t("{0} est démarré : {1}", name, state), when: (color) => color === "red" },
  { action: "stop", label: t("Arrêter"), done: (name, state) => t("{0} est arrêté : {1}", name, state), when: (color) => color !== "red" },
  { action: "restart", label: t("Redémarrer"), done: (name, state) => t("{0} est redémarré : {1}", name, state), when: (color) => color !== "red" },
  { action: "update", label: t("Mettre à jour"), when: () => true },
];

async function api(path, options = {}) {
  const response = await fetch(`api/${path}`, options);
  if (response.status === 204) {
    return null;
  }
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(body.error || t("Erreur {0}", response.status));
    error.status = response.status;
    error.body = body;
    throw error;
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
    const open = element("a", t("Ouvrir"), "button-outline");
    open.href = `https://${service.address}`;
    open.target = "_blank";
    open.rel = "noopener";
    const actions = element("div", undefined, "actions");
    actions.append(open);
    if (currentAccount && currentAccount.role === "admin") {
      const buttons = element("div", undefined, "controls");
      if (service.app === "site-web") {
        const upload = element("button", t("Déposer un ZIP"), "secondary");
        upload.addEventListener("click", () => chooseSiteArchive(service, upload));
        buttons.append(upload);
      }
      // Un Compose libre ne se met pas à jour depuis la marketplace.
      for (const control of CONTROLS.filter((c) => c.when(service.status.color) && !(c.action === "update" && service.app === CUSTOM_APP))) {
        const button = element("button", control.label, "secondary");
        button.addEventListener("click", () => act(service, control, button));
        buttons.append(button);
      }
      const advanced = advancedButton(service, card);
      buttons.append(advanced.button);
      const remove = element("button", t("Désinstaller"), "secondary");
      remove.addEventListener("click", () => uninstall(service, remove));
      buttons.append(remove);
      actions.append(buttons, advanced.panel);
    }
    const state = element("p", undefined, "state");
    state.append(element("span", undefined, `dot ${service.status.color}`), element("span", service.status.message));
    const detail = element("div");
    detail.append(element("p", service.address, "address"), state);
    if (service.update_available) {
      detail.append(element("p", t("Nouvelle version disponible."), "meta"));
    }
    if (service.app === "site-web") {
      detail.append(element("p", t("Nextcloud : dossier externe /srv/pi-appliance/services/{0}/www", service.name), "meta"));
      detail.append(element("p", t("FTP : compte pi sur le port 2121, sauf port personnalisé."), "meta"));
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
const GAUGE_TITLES = { memory: t("Mémoire"), disk: t("Carte SD"), usb: t("Disque USB") };
const GAUGE_LEFT = {
  memory: (free) => t("Il reste {0} de mémoire.", free),
  disk: (free) => t("Il reste {0} de place sur la carte SD.", free),
  usb: (free) => t("Il reste {0} de place sur le disque USB.", free),
};
const GAUGE_AFTER = {
  memory: (share, left) => t("Ce service prendrait {0} : il resterait {1} de mémoire.", share, left),
  disk: (share, left) => t("Ce service prendrait {0} : il resterait {1} de place sur la carte SD.", share, left),
  usb: (share, left) => t("Ce service prendrait {0} : il resterait {1} de place sur le disque USB.", share, left),
};

function sizeText(mb) {
  if (Math.abs(mb) >= 1024) {
    return t("{0} Go", (mb / 1024).toLocaleString(LOCALE, { maximumFractionDigits: 1 }));
  }
  return t("{0} Mo", Math.round(mb));
}

function gauge(kind, values, shareMB) {
  const memory = kind === "memory";
  const share = shareMB || 0;
  const left = values.free_mb - share;
  const color = left < 0 ? "red" : left < values.total_mb * 0.15 ? "orange" : "green";
  const box = element("div", undefined, `gauge ${color}`);
  const title = GAUGE_TITLES[kind];
  box.append(element("p", t("{0} : {1} en tout, {2} déjà pris.", title, sizeText(values.total_mb), sizeText(values.used_mb))));
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
  if (share === 0) {
    box.append(element("p", GAUGE_LEFT[kind](sizeText(values.free_mb)), "meta"));
    // Alerte de remplissage (spécification 4) : la carte SD, ou le disque USB.
    if (!memory && color !== "green") {
      box.append(element("p", kind === "usb"
        ? t("Le disque USB se remplit : désinstallez les services dont vous ne vous servez plus, ou utilisez un disque plus grand.")
        : resources.usb_disk
          ? t("La carte SD se remplit.")
          : t("La carte SD se remplit : branchez un disque USB, la console vous proposera d'y placer les données de vos services."), "warning"));
    }
  } else if (left >= 0) {
    box.append(element("p", GAUGE_AFTER[kind](sizeText(share), sizeText(left)), "meta"));
  } else {
    box.append(element("p", memory
      ? t("Ce service demande {0} de mémoire, il en manquerait {1} : il risque d'être lent. Arrêtez un autre service pour lui faire de la place.", sizeText(share), sizeText(-left))
      : resources.usb_disk
        ? t("Ce service demande {0} sur le disque, il en manquerait {1} : il risque de ne pas s'installer. Désinstallez un service dont vous ne vous servez plus.", sizeText(share), sizeText(-left))
        : t("Ce service demande {0} sur le disque, il en manquerait {1} : il risque de ne pas s'installer. Branchez un disque USB pour avoir plus de place, ou désinstallez un service dont vous ne vous servez plus.", sizeText(share), sizeText(-left)), "warning"));
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
  return value.toLocaleString(LOCALE, { maximumFractionDigits: digits });
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
    tiles.push(nowTile(t("Charge du processeur"), `${number(cpu)} %`,
      color === "green" ? t("Le Pi a de la marge.") : t("Le Pi travaille à plein : les services peuvent ralentir."), color));
  }
  const temperature = resources.temperature_c;
  if (known(temperature)) {
    const color = temperature >= 80 ? "red" : temperature >= 70 ? "orange" : "green";
    const advice = {
      green: t("Température normale."),
      orange: t("Le Pi chauffe : vérifiez qu'il est bien aéré, loin d'une source de chaleur."),
      red: t("Le Pi est trop chaud : il ralentit pour se protéger. Mettez-le dans un endroit aéré ; un boîtier avec ventilateur aide."),
    }[color];
    tiles.push(nowTile(t("Température"), `${number(temperature)} °C`, advice, color));
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
  if (count("green")) parts.push(plural(count("green"), t("service en marche"), t("services en marche")));
  if (count("orange")) parts.push(plural(count("orange"), t("service en difficulté"), t("services en difficulté")));
  if (count("red")) parts.push(plural(count("red"), t("service arrêté"), t("services arrêtés")));
  const worst = count("red") ? "red" : count("orange") ? "orange" : "green";
  piServices.replaceChildren(element("span", undefined, `dot ${worst}`), element("span", `${parts.join(", ")}.`));
  piServices.hidden = false;
}

// Graphiques : une courbe par mesure, chacune avec sa propre échelle.
const CHARTS = [
  { title: t("Charge du processeur"), unit: "%", max: 100, series: [{ key: "cpu_percent" }] },
  { title: t("Mémoire utilisée"), unit: "%", max: 100, series: [{ key: "memory_percent" }] },
  { title: t("Température, au plus haut de chaque heure"), unit: "°C", series: [{ key: "temperature_c" }] },
  { title: t("Disque utilisé"), unit: "%", max: 100, series: [{ key: "disk_percent" }] },
  {
    title: t("Réseau, par heure"), unit: t("Mo"),
    series: [{ key: "received_mb", label: t("reçus") }, { key: "sent_mb", label: t("envoyés"), dashed: true }],
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
  return new Date(time).toLocaleString(LOCALE, options);
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
  svg.setAttribute("aria-label", t("{0} : de {1} à {2} {3}, dernière valeur {4} {5}.", spec.title, number(Math.min(...values), 1), number(Math.max(...values), 1), spec.unit, number(values.at(-1), 1), spec.unit));
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
  tip.textContent = t("Passez le doigt ou la souris sur la courbe pour lire les valeurs.");
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
    const when = new Date(nearest.time).toLocaleString(LOCALE, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
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
    const details = element("details", undefined, "install-details");
    const summary = element("summary", t("Installer"));
    details.append(summary);
    const form = element("form");
    const nameLabel = element("label", t("Nom du service"));
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
    const button = element("button", t("Installer"));
    const fit = gauges(app.memory_mb, app.disk_mb);
    if (fit.length > 0) {
      const box = element("div");
      box.append(...fit);
      form.append(box);
    }
    form.append(button);
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      install(app, form, button, details);
    });
    details.append(form);
    card.append(details, element("p", t("{0} · licence {1}", app.category, app.license), "meta"));
    return card;
  }));
  if (currentAccount && currentAccount.role === "admin") {
    catalogList.append(customComposeCard());
  }
}

async function install(app, form, button, details) {
  const data = new FormData(form);
  const parameters = {};
  for (const parameter of (app.parameters || []).filter((p) => !p.generate)) {
    parameters[parameter.name] = data.get(parameter.name);
  }
  button.disabled = true;
  showMessage([t("Installation de {0}… Le premier téléchargement peut prendre plusieurs minutes.", app.name)], "info");
  try {
    const result = await api("services", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ app: app.id, name: data.get("name"), parameters }),
    });
    const lines = [t("{0} est prêt : https://{1}", app.name, result.instance.address)];
    for (const [label, value] of Object.entries(result.secrets || {})) {
      lines.push(t("{0} : {1} (notez-le, il ne sera plus affiché)", label, value));
    }
    lines.push(...(result.warnings || []));
    showMessage(lines, "success");
    details.open = false;
    await loadServices();
    await refreshResources();
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
}

// Réglages avancés et Docker Compose libre (spécification 6.4) : écrans
// masqués par défaut, pour les administrateurs. Les options qui donnent la
// main sur tout le Pi sont signalées et doivent être confirmées, pas bloquées.
const CUSTOM_APP = "compose-libre";
icons[CUSTOM_APP] = "M8 6l-5 6 5 6M16 6l5 6-5 6M14 4l-4 16";

// withRisks envoie la demande ; si la console signale des options dangereuses,
// elle les montre et renvoie la demande une fois confirmée.
async function withRisks(send) {
  try {
    return await send(false);
  } catch (error) {
    const risks = error.body && error.body.risks;
    if (error.status !== 409 || !risks) {
      throw error;
    }
    const lines = risks.map((risk) => `• ${risk.message}`).join("\n");
    if (!window.confirm(t("Attention, ces options donnent la main sur tout le Pi :\n\n{0}\n\nUn service mal intentionné ou mal écrit pourrait alors abîmer le Pi ou lire toutes vos données. Continuer quand même ?", lines))) {
      return null;
    }
    return send(true);
  }
}

function linesOf(text) {
  return text.split("\n").map((line) => line.trim()).filter((line) => line && !line.startsWith("#"));
}

// parseSettings lit les trois zones de texte ; une ligne illisible lève une erreur.
function parseSettings(service, variables, ports, volumes) {
  const settings = { service, environment: {}, ports: [], volumes: [] };
  for (const line of linesOf(variables)) {
    const at = line.indexOf("=");
    if (at < 1) throw new Error(t("Variable illisible : « {0} » (attendu : NOM=valeur).", line));
    settings.environment[line.slice(0, at).trim()] = line.slice(at + 1).trim();
  }
  for (const line of linesOf(ports)) {
    const match = /^(\d+):(\d+)(?:\/(tcp|udp))?$/.exec(line);
    if (!match) throw new Error(t("Port illisible : « {0} » (attendu : port du Pi:port du conteneur, par exemple 8443:443 ou 5353:53/udp).", line));
    settings.ports.push({ host: Number(match[1]), container: Number(match[2]), protocol: match[3] || "tcp" });
  }
  for (const line of linesOf(volumes)) {
    const parts = line.split(":");
    const readOnly = parts.length === 3 && parts[2] === "ro";
    if (parts.length < 2 || parts.length > 3 || (parts.length === 3 && !readOnly)) {
      throw new Error(t("Volume illisible : « {0} » (attendu : source:dossier du conteneur, ou source:dossier:ro pour la lecture seule).", line));
    }
    settings.volumes.push({ source: parts[0], target: parts[1], read_only: readOnly });
  }
  return settings;
}

function textArea(label, name, value, placeholder) {
  const box = element("label", label);
  const area = element("textarea");
  area.name = name;
  area.rows = 3;
  area.value = value;
  area.placeholder = placeholder;
  area.spellcheck = false;
  area.autocapitalize = "none";
  box.append(area);
  return box;
}

async function advancedPanel(service, panel) {
  const { settings, services } = await api(`services/${encodeURIComponent(service.name)}/advanced`);
  const form = element("form", undefined, "advanced");
  form.append(element("p", t("Écran avancé : ces réglages changent le fonctionnement interne du service. S'il ne démarre plus avec eux, la console remet les réglages d'avant."), "note"));
  const which = element("label", t("Conteneur concerné"));
  const select = element("select");
  select.name = "service";
  for (const name of services) {
    const option = element("option", name);
    option.value = name;
    select.append(option);
  }
  select.value = settings.service;
  which.append(select);
  form.append(which,
    textArea(t("Variables (une par ligne : NOM=valeur)"), "variables",
      Object.entries(settings.environment || {}).map(([key, value]) => `${key}=${value}`).join("\n"), "TZ=Europe/Paris"),
    textArea(t("Ports ouverts directement sur le Pi, sans passer par l'adresse du service (port du Pi:port du conteneur)"), "ports",
      (settings.ports || []).map((p) => `${p.host}:${p.container}${p.protocol === "udp" ? "/udp" : ""}`).join("\n"), "8443:443"),
    textArea(t("Volumes (source:dossier du conteneur, :ro pour la lecture seule ; la source est un nom de volume ou un dossier du Pi)"), "volumes",
      (settings.volumes || []).map((v) => `${v.source}:${v.target}${v.read_only ? ":ro" : ""}`).join("\n"), "photos:/photos"));
  const apply = element("button", t("Appliquer les réglages"));
  form.append(apply);
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    let next;
    try {
      next = parseSettings(data.get("service"), data.get("variables"), data.get("ports"), data.get("volumes"));
    } catch (error) {
      showMessage([error.message], "error");
      return;
    }
    apply.disabled = true;
    showMessage([t("Application des réglages de {0}… Le service redémarre.", service.name)], "info");
    try {
      const result = await withRisks((confirm) => api(`services/${encodeURIComponent(service.name)}/advanced`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ settings: next, confirm_risks: confirm }),
      }));
      if (result) {
        showMessage([t("Les réglages de {0} sont appliqués.", service.name)], "success");
      } else {
        message.hidden = true;
      }
    } catch (error) {
      showMessage([error.message], "error");
    } finally {
      apply.disabled = false;
      await loadServices().catch(() => null);
    }
  });
  panel.replaceChildren(form);
}

function advancedButton(service, card) {
  const button = element("button", t("Réglages avancés"), "secondary");
  button.setAttribute("aria-expanded", "false");
  const panel = element("div");
  panel.hidden = true;
  button.addEventListener("click", async () => {
    const open = panel.hidden;
    panel.hidden = !open;
    button.setAttribute("aria-expanded", String(open));
    if (open) {
      try {
        await advancedPanel(service, panel);
      } catch (error) {
        panel.hidden = true;
        showMessage([error.message], "error");
      }
    }
  });
  return { button, panel };
}

// Carte « Docker Compose libre », à la fin de la marketplace.
function customComposeCard() {
  const card = element("li", undefined, "card app");
  tint(card, CUSTOM_APP);
  card.append(cardHead(CUSTOM_APP, t("Docker Compose libre"),
    element("p", t("Pour les personnes qui connaissent Docker : installez votre propre fichier Docker Compose, hors marketplace."))));
  const details = element("details");
  details.append(element("summary", t("Installer un fichier Docker Compose")));
  const form = element("form");
  form.append(element("p", t("Ce service ne sera ni vérifié ni pris en charge par le projet pi-appliance. Les images doivent être prêtes à l'emploi (pas de « build ») et exister pour le Pi (arm64)."), "note"));
  const nameLabel = element("label", t("Nom du service (son adresse)"));
  const name = element("input");
  name.name = "name";
  name.pattern = "[a-z]([a-z0-9\\-]{0,30}[a-z0-9])?";
  name.required = true;
  nameLabel.append(name);
  const composeLabel = element("label", t("Fichier compose.yaml"));
  const compose = element("textarea");
  compose.name = "compose";
  compose.rows = 10;
  compose.required = true;
  compose.spellcheck = false;
  compose.autocapitalize = "none";
  compose.placeholder = "services:\n  web:\n    image: nginx:1.27";
  composeLabel.append(compose);
  const serviceLabel = element("label", t("Conteneur à ouvrir à l'adresse du service"));
  const service = element("input");
  service.name = "service";
  service.required = true;
  service.placeholder = "web";
  serviceLabel.append(service);
  const portLabel = element("label", t("Son port dans le conteneur"));
  const port = element("input");
  port.name = "port";
  port.type = "number";
  port.min = 1;
  port.max = 65535;
  port.value = 80;
  port.required = true;
  portLabel.append(port);
  const button = element("button", t("Installer"));
  form.append(nameLabel, composeLabel, serviceLabel, portLabel, button);
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    button.disabled = true;
    showMessage([t("Installation de {0}… Le premier téléchargement peut prendre plusieurs minutes.", data.get("name"))], "info");
    try {
      const result = await withRisks((confirm) => api("compose", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: data.get("name"), compose: data.get("compose"), service: data.get("service"),
          port: Number(data.get("port")), confirm_risks: confirm,
        }),
      }));
      if (!result) {
        message.hidden = true;
        return;
      }
      showMessage([t("{0} est prêt : https://{1}", result.instance.name, result.instance.address), ...(result.warnings || [])], "success");
      form.reset();
      await loadServices();
      await refreshResources();
    } catch (error) {
      showMessage([error.message], "error");
    } finally {
      button.disabled = false;
    }
  });
  details.append(form);
  card.append(details);
  return card;
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
    if (!window.confirm(t("Remplacer tout le contenu du site {0} par « {1} » ?", service.name, file.name))) return;
    button.disabled = true;
    showMessage([t("Déploiement du ZIP sur {0}…", service.name)], "info");
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
  if (control.action === "update" && !window.confirm(t("Mettre à jour {0} ? Le service sera coupé quelques minutes ; s'il ne redémarre pas, il reviendra tout seul à la version actuelle.", service.name))) {
    return;
  }
  button.disabled = true;
  showMessage([`${control.label} ${service.name}…`], "info");
  try {
    const result = await api(`services/${encodeURIComponent(service.name)}/${control.action}`, { method: "POST" });
    if (control.action === "update") {
      const lines = [t("{0} est à jour.", service.name)];
      for (const [label, value] of Object.entries(result.secrets || {})) {
        lines.push(t("{0} : {1} (notez-le, il ne sera plus affiché)", label, value));
      }
      showMessage(lines, "success");
      return;
    }
    showMessage([control.done(service.name, result.message)], result.color === "red" && control.action !== "stop" ? "error" : "success");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    await loadServices().catch(() => null);
  }
}

async function uninstall(service, button) {
  if (!window.confirm(t("Désinstaller {0} ?", service.name))) {
    return;
  }
  const keep = window.confirm(t("Garder les données de {0} pour une prochaine installation ?\n\nOK : les garder\nAnnuler : les supprimer", service.name));
  if (!keep && !window.confirm(t("Les données de {0} vont être supprimées définitivement. Continuer ?", service.name))) {
    return;
  }
  button.disabled = true;
  try {
    await api(`services/${encodeURIComponent(service.name)}?data=${keep ? "keep" : "delete"}`, { method: "DELETE" });
    showMessage([keep ? t("{0} est désinstallé, ses données sont gardées.", service.name) : t("{0} est désinstallé.", service.name)], "success");
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
  const name = disk.model || disk.label || t("Disque USB");
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
      t("Copie des données sur {0}… Vos services sont arrêtés pendant la copie, qui peut prendre de quelques minutes à une heure.", diskName(disk)),
      t("Cette page peut ne plus répondre pendant ce temps : revenez-y plus tard, elle se mettra à jour toute seule."),
    ], "info");
    watchStorage();
  } catch (error) {
    showMessage([error.message], "error");
  }
  await loadStorage().catch(() => null);
}

function useDisk(disk) {
  if (!window.confirm(t("Placer les données de vos services sur {0} ?\n\nVos services seront arrêtés pendant la copie. Ensuite, le disque devra rester branché : sans lui, les services ne démarrent pas.", diskName(disk)))) {
    return;
  }
  startMove("storage/use", { partition: disk.partition }, disk);
}

function formatDisk(disk) {
  if (!window.confirm(t("Tout le contenu de {0} va être effacé, pour le préparer à recevoir les données de vos services. Continuer ?", diskName(disk)))) {
    return;
  }
  if (!window.confirm(t("Dernière vérification : les fichiers qui sont sur {0} seront perdus définitivement. Effacer le disque ?", diskName(disk)))) {
    return;
  }
  startMove("storage/format", { disk: disk.device, erase: true }, disk);
}

const MOVE_STATES = {
  starting: t("Préparation de la copie des données sur le disque USB…"),
  formatting: t("Préparation du disque USB…"),
  copying: t("Copie des données sur le disque USB… Vos services sont arrêtés pendant la copie."),
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
    storageState.textContent = t("Les données de vos services sont sur le disque USB. Laissez-le branché : sans lui, les services ne démarrent pas.");
    if (move && move.state === "done" && storageTimer) {
      showMessage([t("Les données de vos services sont maintenant sur le disque USB. Vos services ont redémarré.")], "success");
      return true;
    }
    return false;
  }
  if (move && move.state === "failed" && storageTimer) {
    showMessage([move.message || t("Le déplacement des données a échoué : elles restent sur la carte SD.")], "error");
    clearInterval(storageTimer);
    storageTimer = null;
  }
  if (status.disks.length === 0) {
    storageState.textContent = t("Les données de vos services sont sur la carte SD. Branchez un disque USB pour avoir plus de place : un disque dur ou un SSD est plus rapide et plus durable qu'une carte SD.");
    return false;
  }
  storageState.textContent = status.disks.length > 1
    ? t("Les données de vos services sont sur la carte SD. Vous pouvez les placer sur l'un de ces disques USB :")
    : t("Les données de vos services sont sur la carte SD. Vous pouvez les placer sur ce disque USB :");
  storageDisks.replaceChildren(...status.disks.map((disk) => {
    const item = element("li");
    const button = element("button");
    button.type = "button";
    const ready = disk.fstype === "ext4" && disk.partition;
    const detail = ready ? t("prêt") : disk.fstype ? t("à effacer avant usage") : t("vide, à préparer");
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
// Noms des pays dans la langue de l'interface, donnés par le navigateur.
const regionNames = new Intl.DisplayNames([LOCALE, "fr"], { type: "region" });
const COUNTRIES = Object.fromEntries(["DE", "SA", "AT", "BE", "BR", "CA", "CN", "DZ", "EG", "ES", "US", "FR", "IN", "IR",
  "IT", "JP", "LU", "MA", "MC", "NL", "PT", "GB", "RU", "SN", "CH", "TN", "TR"].map((code) => [code, regionNames.of(code)]));
wifiCountry.replaceChildren(...Object.entries(COUNTRIES)
  .sort((a, b) => a[1].localeCompare(b[1], LOCALE))
  .map(([code, name]) => {
    const option = element("option", name);
    option.value = code;
    return option;
  }));
const LAST_ERRORS = {
  "wrong-password": t("La dernière connexion a échoué : le Wi-Fi a refusé le mot de passe."),
  "not-found": t("La dernière connexion a échoué : ce Wi-Fi est introuvable. Est-il allumé et assez proche ?"),
  timeout: t("La dernière connexion a échoué : le Wi-Fi ne répond pas."),
  failed: t("La dernière connexion a échoué."),
};
const SECURITY = { open: t("ouvert"), wpa: t("protégé"), wpa3: t("protégé"), enterprise: t("entreprise") };
let apClosesAt = 0;
let networkTimer = null;

function signalText(signal) {
  if (signal >= 70) return t("très bon signal");
  if (signal >= 45) return t("bon signal");
  if (signal >= 20) return t("signal faible");
  return t("signal très faible");
}

function minutesText(seconds) {
  const minutes = Math.max(1, Math.round(seconds / 60));
  if (minutes >= 120) return t("{0} heures", Math.round(minutes / 60));
  return minutes === 1 ? t("une minute") : t("{0} minutes", minutes);
}

function showApCountdown() {
  const seconds = Math.round((apClosesAt - Date.now()) / 1000);
  if (seconds > 0) {
    networkAp.textContent = t("Le Wi-Fi de configuration « pi-appliance » se fermera dans {0}. Après, redémarrez le Pi pour le rouvrir.", minutesText(seconds));
  }
}

function showNetworkState(state) {
  let line;
  if (state.accessPoint.open) {
    line = t("Le Pi n'est pas encore relié à votre réseau : choisissez le Wi-Fi de la maison, ou branchez un câble.");
  } else if (state.wifi.connected) {
    line = t("Le Pi est relié au Wi-Fi « {0} ».", state.wifi.ssid);
  } else if (state.wired.connected) {
    line = t("Le Pi est relié à votre box par le câble réseau.");
  } else {
    line = t("Le Pi n'est relié à aucun réseau.");
  }
  if (state.addresses.length > 0 && !state.accessPoint.open) {
    line += t(" Adresse : {0}.", state.addresses.join(", "));
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
    showMessage([t("Pays du Wi-Fi : {0}. Cherchez à nouveau les Wi-Fi si le vôtre n'apparaît pas.", COUNTRIES[result.country] || result.country)], "success");
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
      button.title = t("Les Wi-Fi d'entreprise ne sont pas encore pris en charge.");
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
  if (apClosesAt && !window.confirm(t("Pour chercher les Wi-Fi, le Wi-Fi « pi-appliance » se coupe quelques secondes. Votre téléphone s'y reconnectera tout seul. Continuer ?"))) {
    return;
  }
  wifiScanButton.disabled = true;
  showMessage([t("Recherche des Wi-Fi…")], "info");
  try {
    showWifiList(await api("network/wifi/scan", { method: "POST" }));
    message.hidden = true;
  } catch (error) {
    showMessage([error.message, t("Si la page ne répond plus, reconnectez-vous au Wi-Fi « pi-appliance ».")], "error");
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
      t("Le Pi se connecte au Wi-Fi « {0} ». Le Wi-Fi « pi-appliance » va se fermer.", ssid),
      t("Reconnectez votre téléphone ou votre ordinateur au Wi-Fi « {0} », puis ouvrez https://pi-appliance.local/pi-appliance", ssid),
      t("Si le Wi-Fi « pi-appliance » réapparaît dans une minute, la connexion a échoué : reconnectez-vous-y pour réessayer."),
    ] : [t("Le Pi se connecte au Wi-Fi « {0} ». Si cette page ne répond plus, ouvrez https://pi-appliance.local/pi-appliance", ssid)], "info");
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
    showMessage([t("C'est noté : le Pi utilise le câble réseau. Le Wi-Fi « pi-appliance » va se fermer : ouvrez ensuite https://pi-appliance.local/pi-appliance depuis votre réseau.")], "success");
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

// Adresse sur internet (spécification 7.1) : le jeton n'est jamais relu ni
// affiché par l'API ; seul le formulaire le reçoit lors de son envoi.
const duckDNSSection = document.getElementById("duckdnsSection");
const duckDNSState = document.getElementById("duckdnsState");
const duckDNSDetails = document.getElementById("duckdnsDetails");
const duckDNSForm = document.getElementById("duckdnsForm");
const duckDNSSave = document.getElementById("duckdnsSave");
const duckDNSForget = document.getElementById("duckdnsForget");
let duckDNSTimer = null;
let duckDNSLoaded = false;

function duckDNSDate(value) {
  if (!value) return "";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) || date.getUTCFullYear() < 2 ? "" : date.toLocaleString(LOCALE);
}

function showDuckDNS(status) {
  duckDNSForget.hidden = !status.configured;
  duckDNSDetails.replaceChildren();
  if (!status.configured) {
    duckDNSState.replaceChildren(element("span", undefined, "dot orange"), element("span", t("DuckDNS n'est pas encore configuré.")));
    if (!duckDNSLoaded) {
      duckDNSForm.elements.domain.value = "";
      duckDNSLoaded = true;
    }
    return;
  }
  if (!duckDNSLoaded) duckDNSForm.elements.domain.value = status.domain || "";
  const lastAttempt = duckDNSDate(status.last_attempt);
  const lastSuccess = duckDNSDate(status.last_success);
  let state;
  let color;
  if (status.error) {
    state = lastAttempt
      ? t("La dernière mise à jour a échoué le {0} : {1}.", lastAttempt, status.error)
      : t("La dernière mise à jour a échoué : {0}.", status.error);
    color = "red";
  } else if (lastSuccess) {
    state = t("Dernière mise à jour réussie le {0}.", lastSuccess);
    color = "green";
  } else {
    state = t("La première mise à jour est en cours.");
    color = "orange";
  }
  duckDNSState.replaceChildren(element("span", undefined, `dot ${color}`), element("span", state));
  duckDNSDetails.append(element("p", t("Nom DuckDNS : {0}.", status.hostname || `${status.domain}.duckdns.org`), "meta"));
  if (status.ip) {
    duckDNSDetails.append(element("p", t("Adresse envoyée à DuckDNS : {0}.", status.ip), "meta"));
  }
  if (status.error && status.next_attempt) {
    const nextAttempt = duckDNSDate(status.next_attempt);
    if (nextAttempt) duckDNSDetails.append(element("p", t("Nouvel essai prévu le {0}.", nextAttempt), "meta"));
  }
  duckDNSLoaded = true;
}

async function loadDuckDNS() {
  try {
    showDuckDNS(await api("duckdns"));
  } catch (error) {
    duckDNSState.replaceChildren(element("span", undefined, "dot red"), element("span", error.message));
    duckDNSLoaded = true;
  }
}

duckDNSForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  duckDNSSave.disabled = true;
  try {
    const data = new FormData(duckDNSForm);
    const result = await api("duckdns", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ domain: data.get("domain"), token: data.get("token") }),
    });
    duckDNSForm.elements.token.value = "";
    duckDNSLoaded = true;
    showDuckDNS(result);
    showMessage([t("Les réglages DuckDNS sont enregistrés. La mise à jour démarre.")], "success");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    duckDNSSave.disabled = false;
  }
});

duckDNSForget.addEventListener("click", async () => {
  if (!window.confirm(t("Effacer les réglages DuckDNS ?"))) return;
  duckDNSForget.disabled = true;
  try {
    await api("duckdns", { method: "DELETE" });
    duckDNSForm.reset();
    duckDNSLoaded = true;
    await loadDuckDNS();
    showMessage([t("Les réglages DuckDNS sont effacés.")], "success");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    duckDNSForget.disabled = false;
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
  wizardHelp.textContent = t("Reliez le Pi à votre réseau : choisissez le Wi-Fi de la maison ci-dessous, ou le câble s'il est branché sur votre box.");
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
  wizardHelp.textContent = t("Choisissez vos premiers services dans la marketplace ci-dessous. Vous pourrez en ajouter d'autres plus tard.");
  wizardSkip.hidden = true;
  wizardNext.textContent = t("Terminer la mise en route");
  wizardNext.hidden = false;
  servicesSection.hidden = false;
  catalogSection.hidden = false;
  await loadResources();
  await loadCatalog();
  await loadServices();
}

wizardSkip.addEventListener("click", async () => {
  if (!window.confirm(t("Passer le choix du réseau ? Le Wi-Fi « pi-appliance » restera ouvert, au plus 24 heures, pour le faire plus tard."))) {
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
    showMessage([t("Votre appliance est prête. Vous retrouverez tout ici : vos services, la marketplace et le réseau.")], "success");
    await showConsole(currentAccount, false);
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    wizardNext.disabled = false;
  }
});

// Comptes créés par l'administrateur, invitations, double authentification et
// retour vers un service protégé après la connexion (spécification 8.1, 8.3).
const accountSection = document.getElementById("accountSection");
const accountsSection = document.getElementById("accountsSection");
const accountsList = document.getElementById("accountsList");
const accountForm = document.getElementById("accountForm");
const expiresField = document.getElementById("expiresField");
const inviteBox = document.getElementById("inviteBox");
const inviteHelp = document.getElementById("inviteHelp");
const inviteLink = document.getElementById("inviteLink");
const inviteCopy = document.getElementById("inviteCopy");
const totpState = document.getElementById("totpState");
const totpStart = document.getElementById("totpStart");
const totpSetup = document.getElementById("totpSetup");
const totpQR = document.getElementById("totpQR");
const totpSecret = document.getElementById("totpSecret");
const totpConfirm = document.getElementById("totpConfirm");
const totpDisable = document.getElementById("totpDisable");
const recoveryBox = document.getElementById("recoveryBox");
const recoveryList = document.getElementById("recoveryCodes");
const codeField = document.getElementById("codeField");
const USER_PASSWORD_MIN = 8;
const DAY = 24 * 3600 * 1000;
const ROLE_NAMES = { admin: t("Administrateur"), user: t("Utilisateur"), guest: t("Invité") };
// ?return= : adresse du service protégé qui a renvoyé vers la connexion.
let returnTo = new URLSearchParams(window.location.search).get("return");
let inviteToken = null;

function jsonBody(method, body) {
  return { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) };
}

function needsTOTP(account) {
  return account.totp_required && !account.totp;
}

// Ouvre le service demandé ; faux si l'adresse est refusée (la console s'affiche alors).
async function goBackToService() {
  try {
    const { url } = await api("auth/return", jsonBody("POST", { return: returnTo }));
    window.location.assign(url);
    return true;
  } catch (error) {
    showMessage([error.message], "error");
    returnTo = null;
    window.history.replaceState(null, "", window.location.pathname);
    return false;
  }
}

async function startInvitation(token) {
  try {
    const account = await api("invitation/check", jsonBody("POST", { token }));
    inviteToken = token;
    showAuth("invite");
    const minimum = account.role === "admin" ? ADMIN_PASSWORD_MIN : USER_PASSWORD_MIN;
    authTitle.textContent = t("Bienvenue {0} !", account.name);
    authHelp.textContent = t("Choisissez votre mot de passe, d'au moins {0} caractères : une phrase de plusieurs mots est plus facile à retenir.", minimum);
    authForm.elements.password.minLength = minimum;
    return true;
  } catch (error) {
    window.history.replaceState(null, "", window.location.pathname + window.location.search);
    showMessage([error.message], "error");
    return false;
  }
}

function showAccount(account) {
  accountSection.hidden = false;
  totpSetup.hidden = true;
  totpStart.hidden = account.totp;
  totpDisable.hidden = !account.totp || account.totp_required;
  totpDisable.reset();
  if (account.totp) {
    totpState.textContent = t("La double authentification est activée : à chaque connexion, la console demande aussi le code affiché par votre application. Codes de secours restants : {0}.", account.recovery_codes);
  } else if (account.totp_required) {
    totpState.textContent = t("L'administrateur demande la double authentification pour votre compte : activez-la pour continuer.");
  } else {
    totpState.textContent = t("La double authentification protège votre compte même si quelqu'un apprend votre mot de passe : la console demande en plus un code affiché par une application de votre téléphone.");
  }
}

totpStart.addEventListener("click", async () => {
  totpStart.disabled = true;
  try {
    const setup = await api("account/totp", { method: "POST" });
    totpQR.src = setup.qr;
    totpSecret.textContent = t("Sans appareil photo, saisissez cette clé dans l'application : {0}", setup.secret);
    totpConfirm.reset();
    recoveryBox.hidden = true;
    totpSetup.hidden = false;
    totpStart.hidden = true;
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    totpStart.disabled = false;
  }
});

totpConfirm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = totpConfirm.querySelector("button");
  button.disabled = true;
  try {
    const { recovery_codes: codes } = await api("account/totp/confirm", jsonBody("POST", { code: new FormData(totpConfirm).get("code") }));
    showMessage([t("La double authentification est activée.")], "success");
    // Rester ici le temps de noter les codes de secours, sans repartir vers un service.
    returnTo = null;
    const session = await api("session");
    await showConsole(session.account, session.wizard);
    recoveryList.replaceChildren(...codes.map((code) => element("li", code)));
    recoveryBox.hidden = false;
    recoveryBox.scrollIntoView({ block: "nearest" });
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
});

totpDisable.addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = totpDisable.querySelector("button");
  button.disabled = true;
  try {
    await api("account/totp/disable", jsonBody("POST", { password: new FormData(totpDisable).get("password") }));
    showMessage([t("La double authentification est désactivée.")], "success");
    const session = await api("session");
    currentAccount = session.account;
    recoveryBox.hidden = true;
    showAccount(session.account);
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
});

function dayText(time) {
  return new Date(time).toLocaleDateString(LOCALE, { dateStyle: "long" });
}

function showInvite(reply) {
  inviteBox.hidden = false;
  inviteLink.hidden = reply.sent;
  inviteCopy.hidden = reply.sent;
  inviteLink.value = reply.link || "";
  inviteHelp.textContent = reply.sent
    ? t("Invitation envoyée par e-mail à {0}.", reply.account.email)
    : t("L'appliance ne sait pas encore envoyer d'e-mail : transmettez vous-même ce lien à {0}. Il est valable 7 jours et ne sert qu'une fois.", reply.account.email);
  inviteBox.scrollIntoView({ block: "nearest" });
}

inviteCopy.addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText(inviteLink.value);
    showMessage([t("Lien copié.")], "success");
  } catch {
    inviteLink.select();
  }
});

async function updateAccount(account, changes) {
  const body = { role: account.role, expires: account.expires_at || null, totp_required: account.totp_required, ...changes };
  await api(`accounts/${encodeURIComponent(account.name)}`, jsonBody("PUT", body));
  await loadAccounts();
}

function accountCard(account) {
  const card = element("li", undefined, "card");
  card.append(element("h3", `${account.name} (${ROLE_NAMES[account.role] || account.role})`), element("p", account.email, "address"));
  const facts = [];
  if (account.invited) {
    facts.push(t("Invitation en attente : pas encore de mot de passe."));
  }
  if (account.expires_at) {
    // expires_at est le début du jour qui suit le dernier jour d'accès.
    const ended = new Date(account.expires_at) <= new Date();
    facts.push(ended ? t("Accès terminé depuis le {0}.", dayText(account.expires_at)) : t("Accès jusqu'au {0} inclus.", dayText(new Date(account.expires_at).getTime() - 1)));
  }
  if (account.totp) {
    facts.push(t("Double authentification activée."));
  } else if (account.totp_required) {
    facts.push(t("Double authentification exigée, pas encore activée."));
  } else {
    facts.push(t("Sans double authentification."));
  }
  for (const fact of facts) {
    card.append(element("p", fact, "meta"));
  }
  const buttons = element("div", undefined, "controls");
  const add = (label, run) => {
    const button = element("button", label, "secondary");
    button.type = "button";
    button.addEventListener("click", async () => {
      button.disabled = true;
      try {
        await run();
      } catch (error) {
        showMessage([error.message], "error");
      } finally {
        button.disabled = false;
      }
    });
    buttons.append(button);
  };
  const path = `accounts/${encodeURIComponent(account.name)}`;
  add(account.invited ? t("Nouveau lien d'invitation") : t("Lien pour changer de mot de passe"), async () => {
    showInvite(await api(`${path}/invitation`, { method: "POST" }));
    await loadAccounts();
  });
  if (account.role === "guest") {
    add(t("Prolonger d'une semaine"), () => {
      const from = Math.max(Date.now(), new Date(account.expires_at).getTime());
      return updateAccount(account, { expires: new Date(from + 7 * DAY).toISOString() });
    });
  }
  add(account.totp_required ? t("Ne plus exiger la double authentification") : t("Exiger la double authentification"), () => updateAccount(account, { totp_required: !account.totp_required }));
  if (account.totp) {
    add(t("Retirer la double authentification"), async () => {
      if (!window.confirm(t("Retirer la double authentification de {0} ? À faire seulement si son téléphone et ses codes de secours sont perdus.", account.name))) return;
      await api(`${path}/totp`, { method: "DELETE" });
      await loadAccounts();
    });
  }
  if (account.name !== currentAccount.name) {
    add(t("Supprimer"), async () => {
      if (!window.confirm(t("Supprimer le compte {0} ? La personne ne pourra plus se connecter.", account.name))) return;
      await api(path, { method: "DELETE" });
      await loadAccounts();
    });
  }
  card.append(buttons);
  return card;
}

async function loadAccounts() {
  const { accounts } = await api("accounts");
  accountsList.replaceChildren(...accounts.map(accountCard));
}

accountForm.elements.role.addEventListener("change", () => {
  const guest = accountForm.elements.role.value === "guest";
  expiresField.hidden = !guest;
  accountForm.elements.expires.required = guest;
});

accountForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const data = new FormData(accountForm);
  const body = { name: data.get("name"), email: data.get("email"), role: data.get("role"), totp_required: data.get("totp_required") === "on" };
  if (body.role === "guest") {
    // Dernier jour d'accès compris : fin au début du jour suivant, à l'heure de ce navigateur.
    const [year, month, day] = String(data.get("expires")).split("-").map(Number);
    body.expires = new Date(year, month - 1, day + 1).toISOString();
  }
  const button = accountForm.querySelector("button");
  button.disabled = true;
  try {
    const reply = await api("accounts", jsonBody("POST", body));
    accountForm.reset();
    expiresField.hidden = true;
    accountForm.elements.expires.required = false;
    await loadAccounts();
    showInvite(reply);
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
});

// Écrans de compte : création du compte administrateur au premier démarrage,
// réinitialisation après dépôt du fichier reset, ou connexion (spécification 5.1, 8.5).
const AUTH_MODES = {
  setup: {
    title: t("Bienvenue ! Créez votre compte administrateur"),
    help: t("C'est le compte qui gère l'appliance. Choisissez un mot de passe d'au moins {0} caractères : une phrase de plusieurs mots est plus facile à retenir. L'adresse e-mail servira à retrouver l'accès et à recevoir les alertes.", ADMIN_PASSWORD_MIN),
    button: t("Créer le compte"),
    fields: ["name", "email", "confirm"],
    path: "setup",
  },
  reset: {
    title: t("Nouveau mot de passe administrateur"),
    help: t("Le fichier reset a été trouvé sur la carte SD. Choisissez un nouveau mot de passe d'au moins {0} caractères : vos services et vos données ne changent pas.", ADMIN_PASSWORD_MIN),
    button: t("Changer le mot de passe"),
    fields: ["confirm"],
    path: "reset",
  },
  login: {
    title: t("Connexion"),
    help: t("Entrez votre identifiant et votre mot de passe."),
    button: t("Se connecter"),
    fields: ["name"],
    path: "login",
  },
  // Lien d'invitation (#invite=…) : le titre et l'aide sont complétés par startInvitation.
  invite: {
    title: "",
    help: "",
    button: t("Choisir mon mot de passe"),
    fields: ["confirm"],
    path: "invitation",
  },
};

let authMode = "login";

function showAuth(mode) {
  authMode = mode;
  const config = AUTH_MODES[mode];
  authTitle.textContent = config.title;
  authHelp.textContent = config.help;
  authButton.textContent = config.button;
  for (const field of ["name", "email", "confirm", "code"]) {
    const shown = config.fields.includes(field);
    document.getElementById(`${field}Field`).hidden = !shown;
    authForm.elements[field].required = shown;
  }
  const password = authForm.elements.password;
  password.autocomplete = mode === "login" ? "current-password" : "new-password";
  password.minLength = mode === "login" ? 0 : ADMIN_PASSWORD_MIN;
  authForm.reset();
  duckDNSForm.reset();
  duckDNSLoaded = false;
  authSection.hidden = false;
  servicesSection.hidden = true;
  catalogSection.hidden = true;
  logoutButton.hidden = true;
  emailReminder.hidden = true;
  networkSection.hidden = true;
  duckDNSSection.hidden = true;
  piSection.hidden = true;
  accountSection.hidden = true;
  accountsSection.hidden = true;
  inviteBox.hidden = true;
  recoveryBox.hidden = true;
  wizardSection.hidden = true;
  wizardStep = null;
  clearInterval(networkTimer);
  clearInterval(duckDNSTimer);
  duckDNSTimer = null;
  clearInterval(wizardTimer);
  clearInterval(dashboardTimer);
  clearInterval(storageTimer);
}

authForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const config = AUTH_MODES[authMode];
  const data = new FormData(authForm);
  if (config.fields.includes("confirm") && data.get("password") !== data.get("confirm")) {
    showMessage([t("Les deux mots de passe ne sont pas identiques.")], "error");
    return;
  }
  const body = { password: data.get("password") };
  if (config.fields.includes("name")) {
    body.name = data.get("name");
  }
  if (config.fields.includes("email")) {
    body.email = data.get("email");
  }
  if (authMode === "login" && !codeField.hidden) {
    body.code = data.get("code");
  }
  if (authMode === "invite") {
    body.token = inviteToken;
  }
  authButton.disabled = true;
  try {
    const account = await api(config.path, jsonBody("POST", body));
    message.hidden = true;
    if (authMode === "invite") {
      inviteToken = null;
      window.history.replaceState(null, "", window.location.pathname + window.location.search);
      if (account.totp) {
        // Compte déjà protégé : connexion normale, avec le code.
        showAuth("login");
        showMessage([t("Mot de passe enregistré : connectez-vous.")], "success");
        return;
      }
    }
    const session = await api("session");
    await showConsole(session.account || account, session.wizard);
  } catch (error) {
    if (authMode === "login" && error.body && error.body.code_needed) {
      codeField.hidden = false;
      authForm.elements.code.required = true;
      authForm.elements.code.value = "";
      authForm.elements.code.focus();
    }
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
  duckDNSSection.hidden = true;
  clearInterval(duckDNSTimer);
  duckDNSTimer = null;
  emailReminder.hidden = account.email_verified;
  emailReminder.textContent = t("Votre adresse {0} sera vérifiée par un code envoyé par e-mail dès que l'appliance pourra en envoyer.", account.email);
  showAccount(account);
  accountsSection.hidden = true;
  if (needsTOTP(account)) {
    // Double authentification exigée : rien d'autre tant qu'elle n'est pas activée.
    servicesSection.hidden = true;
    catalogSection.hidden = true;
    return;
  }
  if (returnTo && (await goBackToService())) {
    return;
  }
  if (account.role === "admin" && wizard) {
    await startWizard();
    return;
  }
  if (account.role === "admin") {
    duckDNSSection.hidden = false;
    await loadDuckDNS();
    duckDNSTimer = setInterval(loadDuckDNS, 15000);
    await loadNetwork();
    await loadResources();
    startDashboard();
    accountsSection.hidden = false;
    await loadAccounts();
  }
  await loadCatalog();
  await loadServices();
}

api("health")
  .then(async (health) => {
    status.textContent = t("Appliance en service.");
    statusDot.className = "dot ok";
    footer.textContent = `pi-appliance ${health.version}`;
    const invite = window.location.hash.match(/^#invite=([A-Za-z0-9_-]+)$/);
    if (invite && (await startInvitation(invite[1]))) {
      return;
    }
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
    status.textContent = t("Appliance injoignable : {0}", error.message);
    statusDot.className = "dot down";
  });
