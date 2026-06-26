# steward — required AWS permissions

`steward preflight` verifies the calling AWS principal holds these actions, using
read-only `iam:SimulatePrincipalPolicy` against the caller ARN (from
`sts:GetCallerIdentity`). It **evaluates, it never acts** — running preflight changes
nothing. A denied action prints a remediation and the command exits non-zero.

Most of steward's flow is **local and needs no AWS at all**: `provenance record`,
`gate`, and `log` only read and write the `.steward/` store. The AWS-touching paths
are the ones below.

| Action | Needed by |
|--------|-----------|
| `sts:GetCallerIdentity` | preflight itself (resolves the caller ARN to simulate) |
| `iam:SimulatePrincipalPolicy` | preflight itself (the permission self-check) |
| `iam:ListRoleTags` | `ingest --authorizer iam` — read the principal's `attest:nih-dua-ids` tag |
| `s3:GetObject` | `provenance verify` against an S3 destination — recompute the digest |
| `s3:ListBucket` | `apply-handling` / `closeout` — list the objects under the destination prefix |
| `s3:GetObjectTagging` / `s3:PutObjectTagging` | `apply-handling` — read/write the `steward:data-class` tag |
| `s3:GetObjectRetention` / `s3:PutObjectRetention` | `apply-handling` (set) + `closeout` (check elapsed) — S3 Object Lock retention |
| `s3:ListBucketVersions` | `closeout` — enumerate object versions + delete-markers to destroy |
| `s3:DeleteObject` | `closeout` — destroy every version once retention has elapsed |

**Scoping by command.** The `provenance record` / `gate` / `log` flow is **local — no
AWS at all**. To scope a principal narrowly: preflight needs `sts:GetCallerIdentity` +
`iam:SimulatePrincipalPolicy`; `ingest --authorizer iam` adds `iam:ListRoleTags`;
`apply-handling` adds the S3 tag/retention actions; `closeout` adds
`s3:ListBucketVersions` + `s3:DeleteObject`. The destination bucket must have **S3
Object Lock enabled** for retention to apply.

## Boundary

steward **applies handling and records provenance; it never decides access** (attest
does, via the Cedar PDP reading `context.data.*`). `closeout` **does** destroy data,
but only via the certify-and-confirm path: a dry run by default, refusing until any
Object Lock retention has elapsed and requiring an explicit confirming principal —
never a silent delete. It also **reads, never writes, the `attest:*` namespace** (the
`iam` authorizer reads `attest:nih-dua-ids`; qualify/attest own those tags). See
`business/steward-product-spec.md` and provabl ADR 0004.
