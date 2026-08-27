# Misty public beta readiness audit

Audit date: August 26, 2026

This is a launch-oriented code audit of the current working tree. It covers every user-visible
desktop route, Space surface, public website route, and the supporting native, server, agent, and
extension runtimes. It is not a substitute for the clean-install manual journey pass that must
follow it.

## Status language

- **Beta candidate**: the implementation and automated evidence are strong enough to keep the
  surface visible while it receives focused journey QA.
- **Contained fix**: the product model is established and the remaining work appears bounded to
  correctness, configuration, UI states, or a small interaction cleanup.
- **Coming Soon**: do not expose this in the first public beta. It adds material product, trust,
  platform, or support decisions that cannot be resolved by cosmetic cleanup.
- **Launch blocker**: this is not a feature gate. The release should not be published until it is
  addressed.

## Current automated baseline

| Area | Result | Launch meaning |
| --- | --- | --- |
| Desktop TypeScript | Pass | The current renderer tree passes `tsc --noEmit`. |
| Desktop lint | Pass | The current renderer tree passes ESLint. |
| Desktop tests | 977 pass | All 241 test files pass, including source-size and readability contracts. |
| Native desktop tests | 352 pass | Requires Rust 1.91; the repository default Rust 1.88 cannot compile the current desktop dependency graph. |
| Server tests | Pass | Contract, integration, unit, billing, security, mail, telemetry, and workflow suites pass. |
| Agent runtime | 18 pass | The isolated runtime is healthy under its automated suite. |
| Extensions | 8 pass | The extension frontend/plugin suite passes; native extension behavior also has substantial Rust coverage. |
| Website unit tests | 20 pass | Public-site component logic passes. |
| Website end-to-end | 90 pass, 41 fail, 1 skipped | The dev server defaults to `/v1` while the end-to-end interceptors expect `/api`, breaking auth, billing, and settings scenarios across desktop and mobile. Public unauthenticated and accessibility scenarios largely pass. |

## Launch blockers

| Blocker | Evidence | Required outcome |
| --- | --- | --- |
| Legal documents are stubs | Privacy, Terms, and License all say the document has not been published. | Publish reviewed beta-appropriate policies before account creation or payment is public. |
| Updates are not distributable | Tauri updater has an empty public key and no endpoints, despite UI promising verified updates. | Configure signed update metadata and verify one update, or remove/disable the promise and provide a tested manual update path. |
| Desktop release process is not automated | Native checks exist, but no desktop signing/notarization/release workflow exists alongside the extensions workflow. | Document and successfully rehearse a signed release and emergency replacement process. |
| Paid journey is not green | Authenticated pricing/account end-to-end tests fail; production Stripe configuration cannot be proven from source. | Fix the end-to-end environment and perform a live test-mode purchase, portal, webhook, cancellation, and entitlement pass. |

## Desktop and Space surface classification

