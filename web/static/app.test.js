const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");
const navigation = require("./navigation.js");

class ElementStub {
  constructor(id, options = {}) {
    this.id = id;
    this.dataset = options.dataset || {};
    this.value = options.value || "";
    this.checked = options.checked || false;
    this.textContent = options.textContent || "";
    this.innerHTML = options.innerHTML || "";
    this.disabled = false;
    this.hidden = false;
    this.listeners = new Map();
    this.parentNode = null;
    this.classList = { toggle() {} };
  }

  querySelector() {
    return null;
  }

  addEventListener(type, listener) {
    if (!this.listeners.has(type)) {
      this.listeners.set(type, []);
    }
    this.listeners.get(type).push(listener);
  }

  dispatchEvent(event) {
    event.target = event.target || this;
    event.currentTarget = this;
    for (const listener of this.listeners.get(event.type) || []) {
      listener.call(this, event);
    }
    if (event.bubbles && this.parentNode) {
      this.parentNode.dispatchEvent(event);
    }
  }

  setAttribute() {}
}

function loadApp({ groupedElements, selectElements = groupedElements, hash = "#/controls/water" }) {
  const ids = [
    "statusMessage", "connectionStatus", "pageTitle", "overviewNav", "controlsNav", "locationNav", "moreNav",
    "overviewPanel", "controlsPanel", "locationPanel", "morePanel", "flashLights", "flashCount",
    "openGreyValve", "closeGreyValve", "greyScheduleButton", "greyScheduleDuration", "recordingButton",
    "recordingDuration", "trackingEngineOnly", "trackingStartButton", "trackingStopButton", "trackingInterval",
    "modeOn", "modeSchedule", "modeOff", "targetDown", "targetUp", "boostButton", "cancelBoostButton",
    "scheduleForm",
    "overviewSettingsForm", "comfortCold", "comfortComfort", "comfortWarm", "comfortHot", "batteryCapacity", "gasCapacity",
    "temperatureBody",
  ];
  const elements = Object.fromEntries(ids.map((id) => [id, new ElementStub(id)]));
  const document = {
    getElementById(id) {
      return elements[id] || null;
    },
    querySelectorAll(selector) {
      if (selector === "[data-screen]") return [];
      if (selector === "[data-target]") return [];
      if (selector === "select[data-section-group]") return selectElements;
      if (selector === "[data-section-group]") return groupedElements;
      if (selector === ".section-panel") return [];
      return [];
    },
    addEventListener() {},
    title: "Xtura",
  };
  const window = {
    location: { hash },
    addEventListener() {},
    setInterval() {
      return 1;
    },
    clearInterval() {},
  };
  const context = {
    console,
    document,
    window,
    fetch: async () => ({ ok: true, text: async () => "" }),
    EventSource: function EventSource() {},
    XturaNavigation: navigation,
    module: { exports: {} },
    exports: {},
    require,
    setTimeout,
    clearTimeout,
  };
  const source = fs.readFileSync(path.join(__dirname, "app.js"), "utf8");
  vm.runInNewContext(`${source}\nmodule.exports = { bindActions, renderOverviewSettings, renderOverview, renderTemperature, trendLabel, trendSymbol, overviewTemperatureTone, overviewCurrentState, overviewSupplyState, state };`, context, { filename: "app.js" });
  return { bindActions: context.module.exports.bindActions, renderOverviewSettings: context.module.exports.renderOverviewSettings, renderOverview: context.module.exports.renderOverview, renderTemperature: context.module.exports.renderTemperature, trendLabel: context.module.exports.trendLabel, trendSymbol: context.module.exports.trendSymbol, overviewTemperatureTone: context.module.exports.overviewTemperatureTone, overviewCurrentState: context.module.exports.overviewCurrentState, overviewSupplyState: context.module.exports.overviewSupplyState, state: context.module.exports.state, window, elements };
}

