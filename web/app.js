"use strict";
/* XGrowth Studio — application web (aucune dépendance). */

// ------------------------------------------------------------------ utilitaires
const $ = (s, r = document) => r.querySelector(s);
const $$ = (s, r = document) => [...r.querySelectorAll(s)];
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const store = {
  get(k, d) { try { const v = localStorage.getItem("xg." + k); return v === null ? d : v } catch { return d } },
  set(k, v) { try { localStorage.setItem("xg." + k, v) } catch {} },
};
const len = s => [...(s || "")].length;

const I = {
  logo: '<img src="/favicon.svg" alt="" draggable="false">',
  home: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 10.5 12 3l9 7.5V20a1 1 0 0 1-1 1h-5v-6H9v6H4a1 1 0 0 1-1-1z"/></svg>',
  inbox: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 12h-6l-2 3h-4l-2-3H2"/><path d="M5.5 5h13L22 12v6a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-6z"/></svg>',
  pen: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z"/></svg>',
  cal: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="17" rx="2"/><path d="M16 2v4M8 2v4M3 10h18"/></svg>',
  radar: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="5"/><path d="m12 12 6-6"/></svg>',
  chart: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 3v18h18"/><path d="m7 15 4-4 3 3 6-6"/></svg>',
  gear: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/></svg>',
  spark: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m12 3 1.9 5.8L20 11l-6.1 2.2L12 19l-1.9-5.8L4 11l6.1-2.2z"/><path d="M19 3v4M17 5h4"/></svg>',
  check: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>',
  x: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12"/></svg>',
  send: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m22 2-7 20-4-9-9-4z"/><path d="M22 2 11 13"/></svg>',
  clock: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>',
  reply: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 17 4 12l5-5"/><path d="M20 18v-2a4 4 0 0 0-4-4H4"/></svg>',
  quote: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m17 2 4 4-4 4"/><path d="M3 11v-1a4 4 0 0 1 4-4h14M7 22l-4-4 4-4"/><path d="M21 13v1a4 4 0 0 1-4 4H3"/></svg>',
  shield: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="m9 12 2 2 4-4"/></svg>',
  alert: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z"/><path d="M12 9v4M12 17h.01"/></svg>',
  image: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-5-5L5 21"/></svg>',
  sliders: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6"/></svg>',
  bulb: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 18h6M10 22h4"/><path d="M12 2a7 7 0 0 0-4 12.7c.6.5 1 1.3 1 2.3h6c0-1 .4-1.8 1-2.3A7 7 0 0 0 12 2z"/></svg>',
  refresh: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-2.6-6.4L21 8"/><path d="M21 3v5h-5"/></svg>',
  out: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5M21 12H9"/></svg>',
  sun: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>',
  auto: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 3v18a9 9 0 0 0 0-18z" fill="currentColor"/></svg>',
  moon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/></svg>',
  arrowR: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M13 6l6 6-6 6"/></svg>',
  arrowL: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 12H5M11 6l-6 6 6 6"/></svg>',
  ext: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 3h6v6M10 14 21 3M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/></svg>',
};

// AGPL-3.0 : tout utilisateur du service doit pouvoir obtenir le code source.
const SOURCE_URL = "https://github.com/AllanPoule/XGrowth-Studio";

// ------------------------------------------------------------------ état
let S = null;            // état serveur
let P = null;            // fournisseurs d'IA détectés
const edits = {};        // textes modifiés non enregistrés
let busy = false;
let dirty = false;       // formulaire modifié et pas encore enregistré : pas de rafraîchissement automatique
document.addEventListener("input", e => { if (e.target.closest("#view")) dirty = true });

const PROVIDER_STYLE = {
  "claude-code": { bg: "linear-gradient(135deg,#d97757,#b4532a)", l: "CC" },
  codex: { bg: "linear-gradient(135deg,#10a37f,#0b7a5f)", l: "AI" },
  gemini: { bg: "linear-gradient(135deg,#4285f4,#9b72cb)", l: "G" },
  anthropic: { bg: "linear-gradient(135deg,#8b5cf6,#3b82f6)", l: "API" },
};

// ------------------------------------------------------------------ API
async function api(path, body) {
  const opt = body === undefined ? {} : { method: "POST", headers: { "Content-Type": "application/json", "X-XGrowth": "1" }, body: JSON.stringify(body) };
  const r = await fetch(path, { credentials: "same-origin", ...opt });
  let e;
  try { e = await r.json() } catch { throw new Error("réponse invalide du serveur (" + r.status + ")") }
  if (r.status === 401 && !path.startsWith("/api/auth/")) { boot(); throw new Error("session expirée") }
  if (!e.ok) { const err = new Error(e.error?.message || "erreur"); err.data = e.data; throw err }
  return e.data;
}

function toast(msg, err) {
  const t = document.createElement("div");
  t.className = "toast" + (err ? " err" : "");
  t.innerHTML = (err ? I.alert : I.check) + `<div>${esc(msg)}</div>`;
  $("#toasts").append(t);
  setTimeout(() => t.remove(), err ? 7000 : 3200);
}

// exécute une action avec un bouton en attente puis recharge l'état
async function run(btn, fn, okMsg, opts = {}) {
  let label;
  if (btn) { label = btn.innerHTML; btn.disabled = true; btn.innerHTML = '<span class="spin"></span>' + (opts.wait || "") }
  busy = true;
  try {
    const r = await fn();
    if (okMsg) toast(typeof okMsg === "function" ? okMsg(r) : okMsg);
    if (!opts.noReload) await refresh();
    return r;
  } catch (e) { toast(e.message, true); throw e }
  finally { busy = false; if (btn && btn.isConnected) { btn.disabled = false; btn.innerHTML = label } }
}

async function refresh() { S = await api("/api/state"); render() }

// ------------------------------------------------------------------ formats
const fmtDate = (t, opts = { dateStyle: "medium", timeStyle: "short" }) => t ? new Intl.DateTimeFormat("fr-FR", opts).format(new Date(t)) : "";
const rtf = new Intl.RelativeTimeFormat("fr", { numeric: "auto" });
function rel(t) {
  if (!t) return "";
  const s = (new Date(t) - Date.now()) / 1000, a = Math.abs(s);
  if (a < 60) return s > 0 ? "dans un instant" : "à l'instant";
  if (a < 3600) return rtf.format(Math.round(s / 60), "minute");
  if (a < 86400) return rtf.format(Math.round(s / 3600), "hour");
  if (a < 86400 * 30) return rtf.format(Math.round(s / 86400), "day");
  return fmtDate(t, { dateStyle: "medium" });
}
const nf = n => new Intl.NumberFormat("fr-FR", { notation: n >= 10000 ? "compact" : "standard", maximumFractionDigits: 1 }).format(n || 0);
const acct = n => (S.accounts || []).find(a => a.name === n);
const handleOf = n => acct(n)?.handle || n;
const maxLen = n => acct(n)?.premium ? 4000 : 280;
// affiner l'IA : types de posts et consignes effectifs d'un compte (compte → commun → défaut)
const typesFor = a => (a?.postTypes?.length ? a.postTypes : S.settings.postTypes) || [];
const typeLabel = (accName, key) => typesFor(acct(accName)).find(t => t.key === key)?.label || key;
const inheritedPrompt = k => S.settings.prompts?.[k] || S.defaults?.prompts?.[k] || "";
const tweetURL = p => p.tweetId && !String(p.tweetId).startsWith("dry-") ? `https://x.com/${handleOf(p.account)}/status/${p.tweetId}` : null;
const who = () => store.get("who", "");
const providerName = id => (P || []).find(p => p.id === id)?.name || ({ "claude-code": "Claude Code", codex: "Codex (ChatGPT)", gemini: "Gemini CLI", anthropic: "API Claude" }[id] || id);

function ring(n, max) {
  const r = 10, c = 2 * Math.PI * r, pct = Math.min(n / max, 1);
  const cls = n > max ? "over" : n > max * 0.9 ? "warn" : "";
  return `<svg class="ring ${cls}" viewBox="0 0 26 26"><circle class="bgc" cx="13" cy="13" r="${r}" fill="none" stroke-width="2.5"/><circle class="fg" cx="13" cy="13" r="${r}" fill="none" stroke-width="2.5" stroke-dasharray="${c}" stroke-dashoffset="${c * (1 - pct)}" transform="rotate(-90 13 13)" stroke-linecap="round"/></svg>`;
}

const STATUS = { draft: ["Brouillon", ""], scheduled: ["Planifié", "accent"], validated: ["Validé", "ok"], publishing: ["Envoi…", "warn"], published: ["Publié", "ok"], failed: ["Échec", "bad"], rejected: ["Rejeté", "bad"] };
const statusPill = s => { const [l, c] = STATUS[s] || [s, ""]; return `<span class="pill ${c}">${l}</span>` };
const KIND = { reply: "Réponse", quote: "Citation", post: "" };

// ------------------------------------------------------------------ démarrage
async function boot() {
  applyTheme();
  try {
    const a = await api("/api/auth/status");
    if (a.needsSetup) return renderSetup();
    if (!a.authenticated) return renderLogin();
    [S, P] = await Promise.all([api("/api/state"), api("/api/providers")]);
    if (!S.settings.onboarded) return wizard.start();
    render();
  } catch (e) {
    $("#root").innerHTML = `<div class="authbox"><div class="card"><h3>Studio injoignable</h3><p class="muted">${esc(e.message)}</p><button class="btn" onclick="location.reload()">Réessayer</button></div></div>`;
  }
}

// Thème : « auto » (défaut) suit le système, « light » et « dark » le forcent.
const THEMES = ["auto", "light", "dark"];
const THEME_LABEL = { auto: "Automatique (système)", light: "Clair", dark: "Sombre" };
const systemLight = window.matchMedia ? matchMedia("(prefers-color-scheme: light)") : null;
const themePref = () => { const t = store.get("theme", "auto"); return THEMES.includes(t) ? t : "auto" };
const themeIcon = () => ({ auto: I.auto, light: I.sun, dark: I.moon }[themePref()]);

function applyTheme() {
  const pref = themePref();
  const t = pref === "auto" ? (systemLight?.matches ? "light" : "dark") : pref;
  document.documentElement.dataset.theme = t;
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", t === "light" ? "#f3f5f1" : "#0b1015");
  const b = document.querySelector('[data-act="theme"]');
  if (b) { b.innerHTML = themeIcon(); b.title = `Thème : ${THEME_LABEL[pref]}`; b.setAttribute("aria-label", b.title) }
}
systemLight?.addEventListener?.("change", () => { if (themePref() === "auto") applyTheme() });

// ------------------------------------------------------------------ écrans d'accès
function authShell(inner) {
  $("#root").innerHTML = `<div class="overlay"><div class="authbox">
    <div class="brand" style="justify-content:center;padding-bottom:22px"><span class="mark">${I.logo}</span><span class="brandword">XGrowth<span class="brand-sub">STUDIO</span></span></div>
    <div class="card">${inner}</div>
    <p class="small faint" style="text-align:center;margin-top:16px">Logiciel libre sous AGPL-3.0 · <a href="${SOURCE_URL}" target="_blank" rel="noopener">code source</a></p></div></div>`;
}

