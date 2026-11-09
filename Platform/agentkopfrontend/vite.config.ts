import { defineConfig, loadEnv, type Plugin, type ViteDevServer } from 'vite';
import react from '@vitejs/plugin-react';
import { createSvgIconsPlugin } from 'vite-plugin-svg-icons';
import path from 'path';
import { Readable } from 'node:stream';
import type { ReadableStream as NodeWebReadableStream } from 'node:stream/web';
import {
  isMediaDownloadProxyHost,
  MEDIA_DOWNLOAD_DEV_PATH,
} from './src/services/mediaDownloadProxy';

/** 勿放在 /api 下，否则 dev 会被 vite proxy 转发到后端（后端无此路由 → 502） */
const MEDIA_DOWNLOAD_PATH = MEDIA_DOWNLOAD_DEV_PATH;
const SENSITIVE_PATH_SEGMENTS = new Set(['.git', '.hg', '.svn']);

function sensitivePathGuardPlugin(): Plugin {
  return {
    name: 'sensitive-path-guard',
    enforce: 'pre',
    configureServer(server: ViteDevServer) {
      server.middlewares.use((req, res, next) => {
        const rawPath = (req.url || '/').split('?', 1)[0];
        let pathname: string;
        try {
          pathname = decodeURIComponent(rawPath).replaceAll('\\', '/');
        } catch {
          res.statusCode = 400;
          res.end('Bad Request');
          return;
        }
        const segments = pathname.toLowerCase().split('/').filter(Boolean);
        if (segments.some((segment) => SENSITIVE_PATH_SEGMENTS.has(segment))) {
          res.statusCode = 404;
          res.setHeader('Content-Type', 'text/plain; charset=utf-8');
          res.end('Not Found');
          return;
        }
        next();
      });
    },
  };
}

function safeDownloadName(name: string): string {
  const cleaned = name.replace(/[\r\n"]/g, '_').trim();
  return cleaned.slice(0, 180) || 'download';
}

/** 开发态把 CDN 文件同源转发出去，才能像数据集 export 一样触发下载而不是跳转 */
function mediaDownloadProxyPlugin(): Plugin {
  return {
    name: 'media-download-proxy',
    configureServer(server: ViteDevServer) {
      server.middlewares.use(async (req, res, next) => {
        const rawUrl = req.url || '';
        if (!rawUrl.startsWith(MEDIA_DOWNLOAD_PATH)) {
          next();
          return;
        }
        if (req.method !== 'GET' && req.method !== 'HEAD') {
          res.statusCode = 405;
          res.end('Method Not Allowed');
          return;
        }
        let target: URL;
        try {
          const parsed = new URL(rawUrl, 'http://localhost');
          target = new URL(parsed.searchParams.get('url') || '');
        } catch {
          res.statusCode = 400;
          res.end('invalid url');
          return;
        }
        if (!/^https?:$/i.test(target.protocol) || !isMediaDownloadProxyHost(target.hostname)) {
          res.statusCode = 400;
          res.end('url not allowed');
          return;
        }
        const filename = safeDownloadName(
          new URL(rawUrl, 'http://localhost').searchParams.get('filename') ||
            target.pathname.split('/').pop() ||
            'download',
        );
        try {
          const range = typeof req.headers.range === 'string' ? req.headers.range : '';
          const wantHead = req.method === 'HEAD';
          let upstream = await fetch(target, {
            method: wantHead ? 'HEAD' : 'GET',
            headers: range ? { Range: range } : undefined,
          });
          if (wantHead && (!upstream.ok || !upstream.headers.get('content-length'))) {
            upstream = await fetch(target, {
              method: 'GET',
              headers: { Range: range || 'bytes=0-0' },
            });
          }
          if (!upstream.ok && upstream.status !== 206) {
            res.statusCode = upstream.status;
            res.end('upstream failed');
            return;
          }
          const contentType = upstream.headers.get('content-type') || 'application/octet-stream';
          const contentLength = upstream.headers.get('content-length');
          const contentRange = upstream.headers.get('content-range');
          res.statusCode = upstream.status;
          res.setHeader('Content-Type', contentType);
          res.setHeader('Content-Disposition', `attachment; filename="${filename}"`);
          res.setHeader('Cache-Control', 'no-store');
          if (contentLength) res.setHeader('Content-Length', contentLength);
          if (contentRange) res.setHeader('Content-Range', contentRange);
          if (wantHead) {
            await upstream.body?.cancel?.();
            res.end();
            return;
          }
          if (!upstream.body) {
            res.end(Buffer.from(await upstream.arrayBuffer()));
            return;
          }
          Readable.fromWeb(upstream.body as NodeWebReadableStream).pipe(res);
        } catch {
          res.statusCode = 502;
          res.end('proxy failed');
        }
      });
    },
  };
}

export default defineConfig(({ mode }) => {
  // 加载环境变量
  const env = loadEnv(mode, process.cwd(), '');

  // base 路径优先级：流水线注入系统环境变量 CDN URL > .env 中的 VITE_CDN_URL > 本地开发 '/' > .env 中的 VITE_ROUTER_BASE
  const isDev = mode === 'development';
  const base =
    process.env.VITE_CDN_URL || env.VITE_CDN_URL || (isDev ? '/' : env.VITE_ROUTER_BASE || '/');

  return {
    base,
    plugins: [
      sensitivePathGuardPlugin(),
      mediaDownloadProxyPlugin(),
      react(),
      // SVG 图标方案
      createSvgIconsPlugin({
        iconDirs: [path.resolve(process.cwd(), 'src/assets/icons')],
        symbolId: 'icon-[dir]-[name]',
      }),
    ],
    resolve: {
      alias: {
        '@': path.resolve(__dirname, 'src'),
      },
    },
    css: {
      modules: {
        // CSS Modules 配置
        localsConvention: 'camelCaseOnly',
        generateScopedName: '[name]__[local]___[hash:base64:5]',
      },
      preprocessorOptions: {
        less: {
          // 全局 less 变量与 mixin 注入（mixin 均为带括号定义，不会输出重复 CSS）
          additionalData:
            '@import "@/styles/variables.less"; @import (reference) "@/styles/mixins.less";',
          javascriptEnabled: true,
        },
      },
    },
    server: {
      port: Number(env.VITE_PORT) || 3000,
      // 开发服务默认仅本机可访问，避免源码与仓库元数据暴露到办公网。
      host: env.VITE_DEV_HOST || '127.0.0.1',
      strictPort: true,
      fs: {
        strict: true,
        deny: ['.env', '.env.*', '*.{crt,pem}', '**/.git/**', '**/.hg/**', '**/.svn/**'],
      },
      proxy: {
        '/api': {
          target: env.VITE_PROXY_TARGET || env.VITE_API_BASE_URL,
          changeOrigin: true,
          rewrite: (path) => path.replace(/^\/api/, ''),
          secure: false,
          // configure: (proxy) => {
          //     proxy.on('proxyReq', (proxyReq) => {
          //       // 移除 Origin，避免后端 CORS 校验
          //       proxyReq.removeHeader('Origin')
          //       proxyReq.removeHeader('Referer')
          //     })
          //   },
        },
      },
    },
    build: {
      outDir: 'dist',
      sourcemap: mode !== 'production',
      // 分包策略
      rollupOptions: {
        output: {
          manualChunks: {
            vendor: ['react', 'react-dom', 'react-router-dom'],
            utils: ['axios', 'dayjs', 'lodash-es'],
          },
        },
      },
    },
  };
});
