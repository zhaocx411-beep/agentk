import { assetKey, type MangaCanvasModel } from './foldMangaCanvas';

export interface CanvasLink {
  d: string;
  pending: boolean;
  x1: number;
  y1: number;
  x2: number;
  y2: number;
}

export function assetNodeId(uri: string, index: number): string {
  return `n-${index}-${assetKey(uri).replace(/[^a-zA-Z0-9_-]/g, '').slice(-12)}`;
}

export function pendingNodeId(id: string): string {
  return `p-${id.replace(/[^a-zA-Z0-9_-]/g, '_')}`;
}

export function assetNodeIds(assets: MangaCanvasModel['assets']): Map<string, string> {
  const idByKey = new Map<string, string>();
  assets.forEach((asset, index) => {
    idByKey.set(assetKey(asset.uri), assetNodeId(asset.uri, index));
  });
  return idByKey;
}

export function collectCanvasLinks(
  inner: HTMLDivElement,
  model: MangaCanvasModel,
  ids: Map<string, string>,
): CanvasLink[] {
  const innerRect = inner.getBoundingClientRect();
  const next: CanvasLink[] = [];
  const positionOf = (id: string) => {
    const el = inner.querySelector<HTMLElement>(`[data-node="${CSS.escape(id)}"]`);
    if (!el) return null;
    const rect = el.getBoundingClientRect();
    return {
      x1: rect.right - innerRect.left,
      y1: rect.top - innerRect.top + Math.min(60, rect.height / 2),
      x2: rect.left - innerRect.left,
      y2: rect.top - innerRect.top + Math.min(60, rect.height / 2),
    };
  };
  const curve = (fromId: string, toId: string, pending: boolean) => {
    const a = positionOf(fromId);
    const b = positionOf(toId);
    if (!a || !b) return;
    const bend = Math.max(48, (b.x2 - a.x1) / 2);
    next.push({
      pending,
      x1: a.x1,
      y1: a.y1,
      x2: b.x2,
      y2: b.y2,
      d: `M ${a.x1} ${a.y1} C ${a.x1 + bend} ${a.y1}, ${b.x2 - bend} ${b.y2}, ${b.x2} ${b.y2}`,
    });
  };
  for (const edge of model.edges) {
    const fromId = ids.get(assetKey(edge.from));
    const toId = ids.get(assetKey(edge.to));
    if (fromId && toId) curve(fromId, toId, false);
  }
  for (const item of model.pending) {
    for (const input of item.inputs) {
      const fromId = ids.get(assetKey(input));
      if (fromId) curve(fromId, pendingNodeId(item.id), true);
    }
  }
  return next;
}
