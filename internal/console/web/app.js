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
const catalogSource = document.getElementById("catalogSource");
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
  // X-Pi-Appliance : exigé par la console sur ce qui modifie (apiHeaderGuard) ;
  // un formulaire d'une autre page ne sait pas le poser.
  const headers = { ...(options.headers || {}), "X-Pi-Appliance": "1" };
  const response = await fetch(`api/${path}`, { ...options, headers });
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

// Bouton « Afficher » à côté de chaque mot de passe (TODO-2.9) : sur un
// téléphone surtout, on vérifie ce qu'on tape (mot de passe du Wi-Fi, du compte).
function addRevealButton(input) {
  const button = element("button", t("Afficher"), "secondary");
  button.type = "button";
  const show = (visible) => {
    input.type = visible ? "text" : "password";
    button.textContent = visible ? t("Masquer") : t("Afficher");
    button.setAttribute("aria-pressed", String(visible));
  };
  button.setAttribute("aria-pressed", "false");
  button.addEventListener("click", () => {
    show(input.type === "password");
    input.focus();
  });
  if (input.form) {
    input.form.addEventListener("reset", () => show(false));
    input.form.addEventListener("submit", () => show(false));
  }
  const wrapper = element("span", undefined, "password");
  input.replaceWith(wrapper);
  wrapper.append(input, button);
}

for (const input of document.querySelectorAll('input[type="password"]')) {
  addRevealButton(input);
}

function showMessage(lines, kind) {
  message.replaceChildren(...lines.map((line) => element("p", line)));
  message.className = kind;
  message.hidden = false;
}

// Adresse de la maison : nom en .local, localhost, adresse IP privée ou locale.
function homeAddress(host) {
  const name = host.toLowerCase().replace(/^\[|\]$/g, "");
  if (name.endsWith(".local") || name === "localhost") return true;
  const ipv4 = name.match(/^(\d+)\.(\d+)\.\d+\.\d+$/);
  if (ipv4) {
    const [a, b] = [Number(ipv4[1]), Number(ipv4[2])];
    return a === 10 || a === 127 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168) || (a === 169 && b === 254);
  }
  return name === "::1" || /^(fc|fd|fe8|fe9|fea|feb)/.test(name);
}

// Adresse d'un service sur internet (spécification 8.2, TODO-3.17) :
// https://<adresse internet du Pi>/<nom>/, ou pourquoi il n'en a pas.
const INTERNET_REASONS = {
  home: () => t("Ce service ne s'ouvre qu'à la maison."),
  console: () => t("Depuis internet, ce service s'ouvrira quand la console sera ouverte sur internet (Adresse sur internet) : la connexion passe par elle."),
  no_address: () => t("Ce service ne s'ouvre pas encore depuis internet : le Pi n'a pas d'adresse sur internet (Adresse sur internet)."),
  subpath: () => t("Ce service ne s'ouvre qu'à la maison : il ne marche pas sous une adresse partagée avec la console."),
  update: () => t("Mettez ce service à jour pour l'ouvrir depuis internet."),
  reserved: () => t("Ce service ne s'ouvre pas depuis internet : son nom est réservé à la console. Réinstallez-le sous un autre nom."),
};

async function loadServices() {
  const { services, internet } = await api("services");
  // Console ouverte depuis internet : « Ouvrir » mène à l'adresse internet.
  const outside = Boolean(internet) || !homeAddress(window.location.hostname);
  noServices.hidden = services.length > 0;
  showServicesSummary(services);
  serviceUpdates = services.filter((service) => service.update_available).length;
  showUpdates();
  // Ordre alphabétique des noms (TODO-5.22).
  services.sort((a, b) => a.name.localeCompare(b.name, LOCALE));
  servicesList.replaceChildren(...services.map((service) => {
    const card = element("li", undefined, "card");
    tint(card, service.app);
    card.dataset.tags = (service.tags || []).join(" ");
    const actions = element("div", undefined, "actions");
    // Un service sans page web (VPN) n'a pas d'adresse à ouvrir.
    if (!service.no_web_page) {
      const open = element("a", t("Ouvrir"), "button-outline");
      const href = outside ? service.internet_address : `https://${service.address}`;
      if (href) {
        open.href = href;
        open.target = "_blank";
        open.rel = "noopener";
      } else {
        open.classList.add("disabled");
        open.setAttribute("aria-disabled", "true");
      }
      actions.append(open);
    }
    // Liens de partage d'Etherpad : pour toute personne connectée.
    if (service.app === ETHERPAD_APP && service.access === "users") {
      const shares = sharesButton(service);
      actions.append(shares.button, shares.panel);
    }
    let devices = null;
    if (currentAccount && currentAccount.role === "admin") {
      const buttons = element("div", undefined, "controls");
      if (service.app === WIREGUARD_APP) {
        devices = devicesButton(service);
        buttons.append(devices.button);
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
      // Hors catalogue : l'administrateur choisit les étiquettes (TODO-5.22).
      if (service.tags_editable) {
        actions.append(tagsChooser(service));
      }
      if (!service.no_web_page && service.app !== POSTGRES_APP) {
        actions.append(hostingChooser(service));
      }
      if (devices) {
        actions.append(devices.panel);
      }
    }
    const state = element("p", undefined, "state");
    state.append(element("span", undefined, `dot ${service.status.color}`), element("span", service.status.message));
    const detail = element("div");
    const where = service.no_web_page
      ? (service.host_ports || []).map((port) => t("Port {0} {1} du Pi", port.protocol.toUpperCase(), port.port)).join(", ")
      : service.address;
    detail.append(element("p", where, "address"), state);
    if (!service.no_web_page) {
      // Adresse <service>-<nom>.local, certificat propre à ce service (mdns.sh).
      if (service.address.endsWith(".local") && !outside) {
        detail.append(element("p", t("La première fois, le navigateur affiche une alerte de sécurité pour cette adresse : c'est normal à la maison, vous pouvez continuer."), "meta"));
      }
      if (service.internet_address) {
        detail.append(element("p", t("Depuis internet : {0}", service.internet_address), "address"));
        if (currentAccount && currentAccount.role === "admin") {
          detail.append(element("p", t("Sur internet, ce service partage l'adresse de la console : installez seulement des services de confiance, car une page de ce service pourrait agir dans la console au nom d'une personne connectée."), "note"));
        }
      } else if (INTERNET_REASONS[service.internet_reason] && (outside || !["home", "no_address"].includes(service.internet_reason))) {
        detail.append(element("p", INTERNET_REASONS[service.internet_reason](), outside ? "warning" : "meta"));
      }
      detail.append(accessChooser(service));
    }
    if (service.update_available) {
      detail.append(element("p", t("Nouvelle version disponible."), "meta"));
    }
    if (service.site_files) {
      detail.append(element("p", siteFilesText(service), "meta"));
    }
    if (service.site_files && service.app !== "site-web") {
      detail.append(element("p", buildText(service.build), "meta"));
    }
    if ((service.tags || []).length > 0) {
      detail.append(tagList(service.tags));
    }
    card.append(cardHead(service.app, `${service.name} (${service.app_name})`, detail), actions);
    return card;
  }));
}

// Dépôt des fichiers d'un site par Nextcloud (spécification 6.3) : la
// console crée le dossier « Site <nom> » dans chaque Nextcloud installé.
function siteFilesText(service) {
  const folder = service.nextcloud_folder || `Site ${service.name}`;
  if (service.nextcloud_folder) return t("Déposez les fichiers du site dans le dossier « {0} » de Nextcloud, avec un compte administrateur de Nextcloud.", folder);
  if (service.nextcloud_pending) return t("Le dossier « {0} » va apparaître dans Nextcloud d'ici quelques minutes.", folder);
  return t("Pour déposer les fichiers du site, installez Nextcloud depuis la marketplace : un dossier « {0} » y apparaîtra.", folder);
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
  const fresh = await api("system").catch(() => null);
  // NE PAS cacher « Mon Pi » quand la console ne répond pas un moment : la relecture de chaque minute ne la remontrerait plus (TODO-5.29).
  if (!fresh && !piSection.hidden) return;
  resources = fresh || { memory: null, disk: null, usb_disk: null };
  showHeroLoad();
  const list = gauges(0, 0);
  piGauges.replaceChildren(...list);
  const tiles = nowTiles();
  piNow.replaceChildren(...tiles);
  piSection.hidden = list.length + tiles.length === 0 || wizardStep !== null;
  if (!piSection.hidden) {
    await loadUpdates();
    await loadStorage();
    await loadBackups();
    if (!(await loadCockpit())) watchCockpit();
    await loadMobile();
    await loadHistory();
  }
}

// Application mobile (spécification 5.2) : code QR et lien qui ajoutent ce Pi
// à l'application, pour l'administrateur ; ni mot de passe ni clé.
const mobileBox = document.getElementById("mobileBox");
const mobileQR = document.getElementById("mobileQR");
const mobileLink = document.getElementById("mobileLink");
const mobileCopy = document.getElementById("mobileCopy");
const mobileShare = document.getElementById("mobileShare");

async function loadMobile() {
  const mobile = await api("mobile").catch(() => null);
  mobileBox.hidden = !mobile;
  if (!mobile) return;
  // Le code QR porte en plus le code d'appairage, renouvelé toutes les 5 minutes.
  mobileLink.value = mobile.link;
  if (mobileQR.getAttribute("src") !== mobile.qr) mobileQR.src = mobile.qr;
  mobileShare.hidden = typeof navigator.share !== "function";
}

mobileCopy.addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText(mobileLink.value);
    showMessage([t("Lien copié.")], "success");
  } catch {
    mobileLink.select();
  }
});

mobileShare.addEventListener("click", async () => {
  try {
    await navigator.share({ url: mobileLink.value });
  } catch {
    // Partage annulé : rien à dire.
  }
});

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
  const power = resources.power;
  if (known(power)) {
    if (power.undervoltage_now) {
      tiles.push(nowTile(t("Alimentation"), t("Trop faible"),
        t("Alimentation trop faible : utilisez l'alimentation officielle du Pi. Le Pi ralentit et la carte SD peut s'abîmer."), "red"));
    } else if (power.undervoltage_since_boot) {
      tiles.push(nowTile(t("Alimentation"), t("Trop faible par moments"),
        t("L'alimentation a été trop faible depuis le démarrage du Pi : utilisez l'alimentation officielle du Pi, et évitez les câbles longs ou fins."), "orange"));
    } else {
      tiles.push(nowTile(t("Alimentation"), t("Correcte"), t("Aucune baisse de tension depuis le démarrage du Pi."), "green"));
    }
  }
  return tiles;
}

// showHeroLoad : charge du Pi dans la barre du haut, sur la même ligne que
// « Appliance en service. » (administrateur, api/system) : processeur, mémoire, et disque des
// données (le disque USB s'il y en a un, sinon la carte SD).
const heroLoad = document.getElementById("heroLoad");
function showHeroLoad() {
  const parts = [];
  if (known(resources.cpu_percent)) parts.push(t("CPU {0} %", number(resources.cpu_percent)));
  const memory = resources.memory;
  if (memory && memory.total_mb > 0) parts.push(t("RAM {0} %", number((100 * memory.used_mb) / memory.total_mb)));
  const disk = resources.usb_disk || resources.disk;
  if (disk && disk.total_mb > 0) parts.push(t("Disque {0} / {1}", sizeText(disk.used_mb), sizeText(disk.total_mb)));
  heroLoad.textContent = parts.join(" · ");
  heroLoad.hidden = parts.length === 0;
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
    if (wizardStep === null) {
      loadResources().catch(() => null);
    }
  }, 60000);
}

// Étiquettes des services (marketplace.Tags, spécification 6.5) : libellés traduits.
const TAG_LABELS = {
  "site-web": t("Site web"),
  fichiers: t("Fichiers"),
  documents: t("Documents"),
  collaboration: t("Travail à plusieurs"),
  donnees: t("Base de données"),
  administration: t("Administration"),
  reseau: t("Réseau"),
  maison: t("Maison"),
};
function tagList(tags) {
  const list = element("ul", undefined, "app-tags");
  list.append(...tags.filter((tag) => TAG_LABELS[tag]).map((tag) => element("li", TAG_LABELS[tag])));
  return list;
}

// hostingChooser : hébergement d'une application hors catalogue, par exemple
// le serveur de NautiLink (spécification 6.4) : publication sous le chemin
// /<nom>/ (Docker Compose libre, TODO-2.72), relais d'e-mails (TODO-2.74),
// base dans le PostgreSQL partagé (TODO-2.73), domaines renvoyés vers
// l'adresse internet du service (TODO-3.18).
const POSTGRES_APP = "postgresql";
function hostingChooser(service) {
  const details = element("details", undefined, "tags-chooser");
  details.append(element("summary", t("Hébergement")));
  const form = element("div");
  const send = async (path, method, body, button) => {
    if (button) {
      button.disabled = true;
    }
    try {
      await api(`services/${encodeURIComponent(service.name)}/${path}`, {
        method,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      showMessage([t("{0} : réglage enregistré.", service.name)], "success");
      await loadServices();
    } catch (error) {
      showMessage([error.message], "error");
      await loadServices();
    } finally {
      if (button) {
        button.disabled = false;
      }
    }
  };
  const checkbox = (label, checked, onChange) => {
    const field = element("label", undefined, "check");
    const box = element("input");
    box.type = "checkbox";
    box.checked = checked;
    box.addEventListener("change", () => {
      box.disabled = true;
      onChange(box.checked);
    });
    field.append(box, label);
    return field;
  };
  if (service.app === CUSTOM_APP) {
    form.append(checkbox(t("Ce service marche sous le chemin /{0}/ : le publier sur internet à cette adresse, chemin gardé.", service.name), Boolean(service.sub_path),
      (on) => send("subpath", "PUT", { sub_path: on })));
  }
  form.append(checkbox(t("Envoi d'e-mails : le service envoie par le relais du Pi (serveur {0}, port {1}, sans mot de passe), avec l'envoi des e-mails réglé dans la console.", "pi-appliance-mail", 2525), Boolean(service.mail),
    (on) => send("mail", "PUT", { mail: on })));
  if (service.database) {
    form.append(element("p", t("Base {0} dans le service PostgreSQL « {1} ». Le fichier Compose du service la reprend par les variables PI_DATABASE_URL, PI_DATABASE_HOST, PI_DATABASE_NAME, PI_DATABASE_USER et PI_DATABASE_PASSWORD.", service.database.name, service.database.server), "meta"));
  } else {
    const create = element("button", t("Créer sa base dans PostgreSQL"), "secondary");
    create.type = "button";
    create.addEventListener("click", () => send("database", "POST", {}, create));
    form.append(element("p", t("Base de données : un compte et une base réservés à ce service, dans le service PostgreSQL de la marketplace, joignables par ce service seulement."), "meta"), create);
  }
  const hosts = element("textarea");
  hosts.rows = 2;
  hosts.value = (service.redirect_hosts || []).join("\n");
  hosts.setAttribute("aria-label", t("Domaines renvoyés vers ce service"));
  const save = element("button", t("Enregistrer les domaines"), "secondary");
  save.type = "button";
  save.addEventListener("click", () => send("redirects", "PUT", { hosts: hosts.value.split(/\s+/).filter(Boolean) }, save));
  form.append(element("p", t("Domaines renvoyés vers ce service : un nom par ligne, par exemple nautilink.exemple.org. Faites d'abord pointer son DNS vers l'adresse internet du Pi (alias CNAME). Le renvoi ne marche que si le service est publié sur internet."), "meta"), hosts, save);
  details.append(form);
  return details;
}

// tagsChooser : étiquettes d'un service hors catalogue (Docker Compose libre,
// application retirée), choisies par l'administrateur dans sa fiche (TODO-5.22).
function tagsChooser(service) {
  const details = element("details", undefined, "tags-chooser");
  details.append(element("summary", t("Étiquettes")));
  const form = element("form");
  form.append(element("p", t("Choisissez jusqu'à 4 étiquettes pour retrouver ce service avec la barre du haut."), "meta"));
  for (const [tag, label] of Object.entries(TAG_LABELS)) {
    const field = element("label", undefined, "check");
    const box = element("input");
    box.type = "checkbox";
    box.name = "tag";
    box.value = tag;
    box.checked = (service.tags || []).includes(tag);
    field.append(box, label);
    form.append(field);
  }
  const save = element("button", t("Enregistrer les étiquettes"), "secondary");
  form.append(save);
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const tags = [...form.querySelectorAll('input[name="tag"]:checked')].map((box) => box.value);
    save.disabled = true;
    try {
      await api(`services/${encodeURIComponent(service.name)}/tags`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ tags }),
      });
      showMessage([t("{0} : étiquettes enregistrées.", service.name)], "success");
      await loadServices();
    } catch (error) {
      showMessage([error.message], "error");
    } finally {
      save.disabled = false;
    }
  });
  details.append(form);
  return details;
}

// Barre des tuiles (TODO-5.22) : en haut de la console, les trois types de
// tuiles (Mon Pi, Mes services, Marketplace), puis, pour les types choisis,
// leurs étiquettes : les thèmes de Mon Pi, les étiquettes du catalogue pour les
// services. Choix multiple, « l'un ou l'autre » ; rien de choisi : tout est
// affiché. Une étiquette ne trie que les tuiles de son type. Les étiquettes
// choisies sont gardées sur cet appareil (stockage du navigateur) ; le type,
// lui, ne l'est pas : la console s'ouvre toujours sur « Mon Pi » (TODO-5.25),
// ou, pour un compte qui ne l'a pas, sur tout ce qu'il a. Les tuiles portent data-kind
// (section) et data-theme (Mon Pi) ou data-tags (services) ; la barre ne se
// montre que quand au moins deux types sont à l'écran (pas pendant l'assistant).
const TILE_KINDS = [
  ["pi", t("Mon Pi")],
  ["services", t("Mes services")],
  ["catalog", t("Marketplace")],
];
const PI_THEMES = [
  ["reseau-internet", t("Réseau et internet")],
  ["etat", t("État")],
  ["entretien", t("Entretien")],
  ["comptes", t("Comptes et appareils")],
];
const TILES_KEY = "pi-tiles";
const tileFilters = document.getElementById("tileFilters");
const tileKinds = document.getElementById("tileKinds");
const tileTags = document.getElementById("tileTags");
const tileEmpty = document.getElementById("tileEmpty");
const tileChoice = { kinds: new Set(), tags: new Set() };

(function readTileChoice() {
  try {
    const saved = JSON.parse(window.localStorage.getItem(TILES_KEY) || "{}");
    for (const tag of saved.tags || []) tileChoice.tags.add(String(tag));
  } catch {
    // Stockage indisponible ou illisible : tout est affiché.
  }
})();

function saveTileChoice() {
  try {
    window.localStorage.setItem(TILES_KEY, JSON.stringify({ tags: [...tileChoice.tags] }));
  } catch {
    // Sans stockage, le choix ne dure que le temps de cette page.
  }
}

function setHidden(node, hidden) {
  if (node.hidden !== hidden) node.hidden = hidden;
}

// filterButton : bouton de la barre du haut. single : un seul choix à la fois
// (types Mon Pi, Mes services, Marketplace, qui ne se cumulent pas) ; toucher
// le type choisi le retire et réaffiche tout. Les étiquettes, elles, se cumulent.
// onChoose : appelé quand le bouton vient d'être choisi (Marketplace : relecture
// du catalogue en ligne).
function filterButton(id, label, chosen, single = false, onChoose = undefined) {
  const button = element("button", label, "secondary");
  button.type = "button";
  button.setAttribute("aria-pressed", String(chosen.has(id)));
  button.addEventListener("click", () => {
    if (chosen.has(id)) chosen.delete(id);
    else {
      if (single) chosen.clear();
      chosen.add(id);
    }
    saveTileChoice();
    applyTiles();
    if (chosen.has(id) && onChoose) onChoose();
  });
  return button;
}

// Tuiles à l'écran (sans l'attribut hidden posé par la console), par type.
function shownTiles() {
  const sections = [...document.querySelectorAll("main > section[data-kind]")].filter((section) => !section.hidden);
  const byKind = new Map();
  for (const section of sections) {
    if (!byKind.has(section.dataset.kind)) byKind.set(section.dataset.kind, []);
    byKind.get(section.dataset.kind).push(section);
  }
  return byKind;
}

function cardTags(card) {
  return (card.dataset.tags || "").split(" ").filter(Boolean);
}

// nodeThemes : thèmes d'une case de « Mon Pi » ; une case peut en avoir
// plusieurs, séparés par une espace (« Mises à jour » : état et entretien).
function nodeThemes(node) {
  return (node.dataset.theme || "").split(" ").filter(Boolean);
}

let tilesPending = false;

function applyTiles() {
  tilesPending = false;
  const byKind = shownTiles();
  const kinds = TILE_KINDS.filter(([kind]) => byKind.has(kind));
  const filtering = kinds.length >= 2 && wizardSection.hidden;
  setHidden(tileFilters, !filtering);
  for (const node of document.querySelectorAll("main .filtered")) {
    if (!filtering) node.classList.remove("filtered");
  }
  if (!filtering) return;
  // Un type choisi mais absent de l'écran (autre compte) ne compte pas.
  const chosenKinds = kinds.map(([kind]) => kind).filter((kind) => tileChoice.kinds.has(kind));
  const kindOn = (kind) => chosenKinds.length === 0 || chosenKinds.includes(kind);
  tileKinds.replaceChildren(...kinds.map(([kind, label]) => filterButton(kind, label, tileChoice.kinds, true, kind === "catalog" ? refreshOnlineCatalog : undefined)));
  // Étiquettes proposées : celles des types choisis, présentes à l'écran.
  const sections = (kind) => (chosenKinds.includes(kind) ? byKind.get(kind) || [] : []);
  const themes = new Set(sections("pi").flatMap((section) => [section, ...section.querySelectorAll("[data-theme]")])
    .filter((node) => !node.hidden && node.dataset.theme).flatMap(nodeThemes));
  const tags = new Set([...sections("services"), ...sections("catalog")]
    .flatMap((section) => [...section.querySelectorAll("li[data-tags]")].filter((card) => !card.hidden).flatMap(cardTags)));
  const offered = [
    ...PI_THEMES.filter(([theme]) => themes.has(theme)).map(([theme, label]) => [`pi:${theme}`, label]),
    ...Object.entries(TAG_LABELS).filter(([tag]) => tags.has(tag)),
  ];
  tileTags.replaceChildren(...offered.map(([id, label]) => filterButton(id, label, tileChoice.tags)));
  setHidden(tileTags, offered.length === 0);
  const offeredIds = new Set(offered.map(([id]) => id));
  const chosenThemes = [...tileChoice.tags].filter((id) => offeredIds.has(id) && id.startsWith("pi:")).map((id) => id.slice(3));
  const chosenTags = [...tileChoice.tags].filter((id) => offeredIds.has(id) && !id.startsWith("pi:"));
  let shown = 0;
  for (const [kind, list] of byKind) {
    for (const section of list) {
      let visible = kindOn(kind);
      if (visible && kind === "pi") {
        const themed = section.dataset.theme ? [section] : [...section.querySelectorAll(":scope > [data-theme]")];
        for (const node of themed) {
          const keep = chosenThemes.length === 0 || nodeThemes(node).some((theme) => chosenThemes.includes(theme));
          if (node !== section) node.classList.toggle("filtered", !keep);
        }
        visible = themed.some((node) => !node.hidden && (chosenThemes.length === 0 || nodeThemes(node).some((theme) => chosenThemes.includes(theme))));
      } else if (visible) {
        const cards = [...section.querySelectorAll("li[data-tags], .cards > li")];
        let kept = 0;
        for (const card of cards) {
          const keep = chosenTags.length === 0 || cardTags(card).some((tag) => chosenTags.includes(tag));
          card.classList.toggle("filtered", !keep);
          if (keep) kept += 1;
        }
        visible = chosenTags.length === 0 || kept > 0;
      }
      section.classList.toggle("filtered", !visible);
      if (visible) shown += 1;
    }
  }
  setHidden(tileEmpty, shown > 0);
}

