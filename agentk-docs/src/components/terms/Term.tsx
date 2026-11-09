import Link from '@docusaurus/Link';
import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type CSSProperties,
  type FocusEvent,
  type KeyboardEvent,
  type ReactNode,
} from 'react';
import {concepts, type ConceptId} from './concepts';
import styles from './Term.module.css';

const VIEWPORT_MARGIN = 12;
const POPOVER_GAP = 9;
const POPOVER_MAX_WIDTH = 320;
const HOVER_CLOSE_DELAY_MS = 140;

type PopoverPosition = {
  top: number;
  left: number;
  width: number;
};

export function Term({id}: {id: ConceptId}): ReactNode {
  const concept = concepts[id];
  const popoverId = `term-${useId().replaceAll(':', '')}`;
  const wrapperRef = useRef<HTMLSpanElement>(null);
  const triggerRef = useRef<HTMLAnchorElement>(null);
  const popoverRef = useRef<HTMLSpanElement>(null);
  const closeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pointerFocusRef = useRef(false);
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState<PopoverPosition | null>(null);

  const cancelScheduledClose = useCallback(() => {
    if (closeTimerRef.current !== null) {
      clearTimeout(closeTimerRef.current);
      closeTimerRef.current = null;
    }
  }, []);

  const close = useCallback(() => {
    cancelScheduledClose();
    setOpen(false);
  }, [cancelScheduledClose]);

  const scheduleClose = useCallback(() => {
    cancelScheduledClose();
    closeTimerRef.current = setTimeout(() => {
      const activeElement = document.activeElement;
      if (!wrapperRef.current?.contains(activeElement)) {
        setOpen(false);
      }
    }, HOVER_CLOSE_DELAY_MS);
  }, [cancelScheduledClose]);

  const updatePosition = useCallback(() => {
    const trigger = triggerRef.current;
    const popover = popoverRef.current;
    if (!trigger || !popover) {
      return;
    }

    const triggerRect = trigger.getBoundingClientRect();
    const width = Math.min(
      POPOVER_MAX_WIDTH,
      window.innerWidth - VIEWPORT_MARGIN * 2,
    );
    const height = popover.offsetHeight;
    const below = triggerRect.bottom + POPOVER_GAP;
    const above = triggerRect.top - height - POPOVER_GAP;
    const top =
      above >= VIEWPORT_MARGIN
        ? above
        : below + height <= window.innerHeight - VIEWPORT_MARGIN
          ? below
          : Math.max(
              VIEWPORT_MARGIN,
              window.innerHeight - height - VIEWPORT_MARGIN,
            );
    const centeredLeft = triggerRect.left + triggerRect.width / 2 - width / 2;
    const left = Math.min(
      Math.max(centeredLeft, VIEWPORT_MARGIN),
      window.innerWidth - width - VIEWPORT_MARGIN,
    );

    setPosition({top, left, width});
  }, []);

  useEffect(() => {
    if (!open) {
      setPosition(null);
      return undefined;
    }

    const frame = window.requestAnimationFrame(updatePosition);
    window.addEventListener('resize', updatePosition);
    window.addEventListener('scroll', updatePosition, true);

    return () => {
      window.cancelAnimationFrame(frame);
      window.removeEventListener('resize', updatePosition);
      window.removeEventListener('scroll', updatePosition, true);
    };
  }, [open, updatePosition]);

  useEffect(() => {
    if (!open) {
      return undefined;
    }

    const handlePointerDown = (event: PointerEvent) => {
      if (!wrapperRef.current?.contains(event.target as Node)) {
        close();
      }
    };
    document.addEventListener('pointerdown', handlePointerDown);
    return () => document.removeEventListener('pointerdown', handlePointerDown);
  }, [close, open]);

  useEffect(() => () => cancelScheduledClose(), [cancelScheduledClose]);

  const handleBlur = (event: FocusEvent<HTMLSpanElement>) => {
    if (!event.currentTarget.contains(event.relatedTarget)) {
      close();
    }
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLSpanElement>) => {
    if (event.key === 'Escape' && open) {
      event.preventDefault();
      close();
      triggerRef.current?.focus();
    }
  };

  const popoverStyle: CSSProperties = {
    top: position?.top ?? 0,
    left: position?.left ?? 0,
    width: position?.width ?? POPOVER_MAX_WIDTH,
    visibility: position ? 'visible' : 'hidden',
  };

  return (
    <span
      ref={wrapperRef}
      className={styles.wrapper}
      onBlur={handleBlur}
      onKeyDown={handleKeyDown}
    >
      <a
        ref={triggerRef}
        href={concept.href}
        className={styles.trigger}
        aria-expanded={open}
        aria-controls={open ? popoverId : undefined}
        aria-haspopup="dialog"
        onPointerDown={() => {
          pointerFocusRef.current = true;
        }}
        onClick={(event) => {
          if (
            event.button !== 0 ||
            event.metaKey ||
            event.ctrlKey ||
            event.shiftKey ||
            event.altKey
          ) {
            return;
          }
          event.preventDefault();
          pointerFocusRef.current = false;
          cancelScheduledClose();
          setOpen(true);
        }}
        onFocus={() => {
          if (!pointerFocusRef.current) {
            cancelScheduledClose();
            setOpen(true);
          }
        }}
        onMouseEnter={() => {
          cancelScheduledClose();
          setOpen(true);
        }}
        onMouseLeave={scheduleClose}
      >
        {concept.label}
      </a>
      {open && (
        <span
          ref={popoverRef}
          id={popoverId}
          role="dialog"
          aria-label={`${concept.label} 概念说明`}
          className={styles.popover}
          style={popoverStyle}
          onMouseEnter={cancelScheduledClose}
          onMouseLeave={scheduleClose}
        >
          <span className={styles.label}>{concept.label}</span>
          <span className={styles.description}>{concept.description}</span>
          <Link className={styles.link} to={concept.href}>
            查看概念 →
          </Link>
        </span>
      )}
    </span>
  );
}

function conceptComponent(id: ConceptId): () => ReactNode {
  return function ConceptTerm() {
    return <Term id={id} />;
  };
}

export const Capability = conceptComponent('capability');
export const Tool = conceptComponent('tool');
export const Pipeline = conceptComponent('pipeline');
export const Profile = conceptComponent('profile');
export const Session = conceptComponent('session');
export const Execution = conceptComponent('execution');
export const EventLog = conceptComponent('eventLog');
export const Artifact = conceptComponent('artifact');
