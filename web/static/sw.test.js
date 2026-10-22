const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

function loadWorker() {
  const listeners = new Map();
  const shown = [];
  let skipped = false;
  let claimed = false;
  const self = {
    addEventListener: (name, fn) => listeners.set(name, fn),
    skipWaiting: async () => { skipped = true; },
    registration: { showNotification: async (title, options) => shown.push({ title, ...options }) },
  };
  const clients = { claim: async () => { claimed = true; } };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, "sw.js"), "utf8"), { self, clients });
  return {
    listeners, shown,
    skipped: () => skipped,
    claimed: () => claimed,
    async push(data) {
      let pending;
      listeners.get("push")({ data: { json: () => data }, waitUntil: (promise) => { pending = promise; } });
      await pending;
      return shown[shown.length - 1];
    },
  };
}

test("Alde power push displays its explanation without temperature fields", async () => {
  const worker = loadWorker();
  const result = await worker.push({ kind: "alde_power", alert_id: "alde-power", title: "Alde electric heating exceeds shore allowance", body: "Alde is set to 2 kW; the configured shore limit is 3.1 A." });
  assert.equal(result.title, "Alde electric heating exceeds shore allowance");
  assert.equal(result.body, "Alde is set to 2 kW; the configured shore limit is 3.1 A.");
  assert.equal(result.tag, "xtura-alde-power");
  assert.equal(result.data.url, "/#/more/settings");
});

test("legacy temperature and offline pushes retain their content", async () => {
  const worker = loadWorker();
  const high = await worker.push({ alert_id: "hot", alert_name: "Cabin", sensor_name: "Alde", side: "high", temperature_c: 26, limit_celsius: 25 });
  assert.equal(high.title, "Cabin: High");
  assert.equal(high.body, "Alde: 26.0C (limit 25.0C)");
  assert.equal(high.tag, "xtura-temperature-hot");
  const offline = await worker.push({ alert_id: "cold", alert_name: "Cabin", sensor_name: "Alde", side: "offline" });
  assert.equal(offline.title, "Cabin: Offline");
  assert.match(offline.body, /offline for more than 30 minutes/);
});

test("updated worker activates for existing subscribed clients", async () => {
  const worker = loadWorker();
  for (const name of ["install", "activate"]) {
    let pending;
    assert.equal(typeof worker.listeners.get(name), "function", name);
    worker.listeners.get(name)({ waitUntil: (promise) => { pending = promise; } });
    await pending;
  }
  assert.equal(worker.skipped(), true);
  assert.equal(worker.claimed(), true);
});

test("worker confirms the notification format before subscription upgrade", () => {
  const worker = loadWorker();
  let answer;
  assert.equal(typeof worker.listeners.get("message"), "function");
  worker.listeners.get("message")({ data: { type: "xtura-notification-version" }, ports: [{ postMessage: (value) => { answer = value; } }] });
  assert.equal(answer.version, 2);
});
