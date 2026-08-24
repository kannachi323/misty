# Misty AI-Native Tool Plan

**Prepared:** August 23, 2026  
**Scope:** Current desktop product routes, Space tools, AI Surface adapters, and supporting system features in the working tree.

**Thread-ready briefs:** [AI-Native Workstreams](ai-native-tools/README.md)

## Product goal

> Misty turns the desktop from a collection of tools people operate alone into a shared environment where people and AI can see, create, manipulate, review, and hand off the same work together in real time.

ClickUp helps teams organize and automate work. Misty should help people and agents perform the work together. The product is successful when the user does not have to package context into a chatbot, copy the answer back into a tool, or wonder what the AI changed.

Misty should not attempt to replace every mature standalone application. Each built-in tool needs a focused role inside the Misty environment:

- **Collaborative core:** Spaces, Chat, Notes, Drawings, Planner, Agenda, Roadmaps, and Library should be excellent shared tools because they contain the persistent work.
- **Execution tools:** Browser, Files, Inbox, Code, and Terminal should be credible, dependable work surfaces optimized for context, action, and handoff. They do not need every feature of Chrome, Finder, Gmail, VS Code, or iTerm.
- **Operational tools:** Agents, Activity, Transfers, Extensions, Settings, Members, and Connections should make agency, permissions, recovery, and system state understandable.
- **System flows:** Authentication, installation, updates, deployment selection, and destructive account operations should remain deterministic. They do not need decorative AI.

## The AI-native standard

A tool is not AI-native merely because it has an Ask button. A Misty surface is AI-native only when it satisfies all of these conditions:

1. **Exact scope:** Misty understands the active object, selection, visible state, and relevant Space without the user re-explaining them.
2. **Visible context:** The user can see and remove everything being sent: selected text, canvas objects, page capture, file metadata, terminal output, or connected records.
3. **Native output:** The result becomes a real note edit, drawing object, task set, event, file plan, draft, patch, or command—not text the user must manually transplant.
4. **Collaborative execution:** For interactive work, the user can watch, interrupt, redirect, and continue editing alongside Misty.
5. **Review and recovery:** Consequential changes have a preview, approval, provenance, conflict check, and reliable Undo or recovery path.
6. **Continuity:** Work can move between tools without losing its sources, history, permissions, or relationship to the objective.
7. **Delegation:** Longer work can be handed to a named agent and returned as a reviewable native artifact.

Every base tool must also remain useful when AI is disabled or unavailable.

## Current product assessment

The working tree is farther along than a typical prototype. It already contains a shared AI Surface contract, typed artifacts, selection snapshots, captures, citations, approvals, run streaming, context boundaries, agent selection, and client-side application guards. Notes, Drawings, Chat, Planner, Agenda, Roadmaps, Library, Inbox, Browser, Files, Code, Terminal, Transfers, Extensions, and the photo editor all register contextual behavior.

The primary gap is no longer “add AI to each page.” The gap is turning isolated request/response actions into one coherent collaboration model, and raising the underlying tools to a dependable production bar.

### At-a-glance product stance

| Surface | Misty’s focused version of the full tool | Build stance |
| --- | --- | --- |
| Misty companion | Ambient collaborator + action runtime | **P0 differentiator** |
| Global Search | Spotlight/Raycast + context assembler | **P0 entry point** |
| Workspace | Shared objective-centered work stage | **P0 platform** |
| Spaces | Mixed human-agent project environment | **P0 platform** |
| Chat | Project conversation-to-action stream | **P1 collaborative core** |
| Notes | Shared project notebook | **P1 collaborative core** |
| Drawings | Shared visual-thinking whiteboard | **P1 collaborative core** |
| Planner | Intent-to-executable-work planner | **P1 collaborative core** |
| Agenda | Shared commitments and constraint view | **P2 coordination** |
| Roadmaps | Living dependency and scenario model | **P2 coordination** |
| Library | Curated, permissioned Space memory | **P2 context** |
| Media editor | Safe, versioned project asset editor | **P3 utility** |
| Inbox | Private communication-to-Space bridge | **P2 context bridge** |
| Browser | Research and bounded web-action surface | **P2 execution bridge** |
| Files | Private device-to-Space artifact bridge | **P2 execution bridge** |
| Code | Reviewable implementation surface | **P3 execution** |
| Terminal | Observable, session-scoped execution surface | **P3 execution** |
| Transfers | Deterministic transfer/recovery monitor | **P4 operations** |
| Agents | Roster, delegation, permission, and audit control room | **P2 control plane** |
| Extensions | Safe typed capability ecosystem | **P4 ecosystem** |
| Activity | Attention compression and decision inbox | **P2 coordination** |
| Members/Settings | Trust, access, and policy layer | **P0 trust foundation** |
| Auth/Installer/Updater | Deterministic system flows | **No ambient AI** |

## 1. Misty companion and global control

**What exists now**

