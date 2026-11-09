import type {
  LoadContext,
  Plugin,
} from '@docusaurus/types';

const DEFAULT_TARGET = 'http://127.0.0.1:8080';
type WebpackResult = Exclude<
  ReturnType<NonNullable<Plugin['configureWebpack']>>,
  void
>;

export default function capabilityProxy(context: LoadContext): Plugin<void> {
  const publicPath = `${context.baseUrl.replace(/\/$/, '')}/_agentk/capabilities`;

  return {
    name: 'agentk-capability-dev-proxy',
    configureWebpack(_config, isServer) {
      if (isServer || process.env.NODE_ENV !== 'development') {
        return undefined;
      }

      return {
        devServer: {
          proxy: [
            {
              context: [publicPath],
              target: process.env.AGENTK_DOCS_API_TARGET ?? DEFAULT_TARGET,
              changeOrigin: false,
            },
          ],
        },
      } as unknown as WebpackResult;
    },
  };
}
