/**
 * WorkflowRunner - 运行结果解析工具
 * 从 Pipeline 输出中拆分媒体与文本字段，并处理后端节点重复返回的问题
 */
import type { CatalogPipeline, WorkflowRun } from '@/pages/AgentPlayground/types';
import { isImageUrl, isVideoUrl } from '@/utils/media';

export interface ExtractedMedia {
  videos: string[];
  images: string[];
  /** 去除 media 后剩余的纯文本字段 */
  textFields: Record<string, unknown>;
}

/** 从 output 对象中提取所有 video / image URL，其余字段原样保留 */
export function extractMediaUrls(output: unknown): ExtractedMedia {
  const videos: string[] = [];
  const images: string[] = [];
  const textFields: Record<string, unknown> = {};

  if (!output || typeof output !== 'object') {
    return { videos, images, textFields };
  }

  const obj = output as Record<string, unknown>;

  for (const [key, val] of Object.entries(obj)) {
    if (typeof val === 'string') {
      if (isVideoUrl(val)) {
        videos.push(val);
      } else if (isImageUrl(val)) {
        images.push(val);
      } else {
        textFields[key] = val;
      }
    } else if (Array.isArray(val)) {
      // 数组项中可能包含 videoUrl / imageUrl
      for (const item of val) {
        if (item && typeof item === 'object') {
          const rec = item as Record<string, unknown>;
          if (typeof rec.videoUrl === 'string' && isVideoUrl(rec.videoUrl)) {
            videos.push(rec.videoUrl);
          }
          if (typeof rec.imageUrl === 'string' && isImageUrl(rec.imageUrl)) {
            images.push(rec.imageUrl);
          }
        }
      }
      // 已作为媒体渲染的数组不再重复进文本字段
      const hasMedia = val.some(
        (item) =>
          item &&
          typeof item === 'object' &&
          ('videoUrl' in (item as object) || 'imageUrl' in (item as object)),
      );
      if (!hasMedia) {
        textFields[key] = val;
      }
    } else {
      textFields[key] = val;
    }
  }

  return { videos, images, textFields };
}

/**
 * 运行节点按 name 去重
 * 后端可能针对同一节点返回多条（idx=-1 的聚合行 + 具体行），优先保留带 output 的那条
 */
export function dedupeRunNodes(nodes: WorkflowRun['nodes']): WorkflowRun['nodes'] {
  const map = new Map<string, WorkflowRun['nodes'][0]>();
  for (const node of nodes) {
    const existing = map.get(node.name);
    if (!existing) {
      map.set(node.name, node);
    } else if (existing.output === undefined && node.output !== undefined) {
      map.set(node.name, node);
    }
  }
  return Array.from(map.values());
}

/** Pipeline 下拉展示描述：description 缺失时回退 name */
export function getPipelineDescription(pipeline: CatalogPipeline): string {
  return pipeline.description || pipeline.name;
}