Misty can persist across panes, follow the pointer, attach a region capture, read a surface selection, show suggested actions, stream work, request approval, expose Undo, switch between the built-in Misty and personal agents, and link completed work to durable task history. The current interaction still treats the moving blob as a button, while the navbar control, follow state, composer state, and “go away” behavior are not one simple contract.

**Compared with a full product**

This is not merely a chat widget or command palette. It is the system-wide collaboration layer—closer to a combination of Spotlight, an AI command bar, a visible agent presence, and a permissioned action runtime.

**Copyable goal**

> Make Misty a dependable ambient collaborator: present when invited, context-aware without being invasive, instantly reachable through a stable toggle, and able to make visible, reversible changes in the active tool.

**Build next**

1. Implement one explicit state model: **Off → Follow → Engaged → Acting → Follow**.
2. Make the moving blob click-through and nonessential. The navbar button and global shortcut toggle the stable composer.
3. Freeze or anchor the composer in a predictable location while engaged; Escape closes it but preserves Follow.
4. Show a context receipt before submission: surface, object, selection, capture, privacy boundary, and named agent.
5. Let selection and capture update the receipt while Follow is active without automatically opening the composer.
6. Allow proactive behavior only as a quiet, dismissible nudge with a reason, cooldown, snooze, and per-surface control. Never auto-open or auto-act.
7. Add a persistent action tray for previews, approvals, progress, errors, and Undo so important controls do not depend on a speech bubble disappearing on a timer.
8. Test the same behavior in React surfaces and native Browser webviews.

**Production bar**

Keyboard-only invocation, reduced-motion behavior, coarse-pointer behavior, screen-reader state announcements, no stolen clicks, no silent context expansion, reliable task resumption after navigation/restart, cancel latency under one second, and deterministic recovery from stale artifacts.

## 2. Global Search and launcher

**What exists now**

The current Global Misty UI is primarily a fast Search/launcher experience. It combines server and local results, knows the active route and selected Files items, supports context chips, opens results in the correct surface, and exposes commands. The earlier Search/Ask/Action concept is not presently a complete unified interaction.

**Compared with a full product**

Today it resembles Spotlight or Raycast with Misty-specific objects. A full Misty version should become the cross-tool intent router and context assembler, not another general chat screen.

**Copyable goal**

> Let a user find anything, assemble exact context, and turn intent into a visible answer, native proposal, or delegated task from one keyboard-first entry point.

**Build next**

1. Preserve instant lexical search; never wait for a model before showing results.
2. Add permission-aware semantic retrieval and grounded answers above results with source links.
3. Support multi-select context and verbs such as Compare, Summarize, Open together, Add to Space, and Delegate.
4. Turn the flow into a progression: result → context → answer → proposal → action/run.
5. Add deterministic intent handling for routes, URLs, file paths, and known commands before model routing.
6. Let the user open selected results into adjacent panes as a working set.

**Production bar**

Sub-100 ms local feedback, resilient offline/local search, complete keyboard navigation, stable ranking, deduplication across sources, permission-filtered retrieval, citations that open the exact object, and no raw local path leakage.

## 3. Multi-pane workspace

**What exists now**

Misty already has persistent tabs, split panes, per-Space layouts, focused-pane routing, virtual windows, session restoration, quick open, and an AI host per pane. This is one of the strongest pieces of the product thesis.

**Compared with a full product**

It is not a generic window manager. It is the stage where shared Space context and private execution tools coexist with explicit boundaries.

**Copyable goal**

> Make the workspace a collaborative stage where people and agents can arrange sources and tools around an objective, move context deliberately between panes, and watch work progress without losing place.

**Build next**

1. Add visible pane identity and context boundaries: Shared Space, Private device, Provider, or Attached for this task.
2. Allow dragging an object or selection from one pane into Misty or another tool as a typed reference, not a copied blob of text.
3. Support “open beside,” “compare in split,” and “continue in” as first-class Misty actions.
4. Show where a running agent is working and which pane or object it currently controls.
5. Persist working sets, attached context, and draft proposals across restarts without silently restoring expired grants.
6. Make cross-pane handoffs create provenance links between source and output.

**Production bar**

Crash-safe layout restoration, no duplicate tabs after migrations, correct focus and shortcut routing, accessible pane resizing, sensible minimum sizes, performance with several live surfaces, and clear recovery when one pane fails.

## 4. Spaces

**What exists now**

A Space is a permissioned collaborative context containing people, agents, Chat, Journal, Planner, Library, settings, connections, activity, and a persistent dock layout. Space creation, invitations, roles, usage limits, realtime updates, and section permissions are present.

**Compared with a full product**

A Space is Misty’s answer to a lightweight combination of a Slack workspace, Notion teamspace, project in ClickUp, and shared agent room. It should not become a database builder or an everything-app container.

**Copyable goal**

> Make a Space the fastest way to form a mixed human-and-agent team around an objective, with the right tools, permitted context, and first useful plan available within minutes.

**Build next**

