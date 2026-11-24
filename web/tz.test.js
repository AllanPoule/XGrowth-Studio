"use strict";
// node web/tz.test.js : conversion de fuseau, changement d'heure, semaine du lundi
const assert = require("node:assert/strict");
const TZ = require("./tz.js");
const test = (n, f) => { f(); console.log("ok  " + n) };

test("un compte à Paris voit le jour de Paris, pas celui du navigateur", () => {
  const t = Date.UTC(2026, 10, 18, 23, 30); // 23:30 UTC = 00:30 le 19 à Paris
  assert.equal(TZ.dayKeyIn(t, "Europe/Paris"), "2026-11-19");
  assert.equal(TZ.dayKeyIn(t, "UTC"), "2026-11-18");
  assert.equal(TZ.dayKeyIn(t, "America/New_York"), "2026-11-18");
  assert.equal(TZ.timeIn(t, "Europe/Paris"), "00:30");
  assert.equal(TZ.timeIn(t, "America/New_York"), "18:30");
});
test("passage à l'heure d'été (29 mars 2026) : 09:00 reste 09:00", () => {
  assert.equal(TZ.timeIn(Date.UTC(2026, 2, 28, 8, 0), "Europe/Paris"), "09:00"); // UTC+1
  assert.equal(TZ.timeIn(Date.UTC(2026, 2, 29, 7, 0), "Europe/Paris"), "09:00"); // UTC+2
  assert.equal(TZ.dayKeyIn(Date.UTC(2026, 2, 28, 23, 30), "Europe/Paris"), "2026-03-29");
});
test("retour à l'heure d'hiver (25 octobre 2026)", () => {
  assert.equal(TZ.timeIn(Date.UTC(2026, 9, 24, 7, 0), "Europe/Paris"), "09:00"); // UTC+2
  assert.equal(TZ.timeIn(Date.UTC(2026, 9, 25, 8, 0), "Europe/Paris"), "09:00"); // UTC+1
  assert.equal(TZ.dayKeyIn(Date.UTC(2026, 9, 24, 21, 30), "Europe/Paris"), "2026-10-24");
  assert.equal(TZ.dayKeyIn(Date.UTC(2026, 9, 24, 22, 30), "Europe/Paris"), "2026-10-25");
});
test("la semaine va du lundi au dimanche, y compris autour d'un changement d'heure", () => {
  assert.deepEqual(TZ.weekDays("2026-11-18"), ["2026-11-16", "2026-11-17", "2026-11-18", "2026-11-19", "2026-11-20", "2026-11-21", "2026-11-22"]);
  assert.equal(TZ.weekDays("2026-11-22")[0], "2026-11-16"); // dimanche : même semaine
  assert.equal(TZ.weekDays("2026-11-23")[0], "2026-11-23"); // lundi : nouvelle semaine
  assert.deepEqual(TZ.weekDays("2026-03-25").slice(4), ["2026-03-27", "2026-03-28", "2026-03-29"]);
  assert.equal(TZ.weekDays("2026-03-25", 1)[0], "2026-03-30");
  assert.equal(TZ.weekDays("2026-10-22", 1)[6], "2026-11-01");
  assert.equal(TZ.weekDays("2026-10-22")[6], "2026-10-25");
});
test("fuseau inconnu : repli sur celui de la machine, jamais d'exception", () => {
  assert.equal(TZ.zoneOf({ timezone: "Nulle/Part" }), TZ.browserZone());
  assert.equal(TZ.zoneOf({ timezone: "Europe/Paris" }), "Europe/Paris");
  assert.equal(TZ.zoneOf(undefined), TZ.browserZone());
});
test("le fuseau affiché porte l'écart à l'instant donné", () => {
  assert.equal(TZ.zoneLabel("Europe/Paris", Date.UTC(2026, 0, 15)), "Europe/Paris (UTC+1)");
  assert.equal(TZ.zoneLabel("Europe/Paris", Date.UTC(2026, 6, 15)), "Europe/Paris (UTC+2)");
});
test("libellés de jour", () => {
  assert.equal(TZ.dayLabel("2026-11-16"), "lun.");
  assert.equal(TZ.dayNum("2026-11-16"), 16);
});
