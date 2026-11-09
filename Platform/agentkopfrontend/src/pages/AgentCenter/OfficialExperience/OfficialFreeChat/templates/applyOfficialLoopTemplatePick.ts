/**
 * 选中 agent-loop 灵感模版：写入 prompt，并将 referImages / fileUrl / audioUrl 等预填到附件栏
 */
import type { ChatSuggestionItem } from '@/pages/AgentPlayground/components/chat/ChatEmptyState';
import type { ChatAttachmentBarHandle } from '@/pages/AgentPlayground/components/chat/ChatAttachmentBar';

export function applyOfficialLoopTemplatePick(
  item: ChatSuggestionItem,
  setInputText: (value: string) => void,
  attachmentBar: ChatAttachmentBarHandle | null | undefined,
): void {
  setInputText(item.prompt);
  attachmentBar?.setFromUrls(item.referUrls ?? [], { replace: true });
}