1. Make Space creation objective-first: name the outcome, participants, time horizon, and initial material.
2. Let Misty propose a starter environment—channels, note, task plan, milestones, and agents—for review.
3. Add a concise Space brief that remains the shared orientation point and cites current work.
4. Give agents first-class membership, roles, presence, and activity rather than hiding them behind commands.
5. Add a Space-level context ledger showing what is shared, indexed, connected, or private.
6. Replace a generic Home dashboard with a useful Space landing/briefing unless user research proves a separate Home is necessary. There is currently no dedicated Home route.

**Production bar**

Fast creation, reliable invitations, granular roles, realtime reconnect, offline/read-only behavior, audit history, export/deletion, retention controls, scalable member lists, and a comprehensible empty state for every tool.

## 5. Space Chat

**What exists now**

Space Chat supports conversations, replies, editing, reactions, attachments, mentions, presence, direct-message framing, external channel links, agent runs, typing state, suggestions, and a contextual adapter. Misty can recap, extract actions, explain a thread, and draft a message into the composer.

**Compared with a full product**

It is a project conversation surface, not a full Slack or Discord replacement. Its advantage should be converting discussion into durable work while humans and agents remain visible participants.

**Copyable goal**

> Make Chat the shared conversation-to-action layer where people and agents reach decisions, create durable work, and retain evidence without losing the natural flow of discussion.

**Build next**

1. Let users select a message range and ask for a cited recap, decision log, unresolved questions, or reply.
2. Turn extracted tasks/events/notes into editable cards before creation and link them back to source messages.
3. Allow @Misty and @Agent mentions with visible plan, progress, and handoff states inside the thread.
4. Add “catch me up since…” and participant-specific summaries.
5. Let Misty notice a concrete agreement, but only surface a reviewable nudge—never create work silently.
6. Add scheduled recaps only when configured by a Space owner.

**Production bar**

Reliable ordering and pagination, unread markers, search, thread navigation, attachment safety, moderation/retention, offline drafts, rate limits, notification controls, reconnect behavior, and source-preserving integrations.

## 6. Notes / Journal

**What exists now**

Notes is a collaborative TipTap/Yjs editor with native Space notes, Notion reading/publishing, search, tags, backlinks, source status, and selection-aware AI. Selected text can be improved, shortened, clarified, summarized, or converted into proposed tasks. Text patches are hash- and revision-checked and can be undone.

**Compared with a full product**

It is currently a collaborative project notebook rather than full Notion or Google Docs. That is the correct scope. Misty does not need databases, websites, and every publishing workflow before the writing collaboration is excellent.

**Copyable goal**

> Make Notes the best place to think and write with people and AI in the same document, with inline collaboration, source-aware synthesis, and clean reversible authorship.

**Build next**

1. Show AI suggestions as tracked inline proposals adjacent to the selection—not only in the floating companion.
2. Support Replace, Insert below, Apply pieces, Retry, and Discard with a visible diff.
3. Stream longer generation into a temporary block the user can edit while Misty continues.
4. Add page-level outline, decisions, tasks, questions, tags, and “turn this into” actions.
5. Add source-backed AI blocks for living briefs and status sections.
6. Let a user and Misty co-edit during generation without replacing concurrent collaborator changes.
7. Attribute AI-authored transactions and link extracted tasks/events to exact note ranges.

**Production bar**

Robust collaborative undo/history, comments, mentions, permissions, autosave/offline recovery, import/export fidelity, large-document performance, mobile/tablet editing, accessibility, and conflict-safe range patches.

## 7. Drawings

**What exists now**

Drawings is a collaborative Excalidraw surface with Yjs presence, cursor following, shared binary assets, permissions, and Figma references. Misty can inspect a bounded scene/selection and currently apply only constrained x/y updates to selected elements. The UI advertises layout improvement and diagram creation, but the current client patch contract cannot yet create shapes, text, or connectors.

**Compared with a full product**

It is a collaborative whiteboard, not Figma Design, Illustrator, or a full Miro replacement. Its strategic job is shared visual thinking and rapid structured diagrams.

**Copyable goal**

> Make Drawings a live visual collaboration surface where a person can describe, sketch, select, and reshape ideas while Misty creates and edits native objects in view.

**Build next**

1. Expand the drawing operation schema to validated create/update/delete/group/connect operations for a safe subset of Excalidraw objects.
2. Make “draw a cube” visibly construct native elements on the canvas, with Cancel and Undo.
3. Support selection-scoped alignment, cleanup, labels, clustering, connector repair, and style matching.
4. Generate flowcharts, mind maps, wireframes, and sticky-note sets from prompts or selected notes.
5. Let the user interrupt or manipulate generated objects while Misty adapts to the new scene revision.
6. Add canvas outline, alt text, OCR, and conversion from visual clusters into Notes or Planner items.

**Production bar**

Scene versioning, deterministic operation validation, one-transaction Undo, large-canvas performance, asset lifecycle, export/import, keyboard accessibility, collaboration conflict tests, and no unrestricted model-authored Excalidraw JSON.

