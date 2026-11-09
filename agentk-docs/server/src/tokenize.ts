const CODE_IDENTIFIER =
  /[\p{L}_$][\p{L}\p{N}_$]*(?:[./_:#-][\p{L}\p{N}_$-]+)*/gu;
const segmenter = new Intl.Segmenter('zh-Hans', {granularity: 'word'});

export function normalizeText(value: string): string {
  return value.normalize('NFKC').toLocaleLowerCase('en-US');
}

function splitIdentifier(identifier: string): string[] {
  const parts: string[] = [identifier];
  for (const separated of identifier.split(/[./_:#-]+/u)) {
    if (!separated) {
      continue;
    }
    parts.push(separated);
    parts.push(
      ...separated
        .replace(/([\p{Ll}\p{N}])(\p{Lu})/gu, '$1 $2')
        .replace(/(\p{Lu}+)(\p{Lu}\p{Ll})/gu, '$1 $2')
        .split(/\s+/u),
    );
  }
  return parts.filter(Boolean);
}

export function identifierTokens(value: string): string[] {
  const tokens = new Set<string>();
  for (const match of value.normalize('NFKC').matchAll(CODE_IDENTIFIER)) {
    for (const part of splitIdentifier(match[0])) {
      tokens.add(normalizeText(part));
    }
  }
  return [...tokens];
}

export function tokenize(value: string): string[] {
  const normalized = value.normalize('NFKC');
  const tokens = new Set<string>(identifierTokens(normalized));
  for (const segment of segmenter.segment(normalized)) {
    if (segment.isWordLike) {
      tokens.add(normalizeText(segment.segment));
    }
  }
  return [...tokens].filter(Boolean);
}
