<!--
SPDX-FileCopyrightText: 2026 Playground Logic LLC
SPDX-License-Identifier: Apache-2.0
-->

# steward → attest: the `context.data.*` contract

steward is a **producer**: it appraises a data-ingestion provenance record through the
provabl/evidence kernel and writes the lowered verdict to `.steward/gate-result.json`.
attest's Cedar PDP **consumes** that file as `context.data.*` attributes and gates a
data-read on them — the data-plane counterpart to vet's `context.workload.*`
(`.vet/gate-result.json`). Neither product imports the other; the durable interface is
the JSON file.

## What steward writes (`.steward/gate-result.json`)

```json
{
  "dataset": "phs000178",
  "dest": "s3://sre/genomic/phs000178/",
  "provenance_verified": true,
  "source_verified": true,
  "integrity_checked": true,
  "dua_id": "DUA-2025-001",
  "data_class": "GENOMIC",
  "digest": "sha256:…",
  "policy_met": true,
  "evaluated_at": "2026-06-25T00:00:00Z"
}
```

## The `context.data.*` attributes attest reads

The claim keys are pinned by a golden test in `internal/evidence` so they cannot drift
without a deliberate change here:

| Cedar attribute | Meaning |
|---|---|
| `context.data.Dataset` | the dataset / study id (e.g. `phs000178`) |
| `context.data.ProvenanceVerified` | steward **recomputed** the content digest at the destination and it matched (not merely a mover-asserted digest) |
| `context.data.SourceVerified` | the recorded source URI is on the allowed-sources list |
| `context.data.IntegrityChecked` | an integrity re-check was performed |
| `context.data.DUAId` | the governing Data Use Agreement id |
| `context.data.DataClass` | the dataset's data class (e.g. `GENOMIC`) |
| `context.data.SubjectDigest` | the content digest |
| `context.data.RecordedAt` | when provenance was recorded (a recency signal) |
| `attested` (overall) | `policy_met` — the kernel's overall pass, via `lower.ToAttributes` |

## Example policy (illustrative)

A Cedar policy that permits a read of a controlled dataset only when the data was
ingested with verified provenance under the dataset's DUA, **and** the principal holds
that DUA (from qualify/attest), composes the two `context` groups:

```cedar
forbid (principal, action, resource)
when {
  resource has data_controlled && resource.data_controlled == true &&
  !(context has data && context.data.ProvenanceVerified == true &&
    context.data.DUAId != "" &&
    principal.nih_approval_dua_ids.contains(context.data.DUAId))
};
```

## Reader ownership

The reader lives **only in attest** (mirroring `attest/internal/workload` for vet) — a
small `internal/dataprov` (or similar) that loads `.steward/gate-result.json` into
`context.data.*`. steward owns the producer + this contract; attest owns the consumer.
This is the one-way `producer → attest` direction every suite tool follows.