## 8. Planner: tasks

**What exists now**

Planner has board/list/calendar views, filters, assignees for people and agents, task drawers, Markdown details, activity, shortcuts, and calendar publishing. Misty currently sees the active task plus filter metadata and can summarize status, identify risks, or propose a task set. It does not yet hydrate the full visible task set in the client adapter, and rich plan editing remains limited.

**Compared with a full product**

It is a focused project planner rather than ClickUp, Asana, or Jira. It should optimize the path from shared intent to owned, realistic work instead of accumulating every project-management configuration.

**Copyable goal**

> Make Planner the place where people and agents turn intent into an executable, evidence-backed plan and continuously renegotiate that plan as reality changes.

**Build next**

1. Return editable task-set proposals with titles, descriptions, owners, dates, dependencies, and source evidence.
2. Support selection and bulk actions: break down, clarify acceptance criteria, estimate, assign, reschedule, merge duplicates, and draft status.
3. Add dependencies, subtasks, effort/duration, structured acceptance criteria, and batch mutation APIs.
4. Let Misty explain why a task is risky using workload, dependencies, dates, and recent activity.
5. Add human/agent workload views and never guess an ambiguous assignee.
6. Connect created tasks back to Chat, Notes, Browser, email, or drawings that produced them.

**Production bar**

Fast large boards, reliable filtering/sorting, permissions, recurring tasks if required by the target user, conflict-safe batch changes, imports/exports, notifications, activity history, keyboard operation, and deterministic date/time handling.

## 9. Agenda and Calendar

**What exists now**

Agenda supports day/week/month-like ranges, zoom, native tasks/events, roadmap items, source visibility, Google Calendar connections, and reviewed event/task artifacts. Misty can brief the visible range, find conflicts, suggest a plan, draft an event, or draft preparation tasks.

**Compared with a full product**

It is a Space commitment view, not a replacement for Google Calendar or Fantastical. Its advantage is connecting project commitments to tasks, goals, messages, and agents.

**Copyable goal**

> Make Agenda the shared time-negotiation surface where Misty explains constraints and proposes realistic schedules without taking control of anyone’s calendar.

**Build next**

1. Hydrate visible event/task details for grounded conflict and preparation analysis.
2. Parse selected text, email, chat, webpages, and notes into reviewed event cards.
3. Propose focus blocks for flexible work and show exactly which constraints prevent a plan from fitting.
4. Support attendee-aware time suggestions when permissioned free/busy data exists.
5. After rescheduling, propose downstream task/date updates and reviewed messages to affected people.

**Production bar**

Timezone/DST correctness, recurring events, all-day events, conflict handling, provider reconciliation, offline cache, accessibility, drag/resizing reliability, explicit publishing, and no silent external calendar changes.

## 10. Goals, milestones, and roadmaps

**What exists now**

Misty has roadmap lists, a graph canvas, outline, node definitions, goals, milestones, dependencies, inspectors, autosave, history, version conflict handling, and contextual AI. Current AI writes are deliberately narrow: one revision-anchored title, description, or target-date update to an existing item.

**Compared with a full product**

This is a strategic planning graph, not a full portfolio-management suite. It should explain direction and consequences better than a conventional roadmap database.

**Copyable goal**

> Make Roadmaps a living model of intent, dependency, and risk that humans and agents can explore together before committing changes to the actual plan.

**Build next**

1. Add a versioned graph patch schema for nodes, edges, links, milestones, goals, and layout suggestions.
2. Generate a draft roadmap from a brief with assumptions and open questions clearly separated.
3. Support what-if scenarios in an overlay that never mutates the live graph until accepted.
4. Detect circular dependencies, orphan goals, unowned risks, impossible dates, and stale status.
5. Convert selected graph areas to task proposals and synthesize progress with cited evidence.

**Production bar**

Scalable canvas performance, graph validation, conflict-safe batch patches, stable layout, audit history, keyboard/navigation accessibility, exports, and clear separation of current plan versus scenario.

## 11. Library

**What exists now**

The Space Library is a substantial shared asset system with uploads, collections, albums, people, dates, imports, shared references, duplicates, protected collections, versions, metadata, image/video viewing and editing, and Smart Library analysis/search. Misty currently receives selected item references and can synthesize, compare, organize, or suggest related searches.

**Compared with a full product**

It sits between Dropbox/Drive, a digital asset manager, and Photos. Its Misty-specific role is curated project memory—not a raw mirror of every local file.

**Copyable goal**

> Make Library the durable, permissioned memory of a Space: every important artifact is understandable, retrievable, reusable, connected to its origin, and safe for people and agents to cite.

**Build next**

1. Resolve authorized item content, captions, OCR, transcripts, and time-coded segments through the Context Broker.
2. Add “ask about selection” with page/slide/timecode citations.
3. Support semantic related-items, duplicate review, best-version suggestions, and organization proposals.
4. Convert selected assets into a Note, task plan, drawing reference, presentation brief, or chat attachment.
5. Preserve generated metadata separately from human metadata with model/version/input provenance.
6. Let agents deposit outputs directly into the correct collection with a clear source/run trail.