function scheduleTiles() {
  if (tilesPending) return;
  tilesPending = true;
  window.requestAnimationFrame(applyTiles);
}

// Les sections et les cartes apparaissent au fil des chargements : la barre suit.
new MutationObserver((records) => {
  if (records.some((record) => !tileFilters.contains(record.target))) scheduleTiles();
}).observe(document.querySelector("main"), { subtree: true, childList: true, attributes: true, attributeFilter: ["hidden"] });

// Pied de page (TODO-5.21) : version (pas pour une version de
// développement), commit et sa date, licence et code source ; les mêmes que
// les pages servies par la console (pages.go, footerHTML).
const REPO_URL = "https://github.com/gmoulard/pi-appliance";
const LICENSE_URL = `${REPO_URL}/blob/master/LICENSE`;

function footerLink(href, content) {
  const link = element("a");
  link.href = href;
  link.target = "_blank";
  link.rel = "noopener";
  link.append(content);
  return link;
}

function showFooter(health) {
  const parts = [];
  if (health.release) parts.push(document.createTextNode(`pi-appliance ${String(health.version).replace(/^v/, "")}`));
  if (health.commit) {
    const commit = element("span");
    commit.append(`${t("commit")} `, footerLink(`${REPO_URL}/commit/${encodeURIComponent(health.commit)}`, element("code", health.commit.slice(0, 7))));
    const day = /^\d{4}-\d{2}-\d{2}$/.test(health.date || "") ? new Date(`${health.date}T12:00:00`) : null;
    if (day && !Number.isNaN(day.getTime())) {
      commit.append(` ${t("du {0}", day.toLocaleDateString(LOCALE, { day: "numeric", month: "long", year: "numeric" }))}`);
    }
    parts.push(commit);
  }
  parts.push(footerLink(LICENSE_URL, t("Logiciel libre, licence AGPL-3.0")), footerLink(REPO_URL, t("Code source")));
  footer.replaceChildren(...parts.flatMap((part, index) => (index === 0 ? [part] : [" · ", part])));
}

// Nom du service tapé librement (« Mon blog ») ramené au format d'une adresse
// (« mon-blog ») : minuscules, sans accents, un tiret à la place des espaces et
// autres signes. Même calcul que NormalizeLabel côté serveur
// (marketplace/catalog.go). Pendant la saisie (final faux), un tiret en fin de
// nom est gardé pour pouvoir taper le mot suivant.
function normalizeServiceName(value, final) {
  let name = value.toLowerCase()
    .replace(/æ/g, "ae").replace(/œ/g, "oe").replace(/ß/g, "ss").replace(/ø/g, "o")
    .normalize("NFD").replace(/[\u0300-\u036f]/g, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+/, "");
  if (final) {
    name = name.slice(0, 32).replace(/-+$/, "");
  }
  return name;
}

// Champ « Nom du service » : converti pendant la saisie, puis une dernière fois
// avant l'envoi (clic sur le bouton ou touche Entrée, qui passe aussi par un
// clic), avant que le navigateur ne contrôle le format.
function serviceNameField(input, button) {
  input.autocapitalize = "none";
  input.spellcheck = false;
  input.addEventListener("input", () => {
    const name = normalizeServiceName(input.value, false);
    if (name !== input.value) {
      input.value = name;
    }
  });
  const finish = () => {
    input.value = normalizeServiceName(input.value, true);
  };
  input.addEventListener("change", finish);
  button.addEventListener("click", finish);
}

async function loadCatalog() {
  const { apps, source } = await api("catalog");
  showCatalogSource(source);
  for (const app of apps) {
    icons[app.id] = app.icon;
  }
  catalogList.replaceChildren(...apps.map((app) => {
    const card = element("li", undefined, "card app");
    tint(card, app.id);
    card.dataset.tags = (app.tags || []).join(" ");
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
    if (app.web) {
      form.append(accessField(app.access));
    }
    if (APP_WARNINGS[app.id]) {
      form.append(element("p", APP_WARNINGS[app.id], "note"));
    }
    const button = element("button", t("Installer"));
    serviceNameField(name, button);
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
    card.append(details, element("p", t("{0} · licence {1}", app.category, app.license), "meta"), tagList(app.tags || []));
    return card;
  }));
  // Cockpit, installé sur le système et non dans un conteneur, a sa carte à
  // part, gardée d'un chargement à l'autre (spécification 6.4).
  catalogList.append(cockpitBox);
  // Carte de Cockpit remplie même si « Mon Pi » n'a pas été chargé.
  if (currentAccount && currentAccount.role === "admin" && cockpitStatus === null) {
    loadCockpit().then((done) => { if (!done) watchCockpit(); }).catch(() => {});
  }
  if (currentAccount && currentAccount.role === "admin") {
    catalogList.append(customComposeCard());
  }
}

// Avancement d'une installation (spécification 6.2) : la console relit
// api/services/<nom>/progress chaque seconde tant que l'installation se fait.
const INSTALL_STEPS = [
  ["prepare", t("Préparation")],
  ["download", t("Téléchargement")],
  ["create", t("Création du service")],
  ["start", t("Démarrage")],
  ["check", t("Vérification que le service répond")],
];

function showInstallProgress(box, progress) {
  const current = INSTALL_STEPS.findIndex(([step]) => step === progress.step);
  const list = element("ol", undefined, "steps");
  INSTALL_STEPS.forEach(([step, label], index) => {
    const item = element("li", label);
    if (progress.step === "done" || index < current) {
      item.className = "done";
    } else if (index === current) {
      item.className = progress.error ? "failed" : "current";
    }
    if (index === current && progress.error) {
      item.append(element("p", t("L'installation s'est arrêtée à cette étape."), "meta"));
    }
    if (step === "download" && index === current && progress.total_bytes > 0) {
      const bar = element("progress");
      bar.max = progress.total_bytes;
      bar.value = progress.received_bytes;
      item.append(bar, element("p", t("{0} reçus sur {1}", sizeText(progress.received_bytes / 1048576), sizeText(progress.total_bytes / 1048576)), "meta"));
    }
    if (step === "download" && index === current && progress.images > 1) {
      item.append(element("p", t("Image {0} sur {1}", progress.image, progress.images), "meta"));
    }
    if (step === "check" && index === current && !progress.error) {
      item.append(element("p", t("Certains services mettent plusieurs minutes à répondre la première fois."), "meta"));
    }
    list.append(item);
  });
  box.replaceChildren(list);
}

function watchInstall(name, button) {
  for (const old of button.parentElement.querySelectorAll(".install-progress")) old.remove();
  const box = element("div", undefined, "install-progress");
  box.setAttribute("aria-live", "polite");
  button.before(box);
  showInstallProgress(box, { step: "prepare" });
  const read = () => api(`services/${encodeURIComponent(name)}/progress`);
  let stopped = false;
  let timer = null;
  const poll = async () => {
    const progress = await read().catch(() => null);
    if (stopped) return;
    if (progress && !progress.finished) showInstallProgress(box, progress);
    timer = setTimeout(poll, 1000);
  };
  timer = setTimeout(poll, 1000);
  return async (succeeded) => {
    stopped = true;
    clearTimeout(timer);
    if (succeeded) {
      box.remove();
      return;
    }
    const progress = await read().catch(() => null);
    if (progress && progress.error) {
      showInstallProgress(box, progress);
    } else {
      box.remove();
    }
  };
}

async function install(app, form, button, details) {
  const data = new FormData(form);
  const parameters = {};
  for (const parameter of (app.parameters || []).filter((p) => !p.generate)) {
    parameters[parameter.name] = data.get(parameter.name);
  }
  button.disabled = true;
  showMessage([t("Installation de {0}… Le premier téléchargement peut prendre plusieurs minutes.", app.name)], "info");
  const stop = watchInstall(data.get("name"), button);
  let succeeded = false;
  try {
    const result = await withRisks((confirm) => api("services", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ app: app.id, name: data.get("name"), parameters, access: data.get("access") || undefined, confirm_risks: confirm }),
    }));
    if (!result) {
      message.hidden = true;
      return;
    }
    const lines = [app.web ? t("{0} est prêt : https://{1}", app.name, result.instance.address) : t("{0} est prêt.", app.name)];
    for (const [label, value] of Object.entries(result.secrets || {})) {
      lines.push(t("{0} : {1} (notez-le, il ne sera plus affiché)", label, value));
    }
    lines.push(...(result.warnings || []));
    succeeded = true;
    showMessage(lines, "success");
    details.open = false;
    await loadServices();
    await refreshResources();
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    await stop(succeeded);
    button.disabled = false;
  }
}

// Qui ouvre un service (spécification 8.2) : tout le monde, les personnes
// connectées à la console ou la maison seulement. Valeur par défaut : le manifeste.
const ACCESS_LABELS = {
  public: t("Tout le monde, sur internet"),
  users: t("Les personnes qui ont un compte sur ce Pi"),
  home: t("La maison seulement (et le VPN)"),
};
// Applications qui ont la main sur tout le Pi (Portainer) : niveau posé par
// leur manifeste, ni choisi ni changé.
const ADMINS_ACCESS = "admins";
const ADMINS_LABEL = t("Les administrateurs de ce Pi, à la maison seulement (et le VPN)");
const ADMINS_WARNING = t("Attention : cette application a la main sur tout le Pi. Elle n'est ouverte qu'aux administrateurs, depuis la maison. Ne modifiez pas dans cette application les services installés par la console : leurs mises à jour et leurs sauvegardes pourraient ne plus marcher.");

// Avertissement propre à une application, montré avant de l'installer.
const APP_WARNINGS = {
  "claude-code": t("Claude aura tous les droits sur votre Pi : il peut tout lire, tout modifier, tout effacer ; chaque action passe par votre application Claude, où vous l'acceptez ou la refusez. Il faut un abonnement Claude (Pro ou Max). Après l'installation, ouvrez le service pour vous connecter une fois à votre compte Claude."),
};

function accessLabel(access) {
  return access === ADMINS_ACCESS ? ADMINS_LABEL : ACCESS_LABELS[access] || ACCESS_LABELS.public;
}

function accessSelect(value) {
  const select = element("select");
  select.name = "access";
  for (const [access, label] of Object.entries(ACCESS_LABELS)) {
    const option = element("option", label);
    option.value = access;
    select.append(option);
  }
  select.value = value || "public";
  return select;
}

function accessField(value) {
  if (value === ADMINS_ACCESS) {
    const box = element("div");
    box.append(element("p", `${t("Qui peut l'ouvrir")} : ${ADMINS_LABEL}`, "meta"), element("p", ADMINS_WARNING, "note"));
    return box;
  }
  const label = element("label", t("Qui peut l'ouvrir"));
  label.append(accessSelect(value));
  return label;
}

// accessChooser : niveau d'accès d'un service installé ; l'administrateur le
// change, le service redémarre.
function accessChooser(service) {
  const box = element("div", undefined, "access");
  if (service.access === ADMINS_ACCESS) {
    box.append(element("p", `${t("Qui peut l'ouvrir")} : ${ADMINS_LABEL}`, "meta"), element("p", ADMINS_WARNING, "note"));
    return box;
  }
  if (!currentAccount || currentAccount.role !== "admin") {
    box.append(element("p", `${t("Qui peut l'ouvrir")} : ${accessLabel(service.access)}`, "meta"));
    return box;
  }
  const label = element("label", t("Qui peut l'ouvrir"));
  const select = accessSelect(service.access);
  label.append(select);
  select.addEventListener("change", async () => {
    const previous = service.access;
    select.disabled = true;
    showMessage([t("Changement de l'accès à {0}… Le service redémarre.", service.name)], "info");
    try {
      const instance = await api(`services/${encodeURIComponent(service.name)}/access`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ access: select.value }),
      });
      service.access = instance.access;
      showMessage([t("{0} : accès changé.", service.name)], "success");
      // Adresse sur internet selon le nouvel accès.
      loadServices().catch(() => {});
    } catch (error) {
      select.value = previous || "public";
      showMessage([error.message], "error");
    } finally {
      select.disabled = false;
    }
  });
  box.append(label);
  return box;
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
    // Caméras du Pi (« Caméra et timelapse ») : confirmation de vie privée,
    // l'application n'a pas la main sur tout le Pi.
    if (risks.every((risk) => risk.code === "camera")) {
      if (!window.confirm(t("Ce service voit et enregistre les images des caméras branchées au Pi. Les images restent sur le Pi. Prévenez les personnes filmées : filmer quelqu'un sans le lui dire peut être interdit. Continuer ?"))) {
        return null;
      }
      return send(true);
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

// VPN WireGuard (spécification 6.3) : les appareils autorisés et, pour un
// nouvel appareil, sa configuration en QR code, montrée une seule fois.
const WIREGUARD_APP = "wireguard";

function devicesButton(service) {
  const button = element("button", t("Appareils"), "secondary");
  button.setAttribute("aria-expanded", "false");
  const panel = element("div", undefined, "devices");
  panel.hidden = true;
  button.addEventListener("click", async () => {
    const open = panel.hidden;
    panel.hidden = !open;
    button.setAttribute("aria-expanded", String(open));
    if (open) {
      try {
        await devicesPanel(service, panel);
      } catch (error) {
        panel.hidden = true;
        showMessage([error.message], "error");
      }
    }
  });
  return { button, panel };
}

async function devicesPanel(service, panel, created) {
  const path = `services/${encodeURIComponent(service.name)}/wireguard`;
  const status = await api(path);
  const nodes = [];
  nodes.push(element("p", t("Le téléphone pourra joindre le réseau de la maison ({0}) ; sa navigation sur internet ne passera pas par la maison.", status.home_networks.join(", ")), "note"));
  nodes.push(element("p", t("Sur votre box internet, redirigez le port UDP {0} vers ce Raspberry Pi pour vous connecter de l'extérieur.", status.port), "note"));
  if (status.endpoint_local) {
    nodes.push(element("p", t("Le Pi n'a pas encore d'adresse sur internet : réglez-la dans « Adresse sur internet » pour vous connecter de l'extérieur."), "note"));
  }
  if (created) {
    const box = element("div", undefined, "qr");
    const image = element("img");
    image.src = created.qr_code;
    image.alt = t("QR code de configuration WireGuard");
    image.width = 256;
    image.height = 256;
    const text = element("details");
    text.append(element("summary", t("Configuration en texte")), element("pre", created.config));
    box.append(element("p", t("Scannez ce code avec l'application WireGuard du téléphone (bouton +, puis « Scanner un QR code »). Il ne s'affichera qu'une seule fois."), "note"), image, text);
    nodes.push(box);
  }
  const list = element("ul", undefined, "device-list");
  for (const device of status.devices) {
    const item = element("li");
    const remove = element("button", t("Retirer"), "secondary");
    remove.addEventListener("click", async () => {
      if (!window.confirm(t("Retirer l'appareil « {0} » ? Il ne pourra plus se connecter.", device.name))) return;
      remove.disabled = true;
      try {
        await api(`${path}/devices/${device.id}`, { method: "DELETE" });
        await devicesPanel(service, panel);
      } catch (error) {
        showMessage([error.message], "error");
        remove.disabled = false;
      }
    });
    item.append(element("span", `${device.name} (${device.address})`), remove);
    list.append(item);
  }
  nodes.push(status.devices.length > 0 ? list : element("p", t("Aucun appareil pour l'instant."), "meta"));
  const form = element("form");
  const label = element("label", t("Nom de l'appareil"));
  const input = element("input");
  input.name = "device";
  input.maxLength = 40;
  input.required = true;
  label.append(input);
  const add = element("button", t("Ajouter un appareil"));
  form.append(label, add);
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    add.disabled = true;
    try {
      const device = await api(`${path}/devices`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: input.value }),
      });
      await devicesPanel(service, panel, device);
    } catch (error) {
      showMessage([error.message], "error");
      add.disabled = false;
    }
  });
  nodes.push(form);
  panel.replaceChildren(...nodes);
}

// Liens de partage d'Etherpad (spécification 6.3, 8.2) : un lien ouvre un
// seul document à une personne sans compte, jusqu'à une date. Le lien n'est
// montré qu'une fois ; la console n'en garde que l'empreinte.
const ETHERPAD_APP = "etherpad";

function sharesButton(service) {
  const button = element("button", t("Partager un document"), "secondary");
  button.setAttribute("aria-expanded", "false");
  const panel = element("div", undefined, "devices");
  panel.hidden = true;
  button.addEventListener("click", async () => {
    const open = panel.hidden;
    panel.hidden = !open;
    button.setAttribute("aria-expanded", String(open));
    if (open) {
      try {
        await sharesPanel(service, panel);
      } catch (error) {
        panel.hidden = true;
        showMessage([error.message], "error");
      }
    }
  });
  return { button, panel };
}

async function sharesPanel(service, panel, created) {
  const path = `services/${encodeURIComponent(service.name)}/shares`;
  const { shares } = await api(path);
  const nodes = [element("p", t("Un lien de partage ouvre un seul document à une personne qui n'a pas de compte : elle peut le lire et l'écrire, sans rien voir d'autre."), "note")];
  if (created) {
    const box = element("div");
    const link = element("input");
    link.readOnly = true;
    link.value = created.link;
    link.dir = "ltr";
    link.setAttribute("aria-label", t("Lien de partage"));
    const copy = element("button", t("Copier le lien"), "secondary");
    copy.type = "button";
    copy.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(link.value);
        showMessage([t("Lien copié.")], "success");
      } catch {
        link.select();
      }
    });
    const controls = element("div", undefined, "controls");
    controls.append(copy);
    box.append(element("p", t("Envoyez ce lien à la personne : il ne s'affichera qu'une seule fois. Il marche jusqu'au {0}.", dayText(created.share.expires_at)), "note"), link, controls);
    nodes.push(box);
  }
  const list = element("ul", undefined, "device-list");
  for (const share of shares) {
    const item = element("li");
    const remove = element("button", t("Retirer"), "secondary");
    remove.addEventListener("click", async () => {
      if (!window.confirm(t("Retirer le lien du document « {0} » ? La personne ne pourra plus l'ouvrir.", share.pad))) return;
      remove.disabled = true;
      try {
        await api(`${path}/${share.id}`, { method: "DELETE" });
        await sharesPanel(service, panel);
      } catch (error) {
        showMessage([error.message], "error");
        remove.disabled = false;
      }
    });
    const text = currentAccount && share.owner !== currentAccount.name
      ? t("« {0} », créé par {1}, jusqu'au {2}", share.pad, share.owner, dayText(share.expires_at))
      : t("« {0} », jusqu'au {1}", share.pad, dayText(share.expires_at));
    item.append(element("span", text), remove);
    list.append(item);
  }
  nodes.push(shares.length > 0 ? list : element("p", t("Aucun lien de partage pour l'instant."), "meta"));
  const form = element("form");
  const padLabel = element("label", t("Nom du document ou son adresse"));
  const pad = element("input");
  pad.name = "pad";
  pad.required = true;
  pad.maxLength = 300;
  padLabel.append(pad);
  const daysLabel = element("label", t("Durée du lien"));
  const days = element("select");
  days.name = "days";
  for (const [value, label] of [[1, t("1 jour")], [7, t("7 jours")], [30, t("30 jours")]]) {
    const option = element("option", label);
    option.value = String(value);
    days.append(option);
  }
  days.value = "7";
  daysLabel.append(days);
  const add = element("button", t("Créer le lien"));
  form.append(padLabel, daysLabel, add);
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    add.disabled = true;
    try {
      const reply = await api(path, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ pad: pad.value, days: Number(days.value) }),
      });
      await sharesPanel(service, panel, reply);
    } catch (error) {
      showMessage([error.message], "error");
      add.disabled = false;
    }
  });
  nodes.push(form);
  panel.replaceChildren(...nodes);
}