function renderSetup() {
  authShell(`<h3>Sécurisez votre studio</h3><p class="muted small">Entrez le code d'installation affiché par le serveur au démarrage (aussi dans <code>~/.xgrowth/setup-code.txt</code>), puis choisissez votre mot de passe.</p>
    <form id="f"><label class="l">Code d'installation</label><input name="code" placeholder="XXXXX-XXXXX" autocomplete="one-time-code" required>
    <label class="l">Mot de passe (10 caractères min.)</label><input name="pw" type="password" autocomplete="new-password" minlength="10" required>
    <label class="l">Confirmez</label><input name="pw2" type="password" autocomplete="new-password" required>
    <div class="row" style="margin-top:18px"><button class="btn primary" style="width:100%">Créer le mot de passe</button></div></form>`);
  $("#f").onsubmit = async e => {
    e.preventDefault();
    const f = e.target;
    if (f.pw.value !== f.pw2.value) return toast("Les deux mots de passe diffèrent", true);
    await run(f.querySelector("button"), () => api("/api/auth/setup", { code: f.code.value, password: f.pw.value }), "Studio sécurisé", { noReload: true }).then(boot).catch(() => {});
  };
}

function renderLogin() {
  authShell(`<h3>Connexion</h3><p class="muted small">Votre studio est protégé par mot de passe.</p>
    <form id="f"><label class="l">Mot de passe</label><input name="pw" type="password" autocomplete="current-password" autofocus required>
    <div class="row" style="margin-top:18px"><button class="btn primary" style="width:100%">Entrer</button></div></form>`);
  $("#f").onsubmit = async e => {
    e.preventDefault();
    await run(e.target.querySelector("button"), () => api("/api/auth/login", { password: e.target.pw.value }), null, { noReload: true }).then(boot).catch(() => {});
  };
}

