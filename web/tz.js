"use strict";
/* XGrowth Studio — dates dans le fuseau d'un compte (aucune dépendance).
   Le serveur programme dans le fuseau du compte : le navigateur ne doit jamais
   deviner les jours ni les heures avec son propre fuseau. Les jours sont des
   chaînes « AAAA-MM-JJ » ; seul Intl convertit un instant en jour ou en heure. */
(function (root) {
  const browserZone = () => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  const validZone = z => { try { new Intl.DateTimeFormat("fr-FR", { timeZone: z }); return !!z } catch { return false } };
  // fuseau d'un compte ; comme le serveur, celui de la machine à défaut
  const zoneOf = a => a && validZone(a.timezone) ? a.timezone : browserZone();

  const fmtCache = {};
  function parts(t, zone, opts) {
    const k = zone + JSON.stringify(opts);
    const f = fmtCache[k] || (fmtCache[k] = new Intl.DateTimeFormat("en-CA", { timeZone: zone, hourCycle: "h23", ...opts }));
    const o = {};
    for (const p of f.formatToParts(new Date(t))) o[p.type] = p.value;
    return o;
  }
  // jour civil de l'instant t dans le fuseau
  const dayKeyIn = (t, zone) => { const p = parts(t, zone, { year: "numeric", month: "2-digit", day: "2-digit" }); return `${p.year}-${p.month}-${p.day}` };
  // heure « HH:MM » de l'instant t dans le fuseau
  const timeIn = (t, zone) => { const p = parts(t, zone, { hour: "2-digit", minute: "2-digit" }); return `${p.hour}:${p.minute}` };

  const keyToUTC = k => { const [y, m, d] = k.split("-").map(Number); return Date.UTC(y, m - 1, d) };
  const addDays = (k, n) => new Date(keyToUTC(k) + n * 86400000).toISOString().slice(0, 10);
  // lundi de la semaine du jour k (lundi = 0)
  const mondayOf = k => addDays(k, -((new Date(keyToUTC(k)).getUTCDay() + 6) % 7));
  // les sept jours (lundi → dimanche) de la semaine décalée de offset semaines
  const weekDays = (todayKey, offset = 0) => { const m = addDays(mondayOf(todayKey), 7 * offset); return [...Array(7)].map((_, i) => addDays(m, i)) };
  const dayLabel = k => new Intl.DateTimeFormat("fr-FR", { timeZone: "UTC", weekday: "short" }).format(keyToUTC(k));
  const dayNum = k => +k.slice(8);
  // « Europe/Paris (UTC+1) » à l'instant t : l'écart change avec l'heure d'été
  function zoneLabel(zone, t = Date.now()) {
    const n = new Intl.DateTimeFormat("fr-FR", { timeZone: zone, timeZoneName: "shortOffset" }).formatToParts(new Date(t)).find(p => p.type === "timeZoneName")?.value || "";
    return `${zone} (${n.replace("GMT", "UTC")})`;
  }

  const api = { browserZone, zoneOf, dayKeyIn, timeIn, addDays, mondayOf, weekDays, dayLabel, dayNum, zoneLabel };
  if (typeof module !== "undefined" && module.exports) module.exports = api; else root.TZ = api;
})(typeof window !== "undefined" ? window : globalThis);