// guessComposeService : premier service d'un fichier Docker Compose et le
// port de son conteneur (« ports: - "9001:80" » donne 80), pour préremplir
// le formulaire ; une simple lecture du texte, l'administrateur corrige.
function guessComposeService(text) {
  const result = {};
  const block = /^services:[ \t]*\r?\n((?:[ \t]+.*\r?\n?|[ \t]*\r?\n)*)/m.exec(text);
  if (!block) return result;
  const first = /^([ \t]+)([A-Za-z0-9][\w.-]*):/m.exec(block[1]);
  if (!first) return result;
  result.service = first[2];
  const indent = first[1].length;
  const lines = block[1].slice(first.index + first[0].length).split(/\r?\n/);
  let inPorts = false;
  for (const line of lines) {
    const depth = line.search(/\S/);
    if (depth < 0 || line.trim().startsWith("#")) continue;
    if (depth <= indent) break;
    if (/^\s*ports:\s*$/.test(line)) { inPorts = true; continue; }
    if (inPorts) {
      const item = /^\s*-\s*["']?(?:[^"'\s#]*:)?(\d+)(?:\/\w+)?["']?/.exec(line);
      if (item) { result.port = Number(item[1]); break; }
      if (!/^\s*-/.test(line)) inPorts = false;
    }
  }
  return result;
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
  const examples = element("p", undefined, "note");
  examples.append(footerLink("https://github.com/Websoft9/docker-library/tree/main/apps", t("D'autres exemples de fichiers Docker Compose (Websoft9), à adapter avant de les coller : leurs réglages sont dans un fichier .env à part.")));
  form.append(examples);
  const nameLabel = element("label", t("Nom du service (son adresse)"));
  const name = element("input");
  name.name = "name";
  name.pattern = "[a-z]([a-z0-9\\-]{0,30}[a-z0-9])?";
  name.required = true;
  // Exemple prérempli qui s'installe tel quel (whoami : petite page qui
  // affiche la requête reçue, image arm64) ; à remplacer par son fichier.
  name.defaultValue = "whoami";
  nameLabel.append(name);
  const composeLabel = element("label", t("Fichier compose.yaml"));
  const compose = element("textarea");
  compose.name = "compose";
  compose.rows = 10;
  compose.required = true;
  compose.spellcheck = false;
  compose.autocapitalize = "none";
  compose.defaultValue = "services:\n  whoami:\n    image: traefik/whoami:v1.10\n    restart: unless-stopped\n";
  composeLabel.append(compose);
  // Ou lire le fichier à une adresse (GitHub ou autre) : la console le
  // télécharge et remplit le formulaire, à relire avant d'installer.
  const urlLabel = element("label", t("Lire le fichier à une adresse web (GitHub ou autre)"));
  const urlRow = element("span", undefined, "field-row");
  const url = element("input");
  url.type = "url";
  url.placeholder = "https://github.com/…/compose.yaml";
  url.spellcheck = false;
  url.autocapitalize = "none";
  const fetchButton = element("button", t("Lire le fichier"), "button-outline");
  fetchButton.type = "button";
  urlRow.append(url, fetchButton);
  urlLabel.append(urlRow);
  fetchButton.addEventListener("click", async () => {
    if (!url.value.trim()) {
      url.focus();
      return;
    }
    fetchButton.disabled = true;
    try {
      const result = await api("compose/fetch", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url: url.value.trim() }),
      });
      compose.value = result.compose;
      const guess = guessComposeService(result.compose);
      if (guess.service) {
        service.value = guess.service;
        const slug = normalizeServiceName(guess.service.replace(/^[^A-Za-z]+/, ""), true);
        if (slug) name.value = slug;
      }
      if (guess.port) port.value = guess.port;
      showMessage([t("Fichier lu : {0}. Relisez-le, ainsi que le nom, le conteneur et le port, avant d'installer.", result.url)], "info");
    } catch (error) {
      showMessage([error.message], "error");
    } finally {
      fetchButton.disabled = false;
    }
  });
  const serviceLabel = element("label", t("Conteneur à ouvrir à l'adresse du service"));
  const service = element("input");
  service.name = "service";
  service.required = true;
  service.defaultValue = "whoami";
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
  serviceNameField(name, button);
  form.append(nameLabel, urlLabel, composeLabel, serviceLabel, portLabel, button);
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = new FormData(form);
    button.disabled = true;
    showMessage([t("Installation de {0}… Le premier téléchargement peut prendre plusieurs minutes.", data.get("name"))], "info");
    const stop = watchInstall(data.get("name"), button);
    let succeeded = false;
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
      succeeded = true;
      showMessage([t("{0} est prêt : https://{1}", result.instance.name, result.instance.address), ...(result.warnings || [])], "success");
      form.reset();
      await loadServices();
      await refreshResources();
    } catch (error) {
      showMessage([error.message], "error");
    } finally {
      await stop(succeeded);
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

// Bouton Marketplace : la console relit le catalogue en ligne (une fois par
// minute au plus, spécification 6.1). S'il a changé, la marketplace et les
// services (« Nouvelle version disponible ») sont rechargés ; sinon seule la
// date de lecture change. En cas d'échec, le catalogue affiché reste.
async function refreshOnlineCatalog() {
  try {
    await showCatalogRefresh(await api("catalog/refresh", { method: "POST" }));
  } catch {
    // Pas d'internet ou console injoignable : rien ne change à l'écran.
  }
}

// showCatalogRefresh : réponse de api/catalog/refresh mise à l'écran.
async function showCatalogRefresh({ changed, source }) {
  if (changed) {
    await loadCatalog();
    await loadServices();
  } else {
    showCatalogSource(source);
  }
}

// D'où vient le catalogue : lu en ligne (date) ou intégré à la console.
function showCatalogSource(source) {
  catalogSource.hidden = !source;
  if (!source) return;
  const lines = [source.online
    ? t("Catalogue mis à jour en ligne le {0}.", new Date(source.updated_at).toLocaleString(LOCALE))
    : t("Catalogue intégré à la console : il n'a pas encore pu être lu en ligne.")];
  if (source.refused && source.refused.length) {
    lines.push(t("Pas encore disponibles sur cette console : {0}.", source.refused.join(", ")));
  }
  catalogSource.textContent = lines.join(" ");
}

// Dernière construction d'un générateur de site statique (Hugo, MkDocs…).
function buildText(build) {
  if (!build) return t("Déposez les fichiers source du site dans Nextcloud : il sera construit tout seul.");
  const when = new Date(build.at).toLocaleString(LOCALE);
  if (build.state === "building") return t("Construction du site en cours…");
  if (build.state === "ok") return t("Site construit le {0}.", when);
  return t("La construction du site a échoué le {0} : le site précédent reste en ligne. Le détail est dans le fichier .pi-appliance-build.log du dossier du site.", when);
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

// Sauvegardes (spécification 9.1) : chaque nuit sur un disque USB autre que
// celui des données ; restauration d'un service à une date choisie.
const backupBox = document.getElementById("backupBox");
const backupState = document.getElementById("backupState");
const backupLast = document.getElementById("backupLast");
const backupDisks = document.getElementById("backupDisks");
const backupActions = document.getElementById("backupActions");
const backupNow = document.getElementById("backupNow");
const backupHour = document.getElementById("backupHour");
const backupSettingsBox = document.getElementById("backupSettingsBox");
const backupSettingsForm = document.getElementById("backupSettings");
const backupList = document.getElementById("backupList");
let backupTimer = null;
let backupSettings = null;

backupHour.replaceChildren(...Array.from({ length: 24 }, (_, hour) => {
  const option = element("option", t("{0} h", hour));
  option.value = String(hour);
  return option;
}));

function watchBackups() {
  if (backupTimer) return;
  backupTimer = setInterval(async () => {
    const running = await loadBackups().catch(() => false);
    if (!running) {
      clearInterval(backupTimer);
      backupTimer = null;
    }
  }, 5000);
}

async function backupRequest(path, body) {
  return api(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body || {}),
  });
}

async function useBackupDisk(disk) {
  try {
    if (disk.fstype === "ext4" && disk.partition) {
      if (!window.confirm(t("Utiliser {0} pour les sauvegardes ? Il devra rester branché.", diskName(disk)))) return;
      await backupRequest("backups/disk/use", { partition: disk.partition });
    } else {
      if (!window.confirm(t("Tout le contenu de {0} va être effacé, pour le préparer à recevoir les sauvegardes. Continuer ?", diskName(disk)))) return;
      if (!window.confirm(t("Dernière vérification : les fichiers qui sont sur {0} seront perdus définitivement. Effacer le disque ?", diskName(disk)))) return;
      showMessage([t("Préparation du disque USB…")], "info");
      await backupRequest("backups/disk/format", { disk: disk.device, erase: true });
    }
    showMessage([t("Le disque de sauvegarde est prêt.")], "success");
  } catch (error) {
    showMessage([error.message], "error");
  }
  await loadBackups().catch(() => null);
}

async function restoreService(snapshot, service) {
  const when = new Date(snapshot.created).toLocaleString(LOCALE);
  if (!window.confirm(t("Remettre {0} tel qu'il était le {1} ?\n\nSes données actuelles seront remplacées. Une copie en est faite avant, pour pouvoir revenir en arrière.", service.name, when))) {
    return;
  }
  let body = { service: service.name };
  for (;;) {
    try {
      await backupRequest(`backups/${encodeURIComponent(snapshot.id)}/restore`, body);
      showMessage([t("Restauration de {0}… Le service est arrêté le temps de la copie.", service.name)], "info");
      break;
    } catch (error) {
      if (error.status !== 403 || error.body.code !== "password") {
        showMessage([error.message], "error");
        break;
      }
      const password = window.prompt(t("Cette sauvegarde a été faite sur une autre carte SD. Saisissez le mot de passe administrateur en vigueur à sa date :"));
      if (!password) break;
      body = { service: service.name, password };
    }
  }
  await loadBackups().catch(() => null);
}

backupNow.addEventListener("click", async () => {
  try {
    await backupRequest("backups");
    showMessage([t("Sauvegarde en cours… Chaque service est arrêté le temps de copier ses données.")], "info");
  } catch (error) {
    showMessage([error.message], "error");
  }
  await loadBackups().catch(() => null);
});

function showBackupSettings(settings) {
  const fields = backupSettingsForm.elements;
  fields.enabled.checked = settings.enabled;
  backupHour.value = String(settings.hour);
  backupHour.disabled = !settings.enabled;
  for (const name of ["daily", "weekly", "monthly"]) fields[name].value = String(settings[name]);
}

backupSettingsForm.elements.enabled.addEventListener("change", () => {
  backupHour.disabled = !backupSettingsForm.elements.enabled.checked;
});

backupSettingsForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const fields = backupSettingsForm.elements;
  const settings = {
    enabled: fields.enabled.checked,
    hour: Number(backupHour.value),
    daily: Number(fields.daily.value),
    weekly: Number(fields.weekly.value),
    monthly: Number(fields.monthly.value),
  };
  if (!settings.enabled && backupSettings.enabled
    && !window.confirm(t("Sans sauvegarde de la nuit, vos services ne sont sauvegardés que quand vous cliquez sur « Sauvegarder maintenant ». Arrêter la sauvegarde de la nuit ?"))) {
    showBackupSettings(backupSettings);
    return;
  }
  const button = backupSettingsForm.querySelector("button");
  button.disabled = true;
  try {
    backupSettings = await api("backups/settings", jsonBody("PUT", settings));
    showMessage([t("Réglages des sauvegardes enregistrés.")], "success");
    await loadBackups();
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
});

const BACKUP_KINDS = { nightly: t("de la nuit"), manual: t("à la demande"), "before-restore": t("avant restauration") };

// loadBackups rend vrai tant qu'une opération est en cours.
async function loadBackups() {
  let status;
  try {
    status = await api("backups");
  } catch {
    backupBox.hidden = true;
    return false;
  }
  backupBox.hidden = false;
  backupSettings = status.settings;
  if (!backupSettingsForm.contains(document.activeElement)) showBackupSettings(status.settings);
  backupDisks.replaceChildren();
  backupLast.hidden = !status.last;
  if (status.last) {
    const when = new Date(status.last.at).toLocaleString(LOCALE);
    backupLast.textContent = t("Dernière opération, le {0} : {1}", when, status.last.message);
  }
  const running = status.running !== "";
  backupNow.disabled = running || !status.destination || !status.ready;
  if (running) {
    backupState.textContent = status.running === "restore-file" ? t("Remise en place d'un fichier…")
      : status.running === "restore" ? t("Restauration en cours…") : t("Sauvegarde en cours…");
    watchBackups();
  } else if (!status.ready) {
    backupState.textContent = t("Les sauvegardes attendent votre mot de passe : déconnectez-vous puis reconnectez-vous.");
  } else if (!status.disk_ready && (!status.destination || (status.disk && status.disk.disks.length))) {
    const disks = status.disk ? status.disk.disks : [];
    backupState.textContent = disks.length
      ? t("Choisissez le disque USB qui recevra les sauvegardes (pas celui des données) :")
      : t("Branchez un disque USB pour sauvegarder vos services et vos réglages chaque nuit. Ce doit être un autre disque que celui des données.");
    backupDisks.replaceChildren(...disks.map((disk) => {
      const item = element("li");
      const button = element("button");
      button.type = "button";
      const ready = disk.fstype === "ext4" && disk.partition;
      const detail = ready ? t("prêt") : disk.fstype ? t("à effacer avant usage") : t("vide, à préparer");
      button.append(element("span", diskName(disk)), element("span", detail, "meta"));
      button.addEventListener("click", () => useBackupDisk(disk));
      item.append(button);
      return item;
    }));
  } else {
    const s = status.settings;
    backupState.textContent = s.enabled
      ? t("Une sauvegarde chaque nuit, chiffrée. On garde les {0} dernières quotidiennes, {1} hebdomadaires et {2} mensuelles.", s.daily, s.weekly, s.monthly)
      : t("La sauvegarde de la nuit est désactivée.");
  }
  backupActions.hidden = !status.destination;
  backupSettingsBox.hidden = !status.destination;
  showBackupOnline(status.online);
  showBackupPeer(status.peer);
  backupList.replaceChildren(...status.snapshots.map((snapshot) => {
    const item = element("li");
    const when = new Date(snapshot.created).toLocaleString(LOCALE);
    item.append(element("span", `${when} (${BACKUP_KINDS[snapshot.kind] || snapshot.kind}, ${sizeText(snapshot.size_mb)})`));
    const places = (snapshot.places || []).map((place) => BACKUP_PLACES[place] || place);
    if (places.length) item.append(element("span", t("Gardée : {0}", places.join(", ")), "meta"));
    if (snapshot.locked) {
      const button = element("button", t("Ouvrir avec le mot de passe"), "secondary");
      button.type = "button";
      button.addEventListener("click", () => unlockBackup(snapshot));
      item.append(button);
    }
    for (const service of snapshot.services) {
      if (service.error) continue;
      const button = element("button", t("Restaurer {0}", service.name), "secondary");
      button.type = "button";
      button.disabled = running;
      button.addEventListener("click", () => restoreService(snapshot, service));
      item.append(button);
      if (!service.volumes || !service.volumes.length) continue;
      const browse = element("button", t("Parcourir {0}", service.name), "secondary");
      browse.type = "button";
      browse.addEventListener("click", () => openBackupBrowser(snapshot, service.name, ""));
      item.append(browse);
    }
    return item;
  }));
  return running;
}

const BACKUP_PLACES = { disk: t("sur le disque"), online: t("en ligne"), peer: t("chez votre proche") };

async function askBackupPassword(snapshot) {
  const password = window.prompt(t("Cette sauvegarde a été faite sur une autre carte SD. Saisissez le mot de passe administrateur en vigueur à sa date :"));
  if (!password) return false;
  try {
    await backupRequest(`backups/${encodeURIComponent(snapshot.id)}/unlock`, { password });
    return true;
  } catch (error) {
    showMessage([error.message], "error");
    return false;
  }
}

async function unlockBackup(snapshot) {
  if (await askBackupPassword(snapshot)) await loadBackups().catch(() => null);
}

// Restauration d'un fichier (spécification 9.1) : parcourir la sauvegarde
// d'un service, télécharger un fichier (un dossier en .zip) ou le remettre en place.
const backupBrowser = document.getElementById("backupBrowser");
const backupBrowserTitle = document.getElementById("backupBrowserTitle");
const backupBrowserPath = document.getElementById("backupBrowserPath");
const backupBrowserState = document.getElementById("backupBrowserState");
const backupBrowserList = document.getElementById("backupBrowserList");
let backupBrowsing = null;

document.getElementById("backupBrowserClose").addEventListener("click", () => {
  backupBrowsing = null;
  backupBrowser.hidden = true;
});

// Le premier dossier est un volume « pi-<service>_<nom> » : seul <nom> parle.
function backupEntryName(service, path, name) {
  return path.includes("/") ? name : name.replace(`pi-${service}_`, "");
}

function backupFileURL(snapshot, service, path, action) {
  const query = new URLSearchParams({ service, path });
  return `api/backups/${encodeURIComponent(snapshot.id)}/${action}?${query}`;
}

async function openBackupBrowser(snapshot, service, path) {
  const browsing = { snapshot, service, path };
  backupBrowsing = browsing;
  if (backupBrowser.hidden) {
    backupBrowser.hidden = false;
    backupBrowser.scrollIntoView({ block: "start" });
  }
  backupBrowserTitle.textContent = t("Fichiers de {0}, sauvegarde du {1}", service, new Date(snapshot.created).toLocaleString(LOCALE));
  const crumbs = [["", t("Début")]];
  path.split("/").filter(Boolean).forEach((part, i, parts) => {
    crumbs.push([parts.slice(0, i + 1).join("/"), backupEntryName(service, i ? "x/x" : "x", part)]);
  });
  backupBrowserPath.replaceChildren(...crumbs.map(([target, label]) => {
    const button = element("button", label, "secondary");
    button.type = "button";
    button.addEventListener("click", () => openBackupBrowser(snapshot, service, target));
    return button;
  }));
  backupBrowserList.replaceChildren();
  let reply;
  for (;;) {
    try {
      reply = await api(backupFileURL(snapshot, service, path, "files").slice(4));
    } catch (error) {
      if (error.status === 403 && error.body.code === "password" && await askBackupPassword(snapshot)) continue;
      backupBrowserState.textContent = error.message;
      return;
    }
    if (backupBrowsing !== browsing) return;
    if (!reply.preparing) break;
    backupBrowserState.textContent = t("Lecture de la sauvegarde… {0} fichiers lus.", reply.count);
    await new Promise((resolve) => setTimeout(resolve, 2000));
    if (backupBrowsing !== browsing) return;
  }
  backupBrowserState.textContent = reply.entries.length
    ? (reply.truncated ? t("La liste est coupée : ce dossier contient trop de fichiers pour tous les montrer.") : "")
    : t("Dossier vide.");
  backupBrowserList.replaceChildren(...reply.entries.map((entry) => {
    const item = element("li");
    const name = backupEntryName(service, entry.path, entry.name);
    if (entry.dir) {
      const open = element("button", `${name}/`, "secondary");
      open.type = "button";
      open.addEventListener("click", () => openBackupBrowser(snapshot, service, entry.path));
      item.append(open);
    } else {
      item.append(element("span", `${name} (${sizeText(entry.size / 1048576)})`));
    }
    const download = element("a", t("Télécharger"));
    download.href = backupFileURL(snapshot, service, entry.path, "download");
    download.setAttribute("download", "");
    item.append(download);
    if (entry.path.includes("/")) {
      const putBack = element("button", t("Remettre en place"), "secondary");
      putBack.type = "button";
      putBack.addEventListener("click", () => putBackFile(snapshot, service, entry.path, name));
      item.append(putBack);
    }
    return item;
  }));
}

async function putBackFile(snapshot, service, path, name) {
  if (!window.confirm(t("Remettre {0} à sa place dans {1} ? Le service est arrêté le temps de l'écrire.", name, service))) return;
  const body = { service, path, replace: false };
  for (;;) {
    try {
      await backupRequest(`backups/${encodeURIComponent(snapshot.id)}/put-back`, body);
      showMessage([t("Remise en place de {0}…", name)], "info");
      break;
    } catch (error) {
      if (error.status === 409 && error.body.code === "exists" && !body.replace) {
        if (!window.confirm(t("{0} existe encore. Le remplacer par celui de la sauvegarde ?", name))) break;
        body.replace = true;
        continue;
      }
      showMessage([error.message], "error");
      break;
    }
  }
  await loadBackups().catch(() => null);
  watchBackups();
}

// Sauvegarde en ligne (stockage compatible S3) : les clés restent sur le Pi,
// la console ne les renvoie jamais.
const backupOnlineForm = document.getElementById("backupOnline");
const backupOnlineState = document.getElementById("backupOnlineState");
const backupOnlineRemove = document.getElementById("backupOnlineRemove");

function showBackupOnline(online) {
  const fields = backupOnlineForm.elements;
  backupOnlineRemove.hidden = !online.configured;
  backupOnlineState.textContent = online.configured
    ? t("Envoyées aussi en ligne vers {0}.", `${online.bucket} (${online.endpoint})`)
    : t("Pas encore de stockage en ligne.");
  for (const name of ["access_key_id", "secret_access_key"]) {
    fields[name].required = !online.configured;
    fields[name].placeholder = online.configured ? t("Clés enregistrées : laissez vide pour les garder.") : "";
  }
  if (backupOnlineForm.contains(document.activeElement) || !online.configured) return;
  fields.endpoint.value = online.endpoint;
  fields.region.value = online.region;
  fields.bucket.value = online.bucket;
}

backupOnlineForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const fields = backupOnlineForm.elements;
  const button = backupOnlineForm.querySelector("button");
  button.disabled = true;
  try {
    await api("backups/online", jsonBody("PUT", {
      endpoint: fields.endpoint.value, region: fields.region.value, bucket: fields.bucket.value,
      access_key_id: fields.access_key_id.value, secret_access_key: fields.secret_access_key.value,
    }));
    fields.access_key_id.value = "";
    fields.secret_access_key.value = "";
    showMessage([t("Stockage en ligne enregistré : l'essai d'écriture a réussi.")], "success");
    await loadBackups();
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
});

backupOnlineRemove.addEventListener("click", async () => {
  if (!window.confirm(t("Arrêter l'envoi en ligne ? Les sauvegardes déjà envoyées y restent."))) return;
  try {
    await api("backups/online", { method: "DELETE" });
    await loadBackups();
  } catch (error) {
    showMessage([error.message], "error");
  }
});

// Sauvegarde chez un proche : jumelage par code ; ce Pi accueille aussi les
// sauvegardes de proches dans un espace réservé.
const backupPeerBox = document.getElementById("backupPeerBox");
const backupPeerState = document.getElementById("backupPeerState");
const backupPeerForm = document.getElementById("backupPeer");
const backupPeerRemove = document.getElementById("backupPeerRemove");
const backupHostingState = document.getElementById("backupHostingState");
const backupHostingForm = document.getElementById("backupHosting");
const backupGuests = document.getElementById("backupGuests");
let backupCode = "";

function showBackupPeer(peer) {
  backupPeerBox.hidden = !peer || !peer.available;
  if (backupPeerBox.hidden) return;
  backupPeerForm.hidden = Boolean(peer.peer);
  backupPeerRemove.hidden = !peer.peer;
  backupPeerState.textContent = peer.peer
    ? t("Envoyées aussi chez votre proche ({0}), espace réservé : {1}.", peer.peer.address.replace("https://", ""), sizeText(peer.peer.quota_mb))
    : "";
  const hosting = peer.hosting;
  backupHostingForm.hidden = !hosting.available;
  backupHostingState.textContent = !hosting.available
    ? t("Branchez d'abord un disque de sauvegarde pour accueillir un proche.")
    : hosting.code_expires && backupCode ? t("Code à saisir sur le Pi de votre proche, valable une heure : {0}", backupCode) : "";
  backupGuests.replaceChildren(...hosting.guests.map((guest) => {
    const item = element("li");
    item.append(element("span", t("{0} : {1} utilisés sur {2}", guest.name, sizeText(guest.used_mb), sizeText(guest.quota_mb))));
    const remove = element("button", t("Ne plus accueillir"), "secondary");
    remove.type = "button";
    remove.addEventListener("click", async () => {
      if (!window.confirm(t("Ne plus accueillir {0} ? Ses sauvegardes gardées ici seront effacées.", guest.name))) return;
      try {
        await api(`backups/hosting/${encodeURIComponent(guest.id)}`, { method: "DELETE" });
        await loadBackups();
      } catch (error) {
        showMessage([error.message], "error");
      }
    });
    item.append(remove);
    return item;
  }));
}

backupPeerForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const fields = backupPeerForm.elements;
  try {
    await api("backups/peer", jsonBody("PUT", { address: fields.address.value, code: fields.code.value }));
    fields.code.value = "";
    showMessage([t("Jumelage réussi : vos sauvegardes partiront aussi chez votre proche.")], "success");
    await loadBackups();
  } catch (error) {
    showMessage([error.message], "error");
  }
});

