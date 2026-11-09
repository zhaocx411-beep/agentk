import type { Dispatch, SetStateAction } from 'react';

export interface ClothingDupeMedia {
  id: string;
  name: string;
  url: string;
}

export type ClothingDupeMediaField = 'sourceVideo' | 'productImages' | 'modelImages' | 'bgm';

export const MEDIA_KIND_BY_FIELD: Record<ClothingDupeMediaField, 'video' | 'audio' | 'image'> = {
  sourceVideo: 'video',
  bgm: 'audio',
  productImages: 'image',
  modelImages: 'image',
};

export const UPLOAD_TYPE_HINT: Record<'video' | 'audio' | 'image', string> = {
  video: '请上传视频文件',
  audio: '请上传音频文件',
  image: '请上传图片文件',
};

export const MEDIA_LIMIT_BY_FIELD: Record<ClothingDupeMediaField, number> = {
  sourceVideo: 1,
  productImages: 5,
  modelImages: 5,
  bgm: 1,
};

export const EMPTY_UPLOADING: Record<ClothingDupeMediaField, number> = {
  sourceVideo: 0,
  productImages: 0,
  modelImages: 0,
  bgm: 0,
};

type MediaSetter = Dispatch<SetStateAction<ClothingDupeMedia[]>>;

export function mediaSetters(params: {
  setSourceVideo: MediaSetter;
  setProductImages: MediaSetter;
  setModelImages: MediaSetter;
  setBgm: MediaSetter;
}): Record<ClothingDupeMediaField, MediaSetter> {
  return {
    sourceVideo: params.setSourceVideo,
    productImages: params.setProductImages,
    modelImages: params.setModelImages,
    bgm: params.setBgm,
  };
}
