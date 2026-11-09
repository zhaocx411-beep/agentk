import { create } from "zustand";

export type ToastTone = "info" | "success" | "warning" | "error";

export interface ToastMessage {
  id: string;
  message: string;
  tone: ToastTone;
}

interface ToastState {
  messages: ToastMessage[];
  push(message: string, tone?: ToastTone, durationMs?: number): string;
  dismiss(id: string): void;
}

export const useToastStore = create<ToastState>((set) => ({
  messages: [],
  push(message, tone = "info", durationMs = 4000) {
    const id = crypto.randomUUID();
    set((s) => ({ messages: [...s.messages, { id, message, tone }] }));
    window.setTimeout(() => {
      set((s) => ({ messages: s.messages.filter((item) => item.id !== id) }));
    }, durationMs);
    return id;
  },
  dismiss(id) {
    set((s) => ({ messages: s.messages.filter((item) => item.id !== id) }));
  },
}));

export const toast = {
  info: (message: string) => useToastStore.getState().push(message, "info"),
  success: (message: string) => useToastStore.getState().push(message, "success"),
  warning: (message: string) => useToastStore.getState().push(message, "warning", 5500),
  error: (message: string) => useToastStore.getState().push(message, "error", 7000),
};