backupPeerRemove.addEventListener("click", async () => {
  if (!window.confirm(t("Arrêter l'envoi chez ce proche ? Ce qu'il garde déjà y reste tant qu'il ne l'efface pas."))) return;
  try {
    await api("backups/peer", { method: "DELETE" });
    await loadBackups();
  } catch (error) {
    showMessage([error.message], "error");
  }
});

backupHostingForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const fields = backupHostingForm.elements;
  try {
    const reply = await api("backups/hosting/code", jsonBody("POST", { name: fields.name.value, quota_mb: Number(fields.quota.value) * 1024 }));
    backupCode = reply.code;
    await loadBackups();
  } catch (error) {
    showMessage([error.message], "error");
  }
});

// Mises à jour (spécification 9.2) : la console les signale, l'administrateur
// décide. Les correctifs de sécurité du système passent en premier, en rouge ;
// une nouvelle version de pi-appliance s'installe à la demande, avec retour
// automatique à l'ancienne si elle ne démarre pas.
// Cockpit (spécification 6.4) : installé à la demande ; ouvert sous
// /cockpit/, depuis la maison, aux administrateurs (plus largement si un
// administrateur le choisit, TODO-3.4), avec leur compte de la console
// (connexion unique, compte système créé par la console).
const cockpitBox = document.getElementById("cockpitBox");
const cockpitState = document.getElementById("cockpitState");
const cockpitInstall = document.getElementById("cockpitInstall");
const cockpitOpen = document.getElementById("cockpitOpen");
const cockpitRemove = document.getElementById("cockpitRemove");
let cockpitStatus = null;
let cockpitTimer = null;
// Carte de la Marketplace : icône, couleur, catégorie, licence et étiquette,
// comme celles du catalogue.
icons.cockpit = "M12 3a9 9 0 1 0 0 18a9 9 0 1 0 0-18zM12 12l4-4M12 7v1M7 12h1M16 12h1";
tint(cockpitBox, "cockpit");
cockpitBox.querySelector(".card-head").prepend(tile("cockpit"));
// Carte de Cockpit d'un utilisateur admis (TODO-3.4), dans « Mes services ».
const cockpitUserBox = document.getElementById("cockpitUserBox");
tint(cockpitUserBox, "cockpit");
cockpitUserBox.querySelector(".card-head").prepend(tile("cockpit"));
cockpitBox.append(element("p", t("{0} · licence {1}", t("Administration"), "LGPL-2.1"), "meta"), tagList(["administration"]));

function cockpitBusy(status) {
  return Boolean(status && status.job && (status.job.state === "starting" || status.job.state === "running"));
}

function showCockpit() {
  const status = cockpitStatus;
  const busy = cockpitBusy(status);
  const job = status.job;
  let color = "green";
  let state;
  if (busy) {
    color = "orange";
    state = job.action === "remove"
      ? t("Désinstallation en cours…")
      : t("Installation en cours… Cela prend quelques minutes.");
  } else if (status.installed && status.active) {
    state = status.user
      ? t("Installé. Vous y entrez avec votre compte pi-appliance (compte {0} sur le Pi).", status.user)
      : t("Installé. Vous y entrez avec votre compte pi-appliance : votre compte sur le Pi est créé à la première ouverture.");
  } else if (status.installed) {
    color = "red";
    state = t("Installé, mais il ne répond pas. Redémarrez le Pi.");
  } else if (job && job.state === "failed") {
    color = "red";
    state = job.message || t("L'opération a échoué. Réessayez plus tard.");
  } else {
    color = "";
    state = t("Pas installé.");
  }
  cockpitState.replaceChildren(element("span", undefined, `dot ${color}`), element("span", state));
  const installed = status.installed && !busy;
  cockpitInstall.hidden = busy || installed;
  cockpitOpen.hidden = !installed;
  cockpitRemove.hidden = !installed;
  cockpitOpen.disabled = !status.active;
}

// loadCockpit rend vrai quand plus rien n'est en cours.
async function loadCockpit() {
  try {
    cockpitStatus = await api("cockpit");
  } catch (error) {
    // Pas administrateur (403) ou console sans Cockpit (404) : pas de carte.
    // Sinon la carte reste dans la Marketplace et dit ce qui ne va pas, au
    // lieu de disparaître sans explication.
    cockpitStatus = null;
    if (error.status === 403 || error.status === 404) {
      cockpitBox.hidden = true;
      return true;
    }
    cockpitBox.hidden = false;
    cockpitState.replaceChildren(element("span", undefined, "dot red"), element("span", error.message));
    cockpitInstall.hidden = true;
    cockpitOpen.hidden = true;
    cockpitRemove.hidden = true;
    return true;
  }
  cockpitBox.hidden = false;
  showCockpit();
  loadCockpitAccess();
  return !cockpitBusy(cockpitStatus);
}

// Qui ouvre Cockpit (TODO-3.4) : par défaut les administrateurs, depuis la
// maison ; un administrateur peut l'ouvrir aussi aux utilisateurs et depuis
// internet (si la console y est ouverte), sans double authentification
// exigée, après un avertissement clair. Un utilisateur admis a sa carte dans
// « Mes services ».
const cockpitAccess = document.getElementById("cockpitAccess");
const cockpitAccessState = document.getElementById("cockpitAccessState");
const cockpitUsers = document.getElementById("cockpitUsers");
const cockpitInternet = document.getElementById("cockpitInternet");
const cockpitUserList = document.getElementById("cockpitUserList");
let cockpitAccessStatus = null;

async function loadCockpitAccess() {
  try {
    cockpitAccessStatus = await api("cockpit/access");
  } catch {
    // Console sans Cockpit, ou état illisible : rien de plus à l'écran.
    cockpitAccessStatus = null;
    cockpitAccess.hidden = true;
    cockpitUserList.hidden = true;
    return;
  }
  showCockpitAccess();
}

function showCockpitAccess() {
  const access = cockpitAccessStatus;
  const admin = currentAccount && currentAccount.role === "admin";
  cockpitUserList.hidden = admin || !(access && access.allowed && access.installed && access.active);
  if (!admin || !access) return;
  const installed = cockpitStatus && cockpitStatus.installed && !cockpitBusy(cockpitStatus);
  cockpitAccess.hidden = !installed;
  cockpitUsers.checked = Boolean(access.users);
  cockpitInternet.checked = Boolean(access.internet);
  const online = access.internet && access.console_internet;
  const lines = [access.users
    ? (online
      ? t("Ouvert à tous les comptes de la console (sauf les invités), aussi depuis internet.")
      : t("Ouvert à tous les comptes de la console (sauf les invités), depuis la maison."))
    : (online
      ? t("Ouvert aux administrateurs, aussi depuis internet.")
      : t("Ouvert aux administrateurs, depuis la maison."))];
  if (access.internet && !access.console_internet) {
    lines.push(t("La console n'est pas ouverte sur internet : Cockpit ne l'est donc pas non plus. Ouvrez-la dans « Console sur internet »."));
  }
  cockpitAccessState.textContent = lines.join(" ");
  // Depuis internet, sans Cockpit ouvert sur internet : le bouton n'y mènerait pas.
  if (!access.allowed) cockpitOpen.disabled = true;
}

async function saveCockpitAccess(box, warning) {
  if (box.checked && !window.confirm(warning)) {
    box.checked = false;
    return;
  }
  box.disabled = true;
  try {
    cockpitAccessStatus = await api("cockpit/access", jsonBody("PUT", { users: cockpitUsers.checked, internet: cockpitInternet.checked }));
    showMessage([t("Accès à Cockpit enregistré.")], "success");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    box.disabled = false;
  }
  await loadCockpitAccess();
}

cockpitUsers.addEventListener("change", () => saveCockpitAccess(cockpitUsers,
  t("Ouvrir Cockpit aussi aux utilisateurs de la console ? Toute personne qui a un compte aura tous les droits sur le Pi : elle pourra tout lire, tout modifier, tout effacer, même depuis internet si Cockpit y est ouvert. Un mot de passe volé suffit.")));
cockpitInternet.addEventListener("change", () => saveCockpitAccess(cockpitInternet,
  t("Ouvrir Cockpit aussi depuis internet ? Toute personne qui peut y entrer aura tous les droits sur le Pi, même depuis internet : un mot de passe volé suffit. La double authentification n'est pas exigée : conseillez-la à chaque compte.")));
document.getElementById("cockpitUserOpen").addEventListener("click", () => openCockpit(cockpitAccessStatus && cockpitAccessStatus.path));

function watchCockpit() {
  if (cockpitTimer) return;
  cockpitTimer = setInterval(async () => {
    const action = cockpitStatus && cockpitStatus.job && cockpitStatus.job.action;
    const done = await loadCockpit().catch(() => false);
    if (!done) return;
    clearInterval(cockpitTimer);
    cockpitTimer = null;
    const job = cockpitStatus && cockpitStatus.job;
    if (job && job.state === "done") {
      showMessage([action === "remove" ? t("Cockpit est désinstallé.") : t("Cockpit est installé.")], "success");
    } else if (job && job.state === "failed") {
      showMessage([job.message || t("L'opération a échoué. Réessayez plus tard.")], "error");
    }
  }, 5000);
}

cockpitInstall.addEventListener("click", async () => {
  if (!window.confirm(t("Installer Cockpit ? Il donne la main sur tout le Pi aux administrateurs de la console, depuis la maison."))) return;
  cockpitInstall.disabled = true;
  try {
    await api("cockpit/install", { method: "POST" });
    showMessage([t("Installation lancée : elle prend quelques minutes.")], "success");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    cockpitInstall.disabled = false;
  }
  await loadCockpit();
  watchCockpit();
});

// Ouvre Cockpit dans un nouvel onglet, avec le cookie délégué de la console
// (Traefik le demande à la console avant de laisser passer).
cockpitOpen.addEventListener("click", () => openCockpit(cockpitStatus.path));

async function openCockpit(path) {
  const tab = window.open("", "_blank");
  try {
    const { url } = await api("auth/return", jsonBody("POST", { return: window.location.origin + (path || "/cockpit/") }));
    if (tab) {
      tab.opener = null;
      tab.location.href = url;
    } else {
      window.location.assign(url);
    }
  } catch (error) {
    if (tab) tab.close();
    showMessage([error.message], "error");
  }
}

cockpitRemove.addEventListener("click", async () => {
  if (!window.confirm(t("Désinstaller Cockpit ? Les comptes du Pi créés pour Cockpit seront supprimés."))) return;
  cockpitRemove.disabled = true;
  try {
    await api("cockpit", { method: "DELETE" });
    showMessage([t("Désinstallation lancée.")], "info");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    cockpitRemove.disabled = false;
  }
  await loadCockpit();
  watchCockpit();
});

const updatesBox = document.getElementById("updatesBox");
const updatesState = document.getElementById("updatesState");
const updatesDetails = document.getElementById("updatesDetails");
const updatesApply = document.getElementById("updatesApply");
const updatesCheck = document.getElementById("updatesCheck");
const updatesAppliance = document.getElementById("updatesAppliance");
const updatesChannel = document.getElementById("updatesChannel");
let applianceTimer = null;
let updatesStatus = null;
let updatesTimer = null;
let serviceUpdates = 0;

function applyBusy(system) {
  return Boolean(system && system.apply && ["starting", "running"].includes(system.apply.state));
}

// upgradeBusy : une version de pi-appliance se télécharge ou s'installe.
function upgradeBusy(status) {
  const download = status && status.appliance.download;
  const upgrade = status && status.system && status.system.upgrade;
  return Boolean((download && download.state === "downloading") || (upgrade && ["starting", "running"].includes(upgrade.state)));
}

function showUpdates() {
  const status = updatesStatus;
  if (!status) {
    return;
  }
  const system = status.system;
  const busy = applyBusy(system);
  const download = status.appliance.download;
  const upgrade = system && system.upgrade;
  const upgrading = upgradeBusy(status);
  const security = system ? system.security : 0;
  const others = system ? system.packages.length - security : 0;
  const lines = [];
  let color = "green";
  let state;
  if (upgrading) {
    state = download && download.state === "downloading"
      ? t("Téléchargement de pi-appliance {0}…", download.version)
      : t("Installation de pi-appliance {0}… La console va redémarrer : cette page se rechargera toute seule.", upgrade.version);
    color = "orange";
  } else if (busy) {
    state = system.apply.state === "running"
      ? t("Installation des mises à jour du système : {0} %. Vos services peuvent s'arrêter quelques instants.", system.apply.percent)
      : t("Préparation de l'installation des mises à jour du système…");
    color = "orange";
  } else if (security > 0) {
    state = t("{0} pour le système : installez-les dès que possible.", plural(security, t("correctif de sécurité"), t("correctifs de sécurité")));
    color = "red";
  } else if (others > 0 || status.appliance.available || serviceUpdates > 0) {
    state = t("Des mises à jour sont disponibles.");
    color = "orange";
  } else {
    state = t("Tout est à jour.");
  }
  if (!busy && security > 0 && others > 0) {
    lines.push(element("p", t("Et {0} du système.", plural(others, t("autre mise à jour"), t("autres mises à jour")))));
  } else if (!busy && others > 0) {
    lines.push(element("p", t("Système : {0}.", plural(others, t("mise à jour"), t("mises à jour")))));
  }
  if (status.appliance.available && !upgrading) {
    const line = element("p", t("Une nouvelle version de pi-appliance est sortie : {0}. Elle ne s'installe pas toute seule.", status.appliance.latest));
    if (status.appliance.url) {
      const link = element("a", t("Voir les nouveautés"));
      link.href = status.appliance.url;
      link.target = "_blank";
      link.rel = "noopener";
      line.append(" ", link);
    }
    lines.push(line);
  }
  if (status.appliance.channel === "stable" && status.appliance.current.includes("-")) {
    lines.push(element("p", t("Vous avez une version bêta : vous resterez dessus jusqu'à la prochaine version stable plus récente."), "meta"));
  }
  if (!upgrading && download && download.state === "failed") {
    lines.push(element("p", download.message, "warning"));
  }
  if (!upgrading && upgrade && upgrade.state === "failed" && upgrade.version !== status.appliance.current) {
    lines.push(element("p", upgrade.rolled_back
      ? t("L'installation de pi-appliance {0} a échoué : l'ancienne version a été remise, vos services n'ont pas changé.", upgrade.version)
      : t("L'installation de pi-appliance {0} a échoué. Rien n'a été changé ; réessayez plus tard.", upgrade.version), "warning"));
  }
  if (upgrade && upgrade.state === "done" && upgrade.version === status.appliance.current && Date.now() - Date.parse(upgrade.at) < 86400000) {
    lines.push(element("p", t("pi-appliance {0} est installé.", upgrade.version)));
  }
  if (serviceUpdates > 0) {
    lines.push(element("p", t("{0} : utilisez « Mettre à jour » sur le service, plus bas.",
      plural(serviceUpdates, t("service a une nouvelle version"), t("services ont une nouvelle version")))));
  }
  if (system && system.reboot_required && !busy) {
    lines.push(element("p", t("Redémarrez le Pi pour terminer l'installation des mises à jour."), "warning"));
  }
  if (system && system.apply && system.apply.state === "failed" && system.packages.length > 0) {
    lines.push(element("p", t("La dernière installation des mises à jour a échoué. Réessayez plus tard."), "warning"));
  }
  if (status.system_error) {
    lines.push(element("p", t("La liste des mises à jour du système est illisible pour l'instant."), "meta"));
  }
  if (status.appliance.error) {
    lines.push(element("p", t("Impossible de savoir si une nouvelle version de pi-appliance est sortie : le site de publication ne répond pas."), "meta"));
  }
  if (status.checking) {
    lines.push(element("p", t("Recherche des mises à jour…"), "meta"));
  } else if (system && system.checked_at) {
    lines.push(element("p", t("Dernière recherche : {0}.", new Date(system.checked_at).toLocaleString(LOCALE)), "meta"));
  }
  updatesState.replaceChildren(element("span", undefined, `dot ${color}`), element("span", state));
  updatesDetails.replaceChildren(...lines);
  updatesBox.classList.toggle("alert", security > 0 && !busy);
  updatesApply.hidden = !system || busy || upgrading || system.packages.length === 0;
  updatesApply.disabled = false;
  updatesAppliance.hidden = !status.appliance.installable || busy;
  updatesAppliance.textContent = t("Installer pi-appliance {0}", status.appliance.latest || "");
  updatesAppliance.disabled = false;
  updatesCheck.disabled = busy || upgrading || status.checking;
  updatesChannel.value = status.appliance.channel || "stable";
  updatesChannel.disabled = upgrading;
}

// loadUpdates rend vrai quand plus rien n'est en cours (installation, recherche).
async function loadUpdates() {
  try {
    updatesStatus = await api("updates");
  } catch (error) {
    if ([401, 403, 404].includes(error.status)) {
      updatesBox.hidden = true;
      versionsBox.hidden = true;
      return true;
    }
    // NE PAS cacher la carte sur une autre erreur : Docker et Traefik redémarrent pendant les correctifs du système (TODO-5.29).
    updatesBox.hidden = false;
    updatesState.replaceChildren(element("span", undefined, "dot orange"), element("span", t("La console ne répond pas pour l'instant : nouvel essai dans quelques secondes.")));
    watchUpdates();
    return false;
  }
  updatesBox.hidden = false;
  showUpdates();
  if (versionsBox.hidden) loadVersions();
  else showVersions();
  const busy = applyBusy(updatesStatus.system) || updatesStatus.checking;
  if (busy) {
    watchUpdates();
  }
  if (upgradeBusy(updatesStatus)) {
    watchAppliance(updatesStatus.appliance.download ? updatesStatus.appliance.download.version : updatesStatus.system.upgrade.version);
  }
  return !busy;
}

// watchAppliance suit l'installation d'une version de pi-appliance : la
// console redémarre (les appels échouent un moment), puis répond avec la
// nouvelle version et la page se recharge ; si l'installation échoue,
// l'écran le dit (ancienne version remise).
function watchAppliance(version) {
  if (applianceTimer) {
    return;
  }
  const started = Date.now();
  applianceTimer = setInterval(async () => {
    const health = await api("health").catch(() => null);
    if (health && health.version === version) {
      clearInterval(applianceTimer);
      applianceTimer = null;
      showMessage([t("pi-appliance {0} est installé. La page se recharge…", version)], "success");
      setTimeout(() => window.location.reload(), 2000);
      return;
    }
    if (!health) {
      showMessage([t("La console redémarre avec la nouvelle version… Cette page se rechargera toute seule.")], "info");
      return;
    }
    const status = await api("updates").catch(() => null);
    if (status) {
      updatesStatus = status;
      showUpdates();
    }
    if ((status && !upgradeBusy(status)) || Date.now() - started > 15 * 60000) {
      clearInterval(applianceTimer);
      applianceTimer = null;
      if (status && status.system && status.system.upgrade && status.system.upgrade.state === "failed") {
        showMessage([status.system.upgrade.rolled_back
          ? t("L'installation de pi-appliance {0} a échoué : l'ancienne version a été remise, vos services n'ont pas changé.", version)
          : t("L'installation de pi-appliance {0} a échoué. Rien n'a été changé ; réessayez plus tard.", version)], "error");
      } else if (status && status.appliance.download && status.appliance.download.state === "failed") {
        showMessage([status.appliance.download.message], "error");
      }
    }
  }, 5000);
}

// Canal de version (spécification 5.4) : stable par défaut ; le canal bêta
// demande une confirmation, et rien ne s'installe tout seul.
updatesChannel.addEventListener("change", async () => {
  const channel = updatesChannel.value;
  if (channel === "beta" && !window.confirm(t("Recevoir les versions bêta ?\n\nElles apportent les nouveautés en avance, mais peuvent contenir des erreurs. Rien ne s'installe tout seul : vous choisissez toujours."))) {
    updatesChannel.value = (updatesStatus && updatesStatus.appliance.channel) || "stable";
    return;
  }
  updatesChannel.disabled = true;
  try {
    await api("updates/channel", { method: "PUT", body: JSON.stringify({ channel }) });
    showMessage([channel === "beta" ? t("Vous recevrez désormais les versions bêta.") : t("Vous recevrez désormais les versions stables seulement.")], "success");
    if (versionsDetails.open) fetchVersions();
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    updatesChannel.disabled = false;
  }
  await loadUpdates();
  watchUpdates();
});

updatesAppliance.addEventListener("click", async () => {
  const version = updatesStatus && updatesStatus.appliance.latest;
  if (!version || !window.confirm(t("Installer pi-appliance {0} maintenant ?\n\nLa console et l'accès aux services seront coupés une minute ou deux. Si la nouvelle version ne démarre pas, l'ancienne revient toute seule.", version))) {
    return;
  }
  updatesAppliance.disabled = true;
  try {
    await api("updates/appliance", { method: "POST" });
    showMessage([t("Téléchargement de pi-appliance {0}…", version)], "info");
    watchAppliance(version);
  } catch (error) {
    showMessage([error.message], "error");
  }
  await loadUpdates();
});

function watchUpdates() {
  if (updatesTimer) {
    return;
  }
  updatesTimer = setInterval(async () => {
    const wasApplying = applyBusy(updatesStatus && updatesStatus.system);
    const done = await loadUpdates().catch(() => false);
    if (done) {
      clearInterval(updatesTimer);
      updatesTimer = null;
      const apply = updatesStatus && updatesStatus.system && updatesStatus.system.apply;
      if (wasApplying && apply && apply.state === "done") {
        showMessage([t("Les mises à jour du système sont installées.")], "success");
      } else if (wasApplying && apply && apply.state === "failed") {
        showMessage([t("La dernière installation des mises à jour a échoué. Réessayez plus tard.")], "error");
      }
    }
  }, 5000);
}

updatesApply.addEventListener("click", async () => {
  if (!window.confirm(t("Installer les mises à jour du système maintenant ?\n\nCela peut prendre plusieurs minutes. Vos services peuvent s'arrêter quelques instants pendant l'installation."))) {
    return;
  }
  updatesApply.disabled = true;
  try {
    await api("updates/system", { method: "POST" });
    showMessage([t("Installation des mises à jour du système… Cette page se mettra à jour toute seule.")], "info");
  } catch (error) {
    showMessage([error.message], "error");
  }
  await loadUpdates();
  watchUpdates();
});

updatesCheck.addEventListener("click", async () => {
  updatesCheck.disabled = true;
  try {
    await api("updates/check", { method: "POST" });
  } catch (error) {
    showMessage([error.message], "error");
  }
  await loadUpdates();
  watchUpdates();
});

// Versions de la console (TODO-5.26) : version installée, versions publiées
// sur le canal choisi, et installation de l'une d'elles, plus récente ou plus
// ancienne (même API que pia update), administrateurs seulement. La liste
// n'est lue qu'à l'ouverture (GitHub limite les demandes sans compte).
const versionsBox = document.getElementById("versionsBox");
const versionsCurrent = document.getElementById("versionsCurrent");
const versionsDetails = document.getElementById("versionsDetails");
const versionsState = document.getElementById("versionsState");
const versionsList = document.getElementById("versionsList");
let versionsHealth = null;
let versionsData = null;

