const test = require("node:test");
const assert = require("node:assert/strict");

const {
  composerActionsBlocked,
  messageInputBlocked,
} = require("../src/agentk_capability_demo/static/composer-state.js");

test("uploading keeps message input editable while blocking actions", () => {
  const state = { uploading: true, pendingAsks: [] };

  assert.equal(messageInputBlocked(state), false);
  assert.equal(composerActionsBlocked(state), true);
});

test("active execution states still block message input and actions", () => {
  for (const state of [
    { sending: true, pendingAsks: [] },
    { agentRunning: true, pendingAsks: [] },
    { pendingAsks: [{}] },
  ]) {
    assert.equal(messageInputBlocked(state), true);
    assert.equal(composerActionsBlocked(state), true);
  }
});

test("idle composer keeps message input and actions enabled", () => {
  const state = { pendingAsks: [] };

  assert.equal(messageInputBlocked(state), false);
  assert.equal(composerActionsBlocked(state), false);
});
