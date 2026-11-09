/**
 * 虚拟试穿 Agent：维护发消息所需的 userInput（商品图 + 可选模特图）
 */
import { useCallback, useMemo, useState } from 'react';
import type { SendMessageOptions, VirtualTryOnUserInput } from '@/pages/AgentPlayground/types';

export interface VirtualTryOnMediaItem {
  id: string;
  url: string;
  name?: string;
}

function urlToItem(url: string, index: number): VirtualTryOnMediaItem {
  const tail = url.split('/').pop() || `image-${index}`;
  return { id: `${index}-${tail}`, url, name: tail };
}

function itemsToUrls(items: VirtualTryOnMediaItem[]): string[] {
  return items.map((item) => item.url.trim()).filter(Boolean);
}

export function useVirtualTryOnComposer(_enabled: boolean) {
  const [productImages, setProductImages] = useState<VirtualTryOnMediaItem[]>([]);
  const [modelImage, setModelImage] = useState<VirtualTryOnMediaItem[]>([]);
  const [isUploading, setIsUploading] = useState(false);

  const userInput = useMemo<VirtualTryOnUserInput | undefined>(() => {
    if (!_enabled) return undefined;
    const productImageUrls = itemsToUrls(productImages);
    if (productImageUrls.length === 0) return undefined;
    const modelImageUrl = itemsToUrls(modelImage)[0];
    return {
      productImageUrls,
      ...(modelImageUrl ? { modelImageUrl } : {}),
    };
  }, [_enabled, modelImage, productImages]);

  const sendOptions = useMemo<SendMessageOptions | undefined>(() => {
    if (!userInput) return undefined;
    return { userInput };
  }, [userInput]);

  const applyTemplate = useCallback((_text: string, input: VirtualTryOnUserInput) => {
    setProductImages(input.productImageUrls.map(urlToItem));
    setModelImage(input.modelImageUrl ? [urlToItem(input.modelImageUrl, 0)] : []);
  }, []);

  const clear = useCallback(() => {
    setProductImages([]);
    setModelImage([]);
    setIsUploading(false);
  }, []);

  return {
    productImages,
    setProductImages,
    modelImage,
    setModelImage,
    isUploading,
    setIsUploading,
    userInput,
    sendOptions,
    applyTemplate,
    clear,
    isReady: !!userInput,
  };
}

export type VirtualTryOnComposerModel = ReturnType<typeof useVirtualTryOnComposer>;
