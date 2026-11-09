import fs from 'node:fs/promises';
import path from 'node:path';

import express, { type Express, type RequestHandler } from 'express';

const HASHED_ASSET = /\/assets\/(?:css|js)\/[^/]*\.[a-f0-9]{8,}\.[^/]+$/i;

function setCacheHeaders(responsePath: string, response: express.Response): void {
  if (HASHED_ASSET.test(responsePath)) {
    response.setHeader('Cache-Control', 'public, max-age=31536000, immutable');
    return;
  }
  if (
    responsePath.endsWith('.html') ||
    responsePath.endsWith('.md') ||
    responsePath.endsWith('/llms.txt') ||
    responsePath.endsWith('/llms-full.txt')
  ) {
    response.setHeader('Cache-Control', 'no-cache');
  }
}

export async function openStaticDocs(buildRoot: string): Promise<{
  handler: RequestHandler;
  notFound: RequestHandler;
}> {
  const realBuildRoot = await fs.realpath(buildRoot);
  const notFoundPath = path.join(realBuildRoot, '404.html');
  await fs.access(notFoundPath);

  const expressStatic = express.static(realBuildRoot, {
    index: 'index.html',
    fallthrough: true,
    etag: true,
    lastModified: true,
    redirect: true,
    setHeaders: (response, responsePath) =>
      setCacheHeaders(responsePath, response),
  });
  const handler: RequestHandler = async (request, response, next) => {
    if (!['GET', 'HEAD'].includes(request.method)) {
      next();
      return;
    }
    let pathname: string;
    try {
      pathname = decodeURIComponent(
        new URL(request.url, 'http://localhost').pathname,
      );
    } catch {
      response.status(400).type('text/plain').send('Bad Request');
      return;
    }
    if (pathname.includes('\\') || pathname.includes('\0')) {
      response.status(400).type('text/plain').send('Bad Request');
      return;
    }
    const candidate = path.resolve(realBuildRoot, `.${pathname}`);
    const relative = path.relative(realBuildRoot, candidate);
    if (relative.startsWith('..') || path.isAbsolute(relative)) {
      response.status(403).type('text/plain').send('Forbidden');
      return;
    }
    try {
      const realCandidate = await fs.realpath(candidate);
      const realRelative = path.relative(realBuildRoot, realCandidate);
      if (realRelative.startsWith('..') || path.isAbsolute(realRelative)) {
        response.status(403).type('text/plain').send('Forbidden');
        return;
      }
    } catch {
      next();
      return;
    }
    expressStatic(request, response, next);
  };
  const notFound: RequestHandler = (request, response, next) => {
    if (!['GET', 'HEAD'].includes(request.method)) {
      next();
      return;
    }
    response.status(404).setHeader('Cache-Control', 'no-cache');
    response.sendFile(notFoundPath);
  };
  return { handler, notFound };
}

export function mountStaticDocs(
  app: Express,
  staticDocs: Awaited<ReturnType<typeof openStaticDocs>>,
  baseUrl: string,
): void {
  const mountPath = baseUrl === '/' ? '/' : baseUrl.replace(/\/+$/, '');
  if (mountPath !== '/') {
    app.use((request, response, next) => {
      if (
        ['GET', 'HEAD'].includes(request.method) &&
        request.path === mountPath
      ) {
        response.redirect(308, `${mountPath}/`);
        return;
      }
      next();
    });
  }
  app.use(mountPath, staticDocs.handler);
  app.use(mountPath, staticDocs.notFound);
}