test("rerendering overview does not overwrite dirty settings fields", () => {
  const { renderOverviewSettings, state, elements } = loadApp({ groupedElements: [] });
  state.overviewSettings = { comfort_thresholds: [10, 18, 24, 30], usable_battery_capacity_ah: 100, gas_tank_capacity_litres: 0 };
  state.overviewSettingsDirty = true;
  elements.comfortCold.value = "12";
  renderOverviewSettings();
  assert.equal(elements.comfortCold.value, "12");
});

test("temperature tone uses configured comfort bands", () => {
  const { overviewTemperatureTone } = loadApp({ groupedElements: [] });
  assert.equal(overviewTemperatureTone(8, [10, 18, 24, 30]), "cold");
  assert.equal(overviewTemperatureTone(15, [10, 18, 24, 30]), "comfortable");
  assert.equal(overviewTemperatureTone(21, [10, 18, 24, 30]), "warm");
  assert.equal(overviewTemperatureTone(28, [10, 18, 24, 30]), "hot");
  assert.equal(overviewTemperatureTone(28, [10, 18, 30, 40]), "warm");
  assert.equal(overviewTemperatureTone(undefined, [10, 18, 24, 30]), "unavailable");
});

test("charge current status reflects telemetry freshness", () => {
  const { overviewCurrentState } = loadApp({ groupedElements: [] });
  assert.equal(overviewCurrentState(null), "loading");
  assert.equal(overviewCurrentState({ status: "available", battery: { current_a: 2 } }), "live");
  assert.equal(overviewCurrentState({ status: "available", battery: {} }), "unavailable");
  assert.equal(overviewCurrentState({ status: "stale", battery: {} }), "stale");
});

test("healthy supplies leave their status label blank", () => {
  const { overviewSupplyState } = loadApp({ groupedElements: [] });
  assert.equal(overviewSupplyState(42, "available"), "");
  assert.equal(overviewSupplyState(undefined, "available"), "Unavailable");
  assert.equal(overviewSupplyState(undefined, "stale"), "Stale");
});

test("changing a control inside the water panel does not reset the section hash", () => {
  const waterPanel = new ElementStub("controlsWaterPanel", {
    dataset: { sectionGroup: "controls", section: "water" },
  });
  const controlsSelect = new ElementStub("controlsSection", {
    dataset: { sectionGroup: "controls" },
    value: "water",
  });
  const childControl = new ElementStub("greyScheduleDuration", { value: "45" });
  childControl.parentNode = waterPanel;

  const { bindActions, window } = loadApp({
    groupedElements: [controlsSelect, waterPanel],
    selectElements: [controlsSelect],
  });
  bindActions();

  childControl.dispatchEvent({ type: "change", bubbles: true });

  assert.equal(window.location.hash, "#/controls/water");
});

test("changing the controls dropdown navigates to the selected section", () => {
  const controlsSelect = new ElementStub("controlsSection", {
    dataset: { sectionGroup: "controls" },
    value: "heating",
  });

  const { bindActions, window } = loadApp({
    groupedElements: [controlsSelect],
    hash: "#/controls/heating",
  });
  bindActions();

  controlsSelect.value = "lighting";
  controlsSelect.dispatchEvent({ type: "change", bubbles: true });

  assert.equal(window.location.hash, "#/controls/lighting");
});

test("changing a control inside the tools panel does not reset the section hash", () => {
  const toolsPanel = new ElementStub("moreToolsPanel", {
    dataset: { sectionGroup: "more", section: "tools" },
  });
  const moreSelect = new ElementStub("moreSection", {
    dataset: { sectionGroup: "more" },
    value: "tools",
  });
  const childControl = new ElementStub("recordingDuration", { value: "10" });
  childControl.parentNode = toolsPanel;

  const { bindActions, window } = loadApp({
    groupedElements: [moreSelect, toolsPanel],
    selectElements: [moreSelect],
    hash: "#/more/tools",
  });
  bindActions();

  childControl.dispatchEvent({ type: "change", bubbles: true });

  assert.equal(window.location.hash, "#/more/tools");
});

