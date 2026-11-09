import type { ChatSuggestion } from './ChatEmptyState';

export function getSuggestionLabel(item: ChatSuggestion): string {
  return typeof item === 'string' ? item : item.label;
}

export function getSuggestionPrompt(item: ChatSuggestion): string {
  return typeof item === 'string' ? item : item.prompt;
}
