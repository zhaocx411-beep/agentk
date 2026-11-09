import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';

const rootDir = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(rootDir, 'src'),
    },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'html', 'json-summary'],
      reportsDirectory: 'coverage',
      include: [
        'src/pages/AgentCenter/AgentDetail/lib/toolDefaultsUtils.ts',
        'src/pages/AgentPlayground/api.ts',
        'src/pages/AgentPlayground/hooks/useSSE.ts',
        'src/pages/AgentPlayground/utils/sessionStreamResume.ts',
        'src/pages/ToolGallery/lib/runErrors.ts',
        'src/layouts/MainLayout/menuConfig.tsx',
        'src/router/routeRegistry.ts',
        'src/utils/profileYaml.ts',
      ],
      thresholds: {
        statements: 50,
        branches: 60,
        functions: 50,
        lines: 50,
      },
    },
  },
});
