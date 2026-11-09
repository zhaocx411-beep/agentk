const test = require("node:test");
const assert = require("node:assert/strict");

const { capabilityPollView } = require("../src/agentk_capability_demo/static/run-state.js");

test("an accepted capability run is displayed as generating", () => {
  assert.deepEqual(capabilityPollView("EXECUTION_STATUS_RUNNING", 0), {
    label: "GENERATING",
    active: true,
    terminal: false,
    unavailable: false,
  });
});

test("transient polling failures keep the generating state", () => {
  for (const failures of [1, 2, 3, 4]) {
    assert.deepEqual(capabilityPollView("", failures), {
      label: "GENERATING",
      active: true,
      terminal: false,
      unavailable: false,
    });
  }
});

test("five consecutive polling failures expose a real status error", () => {
  assert.deepEqual(capabilityPollView("", 5), {
    label: "STATUS UNAVAILABLE",
    active: true,
    terminal: false,
    unavailable: true,
  });
});

test("terminal execution states unlock the form", () => {
  assert.deepEqual(capabilityPollView("EXECUTION_STATUS_COMPLETED", 0), {
    label: "COMPLETED",
    active: false,
    terminal: true,
    unavailable: false,
  });
  assert.equal(capabilityPollView("EXECUTION_STATUS_FAILED", 0).label, "FAILED");
  assert.equal(capabilityPollView("EXECUTION_STATUS_CANCELLED", 0).label, "CANCELLED");
});