test("changing the more dropdown navigates to the selected section", () => {
  const moreSelect = new ElementStub("moreSection", {
    dataset: { sectionGroup: "more" },
    value: "system",
  });

  const { bindActions, window } = loadApp({
    groupedElements: [moreSelect],
    hash: "#/more/system",
  });
  bindActions();

  moreSelect.value = "tools";
  moreSelect.dispatchEvent({ type: "change", bubbles: true });

  assert.equal(window.location.hash, "#/more/tools");
});

test("renders the primary temperature card and trend", () => {
  const { renderTemperature, elements, state } = loadApp({ groupedElements: [] });
  state.overviewSettings = { comfort_thresholds: [10, 18, 24, 30] };
  state.overview = {
    temperature: {
      primary_id: "abc",
      primary: { id: "abc", temp: 21.4, humidity: 55, trend: "rising", history: [] },
      sensors: [{ id: "abc", name: "Main", temp: 21.4, trend: "rising" }],
    },
  };
  renderTemperature(state.overview);
  const html = elements.temperatureBody.innerHTML;
  assert.match(html, /21\.4C/);
  assert.match(html, /Humidity 55%/);
  assert.match(html, /data-tone="warm"/);
  assert.match(html, /↑/);
  assert.match(html, /overview-primary-row/);
  assert.match(html, /overview-temperature-chart/);
});

test("renders other sensors inside the big card and '-' when missing", () => {
  const { renderTemperature, elements, state } = loadApp({ groupedElements: [] });
  state.overview = {
    temperature: {
      primary_id: "abc",
      primary: { id: "abc", temp: 20, trend: "steady", history: [] },
      sensors: [
        { id: "abc", name: "Main", temp: 20, trend: "steady" },
        { id: "out", name: "Outside", temp: 12.3, trend: "falling" },
        { id: "alde", name: "Alde", trend: "unavailable" },
      ],
    },
  };
  renderTemperature(state.overview);
  const html = elements.temperatureBody.innerHTML;
  assert.match(html, /overview-sensor-row/);
  assert.match(html, /overview-sub-sensor/);
  assert.match(html, /Outside/);
  assert.match(html, /12\.3C/);
  assert.match(html, /↓/);
  assert.match(html, /Alde/);
  assert.match(html, />-</);
  assert.match(html, /\?/);
});

test("does not render the inner sensor row for a single sensor", () => {
  const { renderTemperature, elements, state } = loadApp({ groupedElements: [] });
  state.overview = {
    temperature: {
      primary_id: "alde",
      primary: { id: "alde", temp: 21, trend: "unavailable", history: [] },
      sensors: [{ id: "alde", name: "Alde", temp: 21, trend: "unavailable" }],
    },
  };
  renderTemperature(state.overview);
  const html = elements.temperatureBody.innerHTML;
  assert.match(html, /21\.0C/);
  assert.doesNotMatch(html, /overview-sensor-row/);
});

test("renders waiting message when temperature data is absent", () => {
  const { renderTemperature, elements, state } = loadApp({ groupedElements: [] });
  renderTemperature({});
  assert.match(elements.temperatureBody.innerHTML, /Waiting for temperature/);
});

test("trend label maps every trend value", () => {
  const { trendLabel } = loadApp({ groupedElements: [] });
  assert.equal(trendLabel("rising"), "Rising");
  assert.equal(trendLabel("falling"), "Falling");
  assert.equal(trendLabel("steady"), "Steady");
  assert.equal(trendLabel("unavailable"), "Trend unavailable");
  assert.equal(trendLabel(undefined), "Trend unavailable");
});

test("trend symbol maps the four states", () => {
  const { trendSymbol } = loadApp({ groupedElements: [] });
  assert.equal(trendSymbol("rising"), "↑");
  assert.equal(trendSymbol("falling"), "↓");
  assert.equal(trendSymbol("steady"), "–");
  assert.equal(trendSymbol("unavailable"), "?");
  assert.equal(trendSymbol(undefined), "?");
});
