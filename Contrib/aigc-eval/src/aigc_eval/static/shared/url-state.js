/**
 * Small URL state layer for the studio page.
 *
 * Page controllers own validation; this module only reads and atomically
 * updates query parameters without causing a reload.
 */
export function readUrlState() {
  return Object.fromEntries(new URLSearchParams(window.location.search).entries());
}

export function replaceUrlState(patch) {
  const url = new URL(window.location.href);
  Object.entries(patch).forEach(([key, value]) => {
    if (value == null || value === "") url.searchParams.delete(key);
    else url.searchParams.set(key, String(value));
  });
  window.history.replaceState(null, "", `${url.pathname}${url.search}${url.hash}`);
}
