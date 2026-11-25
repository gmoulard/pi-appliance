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

// Usage : node console-cards.cjs <adresse de la console> <compte> <mot de passe>
// (lancé par console-cards-test.sh).
const { chromium } = require("playwright");

const [base, name, password] = process.argv.slice(2);
let failures = 0;

function check(label, ok, detail = "") {
  if (ok) {
    console.log(`ok     ${label}`);
  } else {
    failures += 1;
    console.log(`ÉCHEC  ${label}${detail ? ` : ${detail}` : ""}`);
  }
}

const PI_CARDS = [
  "#updatesBox", "#versionsBox", "#piNow", "#nameForm", "#mobileBox", "#backupBox",
  "a[href='diagnostic']", "#piHistory", "#twoFactorOffer", "#duckdnsSection", "#accessSection",
  "#mailSection", "#alertsSection", "#accountsSection", "#accountSection",
];

async function shown(page, selector) {
  return page.evaluate((s) => {
    const node = document.querySelector(s);
    return Boolean(node) && node.getClientRects().length > 0;
  }, selector);
}

async function open(browser, clock) {
  const context = await browser.newContext({ locale: "fr-FR", viewport: { width: 1000, height: 900 } });
  const login = await context.request.post(`${base}api/login`, { headers: { "X-Pi-Appliance": "1" }, data: { name, password } });
  if (!login.ok()) throw new Error(`connexion refusée : ${login.status()}`);
  const page = await context.newPage();
  page.on("pageerror", (error) => check("pas d'erreur JavaScript", false, error.message));
  if (clock) await page.clock.install();
  await page.goto(base);
  await page.waitForFunction(() => document.querySelector("#updatesState").textContent !== ""
    && document.querySelectorAll("#catalog > li").length > 2, undefined, { timeout: 15000 });
  await page.waitForTimeout(300);
  return page;
}

async function choose(page, bar, label) {
  await page.click(`#${bar} button:text-is("${label}")`);
  await page.waitForTimeout(150);
}

(async () => {
  const browser = await chromium.launch();
  try {
    const page = await open(browser, false);
    for (const selector of PI_CARDS) {
      check(`Mon Pi, à l'ouverture : ${selector}`, await shown(page, selector));
    }
    check("Mises à jour : correctif de sécurité annoncé", (await page.textContent("#updatesState")).includes("correctif de sécurité"));

    await choose(page, "tileKinds", "Mon Pi");
    const catalog = await (await page.context().request.get(`${base}api/catalog`)).json();
    for (const app of catalog.apps) {
      const visible = await page.evaluate((title) => [...document.querySelectorAll("#catalog > li h3")]
        .some((h3) => h3.textContent === title && h3.getClientRects().length > 0), app.name);
      check(`tout affiché : carte « ${app.name} » de la marketplace`, visible);
    }
    const titles = await page.$$eval("#catalog > li:not([hidden]) h3", (nodes) => nodes.map((node) => node.textContent));
    const last = titles.pop();
    const sorted = [...titles].sort((a, b) => a.localeCompare(b, "fr", { sensitivity: "base" }));
    check("marketplace par ordre alphabétique", titles.join("|") === sorted.join("|"), titles.join(", "));
    check("marketplace : « Docker Compose libre » en dernier", last === "Docker Compose libre", last);
    for (const selector of [...PI_CARDS, "#servicesSection", "#catalogSection"]) {
      check(`tout affiché : ${selector}`, await shown(page, selector));
    }

    await choose(page, "tileKinds", "Mon Pi");
    const themes = await page.$$eval("#tileTags button", (buttons) => buttons.map((button) => button.textContent));
    check("thèmes de Mon Pi proposés", themes.length === 4, themes.join(", "));
    for (const theme of themes) {
      await choose(page, "tileTags", theme);
      const missing = await page.evaluate((label) => {
        const ids = { "Réseau et internet": "reseau-internet", "État": "etat", "Entretien": "entretien", "Comptes et appareils": "comptes" };
        return [...document.querySelectorAll("main [data-theme]")]
          .filter((node) => !node.hidden && !node.closest("[hidden]") && node.dataset.theme.split(" ").includes(ids[label]))
          .filter((node) => node.getClientRects().length === 0)
          .map((node) => node.id || node.tagName);
      }, theme);
      check(`thème « ${theme} » : ses cartes affichées`, missing.length === 0, missing.join(", "));
      check(`thème « ${theme} » : carte « Mises à jour »`, (await shown(page, "#updatesBox")) === ["État", "Entretien"].includes(theme));
      await choose(page, "tileTags", theme);
    }
    await page.context().close();

    const failing = await open(browser, true);
    await failing.route("**/api/updates", (route) => route.fulfill({ status: 502, body: "Bad Gateway" }));
    await failing.route("**/api/system", (route) => route.fulfill({ status: 502, body: "Bad Gateway" }));
    await failing.click("#updatesCheck");
    await failing.clock.fastForward(61000);
    await failing.waitForTimeout(300);
    check("console injoignable : carte « Mises à jour » gardée", await shown(failing, "#updatesBox"));
    check("console injoignable : « Mon Pi » gardé", await shown(failing, "#piSection"));
    check("console injoignable : c'est dit", (await failing.textContent("#updatesState")).includes("ne répond pas"));
    await failing.unroute("**/api/updates");
    await failing.unroute("**/api/system");
    await failing.clock.fastForward(61000);
    await failing.waitForTimeout(500);
    check("console revenue : carte « Mises à jour » à jour", (await failing.textContent("#updatesState")).includes("correctif de sécurité"));
    await failing.context().close();
  } finally {
    await browser.close();
  }
  console.log(failures === 0 ? "Toutes les cartes attendues sont affichées." : `${failures} échec(s).`);
  process.exit(failures === 0 ? 0 : 1);
})().catch((error) => {
  console.error(error);
  process.exit(1);
});
