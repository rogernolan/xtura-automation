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

function loadApp({ groupedElements, hash = "#/controls/water" }) {
  const ids = [
    "statusMessage", "connectionStatus", "pageTitle", "overviewNav", "controlsNav", "locationNav", "moreNav",
    "overviewPanel", "controlsPanel", "locationPanel", "morePanel", "flashLights", "flashCount",
    "openGreyValve", "closeGreyValve", "greyScheduleButton", "greyScheduleDuration", "recordingButton",
    "recordingDuration", "trackingEngineOnly", "trackingStartButton", "trackingStopButton", "trackingInterval",
    "modeOn", "modeSchedule", "modeOff", "targetDown", "targetUp", "boostButton", "cancelBoostButton",
    "scheduleForm",
  ];
  const elements = Object.fromEntries(ids.map((id) => [id, new ElementStub(id)]));
  const document = {
    getElementById(id) {
      return elements[id] || null;
    },
    querySelectorAll(selector) {
      if (selector === "[data-screen]") return [];
      if (selector === "[data-target]") return [];
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
  vm.runInNewContext(`${source}\nmodule.exports = { bindActions };`, context, { filename: "app.js" });
  return { bindActions: context.module.exports.bindActions, window };
}

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

  const { bindActions, window } = loadApp({ groupedElements: [controlsSelect, waterPanel] });
  bindActions();

  childControl.dispatchEvent({ type: "change", bubbles: true });

  assert.equal(window.location.hash, "#/controls/water");
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
    hash: "#/more/tools",
  });
  bindActions();

  childControl.dispatchEvent({ type: "change", bubbles: true });

  assert.equal(window.location.hash, "#/more/tools");
});
