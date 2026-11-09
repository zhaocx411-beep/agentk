import { fetchJson } from "../shared/api.js";
import { readUrlState, replaceUrlState } from "../shared/url-state.js";

const PROVIDER_LABEL = { agentk: "AgentK" };

/** Keeps health capability, provider selection and URL context in one place. */
export function createProviderController({ state, elements, escapeHtml, onChange }) {
  function applySelection() {
    const selected = state.providers[elements.providerSelect.value] || {};
    elements.useVlm.disabled = !selected.configured;
    if (!selected.configured) elements.useVlm.checked = false;
    replaceUrlState({ provider: elements.providerSelect.value });
    onChange();
  }

  async function load() {
    if (!elements.providerSelect) return;
    try {
      const data = await fetchJson("./api/health");
      state.providers = data.providers || {};
      const names = ["agentk"].filter((name) => state.providers[name]);
      elements.providerSelect.innerHTML = names.map((name) => {
        const info = state.providers[name] || {};
        const disabled = info.configured ? "" : " disabled";
        const suffix = info.configured ? "" : " · 未配置";
        return `<option value="${name}"${disabled}>${escapeHtml(PROVIDER_LABEL[name] || name)}${suffix}</option>`;
      }).join("");
      const urlProvider = readUrlState().provider;
      const defaultName = data.default_provider || "";
      elements.providerSelect.value = state.providers[urlProvider]?.configured
        ? urlProvider
        : state.providers[defaultName]?.configured
          ? defaultName
          : names.find((name) => state.providers[name]?.configured) || defaultName || names[0];
      applySelection();
    } catch (_) {
      state.providers = {};
      elements.providerSelect.innerHTML = `<option value="">模型通道读取失败</option>`;
      elements.providerSelect.disabled = true;
      elements.useVlm.checked = false;
      elements.useVlm.disabled = true;
      onChange();
    }
  }

  function wire() {
    elements.providerSelect.addEventListener("change", applySelection);
  }

  return { load, wire };
}
