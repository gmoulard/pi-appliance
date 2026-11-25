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

// Langue du site (TODO-5.11). Chargé dans <head> de chaque page, sans defer :
// - garde la langue choisie dans le menu « Langue » (lien data-langue) ;
// - sur l'accueil français seulement (attribut data-accueil-fr), ouvre
//   l'accueil de la langue demandée par ?lang=, sinon de la langue choisie
//   la dernière fois, sinon de la première langue du navigateur que le site
//   connaît ; le français, ou une langue inconnue, reste sur cet accueil.
// Les robots des moteurs de recherche ne sont pas renvoyés : chaque langue
// est annoncée par les liens hreflang.
(() => {
  const html = document.documentElement;
  const codes = (html.dataset.langues || "").split(" ").filter(Boolean);
  const racine = html.dataset.racine || "/";
  const KEY = "pi-lang";
  const read = () => {
    try { return window.localStorage.getItem(KEY); } catch { return null; }
  };
  const keep = (code) => {
    try { window.localStorage.setItem(KEY, code); } catch { /* stockage indisponible */ }
  };

  document.addEventListener("click", (event) => {
    const link = event.target.closest?.("a[data-langue]");
    if (link && codes.includes(link.dataset.langue)) keep(link.dataset.langue);
  });

  if (!html.hasAttribute("data-accueil-fr")) return;
  if (/bot|crawl|spider|slurp|preview/i.test(navigator.userAgent)) return;
  let chosen = new URLSearchParams(window.location.search).get("lang");
  if (codes.includes(chosen)) keep(chosen);
  else chosen = read();
  if (!codes.includes(chosen)) {
    chosen = "fr";
    for (const wanted of navigator.languages || [navigator.language || ""]) {
      const code = String(wanted).toLowerCase().split("-")[0];
      if (codes.includes(code)) { chosen = code; break; }
    }
  }
  if (chosen !== "fr") window.location.replace(`${racine}${chosen}/${window.location.hash}`);
})();
