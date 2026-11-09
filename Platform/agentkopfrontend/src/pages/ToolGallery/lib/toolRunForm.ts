/**
 * 校验工具运行必填参数
 */
import type { OfficialToolParameter } from '@/types/officialTools';
import {
  collectToolDefaults,
  isChildValueEmpty,
  isObjectItemDraft,
  isSchemaObjectArrayParam,
  isSchemaObjectParam,
  parseObjectArrayValue,
  parseObjectValue,
  type ParamFormValues,
} from '@/pages/AgentCenter/AgentDetail/lib/toolDefaultsUtils';
import { TOOL_GALLERY_TEXT } from '../mock';

const REFER_IMAGES_FIELD = 'referImages';
const FEATURE_VIDEO_FIELD = 'featureVideo';

export function isReferOrFeatureField(name: string): boolean {
  return name === REFER_IMAGES_FIELD || name === FEATURE_VIDEO_FIELD;
}

/** 在字段描述后补上二选一说明 */
export function annotateReferFeatureParam(param: OfficialToolParameter): OfficialToolParameter {
  const hint =
    param.name === REFER_IMAGES_FIELD
      ? TOOL_GALLERY_TEXT.referImagesPairHint
      : param.name === FEATURE_VIDEO_FIELD
        ? TOOL_GALLERY_TEXT.featureVideoPairHint
        : '';
  if (!hint) return param;
  if (param.description?.includes(hint)) return param;
  return {
    ...param,
    description: param.description ? `${param.description}；${hint}` : hint,
  };
}

export function hasReferOrFeaturePair(
  parameters: OfficialToolParameter[] | undefined,
): boolean {
  const names = new Set((parameters ?? []).map((item) => item.name));
  return names.has(REFER_IMAGES_FIELD) && names.has(FEATURE_VIDEO_FIELD);
}

function hasCollectedValue(param: OfficialToolParameter, values: ParamFormValues): boolean {
  const val = collectToolDefaults([param], values)[param.name];
  if (val === undefined || val === null) return false;
  if (typeof val === 'string') return Boolean(val.trim());
  if (Array.isArray(val)) return val.length > 0;
  return true;
}

/** kling_omni_video：referImages / featureVideo 至少填一个 */
function applyReferOrFeatureConstraint(
  parameters: OfficialToolParameter[],
  values: ParamFormValues,
  invalid: Set<string>,
) {
  const refer = parameters.find((item) => item.name === REFER_IMAGES_FIELD);
  const feature = parameters.find((item) => item.name === FEATURE_VIDEO_FIELD);
  if (!refer || !feature) return;
  if (hasCollectedValue(refer, values) || hasCollectedValue(feature, values)) return;
  invalid.add(REFER_IMAGES_FIELD);
  invalid.add(FEATURE_VIDEO_FIELD);
}

export function validateToolRunArgs(
  parameters: OfficialToolParameter[] | undefined,
  values: ParamFormValues,
): string | null {
  const invalid = getInvalidToolRunFields(parameters, values);
  if (invalid.size === 0) return null;
  if (invalid.has(REFER_IMAGES_FIELD) && invalid.has(FEATURE_VIDEO_FIELD)) {
    return TOOL_GALLERY_TEXT.referOrFeatureRequired;
  }
  const first = invalid.values().next().value;
  return first ? `请填写必填参数「${first}」` : null;
}

function appendInvalidField(
  invalid: Set<string>,
  param: OfficialToolParameter,
  child?: OfficialToolParameter,
  index?: number,
) {
  if (child && index !== undefined) {
    invalid.add(`${param.name}[${index + 1}].${child.name}`);
    return;
  }
  invalid.add(param.name);
}

/** 返回未通过的必填参数名 */
export function getInvalidToolRunFields(
  parameters: OfficialToolParameter[] | undefined,
  values: ParamFormValues,
): Set<string> {
  const invalid = new Set<string>();

  for (const param of parameters ?? []) {
    const raw = values[param.name];

    if (isSchemaObjectArrayParam(param)) {
      const items = parseObjectArrayValue(raw);
      if (param.required && items.length === 0) {
        appendInvalidField(invalid, param);
        continue;
      }
      items.forEach((item, index) => {
        if (!param.required && isObjectItemDraft(item, param.children ?? [])) return;
        for (const child of param.children ?? []) {
          if (!child.required) continue;
          if (isChildValueEmpty(child, item[child.name])) {
            appendInvalidField(invalid, param, child, index);
          }
        }
      });
      continue;
    }

    if (isSchemaObjectParam(param)) {
      const item = parseObjectValue(raw);
      if (param.required) {
        for (const child of param.children ?? []) {
          if (!child.required) continue;
          if (isChildValueEmpty(child, item[child.name])) {
            appendInvalidField(invalid, param, child);
          }
        }
      }
      continue;
    }

    if (!param.required) continue;
    const args = collectToolDefaults([param], values);
    const val = args[param.name];
    if (val === undefined || val === null) {
      appendInvalidField(invalid, param);
      continue;
    }
    if (typeof val === 'string' && !val.trim()) {
      appendInvalidField(invalid, param);
      continue;
    }
    if (Array.isArray(val) && val.length === 0) {
      appendInvalidField(invalid, param);
    }
  }

  applyReferOrFeatureConstraint(parameters ?? [], values, invalid);

  return invalid;
}

export {
  argsToParamFormValues,
  collectToolDefaults,
  initParamFormValues,
  type ParamFormValues,
} from '@/pages/AgentCenter/AgentDetail/lib/toolDefaultsUtils';
