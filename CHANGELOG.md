# Changelog

All notable changes to steward will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`internal/evidence` — the `data://` (ASP, appraiser) pair** (v1 PR3): steward's provider for the
  provabl/evidence kernel, the data-plane analogue of vet's `artifact://` pair. The measurer fetches a
  provenance record (fail-closed `CollectFailed` when none); the appraiser emits the `context.data.*`
  claim contract (`ProvenanceVerified`, `SourceVerified`, `IntegrityChecked`, `DUAId`, `DataClass`,
  `SubjectDigest`, `RecordedAt`) and applies param-driven judgment — crucially, an **unverified digest
  cannot pass** (`integrity_verified=false` → fail), enforcing "recomputed and matched, not asserted."
  `EphemeralAM` (per-run ed25519 key; freshness rides the kernel's outer SIG). Tested through the real
  CVM across pass/unverified/missing-DUA/wrong-class/source-allowlist/missing-record paths, plus a
  **golden test pinning the `context.data.*` claim keys** so the attest contract can't silently drift.

- **`internal/store` + `steward provenance record`** (v1 PR2): the `.steward/` store and the first
  command. `ProvenanceRecord` captures what data was ingested, from where, its digest, the governing
  DUA, and the authorizing principal; `GateResult` is the lowered `context.data.*` shape attest will
  read (PR4). Records are keyed by destination (re-recording a dest supersedes). `record` writes an
  *asserted* digest with `integrity_verified=false` — only `provenance verify` (PR5) recomputes it
  against the destination and flips it, so the provenance claim means "steward recomputed and matched,"
  not "someone told us." Fully unit-tested (round-trip, missing→nil, overwrite-by-dest, list, CLI).

- **Initial repo scaffold** — `steward`, the Provabl suite's data-ingestion stewardship tool
  (move-to-compute governance; the counterpart to the compute-to-data chain). Go 1.26.4,
  Apache-2.0 / Playground Logic LLC, cobra CLI root. Where vet qualifies the software that arrives
  at an SRE, steward qualifies the *data* brought into it. See `business/steward-product-spec.md`
  and provabl ADR 0004. v1 scope: provenance record/verify, the `data://` appraisal gate
  (→ `.steward/gate-result.json` / `context.data.*`), audit log, preflight. Transport (the mover),
  S3 Object Lock handling, and closeout/destruction are deferred — v1 governs data moved out-of-band.
