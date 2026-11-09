export function normalizeBaseUrl(baseUrl: string): string {
  return baseUrl === '/' ? '/' : `/${baseUrl.replace(/^\/|\/$/g, '')}/`;
}

export function stripBaseUrl(
  pathname: string,
  baseUrl: string,
): string | undefined {
  const normalizedPath = pathname.startsWith('/') ? pathname : `/${pathname}`;
  const normalizedBase = normalizeBaseUrl(baseUrl);
  if (normalizedBase === '/') {
    return normalizedPath;
  }

  const basePath = normalizedBase.slice(0, -1);
  if (normalizedPath === basePath || normalizedPath === normalizedBase) {
    return '/';
  }
  if (!normalizedPath.startsWith(`${basePath}/`)) {
    return undefined;
  }
  return normalizedPath.slice(basePath.length) || '/';
}

export function toPageUrl(route: string, siteUrl: URL): string {
  return new URL(route === '/' ? '' : route.replace(/^\//, ''), siteUrl).href;
}

export function withBaseUrl(resourcePath: string, baseUrl: string): string {
  const normalizedPath = resourcePath.startsWith('/')
    ? resourcePath
    : `/${resourcePath}`;
  const normalizedBase = normalizeBaseUrl(baseUrl);
  if (normalizedBase === '/') {
    return normalizedPath;
  }
  return `${normalizedBase.slice(0, -1)}${normalizedPath}`;
}
