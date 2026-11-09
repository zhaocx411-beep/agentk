(function (root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) module.exports = api;
  if (root) root.AgentKSchemaValidation = api;
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";

  const missing = value => value === undefined || value === null || value === "";
  const typeMatches = (type, value) => {
    if (type === "object") return value !== null && typeof value === "object" && !Array.isArray(value);
    if (type === "array") return Array.isArray(value);
    if (type === "integer") return Number.isInteger(value);
    if (type === "number") return typeof value === "number" && Number.isFinite(value);
    if (type === "string") return typeof value === "string";
    if (type === "boolean") return typeof value === "boolean";
    if (type === "null") return value === null;
    return true;
  };
  const displayPath = path => path || "输入";

  function validateNode(schema, value, path, errors) {
    if (schema === false) { errors.push({path, message: `${displayPath(path)} 不允许设置`}); return; }
    if (!schema || schema === true || missing(value)) return;

    if (schema.not && validateBranch(schema.not, value)) {
      errors.push({path, message: `${displayPath(path)} 包含互斥字段或不允许的输入组合`});
      return;
    }

    if (Array.isArray(schema.anyOf) && !schema.anyOf.some(branch => validateBranch(branch, value))) {
      errors.push({path, message: `${displayPath(path)} 需满足至少一组字段要求`});
      return;
    }
    if (Array.isArray(schema.oneOf)) {
      const matches = schema.oneOf.filter(branch => validateBranch(branch, value)).length;
      if (matches !== 1) { errors.push({path, message: `${displayPath(path)} 必须且只能匹配一种输入结构`}); return; }
    }
    if (schema.const !== undefined && value !== schema.const) errors.push({path, message: `${displayPath(path)} 必须为 ${JSON.stringify(schema.const)}`});
    if (Array.isArray(schema.enum) && !schema.enum.some(item => item === value)) errors.push({path, message: `${displayPath(path)} 必须从候选值中选择`});
    if (schema.type && !typeMatches(schema.type, value)) {
      errors.push({path, message: `${displayPath(path)} 类型应为 ${schema.type}`});
      return;
    }

    if (typeof value === "string") {
      if (schema.minLength !== undefined && value.length < schema.minLength) errors.push({path, message: `${displayPath(path)} 至少需要 ${schema.minLength} 个字符`});
      if (schema.maxLength !== undefined && value.length > schema.maxLength) errors.push({path, message: `${displayPath(path)} 最多允许 ${schema.maxLength} 个字符`});
      if (schema.pattern) {
        try { if (!new RegExp(schema.pattern).test(value)) errors.push({path, message: `${displayPath(path)} 格式不符合 ${schema.pattern}`}); } catch { /* malformed provider pattern */ }
      }
      if (schema.format === "uri") {
        try { new URL(value); } catch { errors.push({path, message: `${displayPath(path)} 必须是有效 URI`}); }
      }
    }
    if (typeof value === "number") {
      if (schema.minimum !== undefined && value < schema.minimum) errors.push({path, message: `${displayPath(path)} 不能小于 ${schema.minimum}`});
      if (schema.maximum !== undefined && value > schema.maximum) errors.push({path, message: `${displayPath(path)} 不能大于 ${schema.maximum}`});
      if (schema.exclusiveMinimum !== undefined && value <= schema.exclusiveMinimum) errors.push({path, message: `${displayPath(path)} 必须大于 ${schema.exclusiveMinimum}`});
      if (schema.exclusiveMaximum !== undefined && value >= schema.exclusiveMaximum) errors.push({path, message: `${displayPath(path)} 必须小于 ${schema.exclusiveMaximum}`});
    }
    if (Array.isArray(value)) {
      if (schema.minItems !== undefined && value.length < schema.minItems) errors.push({path, message: `${displayPath(path)} 至少需要 ${schema.minItems} 项`});
      if (schema.maxItems !== undefined && value.length > schema.maxItems) errors.push({path, message: `${displayPath(path)} 最多允许 ${schema.maxItems} 项`});
      if (schema.uniqueItems && new Set(value.map(item => JSON.stringify(item))).size !== value.length) errors.push({path, message: `${displayPath(path)} 不允许重复项`});
      value.forEach((item, index) => validateNode(schema.items || {}, item, `${path}[${index}]`, errors));
    }
    if (value && typeof value === "object" && !Array.isArray(value)) {
      (schema.required || []).forEach(name => {
        if (missing(value[name])) errors.push({path: path ? `${path}.${name}` : name, message: `${path ? `${path}.` : ""}${name} 为必填参数`});
      });
      Object.entries(schema.properties || {}).forEach(([name, child]) => validateNode(child, value[name], path ? `${path}.${name}` : name, errors));
      if (schema.additionalProperties === false) {
        const allowed = new Set(Object.keys(schema.properties || {}));
        Object.keys(value).filter(name => !allowed.has(name)).forEach(name => errors.push({path: path ? `${path}.${name}` : name, message: `${path ? `${path}.` : ""}${name} 不是允许的参数`}));
      }
    }
  }

  function validateBranch(schema, value) {
    const branchErrors = [];
    validateNode(schema, value, "", branchErrors);
    return branchErrors.length === 0;
  }

  function capabilityRules(capabilityId, args) {
    const errors = [];
    if (capabilityId === "agentk.understand_video_segment" && Number(args.endSeconds) <= Number(args.startSeconds)) errors.push({path: "endSeconds", message: "endSeconds 必须大于 startSeconds"});
    if (capabilityId === "agentk.kling_image_to_video" && args.lastFrame && !args.firstFrame) errors.push({path: "lastFrame", message: "lastFrame 必须与 firstFrame 同时提供"});
    if (capabilityId === "agentk.kling_omni_video") {
      if (args.lastFrame && !args.firstFrame) errors.push({path: "lastFrame", message: "lastFrame 必须与 firstFrame 同时提供"});
      if (args.featureVideo && args.audio && args.audio !== "off") errors.push({path: "audio", message: "使用 featureVideo 时 audio 必须为 off"});
      if (args.featureVideo && args.referImages?.length > 4) errors.push({path: "referImages", message: "同时使用 featureVideo 时 referImages 最多允许 4 张"});
    }
    if (capabilityId === "agentk.seedance_generate_video") {
      if (args.lastFrame && !args.firstFrame) errors.push({path: "lastFrame", message: "lastFrame 必须与 firstFrame 同时提供"});
      if (args.referenceAudios?.length && !args.firstFrame && !args.referenceImages?.length && !args.referenceVideos?.length) errors.push({path: "referenceAudios", message: "参考音频必须与图片或视频素材一起使用"});
    }
    if (capabilityId === "agentk.lip_sync_video" && Number(args.soundEndTime) <= Number(args.soundStartTime || 0)) {
      errors.push({path: "soundEndTime", message: "soundEndTime 必须大于 soundStartTime"});
    }
    return errors;
  }

  function validateCapabilityArgs(schema, args, capabilityId = "") {
    const errors = [];
    validateNode(schema || {}, args, "", errors);
    return [...errors, ...capabilityRules(capabilityId, args || {})];
  }

  return { validateCapabilityArgs };
});
