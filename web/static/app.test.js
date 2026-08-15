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
    this.disabled = false;
    this.hidden = false;
    this.listeners = new Map();
    this.parentNode = null;
    this.classList = { toggle() {} };
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
  vm.runInNewContext(`${source}\nmodule.exports = { bindActions, renderOverviewSettings, renderOverview, overviewTemperatureTone, overviewCurrentState, state };`, context, { filename: "app.js" });
  return { bindActions: context.module.exports.bindActions, renderOverviewSettings: context.module.exports.renderOverviewSettings, renderOverview: context.module.exports.renderOverview, overviewTemperatureTone: context.module.exports.overviewTemperatureTone, overviewCurrentState: context.module.exports.overviewCurrentState, state: context.module.exports.state, window, elements };
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
