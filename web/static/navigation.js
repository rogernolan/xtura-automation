/* eslint-env browser, node, es2020 */
const pages = Object.freeze(["overview", "heating", "water", "lighting", "location", "system", "tools", "settings"]);
const pageSet = new Set(pages);
const fallback = () => ({ page: "overview" });

function parse(hash) {
  const parts = String(hash || "").replace(/^#\/?/, "").split("/").filter(Boolean);
  if (parts.length !== 1 || !pageSet.has(parts[0])) return fallback();
  return { page: parts[0] };
}

function toHash(route) {
  const parsed = parse(route && `#/${route.page}`);
  return `#/${parsed.page}`;
}

const navigationApi = { pages, parse, toHash };
globalThis.XturaNavigation = navigationApi;
if (typeof module !== "undefined") module.exports = navigationApi;
