# Narrato ASR Timestamp Boundary Design

## Problem

Azure Whisper can return a final segment whose start belongs to the requested audio window but whose end extends beyond the measured duration. Audio encoders can also add a short tail, allowing Whisper to emit a segment wholly inside padding. Narrato previously rejected any end overhang greater than 0.5 seconds, so provider timestamp drift failed the entire documentary workflow even though the resulting source timeline could be bounded safely.

## Design

Normalize timestamps against `min(source_window_duration, measured_audio_duration)`:

- Reject malformed ranges and starts more than 0.5 seconds outside the effective window.
- Clamp starts in the provider's small negative jitter range to zero.
- Omit segments that start wholly inside encoder padding because they have no source-timeline evidence.
- Clamp the end of a segment that starts inside the window to the effective boundary, regardless of provider end overhang.
- Perform ordering and overlap validation after normalization.

This preserves every segment with a valid source-timeline start while ensuring emitted timestamps never exceed the requested video window. Clearly invalid starts remain non-retryable validation errors.

## Verification

Regression tests cover normal timestamp offsets, measured encoder padding, small and large end overhang, padding-only segments, small negative starts, true start overhang, ordering, and corrected-transcript timestamp preservation. The full repository test suite and Ruff checks must pass before delivery.
