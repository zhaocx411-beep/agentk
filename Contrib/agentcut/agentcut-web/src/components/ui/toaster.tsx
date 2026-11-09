import { AlertTriangle, CheckCircle2, Info, X, XCircle } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useToastStore } from "@/store/toastStore";

const ICONS = {
  info: Info,
  success: CheckCircle2,
  warning: AlertTriangle,
  error: XCircle,
};

export function Toaster() {
  const messages = useToastStore((s) => s.messages);
  const dismiss = useToastStore((s) => s.dismiss);

  return (
    <div className="pointer-events-none fixed right-4 top-16 z-50 flex w-80 flex-col gap-2">
      {messages.map((item) => {
        const Icon = ICONS[item.tone];
        return (
          <div
            key={item.id}
            role="status"
            className={cn(
              "pointer-events-auto flex items-start gap-2 rounded-md border bg-background p-3 text-sm shadow-lg",
              item.tone === "error" && "border-destructive/50",
              item.tone === "warning" && "border-amber-500/50",
            )}
          >
            <Icon
              className={cn(
                "mt-0.5 size-4 shrink-0 text-muted-foreground",
                item.tone === "success" && "text-emerald-500",
                item.tone === "warning" && "text-amber-500",
                item.tone === "error" && "text-destructive",
              )}
            />
            <span className="flex-1">{item.message}</span>
            <Button
              variant="ghost"
              size="icon-sm"
              className="-mr-1 -mt-1 size-6"
              onClick={() => dismiss(item.id)}
            >
              <X className="size-3.5" />
            </Button>
          </div>
        );
      })}
    </div>
  );
}