function versionNumber(version) {
  return String(version || "").replace(/^v/, "");
}

function versionDay(date) {
  const day = /^\d{4}-\d{2}-\d{2}$/.test(date || "") ? new Date(`${date}T12:00:00`) : null;
  return day && !Number.isNaN(day.getTime()) ? day.toLocaleDateString(LOCALE, { day: "numeric", month: "long", year: "numeric" }) : "";
}

async function loadVersions() {
  if (!versionsHealth) {
    versionsHealth = await api("health").catch(() => null);
  }
  versionsBox.hidden = false;
  const health = versionsHealth || {};
  const parts = [];
  const number = health.release ? versionNumber(health.version) : (updatesStatus && updatesStatus.appliance.current) || "";
  parts.push(health.release ? t("Version installée : {0}", number) : t("Version installée : {0} (version de développement)", number || "?"));
  const day = versionDay(health.date);
  if (day) parts.push(t("du {0}", day));
  const line = element("strong", parts.join(" "));
  versionsCurrent.replaceChildren(line);
  if (health.commit) {
    versionsCurrent.append(` · ${t("commit")} `, footerLink(`${REPO_URL}/commit/${encodeURIComponent(health.commit)}`, element("code", health.commit.slice(0, 7))));
  }
  if (versionsDetails.open) {
    await fetchVersions();
  }
}

async function fetchVersions() {
  versionsState.textContent = t("Lecture des versions publiées…");
  try {
    versionsData = await api("updates/versions");
    versionsState.textContent = versionsData.channel === "beta"
      ? t("Versions stables et bêta (choix dans « Mises à jour »).")
      : t("Versions stables seulement (choix dans « Mises à jour »).");
  } catch (error) {
    versionsData = null;
    const code = error.body && error.body.code;
    versionsState.textContent = code === "offline"
      ? t("Impossible de lire la liste des versions : le site de publication ne répond pas.")
      : code === "unavailable" ? t("La liste des versions n'est pas disponible sur cette console.") : error.message;
  }
  showVersions();
}

function showVersions() {
  if (!versionsData) {
    versionsList.replaceChildren();
    return;
  }
  const upgrading = upgradeBusy(updatesStatus);
  const items = versionsData.versions.map((version) => {
    const item = element("li");
    const head = element("span");
    const number = element("strong", versionNumber(version.version));
    number.dir = "ltr";
    head.append(number);
    const facts = [];
    const day = versionDay(version.date);
    if (day) facts.push(day);
    if (version.preview) facts.push(t("bêta"));
    if (version.current) facts.push(t("version installée"));
    if (facts.length) head.append(` · ${facts.join(" · ")}`);
    item.append(head);
    if (version.summary) {
      // Résumé tiré des notes de la version, en français.
      const summary = element("span", version.summary, "meta");
      summary.lang = "fr";
      summary.dir = "auto";
      item.append(summary);
    }
    const link = footerLink(version.url || `${REPO_URL}/releases/tag/${encodeURIComponent(version.version)}`, t("Notes de la version"));
    item.append(link);
    if (!version.current && version.installable && !upgrading) {
      const button = element("button", t("Installer cette version"), "secondary");
      button.type = "button";
      button.addEventListener("click", () => installVersion(version, button));
      item.append(button);
    } else if (!version.current && !version.installable) {
      item.append(element("span", t("Cette version ne s'installe pas depuis la console : il faut flasher la carte SD avec son image."), "meta"));
    }
    return item;
  });
  if (items.length === 0) {
    items.push(element("li", t("Aucune version publiée sur ce canal pour l'instant.")));
  }
  versionsList.replaceChildren(...items);
}

async function installVersion(version, button) {
  const number = versionNumber(version.version);
  const question = version.newer
    ? t("Mettre à jour vers pi-appliance {0} ?\n\nLa console et l'accès aux services seront coupés une minute ou deux. Si la nouvelle version ne démarre pas, l'ancienne revient toute seule.", number)
    : t("Revenir à pi-appliance {0}, une version précédente ?\n\nLes réglages faits depuis peuvent ne pas être compris par l'ancienne version. Les services et leurs données sont gardés.\n\nLa console et l'accès aux services seront coupés une minute ou deux. Si cette version ne démarre pas, celle d'aujourd'hui revient toute seule.", number);
  if (!window.confirm(question)) {
    return;
  }
  button.disabled = true;
  try {
    const answer = await api("updates/appliance", { method: "POST", body: JSON.stringify({ version: version.version }) });
    const target = (answer && answer.version) || version.version;
    showMessage([t("Téléchargement de pi-appliance {0}…", target)], "info");
    watchAppliance(target);
  } catch (error) {
    showMessage([error.message], "error");
    button.disabled = false;
  }
  await loadUpdates();
}

versionsDetails.addEventListener("toggle", () => {
  if (versionsDetails.open) fetchVersions();
});

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
const wifiWpsButton = document.getElementById("wifiWps");
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
  "wps-timeout": t("La dernière association a échoué : aucune box n'a répondu. Appuyez sur le bouton WPS de la box dans les 2 minutes, ou entrez le nom et le mot de passe du Wi-Fi."),
  "wps-failed": t("L'association par le bouton de la box a échoué : entrez le nom et le mot de passe du Wi-Fi."),
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
    networkAp.textContent = named(t("Le Wi-Fi « pi-appliance » ne sert qu'à configurer le Pi : il se fermera dès que le Pi sera relié à votre réseau, ou au plus tard dans {0}. Redémarrez alors le Pi pour le rouvrir.", minutesText(seconds)));
  }
}

// Nom du Pi (name.sh) : il remplace « pi-appliance » dans les phrases qui
// nomment le Wi-Fi de configuration ou l'adresse pi-appliance.local, quelle
// que soit la langue ; le chemin /pi-appliance de la console ne change pas.
let piName = "pi-appliance";

function named(text) {
  return text
    .replace(/pi-appliance\.local/g, `${piName}.local`)
    .replace(/(^|[^\/\w-])pi-appliance(?![\w./-])/g, (match, before) => `${before}${piName}`);
}

// Dernière adresse du Pi sur le réseau de la maison (network.sh, TODO-2.58) :
// inconnue avant la bascule, elle est redonnée si le Wi-Fi de configuration se rouvre.
const networkLastHome = document.getElementById("networkLastHome");

function showLastHome(state) {
  const home = state.lastHome;
  networkLastHome.hidden = !state.accessPoint.open || !home || !home.addresses || home.addresses.length === 0;
  if (!networkLastHome.hidden) {
    networkLastHome.textContent = t("La dernière fois qu'il était relié à votre réseau, le Pi s'appelait « {0} » et avait l'adresse {1} : depuis votre réseau, ouvrez https://{0}.local/pi-appliance ou https://{1}/pi-appliance", home.name, home.addresses[0]);
  }
}

function showNetworkState(state) {
  piName = state.name || (state.accessPoint && state.accessPoint.ssid) || piName;
  nameForm.elements.name.value = piName;
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
  showLastHome(state);
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
  showCountrySuggestion(state);
}

const nameForm = document.getElementById("nameForm");

nameForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const name = nameForm.elements.name.value.trim();
  if (name === piName || !window.confirm(t("Renommer le Pi en « {0} » ? La console s'ouvrira ensuite à l'adresse https://{0}.local/pi-appliance ; cette page sera coupée quelques secondes.", name))) {
    return;
  }
  const button = nameForm.querySelector("button");
  button.disabled = true;
  try {
    const result = await api("network/name", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
    piName = result.name;
    showMessage([t("Le Pi s'appelle maintenant « {0} ». Dans quelques secondes, ouvrez https://{0}.local/pi-appliance", result.name)], "success");
  } catch (error) {
    nameForm.elements.name.value = piName;
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
});

// Pays déduit de la langue du navigateur : la région de la langue (« de-AT »),
// sinon le pays principal de la langue ; proposé, jamais appliqué sans accord.
const LANGUAGE_COUNTRIES = { fr: "FR", en: "GB", es: "ES", pt: "PT", it: "IT", de: "DE", nl: "NL", tr: "TR", zh: "CN", ja: "JP", ar: "SA", fa: "IR" };

function browserCountry() {
  for (const tag of navigator.languages || [navigator.language || ""]) {
    const [language, ...rest] = tag.split("-");
    const region = rest.find((part) => /^[A-Za-z]{2}$/.test(part));
    const code = region ? region.toUpperCase() : LANGUAGE_COUNTRIES[language.toLowerCase()];
    if (code && COUNTRIES[code]) {
      return code;
    }
  }
  return "";
}

const countrySuggestion = document.getElementById("countrySuggestion");

function showCountrySuggestion(state) {
  const code = browserCountry();
  countrySuggestion.hidden = state.countryChosen !== false || !code || code === state.country;
  if (countrySuggestion.hidden) {
    return;
  }
  countrySuggestion.querySelector("span").textContent = t("Le Pi est réglé sur le pays « {0} », mais votre navigateur indique « {1} ». Choisissez le pays où se trouve le Pi.", COUNTRIES[state.country] || state.country, COUNTRIES[code]);
  const button = countrySuggestion.querySelector("button");
  button.textContent = t("Choisir « {0} »", COUNTRIES[code]);
  button.onclick = () => chooseCountry(code);
}

async function chooseCountry(code) {
  wifiCountry.disabled = true;
  try {
    const result = await api("network/country", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ country: code }),
    });
    wifiCountry.value = result.country;
    wifiCountry.dataset.current = result.country;
    countrySuggestion.hidden = true;
    showMessage([t("Pays du Wi-Fi : {0}. Cherchez à nouveau les Wi-Fi si le vôtre n'apparaît pas.", COUNTRIES[result.country] || result.country)], "success");
  } catch (error) {
    wifiCountry.value = wifiCountry.dataset.current;
    showMessage([error.message], "error");
  } finally {
    wifiCountry.disabled = false;
  }
}

wifiCountry.addEventListener("change", () => chooseCountry(wifiCountry.value));

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
  let list = await api("network/wifi").catch(() => ({ networks: [] }));
  // Aucune recherche depuis le démarrage (Pi relié directement à la maison) :
  // la lancer ici, elle ne coupe rien tant que le point d'accès est fermé.
  if (!list.scannedAt && !state.accessPoint.open) {
    list = await api("network/wifi/scan", { method: "POST" }).catch(() => list);
  }
  showWifiList(list);
  clearInterval(networkTimer);
  networkTimer = setInterval(showApCountdown, 30000);
  return state;
}

wifiScanButton.addEventListener("click", async () => {
  if (apClosesAt && !window.confirm(named(t("Pour chercher les Wi-Fi, le Wi-Fi « pi-appliance » se coupe quelques secondes. Votre téléphone s'y reconnectera tout seul. Continuer ?")))) {
    return;
  }
  wifiScanButton.disabled = true;
  showMessage([t("Recherche des Wi-Fi…")], "info");
  try {
    showWifiList(await api("network/wifi/scan", { method: "POST" }));
    message.hidden = true;
  } catch (error) {
    showMessage([error.message, named(t("Si la page ne répond plus, reconnectez-vous au Wi-Fi « pi-appliance »."))], "error");
  } finally {
    wifiScanButton.disabled = false;
  }
});

wifiOtherButton.addEventListener("click", () => chooseWifi(null, null));

wifiWpsButton.addEventListener("click", async () => {
  if (!window.confirm(t("Après avoir validé, appuyez dans les 2 minutes sur le bouton WPS de votre box (souvent marqué « WPS » ou de deux flèches en rond). Pendant ce temps, le Pi n'est plus joignable par cette page. Continuer ?"))) {
    return;
  }
  wifiWpsButton.disabled = true;
  try {
    await api("network/wifi/wps", { method: "POST" });
    wifiForm.hidden = true;
    showMessage(apClosesAt ? [
      named(t("Appuyez maintenant sur le bouton WPS de votre box, dans les 2 minutes. Le Wi-Fi « pi-appliance » va se fermer.")),
      named(t("Reconnectez ensuite votre téléphone ou votre ordinateur au Wi-Fi de votre box, puis ouvrez https://pi-appliance.local/pi-appliance")),
      named(t("Si cette adresse ne s'ouvre pas (certains téléphones Android et ordinateurs Windows ne trouvent pas les adresses en .local), cherchez l'appareil « pi-appliance » dans la liste des appareils de votre box et ouvrez https://<son adresse>/pi-appliance. Le navigateur affichera une alerte sur le certificat : vous pouvez continuer.")),
      named(t("Si le Wi-Fi « pi-appliance » réapparaît dans quelques minutes, l'association a échoué : reconnectez-vous-y pour réessayer, ou entrez le nom et le mot de passe du Wi-Fi.")),
    ] : [named(t("Appuyez maintenant sur le bouton WPS de votre box, dans les 2 minutes. Si cette page ne répond plus, ouvrez https://pi-appliance.local/pi-appliance"))], "info");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    wifiWpsButton.disabled = false;
  }
});

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
      named(t("Le Pi se connecte au Wi-Fi « {0} ». Le Wi-Fi « pi-appliance » va se fermer.", ssid)),
      named(t("Reconnectez votre téléphone ou votre ordinateur au Wi-Fi « {0} », puis ouvrez https://pi-appliance.local/pi-appliance", ssid)),
      named(t("Si cette adresse ne s'ouvre pas (certains téléphones Android et ordinateurs Windows ne trouvent pas les adresses en .local), cherchez l'appareil « pi-appliance » dans la liste des appareils de votre box et ouvrez https://<son adresse>/pi-appliance. Le navigateur affichera une alerte sur le certificat : vous pouvez continuer.")),
      named(t("Si le Wi-Fi « pi-appliance » réapparaît dans une minute, la connexion a échoué : reconnectez-vous-y pour réessayer.")),
    ] : [named(t("Le Pi se connecte au Wi-Fi « {0} ». Si cette page ne répond plus, ouvrez https://pi-appliance.local/pi-appliance", ssid))], "info");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
});

wiredButton.addEventListener("click", async () => {
  wiredButton.disabled = true;
  try {
    const previousName = piName;
    await api("network/wired", { method: "POST" });
    const state = await loadNetwork();
    const lines = piName === previousName ? [] : [t("Un autre appareil de votre réseau s'appelle déjà « {0} » : le Pi a pris le nom « {1} ».", previousName, piName)];
    lines.push(named(t("C'est noté : le Pi utilise le câble réseau. Le Wi-Fi « pi-appliance » va se fermer : ouvrez ensuite https://pi-appliance.local/pi-appliance depuis votre réseau.")));
    if (state && state.addresses.length > 0) {
      lines.push(t("Vous pouvez aussi ouvrir {0} : le navigateur affichera une alerte sur le certificat, vous pouvez continuer.", state.addresses.map((address) => `https://${address}/pi-appliance`).join(", ")));
    }
    showMessage(lines, "success");
    if (wizardStep === "network" && state && state.configured) {
      await wizardCheck(state);
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
    loadAddress();
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

// Adresse <nom>.pi-appliance.com (spécification 7.1) : le Pi vérifie les règles
// du nom, le service d'adresses du projet dit s'il est libre et le réserve.
const addressState = document.getElementById("addressState");
const addressDetails = document.getElementById("addressDetails");
const addressForm = document.getElementById("addressForm");
const addressCheck = document.getElementById("addressCheck");
const addressReserve = document.getElementById("addressReserve");
const addressRelease = document.getElementById("addressRelease");
let addressCheckTimer = null;
let addressCheckCount = 0;
let addressHostname = "";

function showAddress(status) {
  addressDetails.replaceChildren();
  const reserved = Boolean(status.name);
  addressHostname = status.hostname || "";
  addressForm.hidden = reserved || !status.service;
  addressRelease.hidden = !reserved;
  if (!status.service) {
    addressState.replaceChildren(element("span", undefined, "dot orange"), element("span", t("La réservation d'une adresse pi-appliance.com n'est pas possible depuis cette console.")));
    return;
  }
  if (!reserved) {
    addressState.replaceChildren(element("span", undefined, "dot orange"), element("span", status.error || t("Pas encore d'adresse pi-appliance.com.")));
    if (!status.duckdns) {
      addressDetails.append(element("p", t("Réglez d'abord DuckDNS plus bas : votre adresse pointera vers votre nom DuckDNS."), "meta"));
    }
    return;
  }
  const state = status.error
    ? t("Votre adresse est {0}, mais le dernier contact avec le service d'adresses a échoué : {1}", status.hostname, status.error)
    : t("Votre adresse : {0}.", status.hostname);
  addressState.replaceChildren(element("span", undefined, `dot ${status.error ? "red" : "green"}`), element("span", state));
  const link = element("a", `https://${status.hostname}`);
  link.href = `https://${status.hostname}`;
  link.rel = "noopener";
  const linkLine = element("p", undefined, "meta");
  linkLine.append(link);
  addressDetails.append(linkLine);
  if (status.target) addressDetails.append(element("p", t("Elle pointe vers votre nom DuckDNS {0}.", status.target), "meta"));
  const lastSync = duckDNSDate(status.last_sync);
  if (lastSync) addressDetails.append(element("p", t("Dernier contact avec le service d'adresses le {0}.", lastSync), "meta"));
}

// Console sur internet (spécification 8.3) : fermée par défaut, ouverte par un
// administrateur ; la double authentification y est conseillée, plus obligatoire.
const internetBox = document.getElementById("internetBox");
const internetState = document.getElementById("internetState");
const internetOpen = document.getElementById("internetOpen");
const internetClose = document.getElementById("internetClose");
const internetDomainForm = document.getElementById("internetDomainForm");
const internetDomainSave = document.getElementById("internetDomainSave");
const internetDomainRemove = document.getElementById("internetDomainRemove");

function showInternet(status) {
  internetBox.hidden = false;
  // L'adresse personnelle se règle à la maison seulement.
  internetDomainForm.hidden = status.internet;
  internetDomainForm.elements.domain.value = status.domain || "";
  const help = document.getElementById("internetDomainHelp");
  help.replaceChildren();
  if (status.domain_target) {
    help.append(element("p", t("Le plus simple : un alias (entrée CNAME) vers {0}. Il suit l'adresse de votre box, sans mot de passe de votre fournisseur sur le Pi.", status.domain_target), "meta"));
    help.append(element("pre", `${status.domain || "maison.exemple.fr"}. CNAME ${status.domain_target}.`, "stats-preview"));
  } else {
    help.append(element("p", t("Sans DuckDNS, faites pointer le nom vers l'adresse IP publique de votre box (entrée A) ; elle peut changer : réglez plutôt DuckDNS, plus haut."), "meta"));
  }
  internetDomainRemove.hidden = !status.domain;
  internetOpen.hidden = status.enabled || status.internet;
  internetClose.hidden = !status.enabled;
  const dot = element("span", undefined, `dot ${status.enabled ? "ok" : ""}`);
  let text;
  if (status.enabled && status.url) {
    text = t("La console est ouverte sur internet : {0}", status.url);
  } else if (status.enabled) {
    text = t("La console s'ouvrira sur internet dès que le Pi aura une adresse internet (DuckDNS ou pi-appliance.com, ci-dessus).");
  } else if (!status.totp) {
    text = t("La console ne s'ouvre qu'à la maison. Conseillé avant de l'ouvrir : activez la double authentification dans « Mon compte ».");
  } else {
    text = t("La console ne s'ouvre qu'à la maison.");
  }
  internetState.replaceChildren(dot, element("span", text));
}

async function loadInternet() {
  try {
    showInternet(await api("internet-console"));
  } catch {
    internetBox.hidden = true;
  }
}

async function setInternet(enabled) {
  if (enabled && !window.confirm(t("Ouvrir la console sur internet ? Chaque compte pourra s'y connecter depuis internet avec son mot de passe. Conseillé : la double authentification, dans « Mon compte »."))) return;
  if (!enabled && !window.confirm(t("Fermer la console sur internet ? Elle ne s'ouvrira plus qu'à la maison."))) return;
  const button = enabled ? internetOpen : internetClose;
  button.disabled = true;
  try {
    const status = await api(`internet-console?enabled=${enabled}`, { method: "PUT" });
    if (!enabled && status.internet) {
      window.location.reload();
      return;
    }
    showInternet(status);
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
}

async function setInternetDomain(domain) {
  const button = domain ? internetDomainSave : internetDomainRemove;
  button.disabled = true;
  try {
    showInternet(await api("internet-console/domain", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ domain }),
    }));
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
}

internetDomainForm.addEventListener("submit", (event) => {
  event.preventDefault();
  setInternetDomain(internetDomainForm.elements.domain.value.trim());
});
internetDomainRemove.addEventListener("click", () => {
  if (window.confirm(t("Retirer l'adresse personnelle {0} ?", internetDomainForm.elements.domain.value))) setInternetDomain("");
});
internetOpen.addEventListener("click", () => setInternet(true));
internetClose.addEventListener("click", () => setInternet(false));

async function loadAddress() {
  try {
    showAddress(await api("address"));
  } catch (error) {
    addressState.replaceChildren(element("span", undefined, "dot red"), element("span", error.message));
  }
}

async function checkAddressName() {
  const name = addressForm.elements.name.value.trim();
  const count = ++addressCheckCount;
  if (!name) {
    addressCheck.textContent = "";
    return;
  }
  try {
    const result = await api(`address/check?name=${encodeURIComponent(name)}`);
    if (count !== addressCheckCount) return;
    if (!result.valid) {
      addressCheck.textContent = result.reason;
    } else if (result.available) {
      addressCheck.textContent = t("{0} est libre.", result.hostname);
    } else {
      addressCheck.textContent = t("{0} n'est pas libre : {1}.", result.hostname, result.reason);
    }
  } catch (error) {
    if (count === addressCheckCount) addressCheck.textContent = error.message;
  }
}

addressForm.elements.name.addEventListener("input", () => {
  clearTimeout(addressCheckTimer);
  addressCheckTimer = setTimeout(checkAddressName, 400);
});

addressForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  addressReserve.disabled = true;
  try {
    const result = await api("address", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: addressForm.elements.name.value }),
    });
    addressForm.reset();
    addressCheck.textContent = "";
    showAddress(result);
    showMessage([t("L'adresse {0} est réservée. Elle marchera depuis internet d'ici quelques minutes.", result.hostname)], "success");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    addressReserve.disabled = false;
  }
});

addressRelease.addEventListener("click", async () => {
  if (!window.confirm(t("Libérer l'adresse {0} ? Vos services n'y seront plus joignables, et une autre appliance pourra la prendre dans 30 jours.", addressHostname))) return;
  addressRelease.disabled = true;
  try {
    await api("address", { method: "DELETE" });
    await loadAddress();
    showMessage([t("L'adresse est libérée.")], "success");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    addressRelease.disabled = false;
  }
});

// Statistiques anonymes (spécification 8.4) : désactivées par défaut ;
// l'administrateur voit exactement ce qui part avant de les accepter.
const statsSection = document.getElementById("statsSection");
const statsState = document.getElementById("statsState");
const statsList = document.getElementById("statsList");
const statsPreview = document.getElementById("statsPreview");
const statsToggle = document.getElementById("statsToggle");
let statsEnabled = false;

