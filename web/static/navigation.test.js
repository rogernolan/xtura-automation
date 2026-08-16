const assert = require("node:assert/strict");
const test = require("node:test");
const fs = require("node:fs");
const navigation = require("./navigation.js");

test("parses defaults and nested routes", () => {
  assert.deepEqual(navigation.parse("#/overview"), { screen: "overview", section: null });
  assert.deepEqual(navigation.parse("#/controls"), { screen: "controls", section: "heating" });
  assert.deepEqual(navigation.parse("#/controls/water"), { screen: "controls", section: "water" });
  assert.deepEqual(navigation.parse("#/more/tools"), { screen: "more", section: "tools" });
  assert.deepEqual(navigation.parse("#/more/settings"), { screen: "more", section: "settings" });
});

test("falls back for unavailable routes", () => {
  assert.deepEqual(navigation.parse("#/controls/energy"), { screen: "overview", section: null });
  assert.deepEqual(navigation.parse("#/unknown"), { screen: "overview", section: null });
});

test("writes canonical hashes", () => {
  assert.equal(navigation.toHash({ screen: "location", section: null }), "#/location");
  assert.equal(navigation.toHash({ screen: "controls", section: "lighting" }), "#/controls/lighting");
  assert.equal(navigation.toHash({ screen: "more", section: "settings" }), "#/more/settings");
});

test("overview markup follows the approved grouped layout and deep links", () => {
  const html = fs.readFileSync(require("node:path").join(__dirname, "index.html"), "utf8");
  assert.match(html, /class="overview-group overview-temperature-group"/);
  assert.match(html, /class="overview-group overview-power-group"/);
  assert.match(html, /class="overview-group overview-supplies-group"/);
  assert.match(html, /data-overview-route="#\/controls\/heating"/);
  assert.match(html, /data-overview-route="#\/controls\/water"/);
  assert.match(html, /data-overview-route="#\/more\/settings"/);
  assert.match(html, /id="overviewTrend"[^>]*>Trend unavailable</);
  assert.match(html, /id="batteryCurrentState"/);
  const styles = fs.readFileSync(require("node:path").join(__dirname, "styles.css"), "utf8");
  for (const tone of ["cold", "comfortable", "warm", "hot"]) {
    assert.match(styles, new RegExp(`overview-temperature-card\\[data-tone=\\\"${tone}\\\"\\]`));
  }
});
