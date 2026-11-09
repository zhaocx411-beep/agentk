(function (root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) module.exports = api;
  if (root) root.AgentKResultMedia = api;
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";

  function mediaKindFor(artifact = {}, hint = "") {
    const declared = `${artifact.type || ""} ${artifact.mimeType || artifact.mime || ""}`.toLowerCase();
    const field = String(hint).toLowerCase();
    const uri = String(artifact.uri || artifact.url || "").split("#", 1)[0];
    if (declared.includes("image")) return "image";
    if (declared.includes("video")) return "video";
    if (declared.includes("audio") || declared.includes("speech")) return "audio";
    if (declared.includes("file") || declared.includes("document") || declared.includes("subtitle")) return "file";
    if (/image|frame|poster|thumbnail/.test(field)) return "image";
    if (/video|movie|clip/.test(field)) return "video";
    if (/audio|speech|voice|sound/.test(field)) return "audio";
    if (/subtitle|document|file/.test(field)) return "file";
    if (/\.(png|jpe?g|webp|gif|avif)(\?|$)/i.test(uri)) return "image";
    if (/\.(mp4|webm|mov|m4v|mkv|avi)(\?|$)/i.test(uri)) return "video";
    if (/\.(mp3|wav|m4a|aac|ogg|flac)(\?|$)/i.test(uri)) return "audio";
    if (/\.(pdf|txt|md|csv|json|srt|ass|vtt|docx?|xlsx?)(\?|$)/i.test(uri)) return "file";
    return null;
  }

  function mediaArtifactsFor(output) {
    const found = [];
    const seen = new Set();
    const add = (source, hint) => {
      const uri = source?.uri || source?.url;
      if (typeof uri !== "string" || !/^https?:\/\//i.test(uri) || seen.has(uri)) return;
      const kind = mediaKindFor(source, hint);
      if (!kind) return;
      seen.add(uri);
      found.push({
        kind,
        uri,
        description: typeof source.description === "string" ? source.description : "",
        type: typeof source.type === "string" && source.type ? source.type : kind,
      });
    };
    const visit = (value, hint = "", depth = 0) => {
      if (depth > 6 || value === null || value === undefined) return;
      if (typeof value === "string") { add({ uri: value }, hint); return; }
      if (Array.isArray(value)) { value.forEach(item => visit(item, hint, depth + 1)); return; }
      if (typeof value !== "object") return;
      add(value, hint);
      Object.entries(value).forEach(([key, child]) => {
        if (["uri", "url", "type", "mime", "mimeType", "description"].includes(key)) return;
        visit(child, key, depth + 1);
      });
    };
    visit(output);
    return found;
  }

  return { mediaArtifactsFor, mediaKindFor };
});
