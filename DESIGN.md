# Design

GonimeId uses a compact, keyboard-first terminal interface. Screens share the existing `internal/tui` shell, theme, picker behavior, and purple selection accent; new flows should reuse those pieces instead of adding a second visual system.

## Interaction

- Keep the primary action reachable with Enter and show its key in the footer.
- Keep Escape/back behavior consistent with the existing picker flow.
- Use short, stable shortcuts for secondary actions and require confirmation before destructive history changes.
- Timed actions must be visible, cancellable, and safe when interrupted.
- Long-running work uses the shared MiniDot loader: one steady 12 FPS cadence drives both motion and elapsed time, while fast work completes without flashing a loading screen.

## Layout

- Lead with the current task and preserve a usable single-column layout on narrow terminals.
- Add detail panes only when the terminal is wide enough; the list remains the source of selection.
- Keep headers, breadcrumbs, list density, spacing, and footer placement consistent across screens.

## Accessibility

- Never rely on color alone: active tabs use brackets, completed episodes end with an inline green `✓`, and other progress states use text such as `42% watched` or `Not started`.
- Every action is keyboard accessible and core content remains readable without ANSI color.
- Prefer concise operational copy and actionable empty/error states.
