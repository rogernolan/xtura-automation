/* eslint-env browser, node, es2020 */
const defaults = Object.freeze({ controls: "heating", more: "system" });
const sections = Object.freeze({ controls: new Set(["heating", "water", "lighting"]), more: new Set(["system", "tools"]) });
const screens = new Set(["overview", "controls", "location", "more"]);
const fallback = () => ({ screen: "overview", section: null });

function parse(hash) {
  const parts = String(hash || "").replace(/^#\/?/, "").split("/").filter(Boolean);
  const [screen, section] = parts;
  if (!screens.has(screen) || parts.length > 2) return fallback();
  if (screen === "overview" || screen === "location") return section ? fallback() : { screen, section: null };
  const selected = section || defaults[screen];
  return sections[screen].has(selected) ? { screen, section: selected } : fallback();
}

function toHash(route) {
  const parsed = parse(route.section ? `#/${route.screen}/${route.section}` : `#/${route.screen}`);
  return parsed.section ? `#/${parsed.screen}/${parsed.section}` : `#/${parsed.screen}`;
}

const api = { parse, toHash };
globalThis.XturaNavigation = api;
if (typeof module !== "undefined") module.exports = api;