// ------------------------------------------------------------------ coquille
const ROUTES = [
  ["home", "Accueil", I.home], ["queue", "À valider", I.inbox], ["compose", "Créer", I.pen],
  ["calendar", "Calendrier", I.cal], ["watch", "Veille", I.radar], ["published", "Performances", I.chart], ["settings", "Réglages", I.gear],
];
const route = () => (location.hash.replace(/^#\/?/, "") || "home").split("/")[0];
window.addEventListener("hashchange", () => S && render());

function counts() {
  const q = S.posts.filter(p => p.status === "draft" || p.status === "failed").length;
  const w = (S.items || []).filter(i => i.status === "pending").length;
  return { queue: q, watch: w };
}

function render() {
  if (!S) return;
  dirty = false;
  const r = route(), c = counts();
  if (r !== "watch") watchBaseline = undefined;
  const prov = S.ai?.provider, pinfo = (P || []).find(p => p.id === prov);
  const navItem = ([k, l, ic], cls = "") => `<a href="#/${k}" class="${k === r ? "on" : ""} ${cls}">${ic}<span>${l}</span>${c[k] ? `<span class="count">${c[k]}</span>` : ""}</a>`;
  $("#root").innerHTML = `<div class="app">
    <aside class="side">
      <div class="brand"><span class="mark">${I.logo}</span><span class="brandword">XGrowth<span class="brand-sub">STUDIO</span></span></div>
      <nav class="nav">${ROUTES.map(x => navItem(x)).join("")}</nav>
      <div class="grow"></div>
      <button class="aipill" data-go="settings">
        <span class="dot ${pinfo && !pinfo.available ? "off" : ""}"></span>
        <span><b>${esc(providerName(prov))}</b><small>${S.ai?.model ? esc(S.ai.model) : "IA du studio"}</small></span>
      </button>
      <div class="sidefoot"><button class="btn ghost sm" data-act="theme" title="Thème : ${THEME_LABEL[themePref()]}" aria-label="Thème : ${THEME_LABEL[themePref()]}">${themeIcon()}</button><a class="btn ghost sm" href="${SOURCE_URL}" target="_blank" rel="noopener" title="Code source (AGPL-3.0)">Code source</a><span class="spacer"></span><button class="btn ghost sm" data-act="logout" title="Se déconnecter">${I.out}</button></div>
    </aside>
    <div>
      <div class="mtop"><span class="mark">${I.logo}</span><span class="brandword">XGrowth<span class="brand-sub">STUDIO</span></span><span class="spacer"></span><button class="btn ghost sm" data-go="settings">${esc(providerName(prov))}</button></div>
      <main class="main"><div class="wrap" id="view"></div></main>
    </div>
    <nav class="mnav">${ROUTES.filter(x => x[0] !== "published").map(([k, l, ic]) => `<a href="#/${k}" class="${k === r ? "on" : ""}">${ic}<span>${l}</span>${c[k] ? `<span class="count">${c[k]}</span>` : ""}</a>`).join("")}</nav>
  </div>`;
  const views = { home: vHome, queue: vQueue, compose: vCompose, calendar: vCalendar, watch: vWatch, published: vPublished, settings: vSettings };
  (views[r] || vHome)($("#view"));
}

// ------------------------------------------------------------------ composants
function tweetCard(p, mode) {
  const a = acct(p.account), h = a?.handle || p.account;
  const text = edits[p.id] ?? p.text, n = len(text), max = maxLen(p.account);
  const editable = mode === "edit";
  const when = p.status === "published" ? `· ${rel(p.publishedAt)}` : p.scheduledAt ? `· ${I.clock.replace("<svg", '<svg style="width:13px;height:13px;vertical-align:-2px"')} ${fmtDate(p.scheduledAt)}` : `· créé ${rel(p.createdAt)}`;
  const target = p.targetText ? `<div class="tw-target"><div class="who">${p.kind === "reply" ? "En réponse à" : "Cite"}</div>${esc(p.targetText)}</div>` : "";
  const url = tweetURL(p);
  return `<article class="tweet" data-id="${p.id}">
    <div class="av">${esc(h[0] || "x")}</div>
    <div style="min-width:0">
      <div class="tw-head"><b>${esc(a?.displayName || h)}</b><span class="h">@${esc(h)}</span><span class="h small">${when}</span><span class="spacer"></span>${p.postType ? `<span class="pill">${esc(typeLabel(p.account, p.postType))}</span>` : ""}${KIND[p.kind] ? `<span class="pill">${KIND[p.kind]}</span>` : ""}${statusPill(p.status)}</div>
      ${target}
      ${editable ? `<textarea class="tw-edit" data-edit="${p.id}" rows="4">${esc(text)}</textarea>` : `<div class="tw-text">${esc(text)}</div>`}
      <div class="tw-meta">${editable ? `${ring(n, max)}<span class="cnt ${n > max ? "over" : ""}" data-cnt="${p.id}">${n} / ${max}</span>` : ""}
        ${p.mediaPath ? `<span class="pill">🖼 ${esc(p.mediaPath.split("/").pop())}</span>` : ""}
        ${p.validatedBy ? `<span class="pill">${I.shield.replace("<svg", '<svg style="width:12px;height:12px"')} ${esc(p.validatedBy)}</span>` : ""}
        ${p.metrics ? `<span class="small muted">👁 ${nf(p.metrics.views)} · ♥ ${nf(p.metrics.likes)} · ↻ ${nf(p.metrics.retweets)} · ${p.metrics.engagementRatePct.toFixed(1)} %</span>` : ""}
        ${url ? `<a class="small" href="${url}" target="_blank" rel="noopener">Voir sur X ${I.ext.replace("<svg", '<svg style="width:12px;height:12px;vertical-align:-1px"')}</a>` : ""}</div>
      ${p.rationale && editable ? `<div class="tw-why">${I.bulb}<span>${esc(p.rationale)}</span></div>` : ""}
      ${p.visualBrief && (editable || mode === "planned") ? `<div class="tw-why">${I.image}<span><b>Idée de visuel :</b> ${esc(p.visualBrief)}</span></div>` : ""}
      ${p.error ? `<div class="tw-err">${esc(p.error)}</div>` : ""}
      ${mode === "edit" ? queueActions(p) : mode === "planned" ? `<div class="tw-actions"><span class="spacer"></span><button class="btn sm ghost" data-act="unschedule" data-id="${p.id}">Repasser en brouillon</button></div>` : ""}
    </div></article>`;
}

function queueActions(p) {
  return `<div class="tw-actions">
    <button class="btn primary sm" data-act="validate" data-id="${p.id}">${I.check} Valider</button>
    <button class="btn sm" data-act="validate-at" data-id="${p.id}">${I.clock} Choisir l'heure</button>
    <button class="btn sm" data-act="now" data-id="${p.id}">${I.send} Publier maintenant</button>
    <span class="spacer"></span>
    <button class="btn sm ghost" data-act="save" data-id="${p.id}">Enregistrer</button>
    <button class="btn sm ghost danger" data-act="reject" data-id="${p.id}">Rejeter</button>
  </div>
  <div class="feedback"><input data-fb="${p.id}" placeholder="Demandez une retouche : plus court, plus direct, ajoute un exemple…"><button class="btn sm" data-act="rewrite" data-id="${p.id}">${I.spark} Réécrire</button></div>
  <div class="row hidden" data-at="${p.id}" style="margin-top:10px"><input type="datetime-local" style="width:auto"><button class="btn sm primary" data-act="validate-at-go" data-id="${p.id}">Planifier</button></div>`;
}

function empty(icon, title, sub, cta) {
  return `<div class="card empty">${icon}<h3>${title}</h3><p>${sub}</p>${cta || ""}</div>`;
}

function accountSelect(id = "acc") {
  const cur = store.get("acc", S.accounts[0]?.name);
  return `<select id="${id}" style="width:auto">${S.accounts.map(a => `<option value="${esc(a.name)}" ${a.name === cur ? "selected" : ""}>@${esc(a.handle || a.name)}</option>`).join("")}</select>`;
}

// ------------------------------------------------------------------ vues
function vHome(v) {
  const now = Date.now(), d30 = now - 30 * 864e5, d7 = now + 7 * 864e5;
  const pub = S.posts.filter(p => p.status === "published");
  const pub30 = pub.filter(p => new Date(p.publishedAt) > d30);
  const planned = S.posts.filter(p => (p.status === "scheduled" || p.status === "validated") && p.scheduledAt).sort((a, b) => new Date(a.scheduledAt) - new Date(b.scheduledAt));
  const views = pub30.reduce((s, p) => s + (p.metrics?.views || 0), 0);
  const withM = pub30.filter(p => p.metrics?.views);
  const eng = withM.length ? withM.reduce((s, p) => s + p.metrics.engagementRatePct, 0) / withM.length : 0;
  const c = counts();
  const hour = new Date().getHours();
  const hello = hour < 6 ? "Bonne nuit" : hour < 18 ? "Bonjour" : "Bonsoir";
  const next = planned[0];
  const drafts = S.posts.filter(p => p.status === "draft").slice(-3).reverse();
  v.innerHTML = `<div class="head home-head"><div><div class="eyebrow">✦ Votre espace de croissance</div><h1>${hello}${who() ? " " + esc(who()) : ""} 👋</h1><p>${c.queue ? `<b>${c.queue}</b> brouillon${c.queue > 1 ? "s" : ""} attend${c.queue > 1 ? "ent" : ""} votre validation.` : "Votre file est vide : c'est le moment de créer."}</p></div>
      <a class="btn primary" href="#/compose">${I.spark} Créer avec l'IA</a></div>
    <div class="kpis">
      <div class="kpi hero"><div class="k">À valider</div><div class="v">${c.queue}</div><div class="s">brouillons en attente</div></div>
      <div class="kpi"><div class="k">Planifiés</div><div class="v">${planned.filter(p => new Date(p.scheduledAt) < d7).length}</div><div class="s">sur 7 jours</div></div>
      <div class="kpi"><div class="k">Publiés</div><div class="v">${pub30.length}</div><div class="s">sur 30 jours</div></div>
      <div class="kpi"><div class="k">Vues</div><div class="v">${nf(views)}</div><div class="s">sur 30 jours</div>${sparkline(pub30)}</div>
      <div class="kpi"><div class="k">Engagement</div><div class="v">${eng.toFixed(1)} %</div><div class="s">moyen par post</div></div>
    </div>
    <h2>Écrire vite</h2>${composerBox("home")}
    <div class="grid2" style="margin-top:4px">
      <div><h2>Prochain post</h2>${next ? tweetCard(next, "view") : empty(I.cal, "Rien de planifié", "Validez un brouillon : il prend le prochain créneau libre.")}</div>
      <div><h2>Derniers brouillons</h2>${drafts.length ? drafts.map(p => `<a href="#/queue" style="text-decoration:none;color:inherit;display:block">${tweetCard(p, "view")}</a>`).join("") : empty(I.inbox, "Aucun brouillon", "Générez-en depuis Créer ou la Veille.")}</div>
    </div>`;
  bindComposer(v, "home");
}

function sparkline(posts) {
  const pts = posts.filter(p => p.metrics).sort((a, b) => new Date(a.publishedAt) - new Date(b.publishedAt)).map(p => p.metrics.views);
  if (pts.length < 2) return "";
  const max = Math.max(...pts) || 1, w = 160, h = 40;
  const d = pts.map((y, i) => `${(i / (pts.length - 1)) * w},${h - (y / max) * (h - 4) - 2}`).join(" ");
  return `<svg class="spark" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none"><defs><linearGradient id="sg" x1="0" x2="1"><stop offset="0" stop-color="#c7f586"/><stop offset="1" stop-color="#75dec1"/></linearGradient></defs><polyline points="${d}" fill="none" stroke="url(#sg)" stroke-width="2.5" stroke-linejoin="round" stroke-linecap="round"/></svg>`;
}

function composerBox(ctx) {
  if (!S.accounts.length) return empty(I.gear, "Aucun compte", "Ajoutez un compte X dans les réglages.", `<a class="btn primary" href="#/settings">Réglages</a>`);
  return `<div class="composer glow"><textarea id="brief-${ctx}" placeholder="De quoi voulez-vous parler ? Un sujet, une anecdote, une idée en vrac… (vide = idées originales)">${esc(store.get("brief", ""))}</textarea>
    <div class="bar">${accountSelect("acc-" + ctx)}
      <select id="type-${ctx}" style="width:auto" class="${typesFor(acct(store.get("acc", S.accounts[0]?.name))).length ? "" : "hidden"}" title="Type de post">${typeOptions(store.get("acc", S.accounts[0]?.name))}</select>
      <div class="seg" id="n-${ctx}">${[1, 3, 5].map(n => `<button data-n="${n}" class="${n === +store.get("n", 3) ? "on" : ""}">${n}</button>`).join("")}</div>
      <span class="small muted">brouillon(s) par ${esc(providerName(S.ai?.provider))}</span><span class="spacer"></span>
      <button class="btn primary" data-act="generate" data-ctx="${ctx}">${I.spark} Générer</button></div></div>
    <div id="gen-${ctx}"></div>`;
}

function bindComposer(v, ctx) {
  const ta = $("#brief-" + ctx, v);
  if (!ta) return;
  ta.oninput = () => store.set("brief", ta.value);
  ta.onkeydown = e => { if ((e.metaKey || e.ctrlKey) && e.key === "Enter") $(`[data-act=generate][data-ctx=${ctx}]`, v).click() };
  $$(`#n-${ctx} button`, v).forEach(b => b.onclick = () => { store.set("n", b.dataset.n); $$(`#n-${ctx} button`, v).forEach(x => x.classList.toggle("on", x === b)) });
  $("#acc-" + ctx, v).onchange = e => {
    store.set("acc", e.target.value);
    const sel = $("#type-" + ctx, v);
    sel.innerHTML = typeOptions(e.target.value);
    sel.classList.toggle("hidden", !typesFor(acct(e.target.value)).length);
  };
}

function typeOptions(accName) {
  const types = typesFor(acct(accName)), next = S.typeMix?.[accName]?.next;
  return `<option value="">Type : au choix de l'IA${next ? ` (priorité : ${esc(types.find(t => t.key === next)?.label || next)})` : ""}</option>` +
    types.map(t => `<option value="${esc(t.key)}">${esc(t.label)}</option>`).join("");
}

function vQueue(v) {
  const ps = S.posts.filter(p => p.status === "draft" || p.status === "failed").slice().reverse();
  v.innerHTML = `<div class="head"><div><h1>À valider</h1><p>Relisez, retouchez, validez. Rien ne part sur X sans votre feu vert.</p></div>
    <div class="row"><span class="small muted">Validé par</span><input id="who" value="${esc(who())}" placeholder="Votre nom" style="width:150px"></div></div>
    ${ps.length ? ps.map(p => tweetCard(p, "edit")).join("") : empty(I.check, "Tout est validé", "Aucun brouillon en attente.", `<a class="btn primary" href="#/compose">${I.spark} Créer des posts</a>`)}`;
  $("#who", v).oninput = e => store.set("who", e.target.value.trim());
  $$("[data-edit]", v).forEach(ta => {
    const id = ta.dataset.edit, p = S.posts.find(x => x.id === id);
    autosize(ta);
    ta.oninput = () => {
      edits[id] = ta.value;
      autosize(ta);
      const n = len(ta.value), max = maxLen(p.account), c = $(`[data-cnt="${id}"]`, v);
      c.textContent = `${n} / ${max}`; c.classList.toggle("over", n > max);
      c.previousElementSibling.outerHTML = ring(n, max);
    };
  });
}

function vCompose(v) {
  v.innerHTML = `<div class="head"><div><h1>Créer</h1><p>${esc(providerName(S.ai?.provider))} écrit dans votre voix, avec vos leçons et ce qui a marché.</p></div></div>
    ${composerBox("compose")}
    <h2>Répondre ou citer un post X</h2>
    <div class="card"><div class="row"><input id="ref" placeholder="Collez le lien d'un post : https://x.com/…/status/…" style="flex:1;width:auto">
      <button class="btn" data-act="mk-reply">${I.reply} Réponse</button><button class="btn" data-act="mk-quote">${I.quote} Citation</button></div></div>
    <h2>Écrire soi-même</h2>
    <div class="card"><textarea id="manual" placeholder="Votre post…"></textarea>
      <div class="row" style="margin-top:10px"><input id="media" placeholder="Visuel (chemin sur le serveur, facultatif)" style="flex:1;width:auto"><button class="btn" data-act="manual">Ajouter aux brouillons</button></div></div>`;
  bindComposer(v, "compose");
}

function vCalendar(v) {
  const off = +store.get("weekOff", 0);
  const start = new Date(); start.setHours(0, 0, 0, 0); start.setDate(start.getDate() + off * 7);
  const days = [...Array(7)].map((_, i) => { const d = new Date(start); d.setDate(d.getDate() + i); return d });
  const evs = S.posts.filter(p => ["scheduled", "validated", "publishing", "published"].includes(p.status)).map(p => ({ p, t: new Date(p.status === "published" ? p.publishedAt : p.scheduledAt) }));
  const sameDay = (a, b) => a.toDateString() === b.toDateString();
  const planned = S.posts.filter(p => p.status === "scheduled" || p.status === "validated").sort((a, b) => new Date(a.scheduledAt) - new Date(b.scheduledAt));
  v.innerHTML = `<div class="head"><div><h1>Calendrier</h1><p>${planned.length} post${planned.length > 1 ? "s" : ""} planifié${planned.length > 1 ? "s" : ""}.</p></div>
    <div class="row"><button class="btn sm" data-act="week" data-d="-1">${I.arrowL}</button><button class="btn sm" data-act="week" data-d="0">Cette semaine</button><button class="btn sm" data-act="week" data-d="1">${I.arrowR}</button><button class="btn sm" data-act="publish-due">${I.send} Publier ce qui est dû</button></div></div>
    <div class="week">${days.map(d => `<div class="day ${sameDay(d, new Date()) ? "today" : ""}"><div class="d">${d.toLocaleDateString("fr-FR", { weekday: "short" })}<b>${d.getDate()}</b></div>
      ${evs.filter(e => sameDay(e.t, d)).sort((a, b) => a.t - b.t).map(e => `<div class="ev ${e.p.status === "published" ? "pub" : ""}" title="${esc(e.p.text)}"><div class="t">${e.t.toLocaleTimeString("fr-FR", { hour: "2-digit", minute: "2-digit" })} · @${esc(handleOf(e.p.account))}</div><div class="x">${esc(e.p.text)}</div></div>`).join("")}</div>`).join("")}</div>
    <h2>File de publication</h2>${planned.length ? planned.map(p => tweetCard(p, "planned")).join("") : empty(I.cal, "Rien de planifié", "Les posts validés apparaissent ici.")}`;
}

// « New » : contenus récupérés depuis la visite précédente de la veille. La
// référence est figée à l'entrée sur la page (les badges restent pendant la
// visite, même après un rafraîchissement), puis la visite est enregistrée.
let watchBaseline;
const isNew = i => watchBaseline === null || (watchBaseline && new Date(i.fetchedAt) > new Date(watchBaseline));

function vWatch(v) {
  if (watchBaseline === undefined) {
    watchBaseline = S.settings.watchSeenAt || null;
    api("/api/watch/seen", {}).then(r => { if (S) S.settings.watchSeenAt = r.watchSeenAt }).catch(() => {});
  }
  const f = store.get("wf", "pending");
  const fresh = (S.items || []).filter(i => i.status === "pending" && isNew(i)).length;
  const filtered = (S.items || []).filter(i => f === "all" || i.status === f);
  const items = f === "pending" ? sortItems(filtered) : filtered.slice().reverse();
  const targets = S.accounts.flatMap(a => (a.watch || []).map(w => ({ ...w, account: a.name })));
  const lab = { pending: "À trier", kept: "Gardés", drafted: "Rédigés", ignored: "Ignorés", all: "Tout" };
  v.innerHTML = `<div class="head"><div><h1>Veille${fresh ? ` <span class="pill new" title="Récupérés depuis votre dernière visite">${fresh} nouveau${fresh > 1 ? "x" : ""}</span>` : ""}</h1><p>Les comptes et recherches que vous suivez. L'IA trie, vous choisissez quoi en faire.</p></div>
    <div class="row"><button class="btn sm" data-act="fetch">${I.refresh} Actualiser</button><button class="btn sm" data-act="bookmarks">Importer mes signets</button><button class="btn primary sm" data-act="triage">${I.spark} Trier avec l'IA</button></div></div>
    <div class="card"><div class="row">${accountSelect("wacc")}
      <div class="seg" id="wk"><button data-k="user" class="on">Compte</button><button data-k="search">Recherche</button></div>
      <input id="wv" placeholder="@handle à suivre" style="flex:1;width:auto;min-width:180px"><select id="we" style="width:auto"><option value="30">toutes les 30 min</option><option value="60" selected>toutes les heures</option><option value="240">toutes les 4 h</option><option value="1440">une fois par jour</option></select>
      <button class="btn primary sm" data-act="watch-add">Suivre</button></div>
      ${targets.length ? `<div class="chips" style="margin-top:12px">${targets.map(t => `<span class="chip">${t.kind === "user" ? "@" : "🔎 "}${esc(t.value)} <a href="#" data-act="watch-rm" data-id="${t.id}" title="Retirer" style="color:var(--muted);margin-left:4px">✕</a></span>`).join("")}</div>` : `<p class="hint">Suivez des comptes inspirants, des concurrents ou une recherche X (ex. <code>agents IA lang:fr -is:retweet</code>).</p>`}
      <div class="row" style="margin-top:12px"><input id="iref" placeholder="Ou ajoutez un post précis : lien x.com" style="flex:1;width:auto"><button class="btn sm" data-act="item-add">Ajouter</button></div></div>
    <div class="row" style="margin:18px 0 12px"><div class="seg">${Object.entries(lab).map(([k, l]) => `<button data-act="wf" data-f="${k}" class="${k === f ? "on" : ""}">${l}</button>`).join("")}</div></div>
    ${f === "pending" && items.some(i => i.suggestion) ? `<p class="small muted" style="margin:-4px 0 12px">L'IA classe les contenus et signale ceux qui méritent un post. Rien n'est ignoré sans votre clic.</p>` : ""}
    ${items.length ? `<div class="items-grid">${items.slice(0, 120).map(itemCard).join("")}</div>` : empty(I.radar, "Rien ici", f === "pending" ? "Actualisez la veille ou ajoutez des cibles." : "Aucun contenu dans cette catégorie.")}`;
  $$("#wk button", v).forEach(b => b.onclick = () => { $$("#wk button", v).forEach(x => x.classList.toggle("on", x === b)); $("#wv", v).placeholder = b.dataset.k === "user" ? "@handle à suivre" : "Requête X : agents IA lang:fr -is:retweet" });
}

// Ordre « à trier » : suggérés par l'IA (meilleur score d'abord), puis pas encore
// notés (plus récents d'abord), puis jugés peu pertinents.
function itemRank(i) { return i.suggestion === "keep" ? 0 : i.suggestion === "skip" ? 2 : 1 }
function sortItems(items) {
  return items.slice().sort((a, b) => itemRank(a) - itemRank(b) || (b.score || 0) - (a.score || 0) || new Date(b.fetchedAt) - new Date(a.fetchedAt));
}

function itemCard(i) {
  const src = i.source.replace("watch:user:", "@").replace("watch:search:", "🔎 ").replace("bookmarks", "signet").replace("manual", "ajout manuel");
  const sugg = i.suggestion === "keep" ? `<span class="pill ok" title="Suggestion de l'IA">${I.spark.replace("<svg", '<svg style="width:12px;height:12px"')} Suggéré · ${i.score}/10</span>`
    : i.suggestion === "skip" ? `<span class="pill" title="Suggestion de l'IA : rien n'est ignoré sans vous">Peu pertinent · ${i.score}/10</span>` : "";
  const st = { kept: '<span class="pill accent">Gardé</span>', drafted: '<span class="pill ok">Rédigé</span>', ignored: '<span class="pill bad">Ignoré</span>' }[i.status] || "";
  return `<article class="card icard ${i.suggestion === "skip" && i.status === "pending" ? "dim" : ""}">
    <div class="ic-head"><div class="av sm">${esc((i.author || "?")[0])}</div><div class="ic-who"><b>@${esc(i.author)}</b><span class="small muted">${rel(i.fetchedAt)} · ${esc(src)}</span></div>${isNew(i) ? `<span class="pill new" title="Récupéré depuis votre dernière visite">New</span>` : ""}</div>
    ${sugg || st ? `<div class="ic-pills">${sugg}${st}</div>` : ""}
    <div class="ic-text">${esc(i.text)}</div>
    <div class="small muted">♥ ${nf(i.likes)} · 👁 ${nf(i.views)} · <a href="${esc(i.url)}" target="_blank" rel="noopener">Ouvrir sur X</a></div>
    ${i.reason ? `<div class="tw-why ic-why">${I.bulb}<span>${esc(i.reason)}${i.angle ? ` — <b>angle :</b> ${esc(i.angle)}` : ""}</span></div>` : ""}
    <div class="ic-actions">
      <button class="btn primary sm" data-act="item-draft" data-id="${i.id}" data-acc="${esc(i.account)}">${I.spark} En faire un post</button>
      <div class="ic-row">
        <button class="btn sm ghost" data-act="item-reply" data-tid="${i.tweetId}" data-acc="${esc(i.account)}" title="Rédiger une réponse">${I.reply} Répondre</button>
        <button class="btn sm ghost" data-act="item-quote" data-tid="${i.tweetId}" data-acc="${esc(i.account)}" title="Rédiger une citation">${I.quote} Citer</button>
        <span class="spacer"></span>
        ${i.status !== "kept" ? `<button class="btn sm ghost" data-act="item-st" data-st="kept" data-id="${i.id}">Garder</button>` : ""}
        ${i.status !== "ignored" ? `<button class="btn sm ghost" data-act="item-st" data-st="ignored" data-id="${i.id}">Ignorer</button>` : `<button class="btn sm ghost" data-act="item-st" data-st="pending" data-id="${i.id}">Remettre à trier</button>`}
      </div></div></article>`;
}

function vPublished(v) {
  const ps = S.posts.filter(p => p.status === "published").sort((a, b) => new Date(b.publishedAt) - new Date(a.publishedAt));
  const withM = ps.filter(p => p.metrics);
  const tot = k => withM.reduce((s, p) => s + (p.metrics[k] || 0), 0);
  const top = withM.slice().sort((a, b) => b.metrics.engagementRatePct - a.metrics.engagementRatePct).slice(0, 3);
  const last = withM.slice(0, 20).reverse(), maxV = Math.max(1, ...last.map(p => p.metrics.views));
  v.innerHTML = `<div class="head"><div><h1>Performances</h1><p>${ps.length} post${ps.length > 1 ? "s" : ""} publié${ps.length > 1 ? "s" : ""}. Les statistiques se rafraîchissent toutes les 6 h.</p></div><button class="btn sm" data-act="metrics">${I.refresh} Rafraîchir</button></div>
    <div class="kpis"><div class="kpi"><div class="k">Vues</div><div class="v">${nf(tot("views"))}</div></div><div class="kpi"><div class="k">J'aime</div><div class="v">${nf(tot("likes"))}</div></div>
      <div class="kpi"><div class="k">Reposts</div><div class="v">${nf(tot("retweets"))}</div></div><div class="kpi"><div class="k">Réponses</div><div class="v">${nf(tot("replies"))}</div></div></div>
    ${last.length > 1 ? `<h2>Vues des derniers posts</h2><div class="card"><svg viewBox="0 0 ${last.length * 30} 120" style="width:100%;height:140px" preserveAspectRatio="none"><defs><linearGradient id="bg" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#c7f586"/><stop offset="1" stop-color="#75dec1"/></linearGradient></defs>${last.map((p, i) => { const h = Math.max(2, (p.metrics.views / maxV) * 112); return `<rect x="${i * 30 + 5}" y="${120 - h}" width="20" height="${h}" rx="4" fill="url(#bg)"><title>${esc(p.text.slice(0, 80))} — ${p.metrics.views} vues</title></rect>` }).join("")}</svg></div>` : ""}
    ${top.length ? `<h2>Meilleurs posts</h2>${top.map(p => tweetCard(p, "view")).join("")}` : ""}
    <h2>Historique</h2>${ps.length ? `<div class="card" style="padding:6px 8px;overflow-x:auto"><table><tr><th>Date</th><th>Post</th><th class="num">Vues</th><th class="num">♥</th><th class="num">↻</th><th class="num">Eng.</th></tr>
      ${ps.map(p => { const m = p.metrics || {}, u = tweetURL(p); return `<tr><td class="small muted" style="white-space:nowrap">${fmtDate(p.publishedAt, { dateStyle: "short", timeStyle: "short" })}</td><td>${u ? `<a href="${u}" target="_blank" rel="noopener">` : ""}${esc(p.text.slice(0, 110))}${p.text.length > 110 ? "…" : ""}${u ? "</a>" : ""}</td><td class="num">${m.views != null ? nf(m.views) : "–"}</td><td class="num">${m.likes ?? "–"}</td><td class="num">${m.retweets ?? "–"}</td><td class="num">${m.views ? m.engagementRatePct.toFixed(1) + " %" : "–"}</td></tr>` }).join("")}</table></div>` : empty(I.chart, "Pas encore de publication", "Vos posts publiés et leurs statistiques apparaîtront ici.")}`;
}

// ------------------------------------------------------------------ réglages
function providerCards(selected, prefix = "") {
  return `<div class="prov">${(P || []).map(p => {
    const st = PROVIDER_STYLE[p.id] || {};
    const status = p.available ? `<span class="pill ok">${p.kind === "cli" ? "Installé" : "Configuré"}</span>` : `<span class="pill">${p.kind === "cli" ? "Non installé" : "Clé absente"}</span>`;
    return `<button class="pcard ${p.id === selected ? "on" : ""} ${p.available ? "" : "na"}" data-act="${prefix}pick" data-p="${p.id}">
      <span class="check">${I.check}</span><div class="logo" style="background:${st.bg}">${st.l}</div>
      <h3>${esc(p.name)}</h3><div class="small muted">${esc(p.vendor)} · ${p.kind === "cli" ? "CLI locale" : "API"}${p.experimental ? " · expérimental" : ""}</div>
      <div class="row" style="margin-top:10px">${status}${p.version ? `<span class="small faint">${esc(p.version.slice(0, 28))}</span>` : ""}</div>
      <p class="small muted" style="margin:10px 0 0">${esc(p.hint)}</p></button>`;
  }).join("")}</div>`;
}

// ------------------------------------------------------------------ affiner l'IA
const PROMPT_KINDS = [
  ["ideas", "Idées originales", "Quand vous générez sans sujet : ce qu'est une bonne idée pour ce compte, ce qu'il faut éviter."],
  ["item", "Posts tirés de la veille", "Pour un brouillon écrit à partir d'un contenu repéré (actualité, signet, compte suivi)."],
  ["reply", "Réponses", "Pour répondre à un post : ce que la réponse doit apporter."],
  ["quote", "Citations", "Pour citer un post : comment ajouter votre angle."],
  ["rewrite", "Réécritures", "Envoyée avant votre retour à chaque « Réécrire »."],
  ["visual", "Idée de visuel", "Renseignée, l'IA propose un visuel avec chaque brouillon (le studio ne génère pas l'image). Vide : pas de proposition."],
];

// consignes par demande ; `inherit(k)` = valeur héritée, montrée en grisé
function promptsFields(prompts, inherit, prefix) {
  return `<div class="grid2">${PROMPT_KINDS.map(([k, label, hint]) => `<div><label class="l" for="${prefix}${k}">${label}</label>
    <textarea id="${prefix}${k}" data-prompt="${k}" rows="3" placeholder="${esc(inherit(k) || "Aucune : pas d'idée de visuel")}">${esc(prompts?.[k] || "")}</textarea><div class="hint">${hint}</div></div>`).join("")}</div>`;
}
const readPrompts = box => Object.fromEntries($$("[data-prompt]", box).map(t => [t.dataset.prompt, t.value.trim()]));

function typeRow(t = {}) {
  return `<div class="ptype" data-key="${esc(t.key || "")}">
    <div class="ptype-head"><input data-f="label" value="${esc(t.label || "")}" placeholder="Libellé (ex. Coulisses)" aria-label="Libellé du type">
      <input data-f="goal" value="${esc(t.goal || "")}" placeholder="Effet recherché (ex. confiance)" aria-label="Effet recherché">
      <label class="share"><input data-f="share" type="number" min="0" max="100" value="${t.share ?? 25}" aria-label="Part visée en pourcentage"><span>%</span></label>
      <button class="btn ghost sm" data-act="pt-del" title="Retirer ce type" aria-label="Retirer ce type">${I.x}</button></div>
    <textarea data-f="prompt" rows="2" placeholder="Consigne d'écriture de ce type">${esc(t.prompt || "")}</textarea></div>`;
}

// éditeur de types de posts ; `empty` = texte affiché quand la liste est vide
function typesEditor(types, empty) {
  return `<div class="ptypes" data-types>${(types || []).map(typeRow).join("")}</div>
    <p class="hint ptypes-empty">${empty}</p>
    <div class="row" style="margin-top:8px"><button class="btn sm" data-act="pt-add">Ajouter un type</button><button class="btn ghost sm" data-act="pt-defaults">Partir des 4 types par défaut</button></div>`;
}
const readTypes = box => $$(".ptype", $("[data-types]", box)).map(r => ({
  key: r.dataset.key, label: $("[data-f=label]", r).value.trim(), goal: $("[data-f=goal]", r).value.trim(),
  share: +$("[data-f=share]", r).value || 0, prompt: $("[data-f=prompt]", r).value.trim(),
})).filter(t => t.label || t.prompt);

function mixLine(name) {
  const m = S.typeMix?.[name];
  if (!m || !m.total) return "";
  return `<div class="hint">Sur 30 jours (${m.total} posts typés) : ${m.mix.map(x => `${esc(x.label)} ${x.actual} % / ${x.target} %`).join(" · ")}.</div>`;
}

const RULE_STATUS = { proposed: ["Proposée", "warn"], accepted: ["En place", "ok"], refused: ["Refusée", ""], revoked: ["Retirée", ""] };
function rulesBlock() {
  const rules = S.rules || [], pending = (S.learnings || []).filter(l => !l.synthesizedAt).length;
  const scope = r => r.account ? "@" + esc(handleOf(r.account)) : "Tous";
  const row = r => `<tr><td class="small muted" style="width:90px">${scope(r)}</td>
    <td>${r.status === "proposed" ? `<input data-rule="${r.id}" value="${esc(r.text)}" aria-label="Texte de la règle">` : esc(r.text)}${r.rationale ? `<div class="hint">${esc(r.rationale)}</div>` : ""}</td>
    <td style="width:1%;white-space:nowrap"><span class="pill ${RULE_STATUS[r.status][1]}">${RULE_STATUS[r.status][0]}</span>
      ${r.status === "proposed" ? `<button class="btn sm primary" data-act="rule-accept" data-id="${r.id}">Accepter</button><button class="btn sm ghost" data-act="rule-refuse" data-id="${r.id}">Refuser</button>`
        : r.status === "accepted" ? `<button class="btn sm ghost" data-act="rule-revoke" data-id="${r.id}">Retirer</button>`
        : r.status === "revoked" ? `<button class="btn sm ghost" data-act="rule-accept" data-id="${r.id}">Remettre</button>` : ""}</td></tr>`;
  const live = rules.filter(r => r.status === "proposed" || r.status === "accepted"), old = rules.filter(r => r.status === "refused" || r.status === "revoked");
  return `<h3 style="margin-top:22px">Règles apprises</h3>
    <p class="small muted" style="margin-top:0">L'IA relit vos retours (rejets, réécritures, leçons) et en tire des règles d'écriture. Une règle en place guide chaque demande ; une règle refusée ou retirée n'est jamais reproposée.</p>
    <div class="row"><label class="toggle"><input type="checkbox" data-setting="rulesAutoAccept" ${S.settings.rulesAutoAccept ? "checked" : ""}><span class="sw"></span>Appliquer d'office les règles proposées</label>
      <label class="toggle"><input type="checkbox" data-setting="autoSynthesis" ${S.settings.autoSynthesis ? "checked" : ""}><span class="sw"></span>Synthèse chaque semaine</label>
      <span class="spacer"></span><span class="small muted">${pending} retour${pending > 1 ? "s" : ""} à synthétiser</span>
      <button class="btn sm" data-act="synthesize" ${pending < 2 ? "disabled title=\"2 retours au moins\"" : ""}>${I.spark} Synthétiser maintenant</button></div>
    ${live.length ? `<table style="margin-top:12px">${live.map(row).join("")}</table>` : `<p class="hint">Aucune règle apprise pour l'instant.</p>`}
    ${old.length ? `<details class="tune" style="margin-top:10px"><summary>Règles refusées ou retirées (${old.length})</summary><table>${old.map(row).join("")}</table></details>` : ""}`;
}

function vSettings(v) {
  const s = S.settings, sel = S.ai?.provider;
  v.innerHTML = `<div class="head"><div><h1>Réglages</h1><p>Votre IA, vos comptes X et vos garde-fous.</p></div><span class="small faint">v${esc(S.version)}</span></div>
    <h2>Intelligence artificielle</h2>
    <div class="card"><p class="muted small" style="margin-top:0">XGrowth utilise les IA déjà installées sur le serveur : votre abonnement Claude via Claude Code, votre compte ChatGPT via Codex, ou l'API. Elles tournent sans aucun outil : elles écrivent, rien d'autre.</p>
      ${providerCards(sel)}
      <div class="grid" style="margin-top:14px"><div><label class="l">Modèle (vide = défaut du fournisseur)</label><input id="model" value="${esc(S.ai?.model || "")}" placeholder="${esc((P || []).find(p => p.id === sel)?.defaultModel || "")}"></div>
      ${sel === "anthropic" ? `<div><label class="l">Effort</label><select id="effort">${["low", "medium", "high", "xhigh", "max"].map(x => `<option ${s.effort === x ? "selected" : ""}>${x}</option>`).join("")}</select></div>` : ""}</div>
      <div class="row" style="margin-top:14px"><button class="btn" data-act="ai-test">${I.spark} Tester</button><button class="btn primary" data-act="ai-save">Enregistrer</button><button class="btn ghost sm" data-act="ai-refresh">${I.refresh} Détecter à nouveau</button></div>
      <div id="ai-out"></div></div>
    <h2>Comptes X</h2>${S.accounts.map(accountForm).join("")}
    <div class="card"><div class="row between"><div><h3>Ajouter un compte</h3><p class="small muted" style="margin:0">L'assistant vous guide : connexion à X, voix, rythme.</p></div><button class="btn" data-act="wizard-account">Ajouter</button></div></div>
    <h2>Affiner l'IA</h2>
    <div class="card" id="tune-common"><p class="small muted" style="margin-top:0">Valable pour tous les comptes. Chaque compte peut remplacer ces réglages (Comptes X › Modifier › Affiner l'IA pour ce compte).</p>
      <label class="l" for="rules">Règles d'écriture communes</label><textarea id="rules" rows="4" placeholder="Une règle par ligne. Ex. : N'invente jamais un chiffre.">${esc((s.rules || []).join("\n"))}</textarea>
      <div class="hint">Ajoutées à chaque demande, avec les règles apprises en place. Les consignes propres à un compte restent dans sa fiche.</div>
      <h3 style="margin-top:22px">Consignes par demande</h3><p class="small muted" style="margin-top:0">Vide : la consigne par défaut, affichée en grisé.</p>
      ${promptsFields(s.prompts, k => S.defaults?.prompts?.[k], "gp-")}
      <h3 style="margin-top:22px">Types de posts</h3><p class="small muted" style="margin-top:0">Chaque brouillon relève d'un type ; l'IA privilégie le type le plus en retard sur sa part visée (30 derniers jours).</p>
      ${typesEditor(s.postTypes, "Aucun type commun : l'IA écrit sans type, sauf pour les comptes qui ont les leurs.")}
      <div class="row" style="margin-top:14px"><span class="spacer"></span><button class="btn primary" data-act="tune-save">Enregistrer</button></div></div>
    <h2>Garde-fous</h2>
    <div class="card"><div class="grid"><div><label class="l">Écart minimal entre deux posts</label><select id="gap">${[15, 30, 60, 120, 180, 240].map(m => `<option value="${m}" ${s.minGapMinutes === m ? "selected" : ""}>${m < 60 ? m + " min" : m / 60 + " h"}</option>`).join("")}</select></div>
      <div><label class="l">Score minimal pour garder un contenu</label><input id="tmin" type="number" step="0.5" min="0" max="10" value="${s.triageMinScore}"></div>
      <div><label class="l">Contenus triés par passage</label><input id="maxi" type="number" value="${s.maxItemsPerRun}"></div></div>
      <label class="l">Concurrents jamais nommés</label><input id="comp" value="${esc((s.competitors || []).join(", "))}" placeholder="Acme, Globex"><div class="hint">Un post qui en nomme un est bloqué à la validation et juste avant l'envoi.</div>
      <label class="l">Expressions interdites</label><input id="ban" value="${esc((s.bannedPhrases || []).join(", "))}" placeholder="révolutionnaire, game changer">
      <div class="row" style="margin-top:14px"><span class="spacer"></span><button class="btn primary" data-act="guards-save">Enregistrer</button></div></div>
    <h2>Leçons et règles apprises</h2>
    <div class="card"><p class="small muted" style="margin-top:0">Ajoutées à chaque demande à l'IA. Vos retours de réécriture et motifs de rejet arrivent ici automatiquement.</p>
      <div class="row"><select id="la" style="width:auto"><option value="">Tous les comptes</option>${S.accounts.map(a => `<option value="${esc(a.name)}">@${esc(a.handle || a.name)}</option>`).join("")}</select><input id="lt" placeholder="Ex. : jamais de question rhétorique en accroche" style="flex:1;width:auto"><button class="btn" data-act="learn">Ajouter</button></div>
      ${(S.learnings || []).length ? `<table style="margin-top:12px">${S.learnings.slice().reverse().map(l => `<tr><td class="small muted" style="width:120px">${l.account ? "@" + esc(handleOf(l.account)) : "Tous"}</td><td>${esc(l.text)}${l.synthesizedAt ? ` <span class="small faint">· synthétisé</span>` : ""}</td><td style="width:40px"><button class="btn ghost sm" data-act="unlearn" data-id="${l.id}">✕</button></td></tr>`).join("")}</table>` : ""}
      ${rulesBlock()}</div>
    <h2>Journal</h2>
    <div class="card" style="padding:6px 8px;max-height:320px;overflow:auto"><table>${(S.log || []).slice().reverse().map(e => `<tr><td class="small muted" style="white-space:nowrap">${fmtDate(e.at, { dateStyle: "short", timeStyle: "short" })}</td><td><span class="pill ${e.level === "error" ? "bad" : ""}">${e.level}</span></td><td class="small">${esc(e.message)}</td></tr>`).join("") || `<tr><td class="muted">Rien pour l'instant.</td></tr>`}</table></div>`;
  $$("[data-setting]", v).forEach(i => i.onchange = () => run(null, () => api("/api/settings", { [i.dataset.setting]: i.checked }), "Réglage enregistré"));
  $$("[data-types]", v).forEach(syncTypesEmpty);
}

function syncTypesEmpty(box) { box.parentElement.querySelector(".ptypes-empty")?.classList.toggle("hidden", !!box.children.length) }

function accountForm(a) {
  const be = { agentx: "AgentX (session X)", xapi: "API X officielle", dryrun: "Simulation" }[a.backend] || a.backend;
  return `<div class="card" data-acc="${esc(a.name)}">
    <div class="row"><div class="av sm">${esc((a.handle || a.name)[0])}</div><div><b>@${esc(a.handle || a.name)}</b><div class="small muted">${be} · ${esc(a.timezone)} · ${(a.slots || []).join(", ")}</div></div><span class="spacer"></span>
      <button class="btn sm" data-act="acc-check" data-name="${esc(a.name)}">${I.shield} Vérifier la connexion</button><button class="btn sm ghost" data-act="acc-toggle">Modifier</button></div>
    <div class="acc-body hidden">
      <div class="grid"><div><label class="l">Handle X</label><input name="handle" value="${esc(a.handle)}"></div>
        <div><label class="l">Connexion</label><select name="backend">${["agentx", "xapi", "dryrun"].map(b => `<option value="${b}" ${a.backend === b ? "selected" : ""}>${{ agentx: "AgentX (session X)", xapi: "API X officielle", dryrun: "Simulation" }[b]}</option>`).join("")}</select></div>
        <div><label class="l">Session agentx</label><input name="agentxAccount" value="${esc(a.agentxAccount || "")}"></div>
        <div><label class="l">Variable du jeton API X</label><input name="xapiTokenEnv" value="${esc(a.xapiTokenEnv || "")}" placeholder="X_USER_TOKEN"></div>
        <div><label class="l">Fuseau horaire</label><input name="timezone" value="${esc(a.timezone)}"></div>
        <div><label class="l">Créneaux</label><input name="slots" value="${esc((a.slots || []).join(", "))}"></div>
        <div><label class="l">Posts max par jour</label><input name="dailyLimit" type="number" value="${a.dailyLimit || 6}"></div>
        <div><label class="l">Langue</label><input name="language" value="${esc(a.language || "")}"></div></div>
      <label class="l">Voix</label><textarea name="voice">${esc(a.voice)}</textarea>
      <label class="l">Consignes</label><textarea name="instructions">${esc(a.instructions)}</textarea>
      <label class="l">Sujets</label><input name="topics" value="${esc((a.topics || []).join(", "))}">
      <label class="l">Critères de tri de la veille</label><textarea name="triageRules">${esc(a.triageRules)}</textarea>
      <div class="row" style="margin-top:14px;gap:18px">
        <label class="toggle"><input type="checkbox" name="premium" ${a.premium ? "checked" : ""}><span class="sw"></span>X Premium</label>
        <label class="toggle"><input type="checkbox" name="autoTriage" ${a.autoTriage ? "checked" : ""}><span class="sw"></span>Tri automatique</label>
        <label class="toggle"><input type="checkbox" name="autoDraft" ${a.autoDraft ? "checked" : ""}><span class="sw"></span>Brouillons automatiques</label></div>
      <details class="tune" style="margin-top:18px"><summary>${I.sliders} Affiner l'IA pour ce compte</summary>
        <label class="l">Objectifs du compte</label><textarea name="goals" rows="2" placeholder="Ex. : 50 clients pour l'outil d'audit d'ici mars ; être la référence française sur les agents IA">${esc(a.goals || "")}</textarea>
        <div class="hint">Chaque post doit les servir. L'IA les lit avant d'écrire.</div>
        <div class="grid"><div><label class="l">Longueur visée</label><select name="length">${[["court", "Courte (une à trois phrases)"], ["moyen", "Moyenne (un paragraphe)"], ["long", "Longue (X Premium)"]].map(([k, l]) => `<option value="${k}" ${(a.length || "court") === k ? "selected" : ""}>${l}</option>`).join("")}</select>
          <div class="hint">Sans X Premium, la limite reste 280 caractères.</div></div></div>
        <label class="l">Posts de référence</label><textarea name="examples" rows="5" placeholder="Collez des posts dont vous aimez le style (les vôtres ou ceux d'autres), séparés par une ligne contenant seulement ---">${esc((a.examples || []).join("\n---\n"))}</textarea>
        <div class="hint">10 au plus. L'IA s'inspire de leur ton et de leur rythme sans les recopier.</div>
        <h3 style="margin-top:20px">Types de posts de ce compte</h3>
        ${typesEditor(a.postTypes, `Vide : les types communs s'appliquent (${(S.settings.postTypes || []).length || "aucun"}).`)}${mixLine(a.name)}
        <h3 style="margin-top:20px">Consignes propres à ce compte</h3><p class="small muted" style="margin-top:0">Vide : la consigne commune (ou par défaut), affichée en grisé.</p>
        ${promptsFields(a.prompts, inheritedPrompt, "ap-" + esc(a.name) + "-")}
      </details>
      <div class="row" style="margin-top:16px"><button class="btn ghost danger sm" data-act="acc-del" data-name="${esc(a.name)}">Supprimer</button><span class="spacer"></span><button class="btn primary" data-act="acc-save" data-name="${esc(a.name)}">Enregistrer</button></div></div></div>`;
}

function readAccountForm(card, base) {
  const o = { ...base };
  $$("[name]", card).forEach(i => {
    const k = i.name;
    if (i.type === "checkbox") o[k] = i.checked;
    else if (k === "slots" || k === "topics") o[k] = i.value.split(",").map(x => x.trim()).filter(Boolean);
    else if (k === "dailyLimit") o[k] = +i.value;
    else if (k === "examples") o[k] = i.value.split(/^\s*---\s*$/m).map(x => x.trim()).filter(Boolean);
    else o[k] = i.value.trim();
  });
  o.postTypes = readTypes(card);
  o.prompts = readPrompts(card);
  return o;
}

// ------------------------------------------------------------------ actions (délégation)
const actions = {
  async theme() {
    const next = THEMES[(THEMES.indexOf(themePref()) + 1) % THEMES.length];
    store.set("theme", next); applyTheme(); toast(`Thème : ${THEME_LABEL[next]}`);
  },
  async logout() { await api("/api/auth/logout", {}); S = null; boot() },
  async generate(b) {
    const ctx = b.dataset.ctx, acc = $("#acc-" + ctx).value, topic = $("#brief-" + ctx).value, n = +store.get("n", 3), type = $("#type-" + ctx)?.value || "";
    const ps = await run(b, () => api("/api/draft", { account: acc, topic, n, type }), r => `${r.length} brouillon${r.length > 1 ? "s" : ""} prêt${r.length > 1 ? "s" : ""} à valider`, { wait: "Écriture…", noReload: true });
    store.set("brief", "");
    S = await api("/api/state");
    if (route() === "queue") return render();
    render();
    const out = $("#gen-" + ctx);
    if (out) out.innerHTML = `<h2>Tout juste écrits</h2>${ps.map(p => tweetCard(S.posts.find(x => x.id === p.id) || p, "edit")).join("")}`, vQueueBind();
  },
  async validate(b) { await doValidate(b, {}) },
  async now(b) { if (confirm("Publier ce post sur X dans la minute ?")) await doValidate(b, { now: true }) },
  async "validate-at"(b) { $(`[data-at="${b.dataset.id}"]`).classList.toggle("hidden") },
  async "validate-at-go"(b) {
    const v = b.previousElementSibling.value;
    if (!v) return toast("Choisissez une date et une heure", true);
    await doValidate(b, { at: new Date(v).toISOString() });
  },
  async save(b) { const id = b.dataset.id; await run(b, () => api(`/api/posts/${id}/edit`, { text: edits[id] ?? "" }), "Enregistré"); delete edits[id] },
  async reject(b) {
    const id = b.dataset.id, reason = $(`[data-fb="${id}"]`).value.trim() || prompt("Pourquoi ? (facultatif, l'IA en tiendra compte)") || "";
    await run(b, () => api(`/api/posts/${id}/reject`, { reason }), "Rejeté");
  },
  async rewrite(b) {
    const id = b.dataset.id, fb = $(`[data-fb="${id}"]`).value.trim();
    if (!fb) return toast("Écrivez d'abord ce qu'il faut changer", true), $(`[data-fb="${id}"]`).focus();
    await run(b, async () => { if (edits[id] !== undefined) await api(`/api/posts/${id}/edit`, { text: edits[id] }); delete edits[id]; return api(`/api/posts/${id}/rewrite`, { feedback: fb }) }, "Réécrit", { wait: "Réécriture…" });
  },
  async unschedule(b) { await run(b, () => api(`/api/posts/${b.dataset.id}/unschedule`, {}), "Renvoyé en brouillon") },
  async "publish-due"(b) { await run(b, () => api("/api/publish-due", {}), r => r.length ? `${r.length} post(s) traité(s)` : "Rien n'était dû") },
  async week(b) { const d = +b.dataset.d; store.set("weekOff", d === 0 ? 0 : +store.get("weekOff", 0) + d); render() },
  async "mk-reply"(b) { await mkReply(b, false) },
  async "mk-quote"(b) { await mkReply(b, true) },
  async manual(b) { await run(b, () => api("/api/write", { account: store.get("acc", S.accounts[0]?.name), text: $("#manual").value, media: $("#media").value }), "Ajouté à la file") },
  async fetch(b) { await run(b, () => api("/api/fetch", {}), r => r.length ? r.map(x => x.error ? `${x.source} : ${x.error}` : `${x.source.replace("watch:", "")} +${x.added}`).join(" · ") : "Aucune cible suivie", { wait: "Lecture…" }) },
  async bookmarks(b) { await run(b, () => api("/api/bookmarks", { account: $("#wacc").value, n: 20 }), r => `${r.added} signet(s) importé(s)`) },
  async triage(b) { await run(b, () => api("/api/triage", { account: $("#wacc").value }), r => r.map(x => x.error || (x.triaged ? `${x.triaged} contenu(s) noté(s), ${x.kept} suggéré(s)` : "Tout est déjà noté")).join(" · ") || "Rien à trier", { wait: "Tri…" }) },
  async "watch-add"(b) {
    const kind = $("#wk .on").dataset.k;
    await run(b, () => api("/api/watch", { account: $("#wacc").value, kind, value: $("#wv").value, every: +$("#we").value }), "Ajouté à la veille");
  },
  async "watch-rm"(b, e) { e.preventDefault(); await run(null, () => api(`/api/watch/${b.dataset.id}/remove`, {}), "Retiré") },
  async "item-add"(b) { await run(b, () => api("/api/items", { account: $("#wacc").value, ref: $("#iref").value }), "Ajouté") },
  async wf(b) { store.set("wf", b.dataset.f); render() },
  async "item-st"(b) { await run(b, () => api(`/api/items/${b.dataset.id}/status`, { status: b.dataset.st })) },
  async "item-draft"(b) { await run(b, () => api("/api/draft", { account: b.dataset.acc, item: b.dataset.id, n: 2 }), "Brouillons ajoutés à « À valider »", { wait: "Écriture…" }) },
  async "item-reply"(b) { await run(b, () => api("/api/reply", { account: b.dataset.acc, ref: b.dataset.tid }), "Réponse ajoutée à « À valider »", { wait: "Écriture…" }) },
  async "item-quote"(b) { await run(b, () => api("/api/reply", { account: b.dataset.acc, ref: b.dataset.tid, quote: true }), "Citation ajoutée à « À valider »", { wait: "Écriture…" }) },
  async metrics(b) { await run(b, () => api("/api/metrics", {}), r => `${r.updated} post(s) mis à jour`) },
  async pick(b) { S.ai = { provider: b.dataset.p, model: "" }; render() },
  async "ai-refresh"(b) { P = await run(b, () => api("/api/providers?refresh=1"), "Détection terminée", { noReload: true }); render() },
  async "ai-test"(b) {
    const r = await run(b, () => api("/api/ai/test", { provider: S.ai.provider, model: $("#model").value }), null, { wait: "Test…", noReload: true });
    $("#ai-out").innerHTML = aiTestResult(r);
  },
  async "ai-save"(b) { await run(b, () => api("/api/ai/select", { provider: S.ai.provider, model: $("#model").value, effort: $("#effort")?.value || "" }), `IA : ${providerName(S.ai.provider)}`) },
  async "acc-toggle"(b) { b.closest(".card").querySelector(".acc-body").classList.toggle("hidden") },
  async "acc-save"(b) { const card = b.closest(".card"); await run(b, () => api("/api/accounts", readAccountForm(card, acct(b.dataset.name))), "Compte enregistré") },
  async "acc-del"(b) { if (confirm("Supprimer ce compte du studio ? Ses posts restent dans l'historique.")) await run(b, () => api(`/api/accounts/${encodeURIComponent(b.dataset.name)}/remove`, {}), "Compte supprimé") },
  async "acc-check"(b) {
    const r = await run(b, () => api(`/api/accounts/${encodeURIComponent(b.dataset.name)}/check`, {}), null, { wait: "Vérification…", noReload: true });
    r.ok ? toast(`Connecté en tant que @${r.profile?.screenName || "simulation"}${r.profile?.followersCount ? ` · ${nf(r.profile.followersCount)} abonnés` : ""}`) : toast(r.problem, true);
  },
  async "wizard-account"() { wizard.start(2, true) },
  async "guards-save"(b) {
    const L = id => $(id).value.split(",").map(x => x.trim()).filter(Boolean);
    await run(b, () => api("/api/settings", { minGapMinutes: +$("#gap").value, triageMinScore: +$("#tmin").value, maxItemsPerRun: +$("#maxi").value, competitors: L("#comp"), bannedPhrases: L("#ban") }), "Garde-fous enregistrés");
  },
  async "tune-save"(b) {
    const box = $("#tune-common");
    await run(b, () => api("/api/settings", { rules: $("#rules").value.split("\n").map(x => x.trim()).filter(Boolean), prompts: readPrompts(box), postTypes: readTypes(box) }), "Réglages de l'IA enregistrés");
  },
  async "pt-add"(b) { dirty = true; const box = b.parentElement.parentElement.querySelector("[data-types]"); box.insertAdjacentHTML("beforeend", typeRow({ share: 25 })); syncTypesEmpty(box); $(".ptype:last-child [data-f=label]", box).focus() },
  async "pt-defaults"(b) { dirty = true;
    const box = b.parentElement.parentElement.querySelector("[data-types]");
    if (box.children.length && !confirm("Remplacer les types affichés par les 4 types par défaut ?")) return;
    box.innerHTML = (S.defaults?.postTypes || []).map(typeRow).join(""); syncTypesEmpty(box);
  },
  async "pt-del"(b) { dirty = true; const box = b.closest("[data-types]"); b.closest(".ptype").remove(); syncTypesEmpty(box) },
  async synthesize(b) { await run(b, () => api("/api/rules/synthesize", {}), r => r.proposed.length ? `${r.proposed.length} règle(s) tirée(s) de ${r.examined} retours` : `${r.examined} retours relus, aucune règle nouvelle`, { wait: "Synthèse…" }) },
  async "rule-accept"(b) { await run(b, () => api(`/api/rules/${b.dataset.id}/accept`, { text: $(`[data-rule="${b.dataset.id}"]`)?.value || "" }), "Règle en place") },
  async "rule-refuse"(b) { await run(b, () => api(`/api/rules/${b.dataset.id}/refuse`, {}), "Règle refusée") },
  async "rule-revoke"(b) { await run(b, () => api(`/api/rules/${b.dataset.id}/revoke`, {}), "Règle retirée") },
  async learn(b) { await run(b, () => api("/api/learnings", { account: $("#la").value, text: $("#lt").value }), "Leçon ajoutée") },
  async unlearn(b) { await run(b, () => api(`/api/learnings/${b.dataset.id}/remove`, {})) },
};

function autosize(ta) { ta.style.height = "auto"; ta.style.height = ta.scrollHeight + 4 + "px" }

function vQueueBind() { if (route() !== "queue") { const v = $("#view"); $$("[data-edit]", v).forEach(ta => { autosize(ta); ta.oninput = () => { edits[ta.dataset.edit] = ta.value; autosize(ta) } }) } }

async function doValidate(b, extra) {
  let by = who() || $("#who")?.value.trim();
  if (!by) { by = (prompt("Votre nom, pour signer la validation :") || "").trim(); if (!by) return; store.set("who", by) }
  const id = b.dataset.id;
  await run(b, async () => {
    if (edits[id] !== undefined) { await api(`/api/posts/${id}/edit`, { text: edits[id] }); delete edits[id] }
    return api(`/api/posts/${id}/validate`, { by, ...extra });
  }, r => r.status === "validated" ? "Validé : publication dans la minute" : `Planifié ${rel(r.scheduledAt)} (${fmtDate(r.scheduledAt)})`);
}

async function mkReply(b, quote) {
  const ref = $("#ref").value.trim();
  if (!ref) return toast("Collez d'abord le lien du post", true);
  await run(b, () => api("/api/reply", { account: store.get("acc", S.accounts[0]?.name), ref, quote }), quote ? "Citation ajoutée à « À valider »" : "Réponse ajoutée à « À valider »", { wait: "Écriture…" });
}

function aiTestResult(r) {
  return r.ok ? `<div class="sample"><div class="row small muted" style="margin-bottom:6px">${I.check.replace("<svg", '<svg style="width:14px;height:14px;color:var(--ok)"')} ${esc(providerName(r.provider))} répond en ${(r.ms / 1000).toFixed(1)} s</div>« ${esc(r.sample)} »</div>`
    : `<div class="tw-err" style="margin-top:12px">${esc(r.error)}</div>`;
}

document.addEventListener("click", async e => {
  const go = e.target.closest("[data-go]");
  if (go) { location.hash = "#/" + go.dataset.go; return }
  const b = e.target.closest("[data-act]");
  if (!b) return;
  const fn = actions[b.dataset.act] || wizard.actions[b.dataset.act];
  if (!fn) return;
  try { await fn(b, e) } catch {}
});

// ------------------------------------------------------------------ onboarding
const VOICES = [
  ["Expert pédagogue", "J'explique simplement des sujets techniques, avec des exemples concrets. Ton chaleureux, tutoiement, phrases courtes."],
  ["Fondateur transparent", "Je partage les coulisses : chiffres, erreurs, décisions. Honnête, sans langue de bois, première personne."],
  ["Punchy & direct", "Phrases très courtes. Une idée forte par post. Opinions assumées, zéro remplissage."],
  ["Curateur de veille", "Je repère ce qui compte dans mon domaine et je l'explique en une phrase utile. Factuel, toujours la source en tête."],
];
const RHYTHMS = [["Tranquille", ["12:30"], 1], ["Régulier", ["09:00", "12:30", "18:00"], 3], ["Intensif", ["08:30", "11:00", "13:00", "16:30", "19:00"], 5]];

const wizard = {
  w: null,
  start(step = 0, addOnly = false) {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || "Europe/Paris";
    this.w = { step, addOnly, provider: S.ai?.provider || (P || []).find(p => p.available)?.id || "anthropic", model: S.ai?.model || "", test: null,
      method: "agentx", profile: null, session: "", handle: "", voice: "", goals: "", topics: [], language: "français", tz, rhythm: 1, validator: who(),
      competitors: (S.settings.competitors || []).join(", "), watch: [], drafts: null, saved: null };
    this.render();
  },
  steps: ["Bienvenue", "IA", "Compte X", "Voix", "Rythme", "Veille", "Premier post"],
  render() {
    const w = this.w, st = this.steps;
    const first = w.addOnly ? 2 : 0;
    $("#root").innerHTML = `<div class="overlay"><div class="ob">
      <div class="ob-top"><span class="mark">${I.logo}</span><span class="brandword">XGrowth<span class="brand-sub">STUDIO</span></span><div class="steps">${st.slice(first).map((_, i) => `<i class="${i + first === w.step ? "on" : i + first < w.step ? "done" : ""}"></i>`).join("")}</div></div>
      <div id="obv"></div></div></div>`;
    this["s" + w.step]($("#obv"));
  },
  nav(nextLabel = "Continuer", opts = {}) {
    const first = this.w.addOnly ? 2 : 0;
    return `<div class="ob-nav">${this.w.step > first ? `<button class="btn ghost" data-act="ob-back">${I.arrowL} Retour</button>` : this.w.addOnly ? `<button class="btn ghost" data-act="ob-cancel">Annuler</button>` : ""}<span class="spacer"></span>
      ${opts.skip ? `<button class="btn ghost" data-act="ob-skip">${opts.skip}</button>` : ""}<button class="btn primary lg" data-act="ob-next" ${opts.disabled ? "disabled" : ""}>${nextLabel} ${I.arrowR}</button></div>`;
  },
  s0(v) {
    v.innerHTML = `<h1>Votre studio de croissance sur X, <span class="gradtext">piloté par l'IA</span>.</h1>
      <p class="lead">L'IA de votre choix écrit dans votre voix, surveille votre domaine et prépare vos posts. Vous gardez la main : rien ne part sans votre validation.</p>
      <div class="feat">
        <div class="card">${I.spark}<h3>Écrit comme vous</h3><p class="small muted">Votre ton, vos sujets, vos leçons. L'IA apprend de chaque retouche.</p></div>
        <div class="card">${I.radar}<h3>Veille et tri</h3><p class="small muted">Comptes, recherches et signets triés pour n'en garder que l'utile.</p></div>
        <div class="card">${I.shield}<h3>Vous validez</h3><p class="small muted">Garde-fous, créneaux, limites. Chaque post est signé avant l'envoi.</p></div>
      </div>
      <p class="muted">5 minutes pour tout configurer.</p>${this.nav("Commencer")}`;
  },
  s1(v) {
    const w = this.w;
    v.innerHTML = `<h1>Quelle IA écrit pour vous ?</h1>
      <p class="lead">XGrowth a détecté les IA disponibles sur ce serveur. Les CLI utilisent vos abonnements existants (Claude, ChatGPT) : rien à payer en plus.</p>
      ${providerCards(w.provider, "ob-")}
      <div class="row" style="margin-top:16px"><input id="obm" value="${esc(w.model)}" placeholder="Modèle (facultatif) — ${esc((P || []).find(p => p.id === w.provider)?.defaultModel || "")}" style="flex:1;width:auto">
        <button class="btn" data-act="ob-test">${I.spark} Tester cette IA</button><button class="btn ghost sm" data-act="ob-redetect">${I.refresh}</button></div>
      <div id="obt">${w.test && w.test.provider === w.provider ? aiTestResult(w.test) : ""}</div>
      ${this.nav("Continuer", { skip: w.test?.ok ? "" : "Continuer sans tester" })}`;
  },
  s2(v) {
    const w = this.w;
    const m = (id, t, d, badge) => `<button class="pcard ${w.method === id ? "on" : ""}" data-act="ob-method" data-m="${id}"><span class="check">${I.check}</span><h3>${t} ${badge || ""}</h3><p class="small muted" style="margin:6px 0 0">${d}</p></button>`;
    v.innerHTML = `<h1>Connectez votre compte X</h1><p class="lead">Choisissez comment XGrowth publie pour vous.</p>
      <div class="prov">${m("agentx", "Session X (AgentX)", "Le plus complet : publication, visuels, veille, signets. Utilise les cookies de votre navigateur.", '<span class="pill accent">Recommandé</span>')}
        ${m("xapi", "API X officielle", "Jeton OAuth 2.0. Lecture et recherche payantes côté X, pas de visuels.")}
        ${m("dryrun", "Simulation", "Pour essayer : rien n'est publié.")}</div>
      <div id="obx" style="margin-top:18px"></div>${this.nav("Continuer", { disabled: !w.profile && w.method !== "dryrun" && w.method !== "xapi" })}`;
    const x = $("#obx");
    if (w.method === "agentx") {
      x.innerHTML = w.profile ? this.profileCard() : `<div class="card"><h3>Récupérer vos cookies X</h3>
        <ol class="how"><li>Ouvrez <a href="https://x.com" target="_blank" rel="noopener">x.com</a>, connecté au compte à utiliser.</li><li>Outils de développement (⌥⌘I ou F12) → <b>Application</b> → <b>Cookies</b> → <code>https://x.com</code>.</li><li>Copiez les valeurs de <code>auth_token</code> et <code>ct0</code> ci-dessous.</li></ol>
        <div class="grid"><div><label class="l">auth_token</label><input id="oat" type="password" autocomplete="off" spellcheck="false"></div><div><label class="l">ct0</label><input id="oct" type="password" autocomplete="off" spellcheck="false"></div></div>
        <div class="notice" style="margin-top:14px">${I.alert}<div>Ces cookies donnent un accès complet à votre compte : ils restent sur ce serveur (fichier lisible par lui seul) et ne sont jamais renvoyés au navigateur. L'automatisation par session web n'est pas autorisée par les conditions de X : un risque de suspension existe.</div></div>
        <div class="row" style="margin-top:14px"><span class="spacer"></span><button class="btn primary" data-act="ob-connect">${I.shield} Connecter</button></div></div>`;
    } else if (w.method === "xapi") {
      x.innerHTML = `<div class="card"><label class="l">Votre handle X</label><input id="oh" value="${esc(w.handle)}" placeholder="@moncompte">
        <label class="l">Variable d'environnement du jeton (sur le serveur)</label><input id="otk" value="${esc(w.tokenEnv || "X_USER_TOKEN")}">
        <p class="hint">Créez une app sur developer.x.com, générez un jeton OAuth 2.0 utilisateur (scopes tweet.read tweet.write users.read bookmark.read) et définissez-le dans l'environnement du service.</p></div>`;
    } else {
      x.innerHTML = `<div class="card"><label class="l">Handle X à simuler</label><input id="oh" value="${esc(w.handle)}" placeholder="@moncompte"><p class="hint">Vous pourrez passer à une vraie connexion plus tard dans les réglages.</p></div>`;
    }
  },
  profileCard() {
    const p = this.w.profile;
    return `<div class="profile"><div class="av">${esc((p.screenName || "?")[0])}</div><div><b>${esc(p.name || p.screenName)}</b><div class="small muted">@${esc(p.screenName)} · ${nf(p.followersCount)} abonnés · ${nf(p.tweetsCount)} posts</div></div><span class="spacer"></span><span class="pill ok">${I.check.replace("<svg", '<svg style="width:12px;height:12px"')} Connecté</span></div>`;
  },
  s3(v) {
    const w = this.w;
    v.innerHTML = `<h1>Comment voulez-vous sonner ?</h1><p class="lead">Partez d'un style puis ajustez-le avec vos mots. L'IA s'en sert pour chaque post.</p>
      <div class="chips">${VOICES.map(([t, d], i) => `<span class="chip ${w.voice === d ? "on" : ""}" data-act="ob-voice" data-i="${i}">${t}</span>`).join("")}</div>
      <label class="l">Votre voix</label><textarea id="ov" placeholder="Ton, style, personnalité, ce que vous ne faites jamais…">${esc(w.voice)}</textarea>
      <label class="l">Vos sujets (séparés par des virgules)</label><input id="ot" value="${esc(w.topics.join(", "))}" placeholder="IA, produit, entrepreneuriat">
      <label class="l">Vos objectifs (facultatif)</label><input id="og" value="${esc(w.goals)}" placeholder="Ex. : trouver mes 50 premiers clients, recruter un associé">
      <div class="grid"><div><label class="l">Langue des posts</label><input id="ol" value="${esc(w.language)}"></div></div>
      ${this.nav()}`;
  },
  s4(v) {
    const w = this.w;
    v.innerHTML = `<h1>Votre rythme et vos garde-fous</h1><p class="lead">Validez un brouillon : il prend automatiquement le prochain créneau libre.</p>
      <div class="prov">${RHYTHMS.map(([t, slots, n], i) => `<button class="pcard ${w.rhythm === i ? "on" : ""}" data-act="ob-rhythm" data-i="${i}"><span class="check">${I.check}</span><h3>${t}</h3><div class="small muted">${n} post${n > 1 ? "s" : ""} par jour</div><div class="chips" style="margin-top:10px">${slots.map(s => `<span class="pill">${s}</span>`).join("")}</div></button>`).join("")}</div>
      <div class="grid"><div><label class="l">Fuseau horaire</label><input id="otz" value="${esc(w.tz)}"></div><div><label class="l">Votre nom (signe les validations)</label><input id="owho" value="${esc(w.validator)}" placeholder="Prénom"></div></div>
      <label class="l">Concurrents à ne jamais nommer (facultatif)</label><input id="ocomp" value="${esc(w.competitors)}" placeholder="Acme, Globex">
      ${this.nav()}`;
  },
  s5(v) {
    const w = this.w;
    v.innerHTML = `<h1>Que voulez-vous surveiller ?</h1><p class="lead">Des comptes qui vous inspirent, des concurrents, une recherche. L'IA vous signale ce qui mérite un post. Facultatif.</p>
      <div class="card"><div class="row"><div class="seg" id="owk"><button data-k="user" class="on">Compte</button><button data-k="search">Recherche</button></div>
        <input id="owv" placeholder="@handle" style="flex:1;width:auto"><button class="btn" data-act="ob-watch">Ajouter</button></div>
        <div class="chips" style="margin-top:12px">${w.watch.map((t, i) => `<span class="chip on">${t.kind === "user" ? "@" : "🔎 "}${esc(t.value)} <a href="#" data-act="ob-unwatch" data-i="${i}" style="color:var(--muted);margin-left:4px">✕</a></span>`).join("") || `<span class="small muted">Aucune cible pour l'instant.</span>`}</div></div>
      ${this.nav(w.addOnly ? "Créer le compte" : "Continuer", { skip: w.watch.length ? "" : "Passer" })}`;
    $$("#owk button").forEach(b => b.onclick = () => { $$("#owk button").forEach(x => x.classList.toggle("on", x === b)); $("#owv").placeholder = b.dataset.k === "user" ? "@handle" : "agents IA lang:fr -is:retweet" });
  },
  s6(v) {
    const w = this.w;
    v.innerHTML = `<h1>Votre premier post <span class="gradtext">en direct</span></h1>
      <p class="lead">${esc(providerName(w.provider))} écrit trois propositions pour @${esc(w.saved?.handle || "")}. Elles arrivent dans « À valider ».</p>
      <div class="composer glow"><textarea id="obb" placeholder="Un sujet ? (vide = idées tirées de vos thèmes)"></textarea><div class="bar"><span class="spacer"></span><button class="btn primary" data-act="ob-gen">${I.spark} Écrire 3 propositions</button></div></div>
      <div id="obd" style="margin-top:16px">${w.drafts ? w.drafts.map(p => tweetCard(p, "view")).join("") : ""}</div>
      ${this.nav("Ouvrir mon studio", { skip: "" })}`;
  },
  readStep() {
    const w = this.w, val = id => ($("#" + id)?.value || "").trim();
    if (w.step === 1) w.model = val("obm");
    if (w.step === 2 && w.method !== "agentx") { w.handle = val("oh").replace(/^@/, ""); if (w.method === "xapi") w.tokenEnv = val("otk") }
    if (w.step === 3) { w.goals = val("og"); w.voice = val("ov"); w.topics = val("ot").split(",").map(x => x.trim()).filter(Boolean); w.language = val("ol") || "français" }
    if (w.step === 4) { w.tz = val("otz"); w.validator = val("owho"); w.competitors = val("ocomp") }
  },
  async next(b) {
    const w = this.w;
    this.readStep();
    if (w.step === 1) await run(b, () => api("/api/ai/select", { provider: w.provider, model: w.model }), null, { noReload: true });
    if (w.step === 2) {
      if (w.method !== "agentx" && !w.handle) return toast("Indiquez votre handle X", true);
      if (w.method === "agentx") w.handle = w.profile.screenName;
    }
    if (w.step === 4) {
      if (!w.validator) return toast("Indiquez votre nom : il signe chaque validation", true);
      store.set("who", w.validator);
      const comps = w.competitors.split(",").map(x => x.trim()).filter(Boolean);
      await api("/api/settings", { competitors: comps });
    }
    if (w.step === 5) {
      const [, slots, n] = RHYTHMS[w.rhythm];
      const name = w.handle.toLowerCase().replace(/[^a-z0-9_-]/g, "") || "principal";
      const acc = { name, handle: w.handle, displayName: w.profile?.name || "", backend: w.method, agentxAccount: w.method === "agentx" ? w.session : "", xapiTokenEnv: w.tokenEnv || "",
        language: w.language, timezone: w.tz, slots, dailyLimit: n + 1, voice: w.voice, goals: w.goals, topics: w.topics, instructions: "", triageRules: "", autoTriage: w.watch.length > 0, autoDraft: false, premium: false };
      w.saved = await run(b, () => api("/api/accounts", acc), null, { noReload: true });
      for (const t of w.watch) await api("/api/watch", { account: name, kind: t.kind, value: t.value, every: 60 }).catch(err => toast(err.message, true));
      store.set("acc", name);
      if (w.addOnly) { S = await api("/api/state"); location.hash = "#/settings"; render(); return toast(`@${w.handle} ajouté`) }
    }
    if (w.step === 6) return this.finish(b);
    w.step++; this.render(); window.scrollTo(0, 0);
  },
  async finish(b) {
    await run(b, () => api("/api/onboarding/complete", {}), null, { noReload: true });
    S = await api("/api/state");
    location.hash = this.w.drafts ? "#/queue" : "#/home";
    render();
    toast("Bienvenue dans votre studio ✨");
  },
  actions: {
    "ob-next": b => wizard.next(b),
    "ob-skip": b => wizard.next(b),
    "ob-back": () => { wizard.readStep(); wizard.w.step--; wizard.render() },
    "ob-cancel": () => { location.hash = "#/settings"; render() },
    "ob-pick": b => { wizard.readStep(); wizard.w.provider = b.dataset.p; wizard.w.model = ""; wizard.render() },
    "ob-redetect": async b => { P = await run(b, () => api("/api/providers?refresh=1"), null, { noReload: true }); wizard.render() },
    "ob-test": async b => {
      wizard.readStep();
      const w = wizard.w;
      w.test = await run(b, () => api("/api/ai/test", { provider: w.provider, model: w.model }), null, { wait: "L'IA écrit…", noReload: true });
      wizard.render();
    },
    "ob-method": b => { wizard.readStep(); wizard.w.method = b.dataset.m; wizard.render() },
    "ob-connect": async b => {
      const w = wizard.w, at = $("#oat").value.trim(), ct = $("#oct").value.trim();
      if (!at || !ct) return toast("Collez les deux cookies", true);
      w.session = w.session || "x" + Math.random().toString(36).slice(2, 8);
      w.profile = await run(b, () => api("/api/x/agentx", { name: w.session, authToken: at, ct0: ct }), p => `Connecté en tant que @${p.screenName}`, { wait: "Connexion à X…", noReload: true });
      w.handle = w.profile.screenName;
      wizard.render();
    },
    "ob-voice": b => { const d = VOICES[+b.dataset.i][1]; wizard.readStep(); wizard.w.voice = d; wizard.render() },
    "ob-rhythm": b => { wizard.readStep(); wizard.w.rhythm = +b.dataset.i; wizard.render() },
    "ob-watch": () => {
      const kind = $("#owk .on").dataset.k, value = $("#owv").value.trim().replace(kind === "user" ? /^@/ : /^$/, "");
      if (!value) return;
      wizard.w.watch.push({ kind, value }); wizard.render();
    },
    "ob-unwatch": (b, e) => { e.preventDefault(); wizard.w.watch.splice(+b.dataset.i, 1); wizard.render() },
    "ob-gen": async b => {
      const w = wizard.w;
      const ps = await run(b, () => api("/api/draft", { account: w.saved.name, topic: $("#obb").value, n: 3 }), null, { wait: "Écriture…", noReload: true });
      S = await api("/api/state");
      w.drafts = ps; wizard.render();
    },
  },
};

// ------------------------------------------------------------------ rafraîchissement
setInterval(async () => {
  if (!S || busy || document.hidden || $(".overlay")) return;
  if (document.activeElement && /TEXTAREA|INPUT|SELECT/.test(document.activeElement.tagName)) return;
  if (Object.keys(edits).length || dirty) return;
  try { S = await api("/api/state"); render() } catch {}
}, 25000);

boot();