**Production bar**

Large-library pagination, resumable upload, checksum/integrity, permissions, deletion recovery, rendition reliability, search quality, metadata portability, format coverage, storage quotas, and user-controlled reanalysis/deletion.

## 12. Photo and media editing

**What exists now**

The Library viewer supports photo editing, crop controls, versions, metadata, video trimming, playback, and non-destructive edit rendering. Misty can suggest edits and create a guarded image-edit artifact that must preserve the original and render as a new version.

**Compared with a full product**

This is a practical asset editor, not Photoshop, Lightroom, Premiere, or Canva. It should cover common project transformations and AI-assisted variants without becoming a professional creative suite.

**Copyable goal**

> Make media editing a safe, source-preserving collaboration loop where users describe or mark a change, Misty produces reviewable variants, and every result retains provenance.

**Build next**

1. Add visual attachment/pixel context instead of relying on metadata alone.
2. Support brushed-region object removal, background replacement, generative fill, expansion, and destination-specific crops.
3. Produce side-by-side variants with apply-as-version, compare, and revert.
4. Generate captions, alt text, palettes, tags, transcripts, chapters, and highlight proposals.
5. Keep deterministic codecs/renderers responsible for conversion and export.

**Production bar**

Color/orientation fidelity, cancellation, progress, memory limits, format support, source preservation, rendition retries, provenance, accessibility, and clear disclosures for generated pixels.

## 13. Inbox

**What exists now**

Inbox is a unified Gmail/Outlook-style reader with account/folder navigation, thread list/detail, pagination, compose, reply, drafts, sending, and message actions. Misty can read a bounded selected thread, summarize it, identify commitments, inspect attachment metadata, and place a generated reply into the composer without sending.

**Compared with a full product**

It is a project-oriented mail lens, not a complete Gmail or Outlook replacement. It should excel at turning external communication into shared context and reviewed commitments.

**Copyable goal**

> Make Inbox the controlled bridge from private communication to collaborative work: Misty helps understand and draft privately, then the user explicitly promotes selected commitments into a Space.

**Build next**

1. Add cited summaries, question coverage, tone controls, and “answer every request” drafting.
2. Parse tasks, dates, decisions, files, and contacts into editable proposals.
3. Let the user choose which extracted items enter which Space; never share an entire thread implicitly.
4. Add cross-thread/provider search with citations and project grouping.
5. Add attachment content only after explicit attachment/inspection with malware and prompt-injection treatment.
6. Keep send as an exact-recipient, exact-body confirmation.

**Production bar**

Provider token recovery, pagination, caching, threading fidelity, HTML/plain-text rendering, attachment handling, drafts, recipients, signatures, search, rate limits, offline behavior, and send idempotency.

## 14. Browser

**What exists now**

Browser has native per-tab webviews, an omnibox, local history, navigation, reload, downloads/notices, viewport simulation, page annotation, and scoped agent grants. Misty can perform a one-time bounded page inspection, summarize/explain/extract, and apply one validated navigation or click using opaque references. Native Browser has its own companion injection path.

**Compared with a full product**

It is a research and bounded-action browser, not a secure replacement for Chrome or Safari. It should support real signed-in work while making AI access unusually explicit and revocable.

**Copyable goal**

> Make Browser the safest place to research and complete bounded web actions with Misty in the user’s real session, with page-level citations and visible control at every step.

**Build next**

1. Add selection-level Ask/Explain/Translate/Save and exact DOM-range context.
2. Turn annotations and region captures into cited research context.
3. Support multi-tab research sessions with source cards, claim citations, and a reusable research Note.
4. Expand capabilities separately for form fill, tab switching, screenshot, and controlled download; never introduce one generic browser-write grant.
5. Show Observe/Act mode, current grant, expiry, next action, Stop, and Take over.
6. Build compatibility handling and external-browser fallback into the normal flow.

**Production bar**

Isolation, cookie/session safety, downloads, popups, permissions, certificates, crashes, memory suspension, accessibility, web compatibility, prompt-injection defense, action confirmation, and thorough native desktop E2E coverage.

## 15. Files

**What exists now**

Files is one of the deepest tools: tabs/panes, local and remote locations, connected devices, navigation/history, list/grid views, search/indexing, previews, archives, checksums, symlinks, terminal handoff, copy/cut/paste, batch rename, compare, duplicates, Quick Access, Space upload, media understanding, and extension panels. Misty currently sees sanitized selected metadata and can safely apply only a single local rename or trash the complete selected set.

**Compared with a full product**

It is already moving toward a Finder/Explorer plus connected-storage workbench. Its strategic role is the private context and artifact bridge between the user’s device and shared Spaces.

**Copyable goal**

> Make Files the safest way for people and agents to understand, organize, transform, and deliberately promote private device material into collaborative work.

