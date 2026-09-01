const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");
const navigation = require("./navigation.js");

test("home screen metadata fixes the icon and installed app name", () => {
  const html = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");

  assert.match(html, /<meta name="application-name" content="Xtura">/);
  assert.match(html, /<meta name="apple-mobile-web-app-title" content="Xtura">/);
  assert.match(html, /<link rel="apple-touch-icon" href="\/static\/xtura-home-screen\.png">/);
  assert.match(html, /<link rel="icon" type="image\/png" href="\/static\/xtura-home-screen\.png">/);
  assert.doesNotMatch(html, /<link rel="icon" href="data:,">/);
});

class ElementStub {
  constructor(id, options = {}) {
    this.id = id;
    this.dataset = options.dataset || {};
    this.value = options.value || "";
    this.checked = options.checked || false;
    this.textContent = options.textContent || "";
    this.innerHTML = options.innerHTML || "";
    this.disabled = false;
    this.hidden = options.hidden || false;
    this.listeners = new Map();
    this.parentNode = null;
    this.ownerDocument = null;
    this.attributes = new Map(Object.entries(options.attributes || {}));
    const classes = new Set();
    this.classList = {
      add: (...names) => names.forEach((name) => classes.add(name)),
      remove: (...names) => names.forEach((name) => classes.delete(name)),
      contains: (name) => classes.has(name),
      toggle: (name, force) => {
        const next = force === undefined ? !classes.has(name) : force;
        if (next) classes.add(name);
        else classes.delete(name);
        return next;
      },
    };
    this.style = {};
    this.children = options.children || [];
    this.focusables = options.focusables || [];
    this.inert = false;
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

  removeEventListener(type, listener) {
    const listeners = this.listeners.get(type) || [];
    this.listeners.set(type, listeners.filter((registered) => registered !== listener));
  }

  dispatchEvent(event) {
    event.preventDefault = event.preventDefault || (() => {
      event.defaultPrevented = true;
    });
    event.target = event.target || this;
    event.currentTarget = this;
    for (const listener of this.listeners.get(event.type) || []) {
      listener.call(this, event);
    }
    if (event.bubbles && this.parentNode) {
      this.parentNode.dispatchEvent(event);
    }
  }

  focus() {
    if (this.ownerDocument) {
      this.ownerDocument.activeElement = this;
    }
  }

  setAttribute(name, value) {
    this.attributes.set(name, String(value));
  }

  getAttribute(name) {
    return this.attributes.has(name) ? this.attributes.get(name) : null;
  }

  removeAttribute(name) {
    this.attributes.delete(name);
  }

  appendChild(child) {
    this.children.push(child);
    return child;
  }

  append(...children) {
    children.forEach((child) => this.appendChild(child));
  }

  hasAttribute(name) {
    return this.attributes.has(name);
  }

  querySelector(selector) {
    if (selector === "[data-page]") {
      return this.firstPageLink || null;
    }
    return null;
  }

  querySelectorAll(selector) {
    if (selector === "[data-page]") {
      return this.pageLinks || [];
    }
    if (this.focusables.length > 0) {
      return this.focusables;
    }
    return [];
  }

  contains(node) {
    if (node === this) {
      return true;
    }
    return this.children.some((child) => child === node || child.contains(node));
  }
}

function dispatchWithListeners(target, listeners, event) {
  event.preventDefault = event.preventDefault || (() => {
    event.defaultPrevented = true;
  });
  event.target = event.target || target;
  event.currentTarget = target;
  for (const listener of listeners.get(event.type) || []) {
    listener.call(target, event);
  }
}

function loadApp({ hash = "#/overview", reducedMotion = false, fetchImpl = async () => ({ ok: true, text: async () => "" }), localStorage = undefined } = {}) {
  const ids = [
    "statusMessage", "connectionStatus", "pageTitle",
    "appContent",
    "menuButton", "closeMenuButton", "navigationBackdrop", "navigationDrawer",
    "overviewPanel", "heatingPanel", "waterPanel", "lightingPanel", "locationPanel", "systemPanel", "toolsPanel", "settingsPanel",
    "flashLights", "flashCount", "lightsState", "lightsDetail",
    "openGreyValve", "closeGreyValve", "greyScheduleButton", "greyScheduleDuration", "recordingButton", "waterState", "waterDetail", "greyScheduleMessage",
    "waterHistoryChart", "freshWaterUsage", "greyWaterUsage",
    "recordingPanel", "recordingState", "recordingDetail", "recordingDuration", "trackingPanel", "trackingState", "trackingDetail", "trackingManualControls", "trackingEngineOnly", "trackingStartButton", "trackingStopButton", "trackingInterval", "todayTrackMapView", "todayTrackMapStatus", "todayTrackMap", "trackList", "trackMapView", "trackMapBack", "trackMapTitle", "trackMapStatus", "trackMap",
    "modeOn", "modeSchedule", "modeOff", "modeState", "targetState", "modeDetail", "targetValue", "targetDown", "targetUp", "boostButton", "boostRunning", "cancelBoostButton",
    "scheduleForm", "scheduleState", "scheduleDetail", "scheduleSlots", "saveSchedule", "greyScheduleTime", "recordingWaitFor",
    "overviewSettingsForm", "deploymentInfo", "piStatusPanel", "piPowerState", "piStats", "piDetail", "comfortCold", "comfortComfort", "comfortWarm", "comfortHot", "batteryCapacity", "gasCapacity",
    "temperatureBody",
  ];
  const elements = Object.fromEntries(ids.map((id) => [id, new ElementStub(id)]));
  const drawerLayoutReads = [];
  Object.defineProperty(elements.navigationDrawer, "offsetWidth", {
    get() {
      drawerLayoutReads.push(elements.navigationDrawer.classList.contains("is-open"));
      return 320;
    },
  });
  elements.menuButton.setAttribute("aria-expanded", "false");
  elements.navigationBackdrop.hidden = true;
  elements.navigationDrawer.hidden = true;

  const pageLinks = navigation.pages.map((page) => {
    const id = `${page}Link`;
    const link = new ElementStub(id, {
      dataset: { page },
      attributes: { href: `#/${page}` },
    });
    elements[id] = link;
    return link;
  });
  elements.navigationDrawer.pageLinks = pageLinks;
  elements.navigationDrawer.focusables = [elements.closeMenuButton, ...pageLinks];
  elements.navigationDrawer.children = [elements.closeMenuButton, ...pageLinks];
  elements.navigationDrawer.firstPageLink = pageLinks[0];
  elements.appContent.focusables = [elements.menuButton];
  elements.appContent.children = [elements.menuButton];

  elements.aldeCard = new ElementStub("aldeCard", {
    dataset: { overviewRoute: "#/heating" },
  });

  const documentListeners = new Map();
  const document = {
    activeElement: null,
    createElement(tagName) {
      return new ElementStub(tagName);
    },
    getElementById(id) {
      return elements[id] || null;
    },
    querySelector(selector) {
      const match = selector.match(/^\[data-page="([^"]+)"\]$/);
      if (match) {
        return elements[`${match[1]}Link`] || null;
      }
      if (selector === "[data-page]") {
        return pageLinks[0] || null;
      }
      return null;
    },
    querySelectorAll(selector) {
      if (selector === "[data-page]") return pageLinks;
      if (selector === "[data-overview-route]") return [elements.aldeCard];
      if (selector === "[data-target]") return [];
      return [];
    },
    addEventListener(type, listener) {
      if (!documentListeners.has(type)) {
        documentListeners.set(type, []);
      }
      documentListeners.get(type).push(listener);
    },
    dispatchEvent(event) {
      dispatchWithListeners(this, documentListeners, event);
    },
    title: "Xtura",
  };
  for (const element of Object.values(elements)) {
    element.ownerDocument = document;
  }

  const animationFrames = [];
  const windowListeners = new Map();
  const window = {
    location: { hash },
    matchMedia: () => ({ matches: reducedMotion }),
    addEventListener(type, listener) {
      if (!windowListeners.has(type)) {
        windowListeners.set(type, []);
      }
      windowListeners.get(type).push(listener);
    },
    dispatchEvent(event) {
      dispatchWithListeners(this, windowListeners, event);
    },
    setInterval() {
      return 1;
    },
    clearInterval() {},
    requestAnimationFrame(callback) {
      animationFrames.push(callback);
      return animationFrames.length;
    },
    cancelAnimationFrame(frame) {
      animationFrames[frame - 1] = null;
    },
  };
  const context = {
    console,
    document,
    window,
    fetch: fetchImpl,
    EventSource: function EventSource() {},
    XturaNavigation: navigation,
    module: { exports: {} },
    exports: {},
    require,
    setTimeout,
    clearTimeout,
    localStorage,
  };
  const source = fs.readFileSync(path.join(__dirname, "app.js"), "utf8");
  vm.runInNewContext(`${source}\nmodule.exports = { applyRoute, bindActions, loadInitialState, renderOverviewSettings, renderOverview, renderTemperature, renderWater, renderWaterHistory, temperatureChartDomain, temperatureChartHourBoundaries, trendLabel, getTrendState, renderTrendControl, overviewTemperatureTone, overviewCurrentState, overviewSupplyState, formatBatteryCurrent, formatLastSeen, sensorLastSeenText, state };`, context, { filename: "app.js" });
  return {
    applyRoute: context.module.exports.applyRoute,
    bindActions: context.module.exports.bindActions,
    loadInitialState: context.module.exports.loadInitialState,
    renderOverviewSettings: context.module.exports.renderOverviewSettings,
    renderOverview: context.module.exports.renderOverview,
    renderTemperature: context.module.exports.renderTemperature,
    renderWater: context.module.exports.renderWater,
    renderWaterHistory: context.module.exports.renderWaterHistory,
    temperatureChartDomain: context.module.exports.temperatureChartDomain,
    temperatureChartHourBoundaries: context.module.exports.temperatureChartHourBoundaries,
    trendLabel: context.module.exports.trendLabel,
    getTrendState: context.module.exports.getTrendState,
    renderTrendControl: context.module.exports.renderTrendControl,
    overviewTemperatureTone: context.module.exports.overviewTemperatureTone,
    overviewCurrentState: context.module.exports.overviewCurrentState,
    overviewSupplyState: context.module.exports.overviewSupplyState,
    formatBatteryCurrent: context.module.exports.formatBatteryCurrent,
    formatLastSeen: context.module.exports.formatLastSeen,
    sensorLastSeenText: context.module.exports.sensorLastSeenText,
    state: context.module.exports.state,
    document,
    window,
    elements,
    drawerLayoutReads,
    runAnimationFrames() {
      while (animationFrames.length > 0) {
        const callback = animationFrames.shift();
        if (callback) callback();
      }
    },
  };
}

test("rerendering overview does not overwrite dirty settings fields", () => {
  const { renderOverviewSettings, state, elements } = loadApp();
  state.overviewSettings = { comfort_thresholds: [10, 18, 24, 30], usable_battery_capacity_ah: 100, gas_tank_capacity_litres: 0 };
  state.overviewSettingsDirty = true;
  elements.comfortCold.value = "12";
  renderOverviewSettings();
  assert.equal(elements.comfortCold.value, "12");
});

test("temperature tone uses configured comfort bands", () => {
  const { overviewTemperatureTone } = loadApp();
  assert.equal(overviewTemperatureTone(8, [10, 18, 24, 30]), "cold");
  assert.equal(overviewTemperatureTone(15, [10, 18, 24, 30]), "comfortable");
  assert.equal(overviewTemperatureTone(21, [10, 18, 24, 30]), "warm");
  assert.equal(overviewTemperatureTone(28, [10, 18, 24, 30]), "hot");
  assert.equal(overviewTemperatureTone(28, [10, 18, 30, 40]), "warm");
  assert.equal(overviewTemperatureTone(undefined, [10, 18, 24, 30]), "unavailable");
});

test("charge current status reflects telemetry freshness", () => {
  const { overviewCurrentState } = loadApp();
  assert.equal(overviewCurrentState(null), "loading");
  assert.equal(overviewCurrentState({ status: "available", battery: { current_a: 2 } }), "live");
  assert.equal(overviewCurrentState({ status: "available", battery: {} }), "N/A");
  assert.equal(overviewCurrentState({ status: "stale", battery: {} }), "stale");
});

test("formats battery current to fit the 99.9A display slot", () => {
  const { formatBatteryCurrent } = loadApp();
  assert.equal(formatBatteryCurrent(99.9), "+99.9A");
  assert.equal(formatBatteryCurrent(250), "+250A");
  assert.equal(formatBatteryCurrent(-125), "-125A");
});

test("healthy supplies leave their status label blank", () => {
  const { overviewSupplyState } = loadApp();
  assert.equal(overviewSupplyState(42, "available"), "");
  assert.equal(overviewSupplyState(undefined, "available"), "N/A");
  assert.equal(overviewSupplyState(undefined, "stale"), "Stale");
});

test("formatLastSeen returns empty string for invalid input", () => {
  const { formatLastSeen } = loadApp();
  assert.equal(formatLastSeen(null), "");
  assert.equal(formatLastSeen(undefined), "");
  assert.equal(formatLastSeen(""), "");
  assert.equal(formatLastSeen("invalid-date"), "");
});

test("formatLastSeen returns formatted string for valid date", () => {
  const { formatLastSeen } = loadApp();
  const result = formatLastSeen("2026-08-15T12:30:00Z");
  assert.match(result, /^Last seen/);
});

test("sensorLastSeenText returns hidden for recent timestamps", () => {
  const { sensorLastSeenText } = loadApp();
  const now = new Date().toISOString();
  const result = sensorLastSeenText(now);
  assert.equal(result.hidden, true);
  assert.equal(result.text, "");
});

test("sensorLastSeenText returns visible for old timestamps", () => {
  const { sensorLastSeenText } = loadApp();
  const oldTime = new Date(Date.now() - 10 * 60 * 1000).toISOString(); // 10 minutes ago
  const result = sensorLastSeenText(oldTime);
  assert.equal(result.hidden, false);
  assert.match(result.text, /^Last seen/);
});

test("sensorLastSeenText returns hidden for null/undefined", () => {
  const { sensorLastSeenText } = loadApp();
  assert.equal(sensorLastSeenText(null).hidden, true);
  assert.equal(sensorLastSeenText(undefined).hidden, true);
});

test("applyRoute shows the selected page and marks its drawer link", () => {
  const { applyRoute, document, elements } = loadApp();

  applyRoute({ page: "water" });

  assert.equal(elements.pageTitle.textContent, "Water");
  assert.equal(document.title, "Water");
  assert.equal(elements.overviewPanel.hidden, true);
  assert.equal(elements.waterPanel.hidden, false);
  assert.equal(elements.waterLink.getAttribute("aria-current"), "page");
  assert.equal(elements.overviewLink.getAttribute("aria-current"), null);
});

test("applyRoute rewrites legacy hashes to the canonical overview route", () => {
  const { applyRoute, window } = loadApp({ hash: "#/controls/heating" });

  applyRoute(navigation.parse(window.location.hash));

  assert.equal(window.location.hash, "#/overview");
});

test("navigation controls open and close the drawer with focus management", () => {
  const { bindActions, document, elements, runAnimationFrames } = loadApp();
  bindActions();

  elements.menuButton.dispatchEvent({ type: "click" });
  runAnimationFrames();
  assert.equal(elements.navigationDrawer.hidden, false);
  assert.equal(elements.navigationBackdrop.hidden, false);
  assert.equal(elements.navigationDrawer.classList.contains("is-open"), true);
  assert.equal(elements.navigationBackdrop.classList.contains("is-open"), true);
  assert.equal(elements.menuButton.getAttribute("aria-expanded"), "true");
  assert.equal(elements.appContent.inert, true);
  assert.equal(elements.appContent.getAttribute("aria-hidden"), "true");
  assert.equal(document.activeElement, elements.overviewLink);

  document.dispatchEvent({ type: "keydown", key: "Escape" });
  assert.equal(elements.navigationDrawer.hidden, false);
  assert.equal(elements.navigationBackdrop.hidden, false);
  assert.equal(elements.navigationDrawer.classList.contains("is-open"), false);
  assert.equal(elements.navigationBackdrop.classList.contains("is-open"), false);
  assert.equal(elements.menuButton.getAttribute("aria-expanded"), "false");
  assert.equal(elements.appContent.inert, false);
  assert.equal(elements.appContent.getAttribute("aria-hidden"), null);
  assert.equal(document.activeElement, elements.menuButton);

  elements.navigationDrawer.dispatchEvent({ type: "transitionend", propertyName: "transform" });
  assert.equal(elements.navigationDrawer.hidden, true);
  assert.equal(elements.navigationBackdrop.hidden, true);

  elements.menuButton.dispatchEvent({ type: "click" });
  elements.navigationBackdrop.dispatchEvent({ type: "click" });
  elements.navigationDrawer.dispatchEvent({ type: "transitionend", propertyName: "transform" });
  assert.equal(elements.navigationDrawer.hidden, true);
  assert.equal(elements.appContent.inert, false);
  assert.equal(document.activeElement, elements.menuButton);

  elements.menuButton.dispatchEvent({ type: "click" });
  elements.closeMenuButton.dispatchEvent({ type: "click" });
  elements.navigationDrawer.dispatchEvent({ type: "transitionend", propertyName: "transform" });
  assert.equal(elements.navigationDrawer.hidden, true);
  assert.equal(elements.appContent.inert, false);
  assert.equal(document.activeElement, elements.menuButton);
});

test("navigation drawer applies its open state after a rendered closed frame", () => {
  const { bindActions, drawerLayoutReads, elements, runAnimationFrames } = loadApp();
  bindActions();

  elements.menuButton.dispatchEvent({ type: "click" });

  assert.equal(elements.navigationDrawer.hidden, false);
  assert.equal(elements.navigationBackdrop.hidden, false);
  assert.equal(elements.navigationDrawer.classList.contains("is-open"), false);
  assert.equal(elements.navigationBackdrop.classList.contains("is-open"), false);
  assert.deepEqual(drawerLayoutReads, [false]);

  runAnimationFrames();

  assert.equal(elements.navigationDrawer.classList.contains("is-open"), true);
  assert.equal(elements.navigationBackdrop.classList.contains("is-open"), true);
});

test("reopening after repeated closes ignores stale drawer transition cleanup", () => {
  const { bindActions, document, elements, runAnimationFrames } = loadApp();
  bindActions();

  elements.menuButton.dispatchEvent({ type: "click" });
  runAnimationFrames();
  document.dispatchEvent({ type: "keydown", key: "Escape" });
  elements.navigationBackdrop.dispatchEvent({ type: "click" });

  elements.menuButton.dispatchEvent({ type: "click" });
  runAnimationFrames();
  elements.navigationDrawer.dispatchEvent({ type: "transitionend", propertyName: "transform" });

  assert.equal(elements.navigationDrawer.hidden, false);
  assert.equal(elements.navigationBackdrop.hidden, false);
  assert.equal(elements.navigationDrawer.classList.contains("is-open"), true);
  assert.equal(elements.navigationBackdrop.classList.contains("is-open"), true);
});

test("reduced motion closes the drawer immediately", () => {
  const { bindActions, elements } = loadApp({ reducedMotion: true });
  bindActions();

  elements.menuButton.dispatchEvent({ type: "click" });
  elements.closeMenuButton.dispatchEvent({ type: "click" });

  assert.equal(elements.navigationDrawer.hidden, true);
  assert.equal(elements.navigationBackdrop.hidden, true);
  assert.equal(elements.navigationDrawer.classList.contains("is-open"), false);
  assert.equal(elements.navigationBackdrop.classList.contains("is-open"), false);
});

test("navigation drawer keeps Tab and Shift+Tab focus inside drawer controls", () => {
  const { bindActions, document, elements, runAnimationFrames } = loadApp();
  bindActions();

  elements.menuButton.dispatchEvent({ type: "click" });
  runAnimationFrames();
  elements.settingsLink.focus();
  const tabEvent = { type: "keydown", key: "Tab" };
  document.dispatchEvent(tabEvent);

  assert.equal(tabEvent.defaultPrevented, true);
  assert.equal(document.activeElement, elements.closeMenuButton);

  elements.closeMenuButton.focus();
  const shiftTabEvent = { type: "keydown", key: "Tab", shiftKey: true };
  document.dispatchEvent(shiftTabEvent);

  assert.equal(shiftTabEvent.defaultPrevented, true);
  assert.equal(document.activeElement, elements.settingsLink);
});

test("closing navigation drawer does not trap Tab in aria-hidden content", () => {
  const { bindActions, document, elements, runAnimationFrames } = loadApp();
  bindActions();

  elements.menuButton.dispatchEvent({ type: "click" });
  runAnimationFrames();
  elements.closeMenuButton.dispatchEvent({ type: "click" });

  const tabEvent = { type: "keydown", key: "Tab" };
  document.dispatchEvent(tabEvent);

  assert.equal(elements.navigationDrawer.hidden, false);
  assert.equal(elements.navigationDrawer.getAttribute("aria-hidden"), "true");
  assert.equal(tabEvent.defaultPrevented, undefined);
  assert.equal(document.activeElement, elements.menuButton);
});

test("page links navigate and close the drawer", () => {
  const { bindActions, document, window, elements } = loadApp({ hash: "#/overview" });
  bindActions();

  elements.menuButton.dispatchEvent({ type: "click" });
  const event = { type: "click" };
  elements.heatingLink.dispatchEvent(event);
  elements.navigationDrawer.dispatchEvent({ type: "transitionend", propertyName: "transform" });

  assert.equal(event.defaultPrevented, true);
  assert.equal(window.location.hash, "#/heating");
  assert.equal(elements.navigationDrawer.hidden, true);
  assert.equal(elements.navigationBackdrop.hidden, true);
  assert.equal(elements.appContent.inert, false);
  assert.equal(document.activeElement, elements.menuButton);
});

test("overview temperature cards deep link to heating", () => {
  const { bindActions, window, elements } = loadApp({ hash: "#/overview" });
  bindActions();

  elements.aldeCard.dispatchEvent({ type: "click" });

  assert.equal(window.location.hash, "#/heating");
});

test("dynamically rendered temperature card uses delegated deep link", () => {
  const { bindActions, window, elements } = loadApp({ hash: "#/overview" });
  bindActions();
  const generatedCard = {
    dataset: { overviewRoute: "#/heating" },
    closest() {
      return this;
    },
  };

  elements.temperatureBody.dispatchEvent({ type: "click", target: generatedCard });

  assert.equal(window.location.hash, "#/heating");
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
  assert.match(html, /21\.4/);
  assert.match(html, /Humidity 55%/);
  assert.match(html, /data-tone="warm"/);
  assert.match(html, /overview-trend-up is-active/);
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
  assert.match(html, /12\.3/);
  assert.match(html, /overview-trend-down is-active/);
  assert.match(html, /Alde/);
  assert.match(html, />-</);
  assert.match(html, /overview-trend-control/);
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
  assert.match(html, /21\.0/);
  assert.doesNotMatch(html, /overview-sensor-row/);
});

test("renders waiting message when temperature data is absent", () => {
  const { renderTemperature, elements, state } = loadApp({ groupedElements: [] });
  renderTemperature({});
  assert.match(elements.temperatureBody.innerHTML, /Waiting for temperature/);
});

test("renders seven-day water chart as two simple datum-to-datum lines", () => {
  const { renderWaterHistory, state, elements } = loadApp();
  elements.waterHistoryChart.scrollWidth = 720;
  elements.waterHistoryChart.clientWidth = 320;
  const now = Date.now();
  state.waterHistory = {
    samples: [
      { t: new Date(now - 2 * 60 * 60 * 1000).toISOString(), fresh_percent: 80, grey_percent: 20 },
      { t: new Date(now - 30 * 60 * 1000).toISOString(), fresh_percent: 70, grey_percent: 10 },
    ],
    markers: [{ t: new Date(now - 30 * 60 * 1000).toISOString(), events: [{ tank: "fresh", kind: "fill" }, { tank: "grey", kind: "empty" }] }],
    fresh: { event_at: new Date(now - 2 * 24 * 60 * 60 * 1000).toISOString(), days_since: 2, used_percent: 22 },
    grey: { event_at: new Date(now - 24 * 60 * 60 * 1000).toISOString(), days_since: 1, used_percent: 18 },
  };
  renderWaterHistory();
  assert.match(elements.waterHistoryChart.innerHTML, /water-history-fresh/);
  assert.match(elements.waterHistoryChart.innerHTML, /water-history-grey/);
  assert.match(elements.waterHistoryChart.innerHTML, /stroke="#1976d2"/);
  assert.match(elements.waterHistoryChart.innerHTML, /water-history-fresh[^>]*fill="none"/);
  assert.match(elements.waterHistoryChart.innerHTML, /water-history-grey[^>]*fill="none"/);
  assert.match(elements.waterHistoryChart.innerHTML, /<path d="[^"]*L[^"]*" class="water-history-fresh"/);
  assert.match(elements.waterHistoryChart.innerHTML, /100%/);
  assert.match(elements.waterHistoryChart.innerHTML, /0%/);
  assert.match(elements.waterHistoryChart.innerHTML, /[A-Z]{3} \d{1,2}/);
  assert.doesNotMatch(elements.waterHistoryChart.innerHTML, /water-history-point/);
  assert.doesNotMatch(elements.waterHistoryChart.innerHTML, /water-history-marker/);
  assert.equal(elements.waterHistoryChart.scrollLeft, 720);
  assert.equal(elements.freshWaterUsage.textContent, "2 days since last fresh water fill, used 22%");
  assert.equal(elements.greyWaterUsage.textContent, "1 day since last grey water empty, used 18%");
});

test("connects each prepared chart datum directly", () => {
  const { renderWaterHistory, state, elements } = loadApp();
  const base = Date.now() - 2 * 60 * 60 * 1000;
  state.waterHistory = {
    samples: [
      { t: new Date(base).toISOString(), fresh_percent: 80, grey_percent: 20 },
      { t: new Date(base + 10 * 60000).toISOString(), fresh_percent: 81, grey_percent: 21 },
      { t: new Date(base + 20 * 60000).toISOString(), fresh_percent: 82, grey_percent: 22 },
    ],
    markers: [],
  };

  renderWaterHistory();

  const freshPath = elements.waterHistoryChart.innerHTML.match(/<path d="([^"]+)" class="water-history-fresh"/);
  assert.ok(freshPath);
  assert.equal((freshPath[1].match(/L/g) || []).length, 2);
});

