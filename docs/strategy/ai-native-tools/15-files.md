# Files

**Priority:** P2 execution bridge  
**Depends on:** Misty companion; multi-pane workspace  
**Source plan:** [Misty AI-Native Tool Plan](../ai-native-tool-plan.md)

## Codex thread objective

> Make Files the safest way for people and agents to understand, organize, transform, and deliberately promote private device material into collaborative work.

Use this file as the scope for a dedicated Codex thread. Start by validating the current implementation against this brief. Then turn the work into staged, testable slices and implement the highest-leverage coherent slice that fits the thread’s authorization. Preserve existing user work and keep the non-AI tool useful when AI is unavailable.

## What exists now

Files is one of the deepest tools: tabs/panes, local and remote locations, connected devices, navigation/history, list/grid views, search/indexing, previews, archives, checksums, symlinks, terminal handoff, copy/cut/paste, batch rename, compare, duplicates, Quick Access, Space upload, media understanding, and extension panels. Misty currently sees sanitized selected metadata and can safely apply only a single local rename or trash the complete selected set.

## Compared with a full product

It is already moving toward a Finder/Explorer plus connected-storage workbench. Its strategic role is the private context and artifact bridge between the user’s device and shared Spaces.

## Build next

1. Add explicit content inspection for supported files through bounded local extraction and opaque scopes.
2. Add selection-level Summarize, Ask, Convert, Organize, Find related, Find duplicates, and Add to Space.
3. Render organization proposals as before/after trees with conflicts, confidence, and recovery.
4. Add typed copy/move/mkdir/archive/convert plans only after recovery and idempotency are defined.
5. Let Misty explain unknown files and recommend installed extensions without exposing raw paths.
6. Keep device actions local; send only attached bounded content to hosted AI.

## Production bar

Transactional operations where possible, trash/undo, collision handling, symlink safety, permissions, network interruption recovery, watcher correctness, very large directory performance, full keyboard/accessibility, preview sandboxing, and platform-specific behavior tests.

## Required thread outputs

1. A current-code audit that confirms or corrects this brief.
2. A staged implementation plan with dependencies and explicit non-goals.
3. The coherent product slice implemented in that thread, with proportional tests.
4. Desktop-specific verification when native behavior, pointer behavior, captures, webviews, files, PTYs, or OS integration are involved.
5. A concise handoff listing completed behavior, remaining gaps, risks, and the next recommended slice.

## Shared acceptance gate

- The underlying tool remains dependable without AI.
- Context sent to Misty is visible, bounded, removable, and permission-correct.
- AI output becomes a native artifact or native proposal rather than copy/paste text.
- Consequential changes are previewable, conflict-checked, attributable, and recoverable.
- The interaction works with keyboard navigation and respects reduced motion where applicable.
- Failures, stale state, cancellation, navigation, and restart behavior are intentionally handled.

