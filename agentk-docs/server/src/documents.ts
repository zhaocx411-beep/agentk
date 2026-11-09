import fs from 'node:fs/promises';

import * as z from 'zod/v4';

export const SEARCH_DOCUMENTS_FORMAT = 'agentk-docs-search-v1' as const;

export const SEARCH_AREAS = [
  'intro',
  'concepts',
  'client',
  'worker',
  'reference',
  'examples',
  'capabilities',
  'platform',
] as const;

export const SEARCH_KINDS = [
  'overview',
  'guide',
  'concept',
  'example',
  'api',
  'catalog',
  'platform',
] as const;

export const SEARCH_LANGUAGES = ['go', 'python', 'java'] as const;

const sectionSchema = z.object({
  id: z.string().min(1),
  anchor: z.string().min(1).nullable(),
  headingPath: z.array(z.string()),
  markdown: z.string().min(1),
  codeTerms: z.array(z.string()),
});

const pageSchema = z.object({
  route: z.string().startsWith('/'),
  markdownPath: z.string().startsWith('/').endsWith('.md'),
  title: z.string().min(1),
  description: z.string(),
  sidebarGroup: z.string().min(1),
  area: z.enum(SEARCH_AREAS),
  kind: z.enum(SEARCH_KINDS),
  language: z.enum(SEARCH_LANGUAGES).nullable(),
  sections: z.array(sectionSchema).min(1),
});

const documentsSchema = z.object({
  format: z.literal(SEARCH_DOCUMENTS_FORMAT),
  pages: z.array(pageSchema).min(1),
});

export type SearchArea = (typeof SEARCH_AREAS)[number];
export type SearchKind = (typeof SEARCH_KINDS)[number];
export type SearchLanguage = (typeof SEARCH_LANGUAGES)[number];
export type SearchSection = z.infer<typeof sectionSchema>;
export type SearchPage = z.infer<typeof pageSchema>;
export type SearchDocuments = z.infer<typeof documentsSchema>;

function describe(error: z.ZodError): string {
  return error.issues
    .map((issue) => {
      const location = issue.path.join('.');
      return location ? `${location}: ${issue.message}` : issue.message;
    })
    .join('; ');
}

export function validateSearchDocuments(value: unknown): SearchDocuments {
  const result = documentsSchema.safeParse(value);
  if (!result.success) {
    throw new Error(`Invalid search documents: ${describe(result.error)}`);
  }
  return result.data;
}

export async function loadSearchDocuments(
  filePath: string,
): Promise<SearchDocuments> {
  let parsed: unknown;
  try {
    parsed = JSON.parse(await fs.readFile(filePath, 'utf8')) as unknown;
  } catch (error) {
    throw new Error(`Unable to read search documents from ${filePath}`, {
      cause: error,
    });
  }
  return validateSearchDocuments(parsed);
}