function showStats(status) {
  statsEnabled = status.enabled;
  const report = status.report;
  const crashes = report.crashes.reduce((total, crash) => total + crash.count, 0);
  statsList.replaceChildren(
    element("li", t("Version de la console : {0}", report.version)),
    element("li", t("Modèle du Pi : {0}", report.model || t("inconnu"))),
    element("li", t("Applications de la marketplace installées : {0}", report.services.join(", ") || t("aucune"))),
    element("li", t("Services ajoutés hors marketplace : {0} (leur nom et leur contenu ne sont pas envoyés)", report.custom_services)),
    element("li", t("Plantages de la console depuis le dernier envoi : {0}", crashes)),
  );
  statsPreview.textContent = JSON.stringify(report, null, 2);
  statsToggle.textContent = status.enabled ? t("Ne plus envoyer les statistiques") : t("Envoyer les statistiques");
  statsToggle.className = status.enabled ? "secondary" : "";
  let state;
  let color;
  if (!status.enabled) {
    state = t("Les statistiques ne sont pas envoyées.");
    color = "orange";
  } else if (status.error) {
    state = t("Le dernier envoi a échoué : {0}. Nouvel essai dans l'heure.", status.error);
    color = "red";
  } else if (status.last_sent) {
    state = t("Statistiques acceptées. Dernier envoi le {0}.", duckDNSDate(status.last_sent));
    color = "green";
  } else {
    state = t("Statistiques acceptées. Le premier envoi part dans l'heure.");
    color = "green";
  }
  statsState.replaceChildren(element("span", undefined, `dot ${color}`), element("span", state));
}

async function loadStats() {
  // Écran caché quand la console ne propose pas les statistiques anonymes
  // (fonction désactivée, TODO-4.7) : l'API répond alors par une erreur.
  try {
    showStats(await api("stats"));
    statsSection.hidden = false;
  } catch {
    statsSection.hidden = true;
  }
}

statsToggle.addEventListener("click", async () => {
  statsToggle.disabled = true;
  try {
    const enabled = !statsEnabled;
    showStats(await api("stats", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ enabled }),
    }));
    showMessage([enabled ? t("Merci : les statistiques anonymes seront envoyées une fois par jour.") : t("Les statistiques ne seront plus envoyées.")], "success");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    statsToggle.disabled = false;
  }
});

// Accès depuis internet (spécification 7.2) : les ports de la box ouverts vers
// le Pi par UPnP, avec l'accord de l'administrateur, sinon à la main (guide),
// et le test de l'accès. Le Pi ne peut pas se joindre lui-même depuis internet :
// le dernier mot revient à un téléphone en 4G.
const accessSection = document.getElementById("accessSection");
const accessState = document.getElementById("accessState");
const accessPorts = document.getElementById("accessPorts");
const accessLocal = document.getElementById("accessLocal");
const accessEnable = document.getElementById("accessEnable");
const accessDisable = document.getElementById("accessDisable");
const accessTestButton = document.getElementById("accessTest");
const accessChecks = document.getElementById("accessChecks");
const accessPhone = document.getElementById("accessPhone");
let accessPortList = "";

function portLabel(port) {
  return port.protocol === "UDP" ? t("port {0} (UDP)", port.port) : t("port {0}", port.port);
}

function showAccess(status) {
  accessPortList = status.ports.map(portLabel).join(", ");
  const upnp = status.upnp;
  const enabled = Boolean(upnp && upnp.enabled);
  accessEnable.hidden = !status.automatic || enabled;
  accessDisable.hidden = !status.automatic || !enabled;
  accessLocal.hidden = !status.local_ip;
  accessLocal.textContent = status.local_ip ? t("Adresse du Pi sur votre réseau : {0}", status.local_ip) : "";
  accessPorts.replaceChildren();
  accessPorts.hidden = !enabled;
  let dot = "orange";
  let text;
  if (!status.automatic) {
    text = t("À ouvrir à la main sur votre box, vers le Pi : {0}.", accessPortList);
  } else if (!enabled) {
    dot = "";
    text = t("Les ports ne sont pas ouverts automatiquement. À ouvrir sur votre box, vers le Pi : {0}.", accessPortList);
  } else {
    for (const port of upnp.ports) {
      accessPorts.append(port.open
        ? checkLine("green", t("Le {0} de la box mène au Pi.", portLabel(port)))
        : checkLine("red", port.error || t("La box n'a pas ouvert le {0}.", portLabel(port))));
    }
    if (upnp.error) {
      dot = "red";
      text = upnp.error;
    } else {
      dot = "green";
      text = upnp.gateway
        ? t("La box « {0} » fait suivre les ports vers le Pi. Le Pi renouvelle cette ouverture toutes les 30 minutes.", upnp.gateway)
        : t("Ouverture automatique en cours…");
    }
  }
  accessState.replaceChildren(element("span", undefined, `dot ${dot}`), element("span", text));
}

async function loadAccess() {
  try {
    showAccess(await api("internet-access"));
  } catch (error) {
    accessState.replaceChildren(element("span", undefined, "dot red"), element("span", error.message));
  }
}

async function setAccess(enabled) {
  if (enabled && !window.confirm(t("Le Pi va demander à votre box de lui faire suivre : {0}. Vos services « Tout le monde, sur internet » seront alors joignables depuis internet. Continuer ?", accessPortList))) return;
  if (!enabled && !window.confirm(t("Refermer les ports de la box ? Vos services ne seront plus joignables depuis internet."))) return;
  const button = enabled ? accessEnable : accessDisable;
  button.disabled = true;
  accessState.replaceChildren(element("span", undefined, "dot"), element("span", t("La box est en train de répondre…")));
  try {
    showAccess(await api("internet-access", jsonBody("PUT", { enabled })));
  } catch (error) {
    showMessage([error.message], "error");
    await loadAccess();
  } finally {
    button.disabled = false;
  }
}

accessEnable.addEventListener("click", () => setAccess(true));
accessDisable.addEventListener("click", () => setAccess(false));

function accessResultLines(result) {
  const lines = [];
  if (result.hosts.length === 0) {
    lines.push(checkLine("red", t("Le Pi n'a pas encore d'adresse sur internet : réglez DuckDNS ou votre adresse dans « Adresse sur internet ».")));
  }
  if (result.public_ip) {
    lines.push(checkLine("green", t("Votre adresse publique est {0}.", result.public_ip)));
  } else {
    lines.push(checkLine("red", t("Le Pi ne trouve pas son adresse publique : vérifiez qu'il joint internet.")));
  }
  if (result.box && result.shared) {
    lines.push(checkLine("red", t("Votre fournisseur partage l'adresse publique de votre box avec d'autres abonnés : l'accès direct depuis internet est impossible. Chez Free, demandez une « adresse IP fixe V4 full-stack » (gratuite) dans votre espace abonné ; ailleurs, demandez une adresse IPv4 publique à votre fournisseur.")));
  } else if (result.box && result.double_box) {
    lines.push(checkLine("orange", t("Un autre routeur se trouve entre votre box et internet : il faut aussi y ouvrir les ports, ou brancher le Pi sur le premier appareil.")));
  } else if (result.box) {
    lines.push(checkLine("green", t("Votre box « {0} » répond et a bien une adresse publique.", result.box_name)));
  } else {
    lines.push(checkLine("orange", t("La box ne répond pas à l'ouverture automatique (UPnP) : c'est normal si elle est coupée et que vous avez ouvert les ports à la main.")));
  }
  for (const check of result.dns) {
    if (check.error) {
      lines.push(checkLine("red", t("{0} n'existe pas encore sur internet. Après un réglage, attendez quelques minutes.", check.host)));
    } else if (check.matches) {
      lines.push(checkLine("green", t("{0} mène bien à votre box.", check.host)));
    } else {
      lines.push(checkLine("orange", t("{0} mène à {1}, pas à votre box. Si vous venez de la régler, attendez quelques minutes.", check.host, check.addresses.join(", "))));
    }
  }
  if (result.automatic) {
    for (const port of result.ports) {
      lines.push(port.open
        ? checkLine("green", t("Le {0} de la box mène au Pi.", portLabel(port)))
        : checkLine("red", t("La box n'a pas ouvert le {0}.", portLabel(port))));
    }
  } else {
    lines.push(checkLine("orange", t("Le Pi ne peut pas vérifier les ports ouverts à la main sur la box.")));
  }
  return lines;
}

accessTestButton.addEventListener("click", async () => {
  accessTestButton.disabled = true;
  accessChecks.replaceChildren(checkLine("", t("Test en cours…")));
  accessChecks.hidden = false;
  accessPhone.hidden = true;
  try {
    const result = await api("internet-access/test", { method: "POST" });
    accessChecks.replaceChildren(...accessResultLines(result));
    accessPhone.replaceChildren(element("p", t("Le Pi ne peut pas se tester lui-même depuis internet : depuis la maison, son adresse passe par la box sans sortir sur internet. Pour en être sûr, coupez le Wi-Fi d'un téléphone et ouvrez en 4G :")));
    if (result.url) {
      const link = element("a", result.url);
      link.href = result.url;
      link.rel = "noopener";
      link.target = "_blank";
      link.dir = "ltr";
      const line = element("p");
      line.append(link);
      accessPhone.append(line);
    } else {
      accessPhone.append(element("p", t("l'adresse du Pi sur internet, dès qu'il en aura une.")));
    }
    accessPhone.hidden = false;
  } catch (error) {
    accessChecks.replaceChildren(checkLine("red", error.message));
  } finally {
    accessTestButton.disabled = false;
  }
});

// Assistant de premier démarrage (spécification 5.1) : compte administrateur,
// langue, réseau de la maison et son test, adresse sur internet, accès depuis
// internet, autres comptes, puis redémarrage. Chaque étape se passe et se
// refait plus tard dans la console, avec les mêmes cartes. Les statistiques
// n'ont pas d'étape tant que la fonction est désactivée (TODO-4.7). Les
// services s'installent ensuite depuis la marketplace.
const wizardSection = document.getElementById("wizard");
const wizardHelp = document.getElementById("wizardHelp");
const wizardNext = document.getElementById("wizardNext");
const wizardSkip = document.getElementById("wizardSkip");
const wizardContinue = document.getElementById("wizardContinue");
const wizardLanguageField = document.getElementById("wizardLanguageField");
const wizardLanguage = document.getElementById("wizardLanguage");
const wizardTestButton = document.getElementById("wizardTest");
const wizardChecks = document.getElementById("wizardChecks");
const wizardReboot = document.getElementById("wizardReboot");
const wizardRebootButton = document.getElementById("wizardRebootButton");
const wizardRebootCancel = document.getElementById("wizardRebootCancel");
const addressBox = document.getElementById("addressBox");
const internetStateLine = document.getElementById("internetState");
const WIZARD_STEPS = ["language", "network", "check", "address", "access", "users", "reboot"];
const stepItems = Object.fromEntries(WIZARD_STEPS.map((step) => [step, document.getElementById(`step${step[0].toUpperCase()}${step.slice(1)}`)]));
// Étape de la langue déjà vue sur cette adresse de la console (la langue
// choisie est gardée par adresse : après le passage au Wi-Fi de la maison,
// la question revient une fois).
const WIZARD_LANGUAGE_KEY = "pi-wizard-language";
const REBOOT_COUNTDOWN = 10;
let wizardStep = null;
let wizardTimer = null;
let wizardNetwork = "";
let wizardAddress = "";
let wizardPassed = false;
let rebootTimer = null;

for (const language of LANGUAGES) {
  const option = element("option", language.name);
  option.value = language.code;
  option.lang = language.code;
  wizardLanguage.append(option);
}
wizardLanguage.value = LANGUAGE;
wizardLanguage.addEventListener("change", () => {
  try {
    window.localStorage.setItem(LANGUAGE_KEY, wizardLanguage.value);
  } catch {
    // Sans stockage, le choix ne dure que le temps de cette page.
  }
  const url = new URL(window.location.href);
  url.searchParams.set("lang", wizardLanguage.value);
  window.location.replace(url);
});

function sessionFlag(key, set) {
  try {
    if (set) window.sessionStorage.setItem(key, "1");
    return window.sessionStorage.getItem(key) === "1";
  } catch {
    return Boolean(set);
  }
}

// Cartes de la console montrées pendant une étape, remises en état après.
function resetWizardViews() {
  addressBox.hidden = false;
  internetStateLine.hidden = false;
}

function setStep(step) {
  wizardStep = step;
  const index = WIZARD_STEPS.indexOf(step);
  WIZARD_STEPS.forEach((name, position) => {
    stepItems[name].className = position < index ? "done" : position === index ? "current" : "";
  });
  clearInterval(wizardTimer);
  clearInterval(rebootTimer);
  for (const node of [wizardChecks, wizardReboot, wizardTestButton, wizardRebootButton, wizardRebootCancel, wizardContinue, wizardSkip, wizardNext, wizardLanguageField, networkSection, duckDNSSection, accessSection, accountsSection]) {
    node.hidden = true;
  }
  resetWizardViews();
}

async function startWizard() {
  wizardSection.hidden = false;
  servicesSection.hidden = true;
  catalogSection.hidden = true;
  piSection.hidden = true;
  accountSection.hidden = true;
  wizardPassed = false;
  if (!sessionFlag(WIZARD_LANGUAGE_KEY)) {
    setStep("language");
    wizardHelp.textContent = t("Choisissez la langue de la console. Vous pourrez la changer à tout moment en bas de la page.");
    wizardLanguageField.hidden = false;
    wizardContinue.hidden = false;
    return;
  }
  await networkStep();
}

async function networkStep() {
  setStep("network");
  const state = await loadNetwork();
  if (!state || state.configured) {
    await wizardCheck(state);
    return;
  }
  wizardHelp.textContent = t("Reliez le Pi à votre réseau : choisissez le Wi-Fi de la maison ci-dessous, ou le câble s'il est branché sur votre box.");
  wizardSkip.hidden = false;
  // Le choix se fait parfois sans réponse de la page (connexion au Wi-Fi) :
  // l'étape avance dès que l'état du réseau le montre.
  wizardTimer = setInterval(async () => {
    const current = await api("network").catch(() => null);
    if (current && current.configured && wizardStep === "network") {
      await wizardCheck(await loadNetwork());
    }
  }, 5000);
}

// Test du réseau : il regarde la connexion déjà établie, sans couper le
// Wi-Fi de configuration (même puce Wi-Fi, spécification 4).
async function wizardCheck(state) {
  setStep("check");
  wizardNetwork = (state && state.configured) || "";
  wizardAddress = state && !state.accessPoint.open && state.addresses.length > 0 ? state.addresses[0] : "";
  wizardContinue.hidden = false;
  if (!wizardNetwork) {
    wizardHelp.textContent = t("Le Pi n'est pas encore relié à votre réseau : les étapes d'internet attendront. Vous pourrez le relier plus tard dans « Réseau de la maison ».");
    return;
  }
  wizardContinue.hidden = true;
  wizardSkip.hidden = false;
  wizardHelp.textContent = wizardNetwork === "wifi"
    ? t("Le Pi est relié au Wi-Fi de la maison. Vérifiez maintenant que tout fonctionne : le test ne coupe rien.")
    : t("Le Pi est relié à votre box par le câble réseau. Vérifiez maintenant que tout fonctionne : le test ne coupe rien.");
  wizardTestButton.textContent = wizardNetwork === "wifi" ? t("Tester le Wi-Fi") : t("Tester le câble réseau");
  wizardTestButton.className = "";
  wizardTestButton.hidden = false;
}

function checkLine(color, text) {
  const item = element("li");
  item.append(element("span", undefined, `dot ${color}`), element("span", text));
  return item;
}

function showTestResult(result) {
  const { checks } = result;
  const wifi = result.network === "wifi";
  const lines = [];
  if (checks.connected) {
    lines.push(checkLine("green", wifi ? t("Le Pi est relié au Wi-Fi « {0} ».", result.ssid) : t("Le Pi est relié à votre box par le câble réseau.")));
  } else {
    lines.push(checkLine("red", wifi ? named(t("Le Pi n'est pas relié au Wi-Fi de la maison. Si le Wi-Fi « pi-appliance » est réapparu, la connexion a échoué : choisissez à nouveau votre Wi-Fi.")) : t("Le câble réseau ne répond pas : vérifiez qu'il est bien branché au Pi et à la box.")));
  }
  if (checks.connected) {
    lines.push(checks.address
      ? checkLine("green", t("Votre box lui a donné l'adresse {0}.", result.addresses[0]))
      : checkLine("red", t("Votre box ne lui a pas encore donné d'adresse. Attendez une minute, puis testez à nouveau.")));
    lines.push(checks.box
      ? checkLine("green", t("La box répond."))
      : checkLine("red", t("La box ne répond pas. Vérifiez qu'elle est allumée, puis testez à nouveau.")));
    lines.push(checks.internet
      ? checkLine("green", t("Internet est joignable."))
      : checkLine("orange", t("Internet n'est pas joignable. Le Pi fonctionne quand même à la maison, mais ne pourra pas se mettre à jour.")));
    lines.push(checks.localName
      ? checkLine("green", t("Le Pi répond au nom {0}.local.", result.name))
      : checkLine("orange", t("Le nom {0}.local ne répond pas encore : si l'adresse en .local ne s'ouvre pas, utilisez l'adresse du Pi donnée par votre box.", result.name)));
  }
  lines.push(checks.saved
    ? checkLine("green", t("Le Pi se reconnectera tout seul après un redémarrage."))
    : checkLine("red", t("Le réglage du Wi-Fi n'est pas enregistré : choisissez à nouveau votre Wi-Fi.")));
  wizardChecks.replaceChildren(...lines);
  wizardChecks.hidden = false;
}

wizardTestButton.addEventListener("click", async () => {
  wizardTestButton.disabled = true;
  wizardHelp.textContent = t("Test en cours…");
  try {
    const result = await api("network/test", { method: "POST" });
    if (result.name) piName = result.name;
    wizardAddress = result.addresses[0] || wizardAddress;
    showTestResult(result);
    wizardTestButton.textContent = t("Tester à nouveau");
    wizardTestButton.className = result.passed ? "secondary" : "";
    wizardPassed = result.passed;
    if (result.passed) {
      wizardHelp.textContent = t("Le réseau marche. Continuez : le Pi redémarrera à la fin de la mise en route.");
      wizardContinue.hidden = false;
      wizardSkip.hidden = true;
      networkSection.hidden = true;
    } else {
      wizardHelp.textContent = t("Le test a trouvé un problème, expliqué ci-dessous. Corrigez-le, puis testez à nouveau.");
      wizardContinue.hidden = true;
      wizardSkip.hidden = false;
      await loadNetwork();
    }
  } catch (error) {
    wizardHelp.textContent = error.message;
  } finally {
    wizardTestButton.disabled = false;
  }
});

// Adresse sur internet : DuckDNS et domaine personnel ; <nom>.pi-appliance.com
// seulement si le service d'adresses du projet répond (TODO-3.5).
async function addressStep() {
  setStep("address");
  wizardHelp.textContent = t("Donnez au Pi une adresse sur internet, pour ouvrir vos services hors de la maison : un nom DuckDNS (gratuit, sur duckdns.org) et, si vous en avez un, votre propre nom de domaine. Vous pourrez le faire plus tard dans « Adresse sur internet ».");
  duckDNSSection.hidden = false;
  addressBox.hidden = true;
  internetStateLine.hidden = true;
  wizardSkip.hidden = false;
  wizardContinue.hidden = false;
  await loadDuckDNS();
  await loadInternet();
  if (wizardStep !== "address") return;
  internetOpen.hidden = true;
  internetClose.hidden = true;
  const service = await api("address/service").catch(() => null);
  if (wizardStep === "address" && service && service.reachable) {
    addressBox.hidden = false;
    await loadAddress();
  }
}

async function accessStep() {
  setStep("access");
  wizardHelp.textContent = t("Pour que vos services s'ouvrent depuis internet, votre box doit faire suivre certains ports vers le Pi. Le Pi peut le lui demander si vous l'acceptez ; sinon, le guide explique comment faire à la main. Testez ensuite l'accès. Vous pourrez le faire plus tard dans « Accès depuis internet ».");
  accessSection.hidden = false;
  wizardSkip.hidden = false;
  wizardContinue.hidden = false;
  await loadAccess();
}

async function usersStep() {
  setStep("users");
  wizardHelp.textContent = t("Invitez les personnes qui utiliseront vos services : chacune reçoit un lien et choisit elle-même son mot de passe. Vous pourrez en inviter d'autres plus tard dans « Comptes ».");
  accountsSection.hidden = false;
  wizardSkip.hidden = false;
  wizardContinue.hidden = false;
  await loadAccounts().catch((error) => showMessage([error.message], "error"));
}

// Dernière étape : redémarrage, seulement quand le test du réseau a réussi.
function rebootStep() {
  setStep("reboot");
  wizardNext.hidden = false;
  if (wizardPassed) {
    wizardHelp.textContent = t("Tout est en ordre. Redémarrez maintenant le Pi : il doit retrouver votre réseau tout seul. La mise en route sera alors terminée.");
    wizardRebootButton.hidden = false;
    wizardNext.textContent = t("Terminer sans redémarrer");
    wizardNext.className = "secondary";
  } else {
    wizardHelp.textContent = wizardNetwork
      ? t("Le réseau n'a pas été vérifié : terminez la mise en route sans redémarrer. Vous pourrez redémarrer le Pi plus tard.")
      : t("Le Pi n'est pas encore relié à votre réseau. Vous pourrez le faire plus tard dans « Réseau de la maison ». Terminez la mise en route pour continuer.");
    wizardNext.textContent = t("Terminer la mise en route");
    wizardNext.className = "";
  }
}

async function nextStep() {
  switch (wizardStep) {
    case "language":
      sessionFlag(WIZARD_LANGUAGE_KEY, true);
      return networkStep();
    case "check":
      return wizardNetwork ? addressStep() : usersStep();
    case "address":
      return accessStep();
    case "access":
      return usersStep();
    case "users":
      return rebootStep();
  }
}

wizardContinue.addEventListener("click", nextStep);

// Avant le redémarrage : où retrouver le Pi, la suite, et 10 secondes pour changer d'avis.
function rebootLines() {
  const lines = [
    t("Le Pi va redémarrer. Cela prend une ou deux minutes, pendant lesquelles cette page ne répondra plus."),
    t("Ensuite, depuis un téléphone ou un ordinateur relié à votre réseau, ouvrez https://{0}.local/pi-appliance", piName),
  ];
  if (wizardAddress) {
    lines.push(t("Si cette adresse ne s'ouvre pas, ouvrez https://{0}/pi-appliance", wizardAddress));
  } else {
    lines.push(t("Si cette adresse ne s'ouvre pas, cherchez l'appareil « {0} » dans la liste des appareils de votre box et ouvrez https://<son adresse>/pi-appliance", piName));
  }
  lines.push(t("Connectez-vous avec votre identifiant et votre mot de passe, puis choisissez vos services dans la marketplace."));
  return lines;
}

