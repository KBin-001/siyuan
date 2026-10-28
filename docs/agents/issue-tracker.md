# Issue tracker: Local Markdown

Issues and specs live in `.scratch/<feature-slug>/`.

## Conventions

- Store the feature specification as `spec.md` in its feature directory.
- Store each implementation ticket separately as `issues/<NN>-<slug>.md`, numbered from 01 in dependency order.
- Record triage state near the top as `**Status:** <role>` using `docs/agents/triage-labels.md`.
- Record dependencies as `**Blocked by:**` followed by ticket numbers and titles, or `None (can start immediately)`.
- Append discussion under `## Comments`.
- Publish to the tracker by creating the corresponding local Markdown file; fetch a ticket by reading its file.
- Keep credentials, repository encryption keys, real note content, and exported configuration packages outside tracked planning files.

## Wayfinding operations

- Store the effort map in `.scratch/<effort>/map.md`, with child tickets in `issues/`.
- Record ticket kind in `Type:` and progress in `Status:`; use `claimed` and `resolved` for execution state.
- Work on an open, unclaimed ticket only after all its listed blockers are resolved.
- Claim by saving `Status: claimed` before implementation.
- Resolve by appending `## Answer`, setting `Status: resolved`, and adding the result pointer to the map's Decisions-so-far.
- Prefer the lowest numbered eligible ticket. Explicit user instructions determine whether to start implementation.
