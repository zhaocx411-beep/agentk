import { Loader2, SendHorizontal } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";

export interface AskQuestionData {
  id: string;
  label?: string;
  question: string;
  options: string[];
  multiSelect?: boolean;
}

export interface PendingAsk {
  toolCallId: string;
  questions: AskQuestionData[];
}

export function AskUserForm({
  ask,
  submitting,
  onSubmit,
}: {
  ask: PendingAsk;
  submitting: boolean;
  onSubmit(answers: { questionId: string; selected: string[] }[]): void;
}) {
  const [selected, setSelected] = useState<Record<string, string[]>>({});
  const complete = ask.questions.every((question) => (selected[question.id]?.length ?? 0) > 0);

  return (
    <div className="mr-auto w-[90%] rounded-md border border-primary/30 bg-primary/5 p-3 text-sm">
      <strong>需要你的确认</strong>
      <div className="mt-2 space-y-3">
        {ask.questions.map((question) => (
          <fieldset key={question.id}>
            <legend className="mb-1.5 text-xs font-medium">
              {question.label || question.question}
            </legend>
            <div className="space-y-1">
              {question.options.map((option) => {
                const values = selected[question.id] ?? [];
                const checked = values.includes(option);
                return (
                  <label key={option} className="flex cursor-pointer items-center gap-2 text-xs">
                    <input
                      type={question.multiSelect ? "checkbox" : "radio"}
                      name={question.id}
                      checked={checked}
                      onChange={() =>
                        setSelected((current) => ({
                          ...current,
                          [question.id]: question.multiSelect
                            ? checked
                              ? values.filter((value) => value !== option)
                              : [...values, option]
                            : [option],
                        }))
                      }
                    />
                    {option}
                  </label>
                );
              })}
            </div>
          </fieldset>
        ))}
      </div>
      <Button
        size="sm"
        className="mt-3"
        disabled={!complete || submitting}
        onClick={() =>
          onSubmit(
            ask.questions.map((question) => ({
              questionId: question.id,
              selected: selected[question.id] ?? [],
            })),
          )
        }
      >
        {submitting ? <Loader2 className="animate-spin" /> : <SendHorizontal />}
        提交选择
      </Button>
    </div>
  );
}