**Build next**

1. Add explicit content inspection for supported files through bounded local extraction and opaque scopes.
2. Add selection-level Summarize, Ask, Convert, Organize, Find related, Find duplicates, and Add to Space.
3. Render organization proposals as before/after trees with conflicts, confidence, and recovery.
4. Add typed copy/move/mkdir/archive/convert plans only after recovery and idempotency are defined.
5. Let Misty explain unknown files and recommend installed extensions without exposing raw paths.
6. Keep device actions local; send only attached bounded content to hosted AI.

**Production bar**

Transactional operations where possible, trash/undo, collision handling, symlink safety, permissions, network interruption recovery, watcher correctness, very large directory performance, full keyboard/accessibility, preview sandboxing, and platform-specific behavior tests.

## 16. Code

**What exists now**

Code is a lightweight IDE with project explorer, tabs and multibuffers, CodeMirror editing, search, diagnostics, LSP features, symbols, references, rename, formatting, inlay/signature support, embedded terminal, file watching, preferences, and inline rewrite. The shared Misty adapter currently reasons over and patches only one bounded active buffer; it does not yet perform repo-scale edits or a test loop.

**Compared with a full product**

It is not yet VS Code or Cursor. Misty should treat it as an integrated implementation surface for small-to-medium work and agent review, while allowing handoff to external IDEs for specialized workflows.

**Copyable goal**

> Make Code a collaborative implementation surface where the user and Misty inspect the same repository state, negotiate a change, review multi-file diffs, run evidence-producing checks, and hand off longer work to a coding agent.

**Build next**

1. Add exact editor selection and diagnostic context to the shared adapter.
2. Add repository search, symbols, references, diagnostics, git diff, and terminal output as attachable typed context.
3. Support multi-file patch artifacts with per-hunk accept/reject and stale-base detection.
4. Run format/typecheck/test commands as explicit evidence attached to the proposal.
5. Add background coding tasks that return a branch/diff/artifact rather than silently editing the working tree.
6. Support project instructions and respect repository policies.

**Production bar**

Reliable file encoding/newlines, autosave/recovery, LSP lifecycle, large repositories, git integration, diff fidelity, terminal sandboxing, language coverage, extension strategy, accessibility, and no dependency install/push/publish without elevated approval.

## 17. Terminal

**What exists now**

Terminal supports real local PTYs, SSH environments, session state, clipboard/search/zoom shortcuts, configurable appearance/scrollback, and workspace persistence. Misty receives a redacted visible buffer and can explain, diagnose, summarize, security-check, or stage exactly one validated command into the input buffer. Acceptance does not execute it automatically.

**Compared with a full product**

It is a solid embedded terminal rather than iTerm, Warp, or a complete SSH manager. Its value is making execution observable and safely collaborative.

**Copyable goal**

> Make Terminal a shared execution conversation where Misty can observe bounded output, propose the next step, and operate only under visible session-scoped control.

**Build next**

1. Represent command blocks, exit codes, timestamps, cwd, and selected output structurally instead of sending only a text snapshot.
2. Trigger one dismissible “Explain failure” nudge after a non-zero exit when enabled.
3. Add command preview with risk, effect, cwd, environment, and rollback before staging.
4. Define separate Observe, Propose, Write, and Execute capabilities for one PTY.
5. Add Agent mode with state labels, per-command approvals, Stop, and Take over.
6. Improve secret detection and show the user the exact redacted context before sending.

**Production bar**

PTY correctness, SSH host-key verification, reconnect, Unicode, resize, process cleanup, paste protection, secrets, shell integration, performance, accessibility, and zero hidden command execution.

## 18. Transfers

**What exists now**

Transfers is a substantial queue/history surface with batches, tree/table views, pagination, filters, selection, columns, pause/resume/cancel/retry, conflict policies, performance profiles, bandwidth/checksum settings, notices, and activity integration. Misty can diagnose selected rows and apply only currently valid retry/resume proposals.

**Compared with a full product**

It is closer to a transfer manager or sync job monitor than a creative tool. Healthy transfers do not need AI conversation.

**Copyable goal**

> Keep Transfers deterministic during normal operation and use Misty only to compress failures, explain conflicts, and propose safe recovery.

**Build next**

1. Group repeated failures by likely root cause and affected destination/provider.
2. Explain conflict policies with a concrete before/after preview.
3. Propose reviewed recovery batches and verification steps.
4. Generate a support bundle or issue draft from selected failures.
5. Keep Misty silent when the queue is healthy.

**Production bar**

Crash-safe queue persistence, idempotent resume, checksum evidence, cancellation, concurrency limits, partial-file cleanup, provider-specific recovery, accurate progress/speed/ETA, accessibility, and complete audit state.

## 19. Agents page

**What exists now**

The page now centers Misty plus personal agents and removes the retired legacy characters. Users can create/edit agents, choose model/reasoning/run mode/voice, connect MCP tools, activate a companion, and inspect durable task conversations. It is still organized mainly as a roster plus task transcript, and the empty-state copy still tells users to click the moving blob.

