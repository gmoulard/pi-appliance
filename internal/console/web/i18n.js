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

// Langues de la console (spécification 5.3). La clé d'une traduction est la
// phrase française elle-même : t("Il reste {0} de mémoire.", taille). Le
// français fait foi ; une phrase sans traduction s'affiche en français.
// Chaque langue est un fichier i18n/<code>.js, chargé avant app.js.

const LANGUAGES = [
  { code: "fr", name: "Français" },
  { code: "en", name: "English" },
  { code: "es", name: "Español" },
  { code: "pt", name: "Português" },
  { code: "it", name: "Italiano" },
  { code: "de", name: "Deutsch" },
  { code: "nl", name: "Nederlands" },
  { code: "tr", name: "Türkçe" },
  { code: "zh", name: "中文" },
  { code: "ja", name: "日本語" },
  { code: "ar", name: "العربية", rtl: true },
  { code: "fa", name: "فارسی", rtl: true },
];
const LANGUAGE_KEY = "pi-lang";

// Langue choisie dans la console (adresse ?lang=, puis choix gardé par le
// navigateur), sinon celle du navigateur, sinon le français.
function chooseLanguage() {
  const known = (code) => LANGUAGES.some((language) => language.code === code);
  const asked = new URLSearchParams(window.location.search).get("lang");
  if (known(asked)) return asked;
  try {
    const saved = window.localStorage.getItem(LANGUAGE_KEY);
    if (known(saved)) return saved;
  } catch {
    // Stockage du navigateur indisponible : langue du navigateur.
  }
  for (const tag of navigator.languages || [navigator.language || "fr"]) {
    const code = String(tag).toLowerCase().split("-")[0];
    if (known(code)) return code;
  }
  return "fr";
}

const LANGUAGE = chooseLanguage();
// LOCALE : pour les nombres, dates et tris (toLocaleString, localeCompare).
const LOCALE = LANGUAGE;
let translations = {};

function t(key, ...args) {
  const text = Object.hasOwn(translations, key) ? translations[key] : key;
  return text.replace(/\{(\d+)\}/g, (match, index) => (index < args.length ? String(args[index]) : match));
}

// Textes fixes de index.html : nœuds texte et attributs lisibles.
function translatePage() {
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const text = node.nodeValue.trim();
    if (text && Object.hasOwn(translations, text)) {
      node.nodeValue = node.nodeValue.replace(text, translations[text]);
    }
  }
  for (const attribute of ["placeholder", "aria-label", "title", "alt"]) {
    for (const element of document.querySelectorAll(`[${attribute}]`)) {
      const text = element.getAttribute(attribute).trim();
      if (text && Object.hasOwn(translations, text)) {
        element.setAttribute(attribute, translations[text]);
      }
    }
  }
}

// Appelé par chaque fichier i18n/<code>.js.
function piTranslations(code, table) {
  if (code !== LANGUAGE) return;
  translations = table;
  translatePage();
}

function setupLanguagePicker() {
  const picker = document.getElementById("language");
  if (!picker) return;
  for (const language of LANGUAGES) {
    const option = document.createElement("option");
    option.value = language.code;
    option.textContent = language.name;
    option.lang = language.code;
    picker.append(option);
  }
  picker.value = LANGUAGE;
  picker.addEventListener("change", () => {
    try {
      window.localStorage.setItem(LANGUAGE_KEY, picker.value);
    } catch {
      // Sans stockage, le choix ne dure que le temps de cette page.
    }
    const url = new URL(window.location.href);
    url.searchParams.set("lang", picker.value);
    window.location.replace(url);
  });
}

(function start() {
  const language = LANGUAGES.find((candidate) => candidate.code === LANGUAGE);
  document.documentElement.lang = LANGUAGE;
  document.documentElement.dir = language.rtl ? "rtl" : "ltr";
  setupLanguagePicker();
  // Scripts insérés dans l'ordre : la langue, puis la console.
  const scripts = LANGUAGE === "fr" ? ["app.js"] : [`i18n/${LANGUAGE}.js`, "app.js"];
  for (const src of scripts) {
    const script = document.createElement("script");
    script.src = src;
    script.async = false;
    document.body.append(script);
  }
})();
