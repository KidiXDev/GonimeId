# Product

## Platform

terminal

## Users

Anime viewers who use a keyboard-driven terminal workflow and want to search, stream, resume, and manage episodic viewing without leaving GonimeId.

## Product Purpose

GonimeId makes Indonesian-subtitled anime searchable and watchable from the terminal. Success means returning viewers can continue a series, understand episode progress, and move to the next episode with fewer repeated choices.

## Positioning

GonimeId combines multi-source Indonesian anime discovery, mpv playback, downloads, and a local keyboard-first watch history in one terminal application.

## Operating Context

Users launch GonimeId from a terminal, browse with the keyboard, and watch in a separate mpv window. A no-argument launch opens the watch hub; a title argument remains a direct-search shortcut.

## Capabilities and Constraints

- Continue Watching and Recent are grouped by series and stored locally in SQLite.
- Episodes support resume progress plus automatic and manual completion.
- Autoplay is on by default, uses a cancellable countdown, and persists when changed.
- Existing search, download, update, upscale, source, and direct-title CLI behavior must remain compatible.
- Tracking gracefully degrades when SQLite/CGO is unavailable.

## Brand Commitments

Keep the GonimeId name, restrained purple accent, compact full-screen shell, fuzzy filtering, keyboard navigation, and concise operational copy already established in `internal/tui`.

## Evidence on Hand

The repository contains the working search/playback pipeline, Bubble Tea shell and picker components, mpv IPC integration, and SQLite progress tracker. No external product claims or visual assets are required for the watch hub.

## Product Principles

- Episode choice first: opening a saved title always shows the full episode selector before playback options.
- Preserve fast paths: richer interactive behavior must not slow direct CLI commands.
- State must be legible without color and at narrow terminal widths.
- Playback automation must remain cancellable and never advance after an error or manual stop.

## Accessibility & Inclusion

Every status marker has a text label, all actions are keyboard accessible, and layouts remain usable in narrow terminals and without ANSI color.
