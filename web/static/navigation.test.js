const assert = require("node:assert/strict");
const test = require("node:test");
const navigation = require("./navigation.js");

test("parses defaults and nested routes", () => {
  assert.deepEqual(navigation.parse("#/overview"), { screen: "overview", section: null });
  assert.deepEqual(navigation.parse("#/controls"), { screen: "controls", section: "heating" });
  assert.deepEqual(navigation.parse("#/controls/water"), { screen: "controls", section: "water" });
  assert.deepEqual(navigation.parse("#/more/tools"), { screen: "more", section: "tools" });
});

test("falls back for unavailable routes", () => {
  assert.deepEqual(navigation.parse("#/controls/energy"), { screen: "overview", section: null });
  assert.deepEqual(navigation.parse("#/unknown"), { screen: "overview", section: null });
});

test("writes canonical hashes", () => {
  assert.equal(navigation.toHash({ screen: "location", section: null }), "#/location");
  assert.equal(navigation.toHash({ screen: "controls", section: "lighting" }), "#/controls/lighting");
});
