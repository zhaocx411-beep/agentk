import {
  cloneElement,
  isValidElement,
  type ReactElement,
  type ReactNode,
} from 'react';

interface ExperienceRunStatusProps {
  isRunning: boolean;
  hasPendingJobs?: boolean;
  hasError: boolean;
}

/** 工作流工作台用 pipeline 忙闲覆盖顶栏；否则会一直显示 SSE 的「空闲」 */
export function withExperienceRunStatus(
  header: ReactNode,
  run: ExperienceRunStatusProps,
): ReactNode {
  if (!isValidElement(header)) return header;
  return cloneElement(header as ReactElement<ExperienceRunStatusProps>, run);
}
