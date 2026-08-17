const assert = require("node:assert/strict");
const test = require("node:test");
const fs = require("node:fs");
const navigation = require("./navigation.js");

function assertPanelOwnsIds(html, panelId, ids) {
  const match = html.match(new RegExp(`<section id="${panelId}"[^>]*>[\\s\\S]*?<\\/section>`));
  assert.ok(match, `missing panel ${panelId}`);
  for (const id of ids) {
    assert.match(match[0], new RegExp(`id="${id}"`), `${panelId} should contain ${id}`);
  }
}

function assertPanelOwnsDataTargets(html, panelId, targets) {
  const match = html.match(new RegExp(`<section id="${panelId}"[^>]*>[\\s\\S]*?<\\/section>`));
  assert.ok(match, `missing panel ${panelId}`);
  for (const target of targets) {
    assert.match(match[0], new RegExp(`data-target="${target}"`), `${panelId} should contain data-target="${target}"`);
  }
}

test("exposes the canonical page list", () => {
  assert.deepEqual(navigation.pages, ["overview", "heating", "water", "lighting", "location", "system", "tools", "settings"]);
});

test("parses every canonical page", () => {
  for (const page of navigation.pages) {
    assert.deepEqual(navigation.parse(`#/${page}`), { page });
  }
});

test("falls back for legacy, nested, and unknown routes", () => {
  for (const hash of ["", "#", "#/controls/heating", "#/more/tools", "#/unknown", "#/heating/extra"]) {
    assert.deepEqual(navigation.parse(hash), { page: "overview" });
  }
});

test("writes canonical page hashes", () => {
  assert.equal(navigation.toHash({ page: "heating" }), "#/heating");
  assert.equal(navigation.toHash({ page: "overview" }), "#/overview");
  assert.equal(navigation.toHash({ page: "not-a-page" }), "#/overview");
});

test("overview markup uses page panels and drawer navigation", () => {
  const html = fs.readFileSync(require("node:path").join(__dirname, "index.html"), "utf8");
  for (const page of navigation.pages) {
    assert.match(html, new RegExp(`id="${page}Panel"`));
  }
  assert.match(html, /id="appContent"/);
  assert.match(html, /id="menuButton"[^>]*aria-expanded="false"/);
  assert.match(html, /id="navigationBackdrop"/);
  assert.match(html, /id="navigationDrawer"/);
  assert.match(html, /data-page="overview" href="#\/overview"/);
  assert.match(html, /data-page="heating" href="#\/heating"/);
  assert.match(html, /data-page="water" href="#\/water"/);
  assert.match(html, /data-page="lighting" href="#\/lighting"/);
  assert.match(html, /data-page="location" href="#\/location"/);
  assert.match(html, /data-page="system" href="#\/system"/);
  assert.match(html, /data-page="tools" href="#\/tools"/);
  assert.match(html, /data-page="settings" href="#\/settings"/);
  assert.doesNotMatch(html, /data-section-group=/);
  assert.doesNotMatch(html, /class="primary-nav/);
  assert.match(html, /class="overview-group overview-temperature-group"/);
  assert.match(html, /class="overview-group overview-power-group"/);
  assert.match(html, /class="overview-group overview-supplies-group"/);
  assert.match(html, /id="temperatureBody"/);
  assert.match(html, /data-overview-route="#\/water"/);
  assert.match(html, /data-overview-route="#\/more\/settings"/);
  assert.match(html, /data-overview-route="#\/more\/settings"/);
  assert.match(html, /id="batteryCurrent"/);
  assertPanelOwnsIds(html, "heatingPanel", [
    "modeOn",
    "modeSchedule",
    "modeOff",
    "targetDown",
    "targetUp",
    "boostButton",
    "cancelBoostButton",
    "scheduleForm",
    "scheduleSlots",
    "saveSchedule",
  ]);
  assertPanelOwnsDataTargets(html, "heatingPanel", ["5", "18", "21"]);
  assertPanelOwnsIds(html, "waterPanel", [
    "waterState",
    "openGreyValve",
    "closeGreyValve",
    "greyScheduleTime",
    "greyScheduleDuration",
    "greyScheduleButton",
    "greyScheduleMessage",
    "waterDetail",
  ]);
  assertPanelOwnsIds(html, "lightingPanel", [
    "lightsState",
    "flashCount",
    "flashLights",
    "lightsDetail",
  ]);
  assertPanelOwnsIds(html, "locationPanel", [
    "trackingPanel",
    "trackingState",
    "trackingEngineOnly",
    "trackingManualControls",
    "trackingStartButton",
    "trackingStopButton",
    "trackingInterval",
    "trackingDetail",
    "trackList",
  ]);
  assertPanelOwnsIds(html, "systemPanel", [
    "piStatusPanel",
    "piPowerState",
    "piStats",
    "piDetail",
    "deploymentInfo",
  ]);
  assertPanelOwnsIds(html, "toolsPanel", [
    "recordingPanel",
    "recordingState",
    "recordingWaitFor",
    "recordingDuration",
    "recordingButton",
    "recordingDetail",
  ]);
  assertPanelOwnsIds(html, "settingsPanel", [
    "overviewSettingsForm",
    "overviewSettingsState",
    "comfortCold",
    "comfortComfort",
    "comfortWarm",
    "comfortHot",
    "batteryCapacity",
    "gasCapacity",
  ]);
  const styles = fs.readFileSync(require("node:path").join(__dirname, "styles.css"), "utf8");
  for (const tone of ["cold", "comfortable", "warm", "hot"]) {
    assert.match(styles, new RegExp(`overview-temperature-card\\[data-tone=\\\"${tone}\\\"\\]`));
  }
  assert.match(styles, /overview-temperature-card\[data-tone="comfortable"\] \{[^}]*radial-gradient\(/);
  assert.match(styles, /overview-temperature-card\[data-tone="comfortable"\] \{[^}]*var\(--surface\)/);
  assert.match(styles, /\.overview-group-title \{[^}]*color: var\(--text\)/);
  assert.match(styles, /\.overview-temperature-card \{[^}]*width: 100%;[^}]*min-width: 0;/);
  assert.match(styles, /\.overview-sensor-row \{[^}]*display: flex;[^}]*flex-wrap: nowrap;/);
  assert.match(styles, /\.overview-sub-sensor \{[^}]*min-width: 0;[^}]*flex: 1 1 0;/);
  assert.match(styles, /background-image: linear-gradient\(rgba\(231, 226, 216, 0\.6/);
  assert.match(styles, /\.navigation-drawer \{[^}]*transform: translateX\(-16px\)/);
  assert.match(styles, /\.navigation-drawer a\[aria-current="page"\]:hover \{[^}]*background: var\(--accent-pressed\)[^}]*color: #ffffff/);
});
