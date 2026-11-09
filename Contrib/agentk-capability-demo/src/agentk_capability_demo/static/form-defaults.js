(function (root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) module.exports = api;
  if (root) root.AgentKFormDefaults = api;
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";

  const MODEL_EXAMPLES = {
    "agentk.detect_shots": "shots-maas-prod",
    "agentk.generate_speech_indextts": "indextts-maas-prod",
    "agentk.generate_text": "deepseek-v4-pro",
    "agentk.generate_video": "kling-3.0",
    "agentk.separate_audio": "separator-maas-prod",
  };

  const PROMPT_EXAMPLES = {
    "agentk.edit_image": "保留主体构图，把背景改成日落海边，光影自然",
    "agentk.generate_brand_ad": "突出商品质感，节奏明快，使用近景特写和环绕运镜",
    "agentk.generate_image": "一只橘猫坐在未来感工作台前，电影级光影，细节清晰",
    "agentk.generate_text": "用三点总结 AgentK 的核心能力，每点不超过 30 字",
    "agentk.generate_video": "主体缓慢转身看向镜头，镜头平稳推进，环境声自然",
    "agentk.kling_image_to_video": "主体自然眨眼并轻微转头，镜头缓慢推进，环境声自然",
    "agentk.kling_omni_video": "参考 @image_1 的主体和风格，生成一段平稳推进的电影感镜头",
    "agentk.kling_text_to_video": "雨后的霓虹街道，一辆红色跑车驶过，低机位跟拍，电影感光影",
    "agentk.seedance_generate_video": "参考图片1的主体，生成缓慢环绕运镜，保持外观一致",
    "agentk.understand_media": "请描述素材中的主体、场景、动作和画面风格，并指出不确定之处",
    "agentk.understand_video_segment": "请分析该区间的画面、镜头、对白、音乐音效和字幕",
  };

  function clone(value) {
    if (value === undefined || value === null || typeof value !== "object") return value;
    return JSON.parse(JSON.stringify(value));
  }

  function numericExample(schema, name) {
    const lower = schema.minimum ?? schema.exclusiveMinimum;
    let value;
    if (/soundEndTime/i.test(name)) value = 3000;
    else if (/end|duration/i.test(name)) value = 5;
    else if (/timestamp/i.test(name)) value = 1;
    else if (/width/i.test(name)) value = 1280;
    else if (/height/i.test(name)) value = 720;
    else if (/fps/i.test(name)) value = 1;
    else value = lower ?? 1;
    if (schema.exclusiveMinimum !== undefined && value <= schema.exclusiveMinimum) {
      value = schema.type === "integer" ? Math.floor(schema.exclusiveMinimum) + 1 : schema.exclusiveMinimum + 1;
    }
    if (schema.minimum !== undefined) value = Math.max(value, schema.minimum);
    if (schema.maximum !== undefined) value = Math.min(value, schema.maximum);
    return schema.type === "integer" ? Math.round(value) : value;
  }

  function stringExample(schema, context) {
    const name = context.name || "";
    const capabilityId = context.capabilityId || "";
    if (name === "model" && MODEL_EXAMPLES[capabilityId]) return MODEL_EXAMPLES[capabilityId];
    if (capabilityId === "agentk.create_kling_subject" && name === "name") return "测试主体";
    if (name === "faceIndexKey") return "face-index-key-from-identify-faces";
    if (name === "faceId") return "face-id-from-identify-faces";
    if (name === "subjectLists") return "subject-list-from-create-kling-subject";
    if (name === "voiceEmbeddings") return "voice-embedding-from-create-voice-embedding";
    if (/prompt|video_prompt/i.test(name)) return PROMPT_EXAMPLES[capabilityId] || "请描述要执行的内容、风格和预期结果";
    if (/query/i.test(name)) return "AgentK 最新版本 capability";
    if (name.toLowerCase() === "title") return "本地测试广告";
    if (/voice.?id/i.test(name)) return "your-voice-id";
    if (/language/i.test(name)) return "zh";
    if (/text|content|script|description/i.test(name)) return "这是一段用于本地测试的示例文本。";
    if (/product.*url/i.test(name)) return "https://item.taobao.com/item.htm?id=1069836522906";
    if (/subtitle.*url/i.test(name)) return "https://example.com/subtitles.srt";
    if (/image|frame/i.test(name)) return "https://example.com/reference.png";
    if (/video/i.test(name)) return "https://example.com/input.mp4";
    if (/audio|speaker/i.test(name)) return "https://example.com/input.mp3";
    if (/file|document/i.test(name)) return "https://example.com/input.pdf";
    if (/url|uri/i.test(name) || schema.format === "uri") return "https://example.com/input";
    return "请替换为实际值";
  }

  function exampleFor(schema, context = {}) {
    if (schema === true) return {};
    if (!schema || schema === false) return undefined;
    if (schema.default !== undefined) return clone(schema.default);
    if (Array.isArray(schema["x-options"])) return schema["x-options"][0]?.value;
    if (Array.isArray(schema.examples) && schema.examples.length) return clone(schema.examples[0]);
    if (schema.const !== undefined) return clone(schema.const);
    if (Array.isArray(schema.enum) && schema.enum.length) return clone(schema.enum[0]);
    if (Array.isArray(schema.oneOf) && schema.oneOf.length) return exampleFor(schema.oneOf[0], context);
    if (Array.isArray(schema.anyOf) && schema.anyOf.length) return exampleFor(schema.anyOf[0], context);
    if (schema.type === "object" || schema.properties) return defaultsFor(schema, context.capabilityId);
    if (schema.type === "array") {
      const item = exampleFor(schema.items || {}, context);
      return item === undefined ? [] : [item];
    }
    if (schema.type === "boolean") return false;
    if (schema.type === "integer" || schema.type === "number") return numericExample(schema, context.name || "");
    return stringExample(schema, context);
  }

  function selectOptionsFor(schema = {}) {
    if (Array.isArray(schema["x-options"])) {
      return schema["x-options"].map(option => ({
        value: String(option?.value ?? ""),
        label: String(option?.label ?? option?.value ?? ""),
      }));
    }
    if (Array.isArray(schema.enum)) {
      return schema.enum.map(value => ({value: String(value), label: String(value)}));
    }
    return null;
  }

  function defaultsFor(schema, capabilityId = "") {
    if (schema?.default !== undefined) return clone(schema.default);
    if (!schema || schema === true) return {};
    const required = new Set(schema.required || []);
    const result = {};
    Object.entries(schema.properties || {}).forEach(([name, child]) => {
      if (child?.default !== undefined) {
        result[name] = clone(child.default);
      } else if (required.has(name)) {
        const value = exampleFor(child, { capabilityId, name });
        if (value !== undefined && value !== "") result[name] = value;
      }
    });
    const alternatives = Array.isArray(schema.oneOf) ? schema.oneOf : schema.anyOf;
    if (Array.isArray(alternatives) && alternatives.length) {
      const satisfies = branch => (branch.required || []).every(name => {
        const value = result[name];
        return value !== undefined && value !== null && value !== "";
      });
      if (!alternatives.some(satisfies)) {
        const branch = alternatives[0] || {};
        (branch.required || []).forEach(name => {
          if (result[name] !== undefined) return;
          const child = branch.properties?.[name] || schema.properties?.[name];
          const value = exampleFor(child || {}, { capabilityId, name });
          if (value !== undefined && value !== "") result[name] = value;
        });
      }
    }
    return result;
  }

  function uploadAcceptFor(name, schema = {}) {
    const stringValue = !schema.type || schema.type === "string";
    const stringArray = schema.type === "array" && schema.items?.type === "string";
    if ((!stringValue && !stringArray) || schema.enum) return null;
    const field = String(name || "").toLowerCase();
    if (/subtitle.*(url|uri)/.test(field)) return ".srt,.ass,.vtt";
    if (/media(url|uri)s?$/.test(field)) return "image/*,video/*,audio/*";
    if (/firstframe|lastframe|image(url|uri)s?$|refer(?:ence)?images?$|^images?$/.test(field)) return "image/*";
    if (field === "video") return "video/*";
    if (field === "audio") return "audio/*";
    if (/video(url|uri)s?$|referencevideos?$|featurevideo$/.test(field)) return "video/*";
    if (/audio(url|uri)s?$|speakeraudio|referenceaudios?$/.test(field)) return "audio/*";
    if (/(file|document)(url|uri)s?$/.test(field)) return "*/*";
    return null;
  }

  function mergeUploadedUrls(current, uploaded, multiple) {
    if (!multiple) return uploaded[0] ?? current;
    const existing = Array.isArray(current) ? current : [];
    const retained = existing.filter(value => !/^https:\/\/example\.com\//.test(String(value)));
    return [...retained, ...uploaded];
  }

  return { defaultsFor, exampleFor, mergeUploadedUrls, selectOptionsFor, uploadAcceptFor };
});
