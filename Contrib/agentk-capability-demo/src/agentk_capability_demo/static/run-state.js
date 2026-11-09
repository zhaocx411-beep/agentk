(function (root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) module.exports = api;
  if (root) root.AgentKRunState = api;
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";

  const TERMINAL = /COMPLETED|FAILED|CANCELLED/i;

  function capabilityPollView(status = "", failures = 0, failureLimit = 5) {
    const normalized = String(status).replace("EXECUTION_STATUS_", "");
    if (TERMINAL.test(normalized)) {
      return { label: normalized, active: false, terminal: true, unavailable: false };
    }
    if (failures >= failureLimit) {
      return { label: "STATUS UNAVAILABLE", active: true, terminal: false, unavailable: true };
    }
    return { label: "GENERATING", active: true, terminal: false, unavailable: false };
  }

  return { capabilityPollView };
});
