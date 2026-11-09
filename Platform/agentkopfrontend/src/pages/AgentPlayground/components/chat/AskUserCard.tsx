/**
 * ask_user 问卷卡片
 * 模型通过 ask_user 工具发起提问时展示：用户选择选项并提交（POST /answers），
 * 提交成功后会话继续执行；若用户直接在输入框打字，则视为接管，问卷被撤下。
 */
import { useEffect, useMemo, useState } from 'react';
import { Button, Tag } from 'antd';
import { CheckOutlined, QuestionCircleOutlined } from '@ant-design/icons';
import type { AskUserAnswer, AskUserCardStatus, AskUserQuestion } from '../../types';
import styles from '../../AgentPlayground.module.less';

interface AskUserCardProps {
  questions: AskUserQuestion[];
  /** 提交答案（父级负责调用 /answers 接口），返回是否成功 */
  onSubmit?: (answers: AskUserAnswer[]) => Promise<boolean>;
  /** 历史回答；存在时按只读的已提交状态展示 */
  answers?: AskUserAnswer[];
  status?: AskUserCardStatus;
}

function selectionsFromAnswers(answers: AskUserAnswer[]): Record<string, string[]> {
  return Object.fromEntries(answers.map((answer) => [answer.questionId, answer.selected]));
}

function equalSelections(left: Record<string, string[]>, right: Record<string, string[]>): boolean {
  const keys = Object.keys(left);
  if (keys.length !== Object.keys(right).length) return false;
  return keys.every((key) => {
    const leftValues = left[key] ?? [];
    const rightValues = right[key] ?? [];
    return (
      leftValues.length === rightValues.length &&
      leftValues.every((value, index) => value === rightValues[index])
    );
  });
}

function AskUserCard({ questions, onSubmit, answers, status = 'pending' }: AskUserCardProps) {
  const readOnly = status !== 'pending';
  // key = questionId，value = 选中的选项文案列表
  const [selections, setSelections] = useState<Record<string, string[]>>(() =>
    selectionsFromAnswers(answers ?? []),
  );
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!readOnly) return;
    const next = selectionsFromAnswers(answers ?? []);
    setSelections((current) => (equalSelections(current, next) ? current : next));
  }, [answers, readOnly]);

  const toggleOption = (q: AskUserQuestion, option: string) => {
    if (readOnly || submitting) return;
    setSelections((prev) => {
      const current = prev[q.id] ?? [];
      const next = q.multiSelect
        ? current.includes(option)
          ? current.filter((o) => o !== option)
          : [...current, option]
        : [option];
      return { ...prev, [q.id]: next };
    });
  };

  // 所有题目都至少选了一项才允许提交
  const allAnswered = useMemo(
    () => questions.every((q) => (selections[q.id] ?? []).length > 0),
    [questions, selections],
  );

  const handleSubmit = async () => {
    if (!onSubmit || !allAnswered || submitting) return;
    setSubmitting(true);
    try {
      await onSubmit(
        questions.map((q) => ({ questionId: q.id, selected: selections[q.id] ?? [] })),
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className={styles.askCard}>
      <div className={styles.askHeader}>
        <QuestionCircleOutlined className={styles.askIcon} />
        <span>agent-k 想先确认几个细节</span>
      </div>

      <div className={styles.askBody}>
        {questions.map((q) => {
          const selected = selections[q.id] ?? [];
          return (
            <div key={q.id} className={styles.askQuestion}>
              <div className={styles.askQuestionTitle}>
                {q.label && (
                  <Tag className={styles.askLabel} color="blue">
                    {q.label}
                  </Tag>
                )}
                <span className={styles.askQuestionText}>{q.question}</span>
                {q.multiSelect && <span className={styles.askMultiHint}>可多选</span>}
              </div>
              <div className={styles.askOptions}>
                {q.options.map((option) => {
                  const checked = selected.includes(option);
                  return (
                    <button
                      key={option}
                      type="button"
                      className={`${styles.askOption} ${checked ? styles.askOptionSelected : ''}`}
                      onClick={() => toggleOption(q, option)}
                      disabled={readOnly || submitting}
                    >
                      {checked && <CheckOutlined className={styles.askOptionCheck} />}
                      {option}
                    </button>
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>

      <div className={styles.askFooter}>
        {status === 'pending' ? (
          <>
            <Button
              type="primary"
              size="small"
              onClick={handleSubmit}
              disabled={!allAnswered}
              loading={submitting}
            >
              提交回答
            </Button>
            <span className={styles.askFooterHint}>也可以直接在下方输入消息接管对话</span>
          </>
        ) : (
          <span className={styles.askSubmitted}>
            <CheckOutlined />
            {status === 'submitted' ? '已提交' : '已通过文字回复'}
          </span>
        )}
      </div>
    </div>
  );
}

export default AskUserCard;
