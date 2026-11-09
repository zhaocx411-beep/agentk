(function (root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) module.exports = api;
  if (root) root.AgentKComposerState = api;
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";

  function hasPendingAsk(state) {
    return (state.pendingAsks?.length || 0) > 0;
  }

  function composerActionsBlocked(state = {}) {
    return Boolean(
      state.sending || state.uploading || state.agentRunning || hasPendingAsk(state)
    );
  }

  function messageInputBlocked(state = {}) {
    return Boolean(state.sending || state.agentRunning || hasPendingAsk(state));
  }

  return { composerActionsBlocked, messageInputBlocked };
});