test("renders server-prepared chart samples instead of raw samples", () => {
  const { renderWaterHistory, state, elements } = loadApp();
  const now = Date.now();
  state.waterHistory = {
    samples: [{ t: new Date(now).toISOString(), fresh_percent: 0, grey_percent: 0 }],
    chart_samples: [{ t: new Date(now).toISOString(), fresh_percent: 42, grey_percent: 58 }],
    markers: [],
  };

  renderWaterHistory();

  const freshPath = elements.waterHistoryChart.innerHTML.match(/<path d="([^"]+)" class="water-history-fresh"/);
  assert.ok(freshPath);
  assert.match(freshPath[1], /,140\.1$/);
});

test("renders water history during the normal water render", () => {
  const { renderWater, state, elements } = loadApp();
  state.water = {
    command_in_progress: false,
    valve_moving: false,
    valve_known: true,
    scheduled_opening: null,
  };
  state.waterHistory = {
    samples: [{ t: new Date().toISOString(), fresh_percent: 80, grey_percent: 20 }],
    markers: [],
  };

  renderWater();

  assert.match(elements.waterHistoryChart.innerHTML, /water-history-fresh/);
  assert.match(elements.waterHistoryChart.innerHTML, /water-history-grey/);
});

test("keeps water history when an unrelated initial request fails", async () => {
  const history = { samples: [{ t: new Date().toISOString(), fresh_percent: 80, grey_percent: 20 }], markers: [] };
  const { loadInitialState, state } = loadApp({
    fetchImpl: async (path) => {
      if (path === "/v1/water/history") return { ok: true, text: async () => JSON.stringify(history) };
      if (path === "/v1/lights/state") return { ok: false, status: 503, text: async () => JSON.stringify({ error: "unavailable" }) };
      if (path === "/v1/tracks") return { ok: true, text: async () => "[]" };
      return { ok: true, text: async () => "{}" };
    },
  });

  await loadInitialState();

  assert.equal(state.waterHistory.samples.length, 1);
  assert.equal(state.waterHistory.samples[0].fresh_percent, 80);
  assert.equal(state.waterHistory.samples[0].grey_percent, 20);
});