| Surface | Status | Why | Before launch |
| --- | --- | --- | --- |
| Authentication and account switching | Contained fix | Established stores, secure token handling, and tests exist; the complete fresh-account journey still needs a clean-server pass. | Verify register, sign in, reset, sign out, account switch, expired session, and offline recovery. |
| Workspace shell, tabs, splits, and route memory | Beta candidate | Strong automated coverage and explicit migration logic. | Run resize/minimum-size, restore-after-crash, reopen-last-session, and multi-pane keyboard QA. |
| Space creation, rename, leave, delete | Beta candidate | Full API/store paths and guarded destructive flows exist. | Verify empty account, limits, invitations, ownership transfer, and post-delete navigation. |
| Members and invitations | Beta candidate | Permission-aware implementation and server contracts exist. | Verify invite acceptance from a second clean account and every owner/member permission boundary. |
| Space Chat | Beta candidate | Durable conversations, replies, reactions, attachments, mentions, permissions, read state, realtime behavior, and error states are implemented and tested. | Replace browser `confirm`/`alert` deletion handling and perform two-account realtime QA. |
| Journal Notes | Beta candidate | CRUD, search, archive/delete restrictions, block editing, collaboration, assets, and empty/error states exist. | Replace browser prompts/confirms, verify long-note persistence and two-account collaboration. |
| Drawings core | Contained fix | CRUD, canvas collaboration, presence, permissions, and error states exist with useful coverage. | Verify persistence/reconnect and replace browser confirms. Keep Figma out of the first beta. |
| Planner Tasks: board and list | Beta candidate | Full CRUD, filters, assignments, priorities, due dates, versioning, AI proposals, and tests exist. | Perform cross-account drag/edit/conflict QA and ensure empty boards explain the first action. |
| Agenda: day/week/month and native events | Contained fix | Native event APIs and UI are implemented, but the interaction surface is large and less tested than Tasks. | Verify timezones, all-day events, DST, edit/delete, and failure recovery. |
| Goals, Milestones, and Roadmaps | Coming Soon | This is a separate graph/editor product with custom node definitions, layouts, inspectors, and cross-links. It materially expands onboarding and QA. | Gate all three subsections with `ComingSoonSurface`; promote only after a dedicated journey pass. |
| Space Library: basic upload/browse/preview/download | Beta candidate | Permission-aware upload, collections, albums, metadata, previews, quotas, and server storage paths exist. | Test the 128 MB limit, quota edges, unsupported formats, duplicate names, permissions, and failed upload recovery. |
| Smart Library analysis and memory controls | Coming Soon | Explicitly pilot-capped; includes device scanning, extracted content, hosted analysis, privacy explanations, and usage settlement. | Hide analysis/memory entry points while keeping ordinary Library behavior. |
| Files: local browse/search/preview/basic mutations | Contained fix | Native implementation is extensively tested, but Files contributes to the current type failures and exposes several raw prompt-based actions. | Fix types, replace or hide raw prompt actions, and run local/remote destructive-operation QA. |
| Connected cloud storage | Conditional contained fix | Google Drive, OneDrive, and Dropbox native workflows exist, but readiness depends on real OAuth/provider credentials and quota/error behavior. | Keep only providers that pass connect, refresh-after-sleep, browse, upload, download, disconnect, and revoked-token tests. |
| Connected devices and clipboard sharing | Coming Soon | Pairing, peer presence, remote browsing, clipboard consent, and P2P trust form a separate high-risk product; device rename still uses `window.prompt`. | Remove the Connected Devices section from Files for the first beta. |
| Browser | Contained fix | Native webview, history, downloads, offline UI, compatibility notices, permissions, and agent-scoped controls have strong native/UI tests. | Test login-heavy sites, popups, downloads, media, sleep/wake, external links, and incompatible pages. |
| Inbox | Conditional contained fix | Gmail/Outlook read, compose, reply, forward, attachment, cache, and error paths exist with good UI/server tests. Email sending is a high-trust action and requires production OAuth validation. | Hide unless Gmail and/or Outlook passes a complete live OAuth, send, refresh, revoke, and sleep/wake pass. |
| Global Misty and durable conversations | Contained fix | Conversation lifecycle, multimodal uploads, model choice, approvals, cancellation, and runtime/server paths are implemented. Current global-search types/lint/readability are not clean. | Fix build gates, cap spend, verify failure/retry/cancel, and ensure every action remains reviewable. |
| MCP connection management | Coming Soon | Remote server credentials, tool permissions, runtime access tokens, and third-party failure modes add a separate security/support surface. | Hide management UI unless the public beta is explicitly an MCP pilot. |
| Code | Contained fix | File tree, editor, Git, LSP, search, multi-buffer, patch review, and native project handling are implemented with solid tests. | Test real projects, large repositories, missing language servers, binary files, and patch rollback. |
| Terminal and SSH | Contained fix | PTY, UTF-8, input safety, SSH argument safety, environment selection, shortcuts, and AI staging exist. | Test shell lifecycle, process cleanup, copy/paste, resize, SSH host keys, and destructive-command review. |
| Transfers | Removed from public beta | The implementation remains recoverable, but routes, navigation, menus, settings, shortcuts, and saved-workspace restoration no longer expose it. | Reintroduce only after the transfer matrix and recovery UX pass a dedicated release cycle. |
| Built-in Extensions | Removed from public beta | The runtime remains recoverable, but the public renderer no longer exposes extension routes, navigation, settings, workspace tabs, commands, or status controls. | Reintroduce a signed, tightly scoped catalog in a later release. |
| Third-party extension marketplace | Coming Soon | Public third-party installation requires review policy, signing expectations, permission UX, support ownership, and incident response. | Hide arbitrary marketplace installs; retain built-in extensions if desired. |
| Activity and notifications | Contained fix | In-app/native preferences and badge behavior are implemented and tested. | Verify OS permission denial, quiet hours, duplicate notifications, and multi-account isolation. |
| Settings: general, appearance, shortcuts, privacy | Beta candidate | Broad implementation exists and preferences fail closed where relevant. | Replace browser confirms where practical and verify every setting persists and actually changes behavior. |
| Settings: updates | Launch blocker | The UI promises signature verification while updater configuration is empty. | Resolve updater distribution before exposing the control. |
| Proactive recaps | Coming Soon | Scheduled prompts, cadence, timezone, citations, and ongoing AI spend create a distinct automation product. | Gate recap controls for the first beta. |
| Cursor companion | Removed | The pointer-following companion and its settings/client surface have been removed rather than presented as a future feature. | None for the first beta. |
| Floating Misty panel | Coming Soon | The always-on-top orb/panel expands lifecycle, focus, accessibility, and multi-monitor behavior. Its native window is no longer created at startup. | Reintroduce only as an explicit opt-in after cross-platform QA. |

