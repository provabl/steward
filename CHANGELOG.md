# Changelog

All notable changes to steward will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`steward preflight` + the deferred `mover` / `handling` interface seams** (v1 PR6, completes v1):
  `steward preflight` verifies the calling principal holds steward's AWS-touching IAM actions
  (`s3:GetObject` for verifying an S3 destination's digest; `s3:PutObjectTagging` /
  `s3:PutObjectRetention` for the deferred apply-handling path) via read-only
  `iam:SimulatePrincipalPolicy` against the caller — it evaluates, it never acts; a denied action
  prints a remediation and exits non-zero. **Fail-closed** (a credential/simulator failure is an error
  result, not a silent pass); cloned from vet's mockable STS/IAM check and fully fake-tested. Added
  `internal/mover.Mover` (pluggable transport seam — Globus / DataSync / s3cp; "the mover is pluggable,
  the governance is the product") and `internal/handling.Tagger` (data-class tag + Object Lock
  retention seam), **interfaces only — no live impls**: v1 governs out-of-band-moved data, and applying
  retention is high-consequence + CI-untestable, so `steward ingest` / `apply-handling` are deferred
  follow-ons that plug into these contracts. New `docs/required-permissions.md` maps each action to its
  command and live/deferred status.

- **`steward provenance verify` + `steward log`** (v1 PR5): `provenance verify` recomputes the
  sha256 of the bytes at a recorded destination and compares it to the digest captured at `record`
  time; on a match it flips the record to `integrity_verified=true` — which is what makes steward's
  provenance claim mean "steward recomputed and matched," not "a mover told us." On a mismatch the
  record is left unverified and verify exits non-zero (tamper is detectable). The destination read
  is behind an injected `internal/provenance.ObjectReader`, so the recompute logic is fully fake-
  testable; the CLI's production reader handles local / `file://` paths, with S3 and other mover
  schemes deferred to the AWS slice. `steward log` (over `internal/audit`) lists the ingestion audit
  trail — dataset, source, DUA, data class, and verified-state, newest first — filterable by
  `--data-class` / `--dua-id`, with `--json`. End-to-end smoke: gate *fails* on an asserted-only
  digest, `verify` flips it, gate then *passes*; a tampered file fails verify.

- **`steward gate` + `internal/gate`** (v1 PR4): evaluates an ingested destination's provenance
  record through the evidence kernel and writes `.steward/gate-result.json` (the `context.data.*`
  attributes attest's Cedar PDP reads). Runs the canonical `Seq(Nonce, Seq(Meas, Sig))` term, appraises,
  and lowers — judgment lives in the `data://` pair, not the gate. **Fail-closed**: a destination with
  no provenance record, or one whose digest is unverified, does not pass (and still writes a
  fail-closed result so pipelines get the artifact). Policy flags `--dua-required` (default on),
  `--require-data-class`, `--allowed-sources`. Added `data.Dataset` to the claim set so the dataset id
  flows to the PDP. New `docs/integrations/attest.md` documents the `context.data.*` contract +
  reader-ownership (consumer lives in attest). Unit-tested (pass / unverified / missing-record /
  wrong-class) + a record→gate end-to-end check.

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
