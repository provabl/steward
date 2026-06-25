# Changelog

All notable changes to steward will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Initial repo scaffold** — `steward`, the Provabl suite's data-ingestion stewardship tool
  (move-to-compute governance; the counterpart to the compute-to-data chain). Go 1.26.4,
  Apache-2.0 / Playground Logic LLC, cobra CLI root. Where vet qualifies the software that arrives
  at an SRE, steward qualifies the *data* brought into it. See `business/steward-product-spec.md`
  and provabl ADR 0004. v1 scope: provenance record/verify, the `data://` appraisal gate
  (→ `.steward/gate-result.json` / `context.data.*`), audit log, preflight. Transport (the mover),
  S3 Object Lock handling, and closeout/destruction are deferred — v1 governs data moved out-of-band.
