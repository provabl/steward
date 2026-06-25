# steward — required AWS permissions

`steward preflight` verifies the calling AWS principal holds these actions, using
read-only `iam:SimulatePrincipalPolicy` against the caller ARN (from
`sts:GetCallerIdentity`). It **evaluates, it never acts** — running preflight changes
nothing. A denied action prints a remediation and the command exits non-zero.

Most of steward's flow is **local and needs no AWS at all**: `provenance record`,
`gate`, and `log` only read and write the `.steward/` store. The AWS-touching paths
are the ones below.

| Action | Needed by | Status |
|--------|-----------|--------|
| `sts:GetCallerIdentity` | preflight itself (resolves the caller ARN to simulate) | live |
| `iam:SimulatePrincipalPolicy` | preflight itself (the permission self-check) | live |
| `s3:GetObject` | `provenance verify` against an S3 destination — recompute the digest of the ingested bytes | **deferred**¹ |
| `s3:PutObjectTagging` | `apply-handling` — write the data-class tag on the destination prefix | **deferred**² |
| `s3:PutObjectRetention` | `apply-handling` — apply S3 Object Lock retention for the DUA term | **deferred**² |

¹ **`provenance verify` (PR5)** recomputes the content digest against the
destination. v1 verifies **local / `file://`** paths (e.g. a mounted copy of the
landing zone); the S3 reader is the deferred AWS slice. The recompute logic is
transport-agnostic (an injected `ObjectReader`), so enabling S3 is a wiring change —
`s3:GetObject` is listed here so an operator can confirm readiness before it lands.

² **`apply-handling`** (data-class tag + Object Lock retention) is a deferred
follow-on: applying retention is high-consequence and cannot be exercised in CI
without live resources, so v1 ships only the `internal/handling.Tagger` interface
seam. The actions are listed so the principal can be provisioned ahead of the impl.

## Why preflight checks deferred-path actions

The check is read-only, and over-provisioning a *simulation* costs nothing. Listing
the deferred actions lets an operator confirm the steward principal is ready
**before** those paths are enabled, rather than discovering a missing grant when the
impl lands. To scope a principal to only the live paths today, grant
`sts:GetCallerIdentity` + `iam:SimulatePrincipalPolicy` (preflight) and the local
flow needs nothing else.

## Boundary

steward **applies handling and records provenance; it never decides access** (attest
does, via the Cedar PDP reading `context.data.*`) and **never destroys data** outside
the separate, certify-and-confirm closeout path (a deferred follow-on — destruction is
never silent). See `business/steward-product-spec.md` and provabl ADR 0004.
