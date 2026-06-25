# steward

**Data-ingestion stewardship for AWS Secure Research Environments.**

> **Boundary:** steward owns the **data plane's move-to-compute boundary** — it records the
> provenance of data brought *into* a secure account, appraises it, and writes the attribute attest's
> PDP gates a read on. Where [vet](https://github.com/provabl/vet) qualifies the *software* and
> [qualify](https://github.com/provabl/qualify) qualifies the *person*, steward qualifies the *data*.
> It does **not** move bytes (the mover is pluggable/external), decide access (attest does), or build
> the account (ground does).

Part of the [Provabl](https://provabl.dev) suite:
- **[ground](https://ground.provabl.dev)** — deploy correct AWS foundations
- **[attest](https://attest.provabl.dev)** — compile, enforce, and prove compliance
- **[qualify](https://qualify.provabl.dev)** — train and qualify researchers
- **[vet](https://vet.provabl.dev)** — verify the software supply chain
- **steward** — govern data brought into the boundary ← you are here

> Ground your infrastructure, attest your controls, qualify your people, vet your software, steward your data.

---

## What steward does

The compute-to-data model (provabl ADR 0002) lets an SRE reach a dataset *in place*. Its sibling,
**move-to-compute** — copying data *into* the SRE (a Globus transfer to S3, a bulk load) — was
unowned: the suite could prove the person, the software, the machine, and the network path, but
*nothing about a dataset that was copied in*. steward closes that: it records where the bytes came
from and that they're intact, checks the ingestion was authorized against a current
[DUA](https://github.com/provabl/provabl/blob/main/docs/guide/glossary.md#dua-data-use-agreement),
and lowers a verified-provenance attribute attest's Cedar PDP gates the data-read on.

```mermaid
flowchart LR
    data["data brought in<br/>(Globus / DataSync / s3 cp)"] --> steward["<b>steward</b><br/>record + appraise provenance"]
    steward --> json[".steward/gate-result.json"]
    json -->|"context.data.*"| attest["<b>attest</b><br/>Cedar PDP"]
    qualify["qualify approval<br/>(the DUA)"] -.->|"authorizes"| steward
```

**Governance, not transport.** steward does not move bytes — Globus/DataSync/`s3 cp` do. steward wraps
that movement with the governance the suite was missing: an authorization check, a recorded
provenance record, and (later) handling/retention and closeout.

## Core concepts

(terms link to the suite [glossary](https://github.com/provabl/provabl/blob/main/docs/guide/glossary.md))

- **[Move-to-compute](https://github.com/provabl/provabl/blob/main/docs/guide/glossary.md#move-to-compute)** — data is transferred *into* the SRE (vs [compute-to-data](https://github.com/provabl/provabl/blob/main/docs/guide/glossary.md#compute-to-data), where it's reached in place). steward governs the former.
- **Provenance record** — source URI, content digest, transfer-integrity result, governing DUA, authorizing principal: *what came in, from where, under what authority.* The move-to-compute analogue of vet's SBOM/verification record.
- **`data://` [(ASP, appraiser) pair](https://github.com/provabl/provabl/blob/main/docs/guide/glossary.md#asp-attestation-service-provider)** — steward registers one against the [evidence kernel](https://github.com/provabl/provabl/blob/main/docs/guide/glossary.md#evidence-kernel), so the provenance is *appraised and fresh*, not a bare assertion.
- **[Lowered attribute](https://github.com/provabl/provabl/blob/main/docs/guide/glossary.md#lowered-attribute)** — the verdict becomes `.steward/gate-result.json` (→ `context.data.*`); attest reads it. **attest stays the [PDP](https://github.com/provabl/provabl/blob/main/docs/guide/glossary.md#pdp-policy-decision-point); steward never decides.**

## Usage

The move-to-compute lifecycle: **record** what came in, **verify** the bytes are intact, **gate** the
destination (writing the attribute attest reads), and **log** the audit trail.

```bash
# 1. Record provenance for data that landed in the SRE (asserted digest).
steward provenance record \
    --dataset phs000178 \
    --dest s3://sre-genomic/dbgap/phs000178/ \
    --source globus:dtn.ncbi.nlm.nih.gov/dbgap/phs000178 \
    --digest sha256:8e1ebe… --dua-id DUA-2025-001 --data-class GENOMIC \
    --authorized-by compliance@mru.edu

# 2. Verify: recompute the digest against the destination bytes.
#    On a match this flips integrity_verified=true — the provenance claim now
#    means "steward recomputed and matched," not "a mover told us." Mismatch → exit 1.
steward provenance verify --dest s3://sre-genomic/dbgap/phs000178/

# 3. Gate: appraise through the evidence kernel, write .steward/gate-result.json
#    (context.data.*). An unverified digest fails closed; exits non-zero if policy isn't met.
steward gate --dest s3://sre-genomic/dbgap/phs000178/ --dua-required

# 4. Audit trail: what came in, from where, under what DUA, and whether it's verified.
steward log --data-class GENOMIC

# Preflight (optional, for the AWS-touching paths): confirm the calling principal
# holds the IAM actions steward needs. Read-only — it evaluates, it never acts.
steward preflight --region us-west-2
```

> **v1 verify scope:** `verify` recomputes against local / `file://` destinations (e.g. a mounted
> copy of the landing zone). S3 and other mover-specific schemes are deferred to the AWS slice; the
> recompute logic itself is transport-agnostic (an injected reader), so adding S3 is a wiring change.

## Install

```bash
go install github.com/provabl/steward/cmd/steward@latest   # requires Go 1.26.4+
# or build from a clone: go build ./cmd/steward
```

**Prerequisites.** Go 1.26.4+. The provenance record/gate/log flow runs locally with no AWS access;
`provenance verify` (digest re-check against an object) needs read access to the destination. Run
`steward preflight` to confirm the calling principal holds what the AWS-touching paths need.

## Status

**v1 complete** — provenance `record`/`verify`, the `data://` appraisal `gate` (→ `.steward/gate-result.json`
/ `context.data.*`), `log`, and `preflight` all ship. The `mover` (transport) and `handling` (data-class
tag + Object Lock retention) **interface seams** are defined; their live impls — `steward ingest` (drive
a mover) and `apply-handling` (S3 Object Lock) — plus closeout/destruction are deferred follow-ons
(retention/destruction are high-consequence and need live AWS). See `business/steward-product-spec.md`
(in the umbrella) and provabl ADR 0004 for the full roadmap.

## License

Apache 2.0. Copyright 2026 Playground Logic LLC.