test("renders explicit water no-event summaries", () => {
  const { renderWaterHistory, state, elements } = loadApp();
  state.waterHistory = { samples: [] };
  renderWaterHistory();
  assert.equal(elements.freshWaterUsage.textContent, "No fresh water fill recorded.");
  assert.equal(elements.greyWaterUsage.textContent, "No grey water empty recorded.");
  assert.match(elements.waterHistoryChart.innerHTML, /No water history available/);
});

test("trend label maps every trend value", () => {
  const { trendLabel } = loadApp({ groupedElements: [] });
  assert.equal(trendLabel("rising"), "Rising");
  assert.equal(trendLabel("falling"), "Falling");
  assert.equal(trendLabel("steady"), "Steady");
  assert.equal(trendLabel("unavailable"), "Trend N/A");
  assert.equal(trendLabel(undefined), "Trend N/A");
});

test("trend state maps the four states", () => {
  const { getTrendState } = loadApp({ groupedElements: [] });
  assert.equal(JSON.stringify(getTrendState("rising")), JSON.stringify({ up: true, flat: false, down: false }));
  assert.equal(JSON.stringify(getTrendState("falling")), JSON.stringify({ up: false, flat: false, down: true }));
  assert.equal(JSON.stringify(getTrendState("steady")), JSON.stringify({ up: false, flat: true, down: false }));
  assert.equal(JSON.stringify(getTrendState("unavailable")), JSON.stringify({ up: false, flat: false, down: false }));
  assert.equal(JSON.stringify(getTrendState(undefined)), JSON.stringify({ up: false, flat: false, down: false }));
});

test("temperature chart domain contains rounded axis bounds", () => {
  const { temperatureChartDomain } = loadApp({ groupedElements: [] });
  assert.equal(JSON.stringify(temperatureChartDomain([21, 22])), JSON.stringify({ minTemp: 21, maxTemp: 22, yBottom: 20, yTop: 25, ySpan: 5 }));
});

test("temperature chart hour boundaries cross midnight", () => {
  const { temperatureChartHourBoundaries } = loadApp({ groupedElements: [] });
  const start = new Date("2026-08-17T23:30:00").getTime();
  const end = new Date("2026-08-18T01:30:00").getTime();
  const boundaries = temperatureChartHourBoundaries([start, end]);
  assert.equal(JSON.stringify(boundaries.map(({ label }) => label)), JSON.stringify(["00:00", "01:00"]));
  assert.ok(boundaries.every(({ ms }) => ms > start && ms < end));
});