## Public website classification

| Surface | Status | Before launch |
| --- | --- | --- |
| Home and feature storytelling | Beta candidate | Align claims with the final visible desktop surface and remove “operating system” language if the beta is intentionally narrow. |
| Download page | Contained fix | Show only clean-machine-tested signed builds; include OS minimums, architecture, checksum, install help, and known limitations. |
| Registration and sign in | Contained fix | Fix authenticated end-to-end test routing and verify live cookie/domain behavior. |
| Pricing | Conditional contained fix | Keep informational pricing if desired, but do not enable paid checkout until legal and billing gates pass. |
| Account settings | Contained fix | Fix end-to-end tests and verify export, deletion, avatar, telemetry preferences, device licensing, usage, and billing. |
| Roadmap | Beta candidate | Make it the canonical destination for every Coming Soon link and keep availability statements accurate. |
| Changelog | Contained fix | Change “Private beta” language if this is the public beta release and publish actual known limitations. |
| Blog | Beta candidate | The only post is explicitly marked archival; add a current public-beta launch post when ready. |
| Privacy, Terms, License | Launch blocker | Replace all stubs with reviewed documents. |
| Forum | Coming Soon | It is static invented data, has nonfunctional posting/reply controls, and is not currently routed. Do not publish it. |
| Social links | Contained fix | X, Discord, YouTube, and Instagram are visible `#` placeholders. Remove each one until a real destination exists. |
| Public releases repository | Beta candidate | `misty-org/misty-public` is already the website's public release destination and can also host issue forms without exposing source. |

## Recommended first public navigation

Keep the initial visible navigation intentionally small:

1. Spaces
   - Journal Notes
   - Planner Tasks
   - Chat
   - Library
2. Files
3. Browser
4. Misty
5. Settings

Promote Code, Terminal, Transfers, Inbox, Drawings, and built-in Extensions only after their manual
journeys pass. This is a launch-order recommendation, not a claim that their implementations should
be discarded.

## Coming Soon component

The shared `ComingSoonSurface` component lives in `app/src/shared/ui/coming-soon-surface.tsx`. It
fills its containing route or pane, renders a feature-specific “is coming soon” heading, and places
one external link to `https://mistysys.com/roadmap` at the bottom. Routes should use this component
instead of one-off placeholders so gated areas stay honest and visually consistent.

The first public-beta cut now uses it for Goals, Milestones, Roadmaps, smart Library analysis and
memory controls, MCP connection management, and recurring briefings. Connected Devices is removed
from Files, and the floating Misty panel is disabled at native startup.

## Onboarding implemented

The account-scoped first-run flow now guides a person through purpose, an existing or new private
Space, a real first action, and an explicit hosted-AI choice. Completion and Skip persist per
account and deployment. Finishing opens the selected note, task, chat, or Library action instead of
ending on a generic success screen.

## Help, recovery, and feedback implemented

The profile menu now exposes Report a problem, which opens a dedicated Help & recovery settings
section. People can recover the last closed tab, reset only the saved workspace arrangement, or
reload after an explicit unsaved-work warning. The same section creates a prefilled public ticket
in `misty-org/misty-public` for review. A redacted diagnostic JSON bundle is generated and
downloaded separately; it is never uploaded or attached automatically.

## Next execution order

1. Fix the website end-to-end API base and rerun the full suite.
2. Publish legal documents and configure signed updates.
3. Run the clean-install launch journey and promote conditional surfaces one at a time.
