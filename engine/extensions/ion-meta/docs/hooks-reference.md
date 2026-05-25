# Hooks Reference (pointer)

This in-tree doc was retired to prevent drift against the canonical reference.

Read the canonical source instead:

- **Inside an Ion session**: call `ion_read_doc path: hooks/reference.md` from the ion-meta orchestrator. The hook catalog itself is exposed through `ion_list_hooks` (live, parsed from the SDK source -- never drifts).
- **In the repo**: `docs/hooks/reference.md` is the source of truth.
- **At an installed ion-meta**: the canonical copy is bundled at `~/.ion/extensions/ion-meta/docs/canonical/hooks/reference.md`.

ion-meta's `catalog.ts` derives the hook list from the bundled SDK's `types.ts` at runtime, so the catalog never drifts from the engine's contract. The reference doc adds payload types, return semantics, and worked patterns -- consult it when the catalog alone is not enough.