**Compared with a full product**

It should not imitate ClickUp’s AI Hub, a bot marketplace, or a developer-only orchestration dashboard. It is the control room for who may collaborate, where they may appear, what they may do, and what they have done.

**Copyable goal**

> Make Agents the roster, delegation, permission, and accountability layer for Misty’s mixed human-agent workspace—not another gallery of chatbots.

**Build next**

1. Replace generic All/Mine/Recent concepts with **Where**, **When**, **Context**, **Actions**, and **Consent** for each agent.
2. Show which surfaces can invoke the agent and whether it may suggest, ask before changing, or auto-run reversible routine work.
3. Add job-based starter agents and save a successful action as a reusable skill.
4. Build a unified run inbox: active, waiting for approval, completed, failed, scheduled, and paused.
5. Add replayable activity: context receipts, tool calls, approvals, artifacts, cost, sources, and final effects.
6. Let Misty hand work to a specialist and keep that handoff visible to the user.
7. Remove all remaining interaction copy that makes clicking/chasing the blob mandatory.

**Production bar**

Plain-language permissions, least privilege, revocation, audit export, schedules/triggers, budgets, rate limits, versioned instructions, tool-health states, retry/cancel, secure credentials, and clear separation between personal and Space agents.

## 20. Extensions

**What exists now**

Extensions has a catalog, search, detail view, declared capabilities/permissions, install/enable/disable/uninstall behavior, panels and commands inside Files, and six bundled extensions: Backups, Image Optimizer, Quick Convert, Storage Report, Themes, and yt-dlp. Misty can explain or compare catalog entries and propose installation only when the exact permission list matches the manifest.

**Compared with a full product**

It is an early capability marketplace, not the Chrome Web Store or VS Code Marketplace. Its value is extending what Misty and agents can safely do with typed inputs and outputs.

**Copyable goal**

> Make Extensions the safe capability layer that lets Misty gain specialized deterministic tools without turning arbitrary plugins into unrestricted agents.

**Build next**

1. Search by desired outcome and current selection, not only name/category.
2. Explain why an extension matches, where it appears, what leaves the device, and each permission.
3. Expose extension commands as typed Misty actions with previewable inputs/outputs.
4. Compose compatible extension steps into a reviewed workflow.
5. Add verification, signing, compatibility, update notes, rollback, health, and crash isolation.
6. Provide a developer kit with schemas, test fixtures, permission linting, and accessibility checks.

**Production bar**

Signed packages, reproducible checksums, sandboxing, permission enforcement, safe tool binaries, rollback, dependency isolation, compatibility policy, update channels, telemetry boundaries, and a review process.

## 21. Activity and attention

**What exists now**

Activity is currently a notification/attention store and popover rather than a route. It merges Space and local activity, deduplicates, tracks unread/attention state, navigates to source objects, synchronizes the native badge, and sends sanitized desktop notifications. Recurring AI recaps exist as a backend/settings concept for Home, Activity, and Global Misty.

**Compared with a full product**

It is a focused notification center, not an analytics dashboard or social feed. Its AI role is attention compression.

**Copyable goal**

> Make Activity explain what changed, why it matters, and what needs a decision—while preserving access to the complete unfiltered event stream.

**Build next**

1. Group repeated events into outcome-oriented narratives by Space and object.
2. Add a cited Catch me up view with direct next actions.
3. Explain why an item is considered important and let the user correct ranking.
4. Surface agent waiting/failed/completed states consistently.
5. Deliver configured daily/weekly recaps here or on the relevant Space landing page.
6. Never silently hide or reprioritize the underlying feed.

**Production bar**

Deduplication, pagination, durable read state, quiet hours/digests, native notification reliability, source navigation, cross-device sync, accessibility, and explainable ranking.

## 22. Members, connections, permissions, and Settings

**What exists now**

Space management includes invitations, roles, granular permission groups, human and agent members, Space details, usage, suggestions, and connections for services such as Notion, Slack, Discord, Google Calendar, GitHub, Figma, and mail providers in their relevant surfaces. App Settings includes General, Appearance, Notifications, Files, Search, Browser, Terminal, Code, Transfers, Extensions, Agents, Misty, Models, Privacy, Shortcuts, Updates, and Advanced/deployment controls. Misty settings already expose the master switch, cursor companion, retention, provider/usage, per-surface proactive controls, pinned agents, and scheduled briefings.

**Compared with a full product**

This is Misty’s trust and policy layer. It should be as legible as a consumer settings app while enforcing controls expected from serious collaboration and agent platforms.

**Copyable goal**

> Make access, sharing, automation, retention, cost, and agent behavior understandable enough that a user can predict what Misty will see and do before it happens.

**Build next**

