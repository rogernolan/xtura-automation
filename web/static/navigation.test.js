const assert = require("node:assert/strict");
const test = require("node:test");
const fs = require("node:fs");
const navigation = require("./navigation.js");

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
  assert.match(html, /data-overview-route="#\/heating"/);
  assert.match(html, /data-overview-route="#\/water"/);
  assert.match(html, /data-overview-route="#\/system"/);
  assert.match(html, /data-overview-route="#\/settings"/);
  assert.match(html, /id="overviewTrend"[^>]*>Trend unavailable</);
  assert.match(html, /id="batteryCurrentState"/);
  const styles = fs.readFileSync(require("node:path").join(__dirname, "styles.css"), "utf8");
  for (const tone of ["cold", "comfortable", "warm", "hot"]) {
    assert.match(styles, new RegExp(`overview-temperature-card\\[data-tone=\\\"${tone}\\\"\\]`));
  }
  assert.match(styles, /overview-temperature-card\[data-tone="comfortable"\] \{[^}]*radial-gradient\(/);
  assert.match(styles, /overview-temperature-card\[data-tone="comfortable"\] \{[^}]*var\(--surface\)/);
  assert.match(styles, /\.overview-group-title \{[^}]*color: var\(--text\)/);
  assert.match(styles, /background-image: linear-gradient\(rgba\(231, 226, 216, 0\.6/);
});