function showReboot(lines, seconds = 0) {
  wizardReboot.replaceChildren(...lines.map((line) => element("p", line)));
  if (seconds > 0) {
    wizardReboot.append(element("p", t("Redémarrage dans {0} secondes.", seconds), "countdown"));
  }
  wizardReboot.hidden = false;
}

wizardRebootButton.addEventListener("click", () => {
  let seconds = REBOOT_COUNTDOWN;
  showReboot(rebootLines(), seconds);
  wizardRebootButton.hidden = true;
  wizardNext.hidden = true;
  wizardRebootCancel.hidden = false;
  clearInterval(rebootTimer);
  rebootTimer = setInterval(() => {
    seconds -= 1;
    if (seconds > 0) {
      showReboot(rebootLines(), seconds);
      return;
    }
    clearInterval(rebootTimer);
    rebootPi();
  }, 1000);
});

wizardRebootCancel.addEventListener("click", () => {
  clearInterval(rebootTimer);
  wizardReboot.hidden = true;
  wizardRebootCancel.hidden = true;
  wizardRebootButton.hidden = false;
  wizardNext.hidden = false;
});

async function rebootPi() {
  wizardRebootCancel.hidden = true;
  try {
    await api("system/reboot", { method: "POST" });
    // Le redémarrage attend quelques secondes : la fin de la mise en route est
    // notée avant ; sinon, l'assistant reviendra simplement à la connexion suivante.
    await api("wizard", { method: "POST" }).catch(() => null);
  } catch (error) {
    wizardReboot.hidden = true;
    wizardRebootButton.hidden = false;
    wizardNext.hidden = false;
    showMessage([error.message], "error");
    return;
  }
  wizardStep = null;
  stepItems.reboot.className = "done";
  wizardHelp.textContent = t("Le Pi redémarre…");
  showReboot(rebootLines());
  watchReboot();
}

// La page revient d'elle-même quand le Pi répond à nouveau, à la même adresse.
function watchReboot() {
  const started = Date.now();
  let down = false;
  const timer = setInterval(async () => {
    const up = await api("health").then(() => true, () => false);
    if (!up) {
      down = true;
      return;
    }
    if (down) {
      clearInterval(timer);
      window.location.reload();
    } else if (Date.now() - started > 3 * 60 * 1000) {
      clearInterval(timer);
      showMessage([t("Le Pi n'a pas redémarré. Débranchez-le, attendez dix secondes, puis rebranchez-le.")], "error");
    }
  }, 5000);
}

wizardSkip.addEventListener("click", async () => {
  if (wizardStep !== "network") {
    await nextStep();
    return;
  }
  if (!window.confirm(named(t("Passer le choix du réseau ? Le Wi-Fi « pi-appliance » restera ouvert, au plus 24 heures, pour le faire plus tard.")))) {
    return;
  }
  await wizardCheck(null);
});

