#!/usr/bin/env node
import { loadPublicUrl } from './config.js';
import {
  SEARCH_AREAS,
  SEARCH_KINDS,
  SEARCH_LANGUAGES,
} from './documents.js';
import { renderSearchHits } from './mcp.js';
import { REPOSITORY_ROOT } from './paths.js';
import { openDocsRepository } from './repository.js';
import type { SearchRequest } from './search.js';

function usage(): string {
  return [
    'Usage: pnpm search:docs -- <query> [options]',
    '',
    'Options:',
    `  --area <${SEARCH_AREAS.join('|')}>`,
    `  --kind <${SEARCH_KINDS.join('|')}>`,
    `  --language <${SEARCH_LANGUAGES.join('|')}>`,
    '  --limit <1-20>',
  ].join('\n');
}

function takeValue(args: string[], index: number, flag: string): string {
  const value = args[index + 1];
  if (!value || value.startsWith('--')) {
    throw new Error(`${flag} requires a value`);
  }
  args.splice(index, 2);
  return value;
}

function enumOption<T extends string>(
  value: string,
  values: readonly T[],
  flag: string,
): T {
  if (!values.includes(value as T)) {
    throw new Error(`${flag} must be one of ${values.join(', ')}`);
  }
  return value as T;
}

function parseArgs(argv: string[]): SearchRequest {
  const args = [...argv];
  const request: {
    area?: (typeof SEARCH_AREAS)[number];
    kind?: (typeof SEARCH_KINDS)[number];
    language?: (typeof SEARCH_LANGUAGES)[number];
    limit?: number;
  } = {};

  for (let index = 0; index < args.length; ) {
    const flag = args[index];
    if (flag === '--') {
      args.splice(index, 1);
      continue;
    }
    if (!flag?.startsWith('--')) {
      index += 1;
      continue;
    }
    const value = takeValue(args, index, flag);
    switch (flag) {
      case '--area':
        request.area = enumOption(value, SEARCH_AREAS, flag);
        break;
      case '--kind':
        request.kind = enumOption(value, SEARCH_KINDS, flag);
        break;
      case '--language':
        request.language = enumOption(value, SEARCH_LANGUAGES, flag);
        break;
      case '--limit':
        request.limit = Number(value);
        break;
      default:
        throw new Error(`Unknown option ${flag}`);
    }
  }

  const query = args.join(' ').trim();
  if (!query) {
    throw new Error(usage());
  }
  return { query, ...request };
}

async function main(): Promise<void> {
  const request = parseArgs(process.argv.slice(2));
  const repository = await openDocsRepository(
    REPOSITORY_ROOT,
    loadPublicUrl(),
  );
  console.log(renderSearchHits(repository.search(request)));
}

main().catch((error: unknown) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
});