1. Add a “What Misty can access now” dashboard for active Space, browser, file, provider, terminal, and agent grants.
2. Show personal versus shared context and temporary versus persistent access.
3. Add permission presets with an advanced exact-capability view.
4. Add connection health, last use, scopes, affected Spaces/agents, and one-click revocation.
5. Consolidate duplicated AI/Agent settings into one understandable policy hierarchy.
6. Use Misty to explain settings and permission consequences, but never let AI alter security, billing, deployment, or retention controls without exact deterministic confirmation.

**Production bar**

Permission enforcement on the server, audit logs, session/device management, secure credential storage, data export/deletion, billing/usage accuracy, self-hosted parity disclosures, accessibility, and safe defaults.

## 23. Authentication, installation, updates, and deployment

**What exists now**

Misty has sign-in/register/invitation flows, account switching, desktop installation/readiness, updater behavior, hosted/self-hosted deployment configuration, and diagnostics.

**Compared with a full product**

These are system-critical flows, not collaboration tools.

**Copyable goal**

> Keep system-critical setup and recovery deterministic, fast, accessible, and honest; use AI only for optional plain-language explanation after the real error and recovery action are known.

**Build next**

1. Improve deterministic diagnostics and recovery before adding any AI explanation.
2. Preserve exact error codes and support bundles beneath friendly copy.
3. Make account, deployment, and local-data boundaries unmistakable.
4. Never let an agent change deployment, credentials, subscription, update channel, or account deletion state through conversational ambiguity.

**Production bar**

Accessible and idempotent account flows, strict account-switch isolation, secure credential handling, signed and rollback-capable updates, validated deployment URLs, recoverable installation, actionable deterministic errors, safe retry behavior, support bundles, and native desktop end-to-end coverage for every critical path.

## Delivery sequence

### Phase 0 — One collaboration contract

Do this before adding more surface features.

- Implement Off/Follow/Engaged/Acting and stable navbar/shortcut invocation.
- Make the blob presence-only and click-through.
- Ship context receipts, privacy boundary labels, persistent action tray, previews, approvals, provenance, and recovery.
- Add desktop E2E tests covering selection, capture, navigation, native Browser, approval, cancel, stale state, and Undo.
- Instrument time-to-first-response, artifact acceptance, apply failure, undo, cancel, and suggestion dismissal without logging content.

### Phase 1 — Prove real-time collaboration

Focus on Notes and Drawings.

- Notes: inline tracked proposals, streaming blocks, clean collaborative attribution, task/event extraction.
- Drawings: safe native element creation, visible generation, interruption, refinement, and one-step Undo.
- Demo criterion: one person and Misty can co-create a written brief and visual diagram without copying content through chat.

### Phase 2 — Turn collaboration into coordinated work

Focus on Chat, Planner, Agenda, Roadmaps, and Activity.

- Convert conversations/notes/drawings into reviewed plans with source links.
- Add richer task/dependency schemas and graph patches.
- Show human and agent ownership, progress, blockers, and approvals in shared surfaces.
- Demo criterion: a mixed team turns discussion into an owned plan, adapts it, and can explain every change.

### Phase 3 — Connect private context to shared work

Focus on Browser, Files, Inbox, and Library.

- Build bounded context hydration and citations.
- Add explicit promotion from private/provider/device sources into a Space.
- Support research sessions and source-preserving output.
- Demo criterion: a user researches, selects private material, and produces a shared cited artifact without leaking unrelated context.

### Phase 4 — Execute and delegate

Focus on Code, Terminal, Agents, and Extensions.

- Add multi-file reviewable changes, test evidence, PTY capability states, agent handoffs, run oversight, and typed extension actions.
- Demo criterion: a Space task can be delegated, executed in bounded private tools, and returned as a reviewable artifact with evidence and audit history.

### Phase 5 — Production hardening

- Offline/reconnect and crash recovery.
- Accessibility and reduced-motion/coarse-pointer parity.
- Large-data performance and memory budgets.
- Security/prompt-injection evaluations and permission audits.
- Cross-platform desktop/tablet behavior.
- Retention, export, deletion, observability, support bundles, and upgrade migrations.

## Prioritization rule

Evaluate every proposed feature with this sequence:

1. Does the underlying tool work reliably without AI?
2. Does this reduce context packaging or copy/paste?
3. Does it produce or manipulate a native Misty artifact?
4. Can the user see, interrupt, review, and undo it?
5. Does it improve collaboration between people and agents?
6. Does it preserve private versus shared boundaries?
7. Is this something Misty uniquely enables by controlling the environment?

If a feature could be reproduced unchanged as a generic chat sidebar inside ClickUp, it is not differentiated enough.

## North-star proof

The product should be able to demonstrate this end to end:

> A user creates a Space around an objective, brings in only the relevant private sources, works live with Misty in Notes and Drawings, converts the result into an owned plan, delegates an implementation task to a specialist agent, watches bounded work happen in Browser/Code/Terminal, and receives a cited, reversible result that the whole team can understand.

That is the goal. Individual tools exist to make this collaborative loop possible; they are not a checklist of miniature standalone applications.
