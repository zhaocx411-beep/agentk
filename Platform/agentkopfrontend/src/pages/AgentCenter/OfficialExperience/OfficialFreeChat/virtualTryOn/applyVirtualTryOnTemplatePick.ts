import type { ChatSuggestionItem } from '@/pages/AgentPlayground/components/chat/ChatEmptyState';
import type { VirtualTryOnUserInput } from '@/pages/AgentPlayground/types';
import type { CreationTemplateBundle } from '../../studio/lib/templateTasks';
import type { VirtualTryOnComposerModel } from './useVirtualTryOnComposer';

export interface VirtualTryOnSuggestionItem extends ChatSuggestionItem {
  userInput?: VirtualTryOnUserInput;
}

function asUrlList(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is string => typeof item === 'string' && !!item.trim()).map((item) => item.trim());
}

/** TEMPLATE extraFields → 虚拟试穿 userInput */
export function userInputFromTemplateExtra(
  extra: Record<string, unknown> | undefined,
): VirtualTryOnUserInput | undefined {
  if (!extra) return undefined;
  const productImageUrls = asUrlList(extra.productImageUrls);
  if (productImageUrls.length === 0) return undefined;
  const modelImageUrl = typeof extra.modelImageUrl === 'string' ? extra.modelImageUrl.trim() : '';
  return {
    productImageUrls,
    ...(modelImageUrl ? { modelImageUrl } : {}),
  };
}

export function suggestionFromTryOnBundle(bundle: CreationTemplateBundle): VirtualTryOnSuggestionItem {
  const extra = bundle.extraFields;
  const label =
    (typeof extra.title === 'string' && extra.title.trim()) ||
    bundle.template.name ||
    bundle.sample.title;
  const prompt =
    (typeof extra.video_prompt === 'string' && extra.video_prompt.trim()) ||
    bundle.template.prompt ||
    label;
  const userInput = userInputFromTemplateExtra(extra);
  const extraUrls = [
    ...(userInput?.productImageUrls ?? []),
    ...(userInput?.modelImageUrl ? [userInput.modelImageUrl] : []),
  ];
  return {
    label,
    prompt,
    referUrls: [...new Set([...(bundle.referUrls ?? []), ...extraUrls])],
    userInput,
  };
}

export function applyVirtualTryOnTemplatePick(
  item: VirtualTryOnSuggestionItem,
  setInputText: (value: string) => void,
  composer: VirtualTryOnComposerModel,
): void {
  setInputText(item.prompt);
  if (item.userInput) {
    composer.applyTemplate(item.prompt, item.userInput);
  }
}