wizardNext.addEventListener("click", async () => {
  wizardNext.disabled = true;
  try {
    await api("wizard", { method: "POST" });
    wizardStep = null;
    clearInterval(rebootTimer);
    wizardSection.hidden = true;
    showMessage([t("Votre appliance est prête. Choisissez maintenant vos services dans la marketplace, ci-dessous. Vous retrouverez aussi ici le réseau et l'état du Pi.")], "success");
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
const passwordForm = document.getElementById("passwordForm");
const recoveryBox = document.getElementById("recoveryBox");
const recoveryList = document.getElementById("recoveryCodes");
const codeField = document.getElementById("codeField");
const USER_PASSWORD_MIN = 8;
const DAY = 24 * 3600 * 1000;
const ROLE_NAMES = { admin: t("Administrateur"), user: t("Utilisateur"), guest: t("Invité") };
// ?return= : adresse du service protégé qui a renvoyé vers la connexion ;
// ?state= : lie le code de retour au cookie posé par ce service sur ce navigateur.
let returnTo = new URLSearchParams(window.location.search).get("return");
const returnState = new URLSearchParams(window.location.search).get("state") || "";
let inviteToken = null;

function jsonBody(method, body) {
  return { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) };
}

// Double authentification exigée par l'administrateur et aucune méthode active.
function needsTOTP(account) {
  return account.totp_required && !account.two_factor;
}

// ?oidc= : demande de connexion d'un service par OpenID Connect (spécification
// 8.1), reprise une fois connecté : la console renvoie au service avec un code.
let oidcRequest = new URLSearchParams(window.location.search).get("oidc");

async function goBackToOIDC() {
  try {
    const { url } = await api("oidc/authorize", jsonBody("POST", { request: oidcRequest }));
    window.location.assign(url);
    return true;
  } catch (error) {
    showMessage([error.message], "error");
    oidcRequest = null;
    window.history.replaceState(null, "", window.location.pathname);
    return false;
  }
}

// Ouvre le service demandé ; faux si l'adresse est refusée (la console s'affiche alors).
async function goBackToService() {
  try {
    const { url } = await api("auth/return", jsonBody("POST", { return: returnTo, state: returnState }));
    window.location.assign(url);
    return true;
  } catch (error) {
    showMessage([error.message], "error");
    returnTo = null;
    window.history.replaceState(null, "", window.location.pathname);
    return false;
  }
}

// Lien « mot de passe oublié » reçu par e-mail (#reset=…, spécification 8.5).
let resetToken = null;

async function startRecovery(token) {
  try {
    const account = await api("password/check", jsonBody("POST", { token }));
    resetToken = token;
    showAuth("recover");
    const minimum = account.role === "admin" ? ADMIN_PASSWORD_MIN : USER_PASSWORD_MIN;
    authHelp.textContent = t("Compte {0} : choisissez un nouveau mot de passe d'au moins {1} caractères. Une phrase de plusieurs mots est plus facile à retenir.", account.name, minimum);
    authForm.elements.password.minLength = minimum;
    return true;
  } catch (error) {
    window.history.replaceState(null, "", window.location.pathname + window.location.search);
    showMessage([error.message], "error");
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

// Appareils appairés avec l'application mobile (spécification 8.1) : ils
// ouvrent la console sans mot de passe, jusqu'à ce qu'on les retire.
const devicesList = document.getElementById("devicesList");
const noDevices = document.getElementById("noDevices");

async function loadDevices() {
  const { devices } = await api("account/devices").catch(() => ({ devices: [] }));
  noDevices.hidden = devices.length > 0;
  devicesList.replaceChildren(...devices.map((device) => {
    const item = element("li");
    const text = element("div");
    text.append(
      element("strong", device.name),
      element("p", t("Appairé le {0}, dernière connexion le {1}.", dayText(device.paired_at), dayText(device.last_used)), "meta"),
    );
    const path = `account/devices/${encodeURIComponent(device.id)}`;
    const rename = element("button", t("Renommer"), "secondary");
    rename.type = "button";
    rename.addEventListener("click", async () => {
      const name = window.prompt(t("Nom de l'appareil"), device.name);
      if (name === null || name.trim() === "" || name === device.name) return;
      try {
        await api(path, jsonBody("PUT", { name }));
        await loadDevices();
      } catch (error) {
        showMessage([error.message], "error");
      }
    });
    const remove = element("button", t("Retirer"), "secondary");
    remove.type = "button";
    remove.addEventListener("click", async () => {
      if (!window.confirm(t("Retirer « {0} » ? Ce téléphone devra être appairé de nouveau pour ouvrir la console sans mot de passe.", device.name))) return;
      remove.disabled = true;
      try {
        await api(path, { method: "DELETE" });
        showMessage([t("L'appareil « {0} » est retiré.", device.name)], "success");
        await loadDevices();
      } catch (error) {
        showMessage([error.message], "error");
        remove.disabled = false;
      }
    });
    const controls = element("div", undefined, "controls");
    controls.append(rename, remove);
    item.append(text, controls);
    return item;
  }));
}

// ?appareil= : retour d'un appairage ou d'une connexion depuis l'application mobile.
const DEVICE_MESSAGES = {
  appaire: [t("Ce téléphone est appairé : l'application mobile ouvrira désormais la console sans mot de passe."), "success"],
  retire: [t("Ce téléphone n'est plus appairé à ce Pi."), "success"],
  code: [t("Ce code a expiré ou a déjà servi : affichez de nouveau le code dans « Mon Pi », puis scannez-le avec l'application mobile."), "error"],
  inconnu: [t("Ce téléphone n'est plus reconnu par le Pi : connectez-vous, puis scannez de nouveau le code de « Mon Pi » avec l'application mobile."), "error"],
  heure: [t("L'heure du téléphone et celle du Pi ne concordent pas : vérifiez l'heure du téléphone, puis réessayez."), "error"],
  essais: [t("Trop d'essais : attendez quelques minutes avant de réessayer."), "error"],
  refus: [t("La connexion depuis l'application mobile a échoué : réessayez depuis l'application."), "error"],
};

function showDeviceMessage() {
  const params = new URLSearchParams(window.location.search);
  const found = DEVICE_MESSAGES[params.get("appareil")];
  if (!params.has("appareil")) return;
  params.delete("appareil");
  const rest = params.toString();
  window.history.replaceState(null, "", window.location.pathname + (rest ? `?${rest}` : "") + window.location.hash);
  if (found) showMessage([found[0]], found[1]);
}

function showAccount(account) {
  accountSection.hidden = false;
  loadDevices();
  showEmailCode(account);
  totpSetup.hidden = true;
  totpStart.hidden = account.totp;
  totpDisable.hidden = !account.totp || (account.totp_required && !account.email_code);
  totpDisable.reset();
  passwordForm.reset();
  passwordForm.elements.password.minLength = account.role === "admin" ? ADMIN_PASSWORD_MIN : USER_PASSWORD_MIN;
  if (account.totp) {
    totpState.textContent = t("La double authentification est activée : à chaque connexion, la console demande aussi le code affiché par votre application. Codes de secours restants : {0}.", account.recovery_codes);
  } else if (needsTOTP(account)) {
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
    if (!internetBox.hidden) loadInternet();
    // Rester ici le temps de noter les codes de secours, sans repartir vers un service.
    returnTo = null;
    oidcRequest = null;
    const session = await api("session");
    await showConsole(session.account, session.wizard);
    showRecoveryCodes(codes);
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

passwordForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const fields = passwordForm.elements;
  if (fields.password.value !== fields.confirm.value) {
    showMessage([t("Les deux nouveaux mots de passe sont différents.")], "error");
    return;
  }
  const button = passwordForm.querySelector("button");
  button.disabled = true;
  try {
    const reply = await api("account/password", jsonBody("POST", { current: fields.current.value, password: fields.password.value }));
    passwordForm.reset();
    const lines = [t("Votre mot de passe est changé. Vos autres connexions sont fermées.")];
    if (reply.backups) {
      lines.push(t("Ce mot de passe protège aussi vos sauvegardes. Les sauvegardes déjà faites gardent l'ancien : notez-le et gardez-le en lieu sûr. Pour remettre une de ces sauvegardes sur une nouvelle carte SD, il faudra le mot de passe en vigueur à sa date."));
    }
    showMessage(lines, "success");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
});

// Code par e-mail (spécification 8.1, TODO-3.15) : vérifier l'adresse par un
// code active la double authentification par e-mail, méthode par défaut quand
// le Pi sait envoyer des e-mails ; l'application reste possible en plus.
const emailCodeCard = document.getElementById("emailCodeCard");
const emailCodeState = document.getElementById("emailCodeState");
const emailCodeStart = document.getElementById("emailCodeStart");
const emailCodeConfirm = document.getElementById("emailCodeConfirm");
const emailCodeDisable = document.getElementById("emailCodeDisable");
// emailCodes : le Pi sait envoyer des codes par e-mail (api/session).
let emailCodes = false;

function showEmailCode(account) {
  emailCodeConfirm.hidden = true;
  emailCodeConfirm.reset();
  emailCodeDisable.reset();
  emailCodeDisable.hidden = !account.email_code || (account.totp_required && !account.totp);
  emailCodeStart.hidden = account.email_code || !emailCodes;
  emailCodeStart.textContent = account.email_verified ? t("Recevoir un code par e-mail") : t("Vérifier mon adresse e-mail");
  if (account.email_code) {
    emailCodeState.textContent = t("Activé : à chaque connexion, la console envoie un code à {0}. Codes de secours restants : {1}.", account.email, account.recovery_codes);
  } else if (!emailCodes) {
    emailCodeState.textContent = t("Le Pi ne sait pas encore envoyer d'e-mail : un administrateur peut le régler dans « Envoi des e-mails ». En attendant, utilisez une application d'authentification.");
  } else {
    emailCodeState.textContent = t("Le plus simple : à chaque connexion, la console envoie un code à {0}, à recopier en plus du mot de passe. Un code confirme d'abord que l'adresse est la bonne.", account.email);
  }
}

async function sendEmailCode() {
  emailCodeStart.disabled = true;
  try {
    const reply = await api("account/email/send", jsonBody("POST", { lang: LANGUAGE }));
    emailCodeConfirm.hidden = false;
    emailCodeStart.textContent = t("Renvoyer le code");
    showMessage([t("Un code vient de partir à {0} : il est valable 10 minutes. Regardez aussi dans les courriers indésirables.", reply.email)], "success");
    emailCodeConfirm.elements.code.focus();
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    emailCodeStart.disabled = false;
  }
}

emailCodeStart.addEventListener("click", sendEmailCode);

emailCodeConfirm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = emailCodeConfirm.querySelector("button");
  button.disabled = true;
  try {
    const reply = await api("account/email/confirm", jsonBody("POST", { code: new FormData(emailCodeConfirm).get("code") }));
    showMessage([t("Votre adresse e-mail est vérifiée : à chaque connexion, la console vous enverra un code.")], "success");
    returnTo = null;
    oidcRequest = null;
    const session = await api("session");
    await showConsole(session.account, session.wizard);
    showRecoveryCodes(reply.recovery_codes);
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
});

emailCodeDisable.addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = emailCodeDisable.querySelector("button");
  button.disabled = true;
  try {
    await api("account/email/disable", jsonBody("POST", { password: new FormData(emailCodeDisable).get("password") }));
    showMessage([t("La console ne vous enverra plus de code par e-mail.")], "success");
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

// Codes de secours, montrés une seule fois après l'activation d'une première méthode.
function showRecoveryCodes(codes) {
  if (!codes || codes.length === 0) return;
  // Pendant l'assistant, « Mon compte » est caché : les codes doivent rester visibles.
  accountSection.hidden = false;
  recoveryList.replaceChildren(...codes.map((code) => element("li", code)));
  recoveryBox.hidden = false;
  recoveryBox.scrollIntoView({ block: "nearest" });
}

// Proposition de la double authentification à chaque nouveau compte
// (spécification 8.1, TODO-3.14) : premier administrateur, invitation
// acceptée, compte créé par pia user add. Exigée par l'administrateur : pas de
// « Plus tard ».
const twoFactorOffer = document.getElementById("twoFactorOffer");
const twoFactorOfferHelp = document.getElementById("twoFactorOfferHelp");
const offerEmail = document.getElementById("offerEmail");
const offerApp = document.getElementById("offerApp");
const offerLater = document.getElementById("offerLater");

function showOffer(account) {
  const required = needsTOTP(account);
  twoFactorOffer.hidden = !(account.two_factor_offer || required);
  if (twoFactorOffer.hidden) return;
  offerEmail.hidden = !emailCodes;
  offerLater.hidden = required;
  offerApp.className = emailCodes ? "secondary" : "";
  const lines = [];
  lines.push(required
    ? t("L'administrateur demande la double authentification pour votre compte : choisissez comment recevoir vos codes pour continuer.")
    : t("Conseillé : avec la double authentification, quelqu'un qui apprend votre mot de passe ne peut pas se connecter. À chaque connexion, la console demande en plus un code à 6 chiffres."));
  lines.push(emailCodes
    ? t("Le plus simple : le code arrive par e-mail à {0}. Vous pouvez aussi utiliser une application du téléphone, ou les deux.", account.email)
    : t("Le code est affiché par une application du téléphone (Aegis, Google Authenticator, Microsoft Authenticator…)."));
  twoFactorOfferHelp.textContent = lines.join(" ");
}

function openAccountCard(card) {
  accountSection.hidden = false;
  card.scrollIntoView({ block: "start" });
}

offerEmail.addEventListener("click", async () => {
  openAccountCard(emailCodeCard);
  await sendEmailCode();
});

offerApp.addEventListener("click", () => {
  openAccountCard(document.getElementById("totpCard"));
  if (!totpStart.hidden) totpStart.click();
});

offerLater.addEventListener("click", async () => {
  offerLater.disabled = true;
  try {
    currentAccount = await api("account/two-factor/later", { method: "POST" });
    twoFactorOffer.hidden = true;
    showMessage([t("D'accord. Vous pourrez l'activer quand vous voudrez dans « Mon compte ».")], "info");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    offerLater.disabled = false;
  }
});

// Envoi des e-mails par le SMTP du Pi (spécification 10.2, TODO-3.16), pour
// l'administrateur : le mot de passe n'est jamais renvoyé par le Pi.
const mailSection = document.getElementById("mailSection");
const mailState = document.getElementById("mailState");
const mailForm = document.getElementById("mailForm");
const mailTest = document.getElementById("mailTest");
const mailForget = document.getElementById("mailForget");
let mailStatus = null;

function showMail(status) {
  mailStatus = status;
  const fields = mailForm.elements;
  fields.host.value = status.host || "";
  fields.port.value = status.configured ? status.port : "";
  fields.security.value = status.security || "starttls";
  fields.username.value = status.username || "";
  fields.password.value = "";
  fields.password.placeholder = status.password_set ? t("Enregistré : laissez vide pour le garder") : "";
  fields.from.value = status.from || "";
  mailTest.hidden = !status.configured;
  mailForget.hidden = !status.configured;
  const dot = element("span", undefined, `dot ${status.configured ? "ok" : ""}`);
  let text;
  if (status.configured) {
    text = t("Le Pi envoie ses e-mails par {0}, au nom de {1}. Envoyez-vous un e-mail d'essai pour vérifier.", status.host, status.from);
  } else if (status.project_service) {
    text = t("Pas encore réglé : le Pi essaie le service d'envoi du projet pour les invitations et les mots de passe oubliés, mais ne peut pas envoyer de code de connexion.");
  } else {
    text = t("Pas encore réglé : le Pi n'envoie aucun e-mail. Les invitations sont à transmettre vous-même.");
  }
  mailState.replaceChildren(dot, element("span", text));
}

async function loadMail() {
  try {
    showMail(await api("mail"));
    mailSection.hidden = false;
  } catch {
    mailSection.hidden = true;
  }
}

mailForm.elements.host.placeholder = "smtp.gmail.com";
mailForm.elements.port.placeholder = "587";
mailForm.elements.security.addEventListener("change", () => {
  mailForm.elements.port.placeholder = mailForm.elements.security.value === "tls" ? "465" : "587";
});

mailForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const fields = mailForm.elements;
  const button = mailForm.querySelector("button");
  button.disabled = true;
  try {
    const status = await api("mail", jsonBody("PUT", {
      host: fields.host.value,
      port: Number(fields.port.value) || 0,
      security: fields.security.value,
      username: fields.username.value,
      password: fields.password.value,
      from: fields.from.value,
    }));
    showMail(status);
    emailCodes = true;
    recoveryEmail = true;
    showEmailCode(currentAccount);
    showMessage([t("Réglage enregistré. Envoyez-vous un e-mail d'essai pour vérifier qu'il marche.")], "success");
    loadAlerts();
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
});

mailTest.addEventListener("click", async () => {
  mailTest.disabled = true;
  try {
    const reply = await api("mail/test", jsonBody("POST", { lang: LANGUAGE }));
    showMessage([t("E-mail d'essai envoyé à {0} : vérifiez qu'il est arrivé (regardez aussi dans les courriers indésirables).", reply.email)], "success");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    mailTest.disabled = false;
  }
});

mailForget.addEventListener("click", async () => {
  const count = mailStatus ? mailStatus.email_code_accounts : 0;
  const question = count > 0
    ? t("Ne plus envoyer d'e-mails ? {0} compte(s) reçoivent leur code de connexion par e-mail : ils devront utiliser leur application d'authentification ou un code de secours.", count)
    : t("Ne plus envoyer d'e-mails depuis le Pi ?");
  if (!window.confirm(question)) return;
  mailForget.disabled = true;
  try {
    showMail(await api("mail", { method: "DELETE" }));
    const session = await api("session");
    emailCodes = Boolean(session.email_codes);
    recoveryEmail = Boolean(session.recovery_email);
    showEmailCode(session.account);
    showMessage([t("Le Pi n'envoie plus d'e-mails par ce serveur.")], "success");
    loadAlerts();
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    mailForget.disabled = false;
  }
});

// Alertes par e-mail (spécification 8.6, TODO-4.2), pour l'administrateur :
// chaque case active ou coupe un type d'alerte.
const alertsSection = document.getElementById("alertsSection");
const alertsState = document.getElementById("alertsState");
const alertsForm = document.getElementById("alertsForm");

function showAlerts(status) {
  for (const setting of status.types) {
    const box = alertsForm.elements[setting.type];
    if (box) box.checked = setting.enabled;
  }
  const ready = status.mail && status.recipients > 0;
  const dot = element("span", undefined, `dot ${ready ? "ok" : ""}`);
  let text;
  if (!status.mail) {
    text = t("Les alertes partiront dès que l'envoi des e-mails sera réglé, juste au-dessus.");
  } else if (status.recipients === 0) {
    text = t("Aucun administrateur n'a encore vérifié son adresse e-mail : vérifiez la vôtre dans « Mon compte » pour recevoir les alertes.");
  } else if (!status.verified) {
    text = t("Les alertes partent à {0} administrateur(s). Vérifiez votre adresse dans « Mon compte » pour les recevoir aussi.", status.recipients);
  } else {
    text = t("Les alertes partent à {0} administrateur(s), vous compris.", status.recipients);
  }
  alertsState.replaceChildren(dot, element("span", text));
}

async function loadAlerts() {
  try {
    showAlerts(await api("alerts"));
    alertsSection.hidden = false;
  } catch {
    alertsSection.hidden = true;
  }
}

alertsForm.addEventListener("change", async (event) => {
  const box = event.target;
  if (!(box instanceof HTMLInputElement) || box.type !== "checkbox") return;
  box.disabled = true;
  try {
    showAlerts(await api("alerts", jsonBody("PUT", { type: box.name, enabled: box.checked })));
  } catch (error) {
    box.checked = !box.checked;
    showMessage([error.message], "error");
  } finally {
    box.disabled = false;
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
  if (account.totp && account.email_code) {
    facts.push(t("Double authentification activée : code par e-mail et application."));
  } else if (account.email_code) {
    facts.push(t("Double authentification activée : code par e-mail."));
  } else if (account.totp) {
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
  if (account.two_factor) {
    add(t("Retirer la double authentification"), async () => {
      if (!window.confirm(t("Retirer la double authentification de {0} (code par e-mail et application) ? À faire seulement si son téléphone ou sa boîte e-mail, et ses codes de secours, sont perdus.", account.name))) return;
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
    fields: ["name", "email", "password", "confirm"],
    path: "setup",
  },
  reset: {
    title: t("Nouveau mot de passe administrateur"),
    help: t("Le fichier reset a été trouvé sur la carte SD. Choisissez un nouveau mot de passe d'au moins {0} caractères : vos services et vos données ne changent pas.", ADMIN_PASSWORD_MIN),
    button: t("Changer le mot de passe"),
    fields: ["password", "confirm"],
    path: "reset",
  },
  login: {
    title: t("Connexion"),
    help: t("Entrez votre identifiant et votre mot de passe. L'identifiant est le nom choisi à la création du compte, pas l'adresse e-mail. Mot de passe administrateur oublié : le guide explique comment le changer avec le fichier reset."),
    button: t("Se connecter"),
    fields: ["name", "password"],
    path: "login",
  },
  // Mot de passe oublié : un lien est envoyé par e-mail (spécification 8.5).
  forgot: {
    title: t("Mot de passe oublié"),
    help: t("Entrez votre identifiant ou l'adresse e-mail de votre compte : vous recevrez par e-mail un lien pour choisir un nouveau mot de passe. Le lien est valable une heure."),
    button: t("Recevoir le lien par e-mail"),
    fields: ["who"],
    path: "password/forgot",
  },
  // Lien reçu par e-mail (#reset=…) : l'aide est complétée par startRecovery.
  recover: {
    title: t("Nouveau mot de passe"),
    help: "",
    button: t("Changer le mot de passe"),
    fields: ["password", "confirm"],
    path: "password/reset",
  },
  // Lien d'invitation (#invite=…) : le titre et l'aide sont complétés par startInvitation.
  invite: {
    title: "",
    help: "",
    button: t("Choisir mon mot de passe"),
    fields: ["password", "confirm"],
    path: "invitation",
  },
};

let authMode = "login";
// loginMethods : méthodes de double authentification du compte qui se connecte.
let loginMethods = [];
const emailCodeButton = document.getElementById("emailCodeButton");
// recoveryEmail : le Pi sait envoyer le lien « mot de passe oublié » (api/session).
let recoveryEmail = false;
const forgotButton = document.getElementById("forgotButton");
const loginButton = document.getElementById("loginButton");

function showAuth(mode) {
  authMode = mode;
  const config = AUTH_MODES[mode];
  authTitle.textContent = config.title;
  authHelp.textContent = config.help;
  if (mode === "login" && recoveryEmail) {
    authHelp.textContent = t("Entrez votre identifiant et votre mot de passe. L'identifiant est le nom choisi à la création du compte, pas l'adresse e-mail.");
  }
  authButton.textContent = config.button;
  forgotButton.hidden = !(mode === "login" && recoveryEmail);
  loginButton.hidden = mode !== "forgot";
  for (const field of ["name", "who", "email", "password", "confirm", "code"]) {
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
  heroLoad.hidden = true;
  logoutButton.hidden = true;
  emailReminder.hidden = true;
  networkSection.hidden = true;
  duckDNSSection.hidden = true;
  accessSection.hidden = true;
  piSection.hidden = true;
  accountSection.hidden = true;
  accountsSection.hidden = true;
  twoFactorOffer.hidden = true;
  mailSection.hidden = true;
  alertsSection.hidden = true;
  emailCodeButton.hidden = true;
  loginMethods = [];
  inviteBox.hidden = true;
  recoveryBox.hidden = true;
  wizardSection.hidden = true;
  wizardStep = null;
  clearInterval(networkTimer);
  clearInterval(duckDNSTimer);
  duckDNSTimer = null;
  clearInterval(wizardTimer);
  clearInterval(rebootTimer);
  clearInterval(dashboardTimer);
  clearInterval(storageTimer);
  clearInterval(backupTimer);
  backupTimer = null;
  clearInterval(updatesTimer);
  updatesTimer = null;
  updatesStatus = null;
  versionsBox.hidden = true;
  versionsDetails.open = false;
  versionsData = null;
  versionsHealth = null;
  clearInterval(cockpitTimer);
  cockpitTimer = null;
  cockpitStatus = null;
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
  if (authMode === "login" || authMode === "setup") {
    body.lang = LANGUAGE;
  }
  if (authMode === "invite") {
    body.token = inviteToken;
  }
  if (authMode === "recover") {
    body.token = resetToken;
  }
  if (authMode === "forgot") {
    delete body.password;
    body.name = data.get("who");
    body.lang = LANGUAGE;
  }
  authButton.disabled = true;
  try {
    const account = await api(config.path, jsonBody("POST", body));
    message.hidden = true;
    if (authMode === "forgot") {
      showAuth("login");
      showMessage([
        t("Si ce compte existe, un e-mail vient de lui être envoyé avec un lien valable une heure."),
        t("Rien reçu après quelques minutes ? Regardez dans les courriers indésirables, et vérifiez l'identifiant ou l'adresse e-mail. L'administrateur peut aussi vous donner un nouveau lien."),
      ], "success");
      return;
    }
    if (authMode === "recover") {
      resetToken = null;
      window.history.replaceState(null, "", window.location.pathname + window.location.search);
      const session = await api("session");
      recoveryEmail = Boolean(session.recovery_email);
      emailCodes = Boolean(session.email_codes);
      if (!session.account) {
        showAuth("login");
        authForm.elements.name.value = account.name;
        showMessage([t("Mot de passe enregistré : connectez-vous.")], "success");
        return;
      }
      await showConsole(session.account, session.wizard);
      return;
    }
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
    // Arrivée par un lien d'invitation : l'état du Pi n'a pas encore été lu.
    emailCodes = Boolean(session.email_codes);
    recoveryEmail = Boolean(session.recovery_email);
    await showConsole(session.account || account, session.wizard);
  } catch (error) {
    if (authMode === "login" && error.body && error.body.code_needed) {
      const first = codeField.hidden;
      codeField.hidden = false;
      authForm.elements.code.required = true;
      authForm.elements.code.value = "";
      authForm.elements.code.focus();
      if (error.body.methods) loginMethods = error.body.methods;
      emailCodeButton.hidden = !loginMethods.includes("email");
      // Code par e-mail seul : il part tout de suite.
      if (first && loginMethods.includes("email") && !loginMethods.includes("totp")) {
        await requestLoginCode();
        return;
      }
    }
    showMessage([error.message], "error");
  } finally {
    authButton.disabled = false;
  }
});

// Code de connexion par e-mail, après un mot de passe juste.
async function requestLoginCode() {
  emailCodeButton.disabled = true;
  try {
    const reply = await api("login/email-code", jsonBody("POST", { name: authForm.elements.name.value, password: authForm.elements.password.value, lang: LANGUAGE }));
    emailCodeButton.textContent = t("Renvoyer le code");
    showMessage([t("Un code vient de partir à {0} : il est valable 10 minutes. Regardez aussi dans les courriers indésirables.", reply.email)], "success");
    authForm.elements.code.focus();
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    emailCodeButton.disabled = false;
  }
}

emailCodeButton.addEventListener("click", requestLoginCode);

forgotButton.addEventListener("click", () => {
  const name = authForm.elements.name.value;
  message.hidden = true;
  showAuth("forgot");
  authForm.elements.who.value = name;
  authForm.elements.who.focus();
});

loginButton.addEventListener("click", () => {
  message.hidden = true;
  showAuth("login");
});

logoutButton.addEventListener("click", async () => {
  await api("logout", { method: "POST" }).catch(() => null);
  message.hidden = true;
  showAuth("login");
});

// Remise à zéro (spécification 8.5, TODO-2.52) : page /pi-appliance/reset-to-factory,
// pour un administrateur qui redonne son mot de passe, ou pendant une
// réinitialisation par le fichier reset (accès à la carte SD).
const FACTORY_PAGE = /\/reset-to-factory$/.test(window.location.pathname);
const factorySection = document.getElementById("factorySection");
const factoryForm = document.getElementById("factoryForm");
const factoryEmailCode = document.getElementById("factoryEmailCode");

factoryEmailCode.addEventListener("click", async () => {
  factoryEmailCode.disabled = true;
  try {
    const reply = await api("login/email-code", jsonBody("POST", { name: currentAccount.name, password: factoryForm.elements.password.value, lang: LANGUAGE }));
    showMessage([t("Un code vient de partir à {0} : il est valable 10 minutes. Regardez aussi dans les courriers indésirables.", reply.email)], "success");
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    factoryEmailCode.disabled = false;
  }
});

function showFactory(byResetFile) {
  authSection.hidden = true;
  factorySection.hidden = false;
  for (const id of ["factoryPassword", "factoryCode"]) {
    document.getElementById(id).hidden = byResetFile;
  }
  factoryEmailCode.hidden = byResetFile || !currentAccount || !currentAccount.email_code;
  factoryForm.elements.password.required = !byResetFile;
}

factoryForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const data = new FormData(factoryForm);
  const deleteServices = data.get("delete_services") === "on";
  const question = deleteServices
    ? t("Effacer les comptes, les réglages, les services et toutes leurs données ? Rien ne pourra être récupéré.")
    : t("Effacer les comptes et les réglages ? Les services et leurs données sont gardés.");
  if (!window.confirm(question)) {
    return;
  }
  const button = factoryForm.querySelector("button");
  button.disabled = true;
  try {
    const result = await api("factory-reset", jsonBody("POST", {
      confirm: data.get("confirm") === "on",
      delete_services: deleteServices,
      password: data.get("password") || "",
      code: data.get("code") || "",
    }));
    factorySection.hidden = true;
    showMessage([t("Le Pi est remis à zéro. La mise en route va s'ouvrir : créez le compte administrateur."), ...(result.problems || [])], "success");
    window.setTimeout(() => window.location.assign("./"), 4000);
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    button.disabled = false;
  }
});

// Diagnostic du Pi (spécification 8.5, TODO-2.55) : page /pi-appliance/diagnostic,
// pour un administrateur, ou sans compte après le dépôt du fichier diagnostic
// (ou reset) sur la carte SD ; aussi servie sur le port 8443 quand Traefik
// ne répond plus (console de secours).
const DIAGNOSTIC_PAGE = /\/diagnostic$/.test(window.location.pathname);
const diagnosticSection = document.getElementById("diagnosticSection");
const diagnosticUnits = document.getElementById("diagnosticUnits");
const diagnosticErrors = document.getElementById("diagnosticErrors");
const diagnosticDownload = document.getElementById("diagnosticDownload");
const diagnosticClose = document.getElementById("diagnosticClose");

const DIAGNOSTIC_UNITS = {
  console: () => t("La console"),
  traefik: () => t("L'accès à la console par le navigateur (Traefik)"),
  "traefik-check": () => t("La réparation automatique de Traefik"),
  docker: () => t("Les services installés (Docker)"),
  network: () => t("Le réseau (Wi-Fi et câble)"),
  ap: () => t("Le Wi-Fi de configuration"),
  firewall: () => t("La protection du Wi-Fi de configuration"),
  avahi: () => t("L'adresse nom.local du Pi"),
  mdns: () => t("Les adresses des services à la maison"),
  secrets: () => t("Le nom et les clés du Pi"),
};
const DIAGNOSTIC_STATES = {
  ok: ["green", () => t("en marche")],
  starting: ["orange", () => t("en train de démarrer")],
  restarting: ["red", () => t("redémarre en boucle")],
  failed: ["red", () => t("en panne")],
  stopped: ["red", () => t("arrêté")],
  missing: ["red", () => t("absent")],
};

function uptimeText(seconds) {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  if (days > 0) return t("{0} j {1} h", days, hours);
  return t("{0} h {1} min", hours, Math.floor((seconds % 3600) / 60));
}

function diagnosticTiles(report) {
  const tiles = [];
  const status = report.status_error ? null : report;
  if (status) {
    const traefik = status.traefik;
    const restarts = traefik.restarts > 0 ? ` ${t("Traefik a redémarré {0} fois.", traefik.restarts)}` : "";
    tiles.push(traefik.https
      ? nowTile(t("Accès par le navigateur"), t("Correct"), t("La console répond par HTTPS.") + restarts, "green")
      : nowTile(t("Accès par le navigateur"), t("En panne"),
        t("Traefik ne répond pas : le Pi essaie de le réparer tout seul toutes les 2 minutes. S'il reste en panne, débranchez puis rebranchez l'alimentation du Pi.") + restarts, "red"));
  }
  const power = report.resources && report.resources.power;
  const counts = status && status.undervoltage;
  let history = "";
  if (counts && known(counts.previousBoot)) {
    history = ` ${t("{0} baisses de tension depuis le démarrage, {1} au démarrage précédent.", counts.thisBoot, counts.previousBoot)}`;
  } else if (counts) {
    history = ` ${t("{0} baisses de tension depuis le démarrage.", counts.thisBoot)}`;
  }
  if (known(power) || counts) {
    const before = counts && (counts.thisBoot > 0 || counts.previousBoot > 0);
    if (known(power) && power.undervoltage_now) {
      tiles.push(nowTile(t("Alimentation"), t("Trop faible"),
        t("Alimentation trop faible : utilisez l'alimentation officielle du Pi. Le Pi ralentit et la carte SD peut s'abîmer.") + history, "red"));
    } else if ((known(power) && power.undervoltage_since_boot) || before) {
      tiles.push(nowTile(t("Alimentation"), t("Trop faible par moments"),
        t("L'alimentation a été trop faible depuis le démarrage du Pi : utilisez l'alimentation officielle du Pi, et évitez les câbles longs ou fins.") + history, "orange"));
    } else {
      tiles.push(nowTile(t("Alimentation"), t("Correcte"), t("Aucune baisse de tension depuis le démarrage du Pi."), "green"));
    }
  }
  const disk = report.resources && report.resources.disk;
  if (known(disk)) {
    const full = disk.free_mb < disk.total_mb * 0.15;
    tiles.push(nowTile(t("Carte SD"), sizeText(disk.free_mb),
      full ? t("La carte SD se remplit.") : t("Il reste {0} de place sur la carte SD.", sizeText(disk.free_mb)), full ? "orange" : "green"));
  }
  if (status && status.uptimeSeconds > 0) {
    tiles.push(nowTile(t("Allumé depuis"), uptimeText(status.uptimeSeconds), "", "green"));
  }
  return tiles;
}

function showDiagnosticReport(report) {
  document.getElementById("diagnosticTiles").replaceChildren(...diagnosticTiles(report));
  const open = document.getElementById("diagnosticOpen");
  open.hidden = !report.by_file;
  open.textContent = report.open_until
    ? t("Ouvert sans compte grâce au fichier diagnostic de la carte SD, jusqu'au {0}.", new Date(report.open_until).toLocaleString(LOCALE))
    : t("Ouvert sans compte grâce au fichier de la carte SD.");
  diagnosticClose.hidden = !report.open_until;
  if (report.status_error) {
    diagnosticUnits.replaceChildren(element("li", report.status_error, "warning"));
    diagnosticErrors.hidden = true;
    document.getElementById("diagnosticNoErrors").hidden = true;
    return;
  }
  diagnosticUnits.replaceChildren(...report.units.map((unit) => {
    const [color, text] = DIAGNOSTIC_STATES[unit.state] || ["red", () => unit.state];
    const label = DIAGNOSTIC_UNITS[unit.id] ? DIAGNOSTIC_UNITS[unit.id]() : unit.unit;
    const item = element("li");
    const line = element("span", undefined, "state");
    line.append(element("span", undefined, `dot ${color}`), element("span", t("{0} : {1}", label, text())));
    item.append(line);
    if (unit.restarts > 0) {
      item.append(element("p", t("Redémarré {0} fois depuis le démarrage du Pi.", unit.restarts), "meta"));
    }
    return item;
  }));
  diagnosticErrors.textContent = report.errors.join("\n");
  diagnosticErrors.hidden = report.errors.length === 0;
  document.getElementById("diagnosticNoErrors").hidden = report.errors.length > 0;
}

// Rend false si la page demande une connexion (pas d'administrateur, pas de fichier).
async function showDiagnostic() {
  let report;
  try {
    report = await api("diagnostic");
  } catch (error) {
    if (error.status === 401 || error.status === 403) {
      return false;
    }
    showMessage([error.message], "error");
    return true;
  }
  authSection.hidden = true;
  diagnosticSection.hidden = false;
  document.getElementById("diagnosticRescue").textContent = window.location.port === "8443"
    ? t("Console de secours : Traefik ne répond pas, seule cette page est ouverte.")
    : t("Si la console ne s'ouvre plus, cette page reste joignable à {0}.", `https://${window.location.hostname}:8443${window.location.pathname}`);
  showDiagnosticReport(report);
  return true;
}

document.getElementById("diagnosticRefresh").addEventListener("click", () => showDiagnostic());

diagnosticDownload.addEventListener("click", async () => {
  diagnosticDownload.disabled = true;
  showMessage([t("Préparation des journaux… Cela peut prendre une minute.")], "info");
  try {
    const response = await fetch("api/diagnostic/logs");
    if (!response.ok) {
      const body = await response.json().catch(() => ({}));
      throw new Error(body.error || t("Erreur {0}", response.status));
    }
    const name = (response.headers.get("Content-Disposition") || "").match(/filename="([^"]+)"/);
    const link = document.createElement("a");
    link.href = URL.createObjectURL(await response.blob());
    link.download = name ? name[1] : "pi-appliance-diagnostic.txt";
    document.body.append(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(link.href);
    message.hidden = true;
  } catch (error) {
    showMessage([error.message], "error");
  } finally {
    diagnosticDownload.disabled = false;
  }
});

diagnosticClose.addEventListener("click", async () => {
  try {
    await api("diagnostic", { method: "DELETE" });
    diagnosticSection.hidden = true;
    showMessage([t("L'accès sans compte est fermé.")], "success");
    showAuth("login");
  } catch (error) {
    showMessage([error.message], "error");
  }
});

async function showConsole(account, wizard) {
  if (FACTORY_PAGE && account.role === "admin") {
    currentAccount = account;
    logoutButton.hidden = false;
    showFactory(false);
    return;
  }
  if (DIAGNOSTIC_PAGE && account.role === "admin" && !needsTOTP(account)) {
    currentAccount = account;
    logoutButton.hidden = false;
    await showDiagnostic();
    return;
  }
  currentAccount = account;
  // À chaque ouverture, « Mon Pi » d'abord (TODO-5.25) pour un administrateur ;
  // un autre compte, qui n'a pas « Mon Pi » (seulement son compte), voit tout
  // ce qu'il a.
  tileChoice.kinds.clear();
  if (account.role === "admin") tileChoice.kinds.add("pi");
  scheduleTiles();
  authSection.hidden = true;
  logoutButton.hidden = false;
  servicesSection.hidden = false;
  catalogSection.hidden = account.role !== "admin";
  duckDNSSection.hidden = true;
  accessSection.hidden = true;
  resetWizardViews();
  statsSection.hidden = true;
  clearInterval(duckDNSTimer);
  duckDNSTimer = null;
  emailReminder.hidden = account.email_verified;
  emailReminder.textContent = emailCodes
    ? t("Votre adresse {0} n'est pas encore vérifiée : vérifiez-la dans « Mon compte », un code vous sera envoyé.", account.email)
    : t("Votre adresse {0} sera vérifiée par un code envoyé par e-mail dès que l'appliance pourra en envoyer.", account.email);
  showAccount(account);
  showOffer(account);
  accountsSection.hidden = true;
  mailSection.hidden = true;
  alertsSection.hidden = true;
  if (needsTOTP(account)) {
    // Double authentification exigée : rien d'autre tant qu'elle n'est pas activée.
    servicesSection.hidden = true;
    catalogSection.hidden = true;
    return;
  }
  if (returnTo && (await goBackToService())) {
    return;
  }
  if (oidcRequest && (await goBackToOIDC())) {
    return;
  }
  // Catalogue en ligne relu tout de suite, en arrière-plan (TODO-5.25) : la
  // console ne l'attend pas ; une fois par minute au plus, l'ancien catalogue
  // reste en cas d'échec ; la marketplace suit quand la réponse arrive.
  const catalogRefreshed = api("catalog/refresh", { method: "POST" }).catch(() => null);
  if (account.role === "admin" && wizard) {
    await startWizard();
    return;
  }
  // État du Pi (barre du haut, « Mon Pi ») lu en parallèle du reste, sans
  // retarder l'ouverture de la page ; attendu seulement avant le catalogue, dont
  // les jauges d'installation s'en servent.
  let resourcesReady = Promise.resolve();
  if (account.role === "admin") {
    resourcesReady = loadResources().catch(() => null);
    startDashboard();
    duckDNSSection.hidden = false;
    await loadDuckDNS();
    await loadAddress();
    await loadInternet();
    accessSection.hidden = false;
    await loadAccess();
    duckDNSTimer = setInterval(loadDuckDNS, 15000);
    await loadStats();
    await loadNetwork();
    accountsSection.hidden = false;
    await loadAccounts();
    await loadMail();
    await loadAlerts();
  }
  await resourcesReady;
  await loadCatalog();
  await loadServices();
  if (account.role !== "admin") await loadCockpitAccess();
  catalogRefreshed.then((result) => result && showCatalogRefresh(result)).catch(() => null);
}

api("health")
  .then(async (health) => {
    status.textContent = t("Appliance en service.");
    statusDot.className = "dot ok";
    showFooter(health);
    showDeviceMessage();
    const invite = window.location.hash.match(/^#invite=([A-Za-z0-9_-]+)$/);
    if (invite && (await startInvitation(invite[1]))) {
      return;
    }
    const recovery = window.location.hash.match(/^#reset=([A-Za-z0-9_-]+)$/);
    if (recovery && (await startRecovery(recovery[1]))) {
      return;
    }
    if (DIAGNOSTIC_PAGE && (await showDiagnostic())) {
      return;
    }
    const session = await api("session");
    recoveryEmail = Boolean(session.recovery_email);
    emailCodes = Boolean(session.email_codes);
    if (session.reset) {
      if (FACTORY_PAGE) {
        showFactory(true);
        return;
      }
      showAuth("reset");
    } else if (session.setup) {
      showAuth("setup");
    } else if (!session.account) {
      showAuth("login");
      if (FACTORY_PAGE) {
        showMessage([t("Connectez-vous avec un compte administrateur pour remettre le Pi à zéro.")], "info");
      }
      if (DIAGNOSTIC_PAGE) {
        showMessage([t("Connectez-vous avec un compte administrateur pour voir le diagnostic du Pi, ou déposez le fichier diagnostic sur la carte SD (voir le guide).")], "info");
      }
    } else {
      await showConsole(session.account, session.wizard);
    }
  })
  .catch((error) => {
    status.textContent = t("Appliance injoignable : {0}", error.message);
    statusDot.className = "dot down";
  });
