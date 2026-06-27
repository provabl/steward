# Movers — the pluggable transport seam

steward governs move-to-compute ingestion; it does **not** own the transport. The
part that actually copies bytes into the secure account — Globus, AWS DataSync,
`aws s3 cp`, rclone, an institutional wrapper script — is a **pluggable mover**
behind the `mover.Mover` interface (`internal/mover`). This records *why* movers
are pluggable, *how* they plug in, and the safety properties that make a fully
generic mover sound. It refines commitment #1 of
[provabl ADR 0004](https://github.com/provabl/provabl/blob/main/docs/adr/0004-data-steward-move-to-compute.md)
("governance, not transport — the mover is replaceable; the governance is the product").

## Why pluggable

- **The transport landscape is heterogeneous and not steward's domain.** Globus
  High-Assurance is the dbGaP norm; other sites use DataSync, `s3 cp`, or rclone.
  Each has its own auth, configuration, async lifecycle, and failure modes.
  Hard-coding one would couple steward's governance to a transport choice that
  varies per institution.
- **It keeps steward small.** steward's value is the *governance wrapper*
  (authorize-before, provenance-during, handling/closeout-after), not transport
  code. The seam lets that wrapper be developed and tested without any transport.
- **It is already what makes steward testable.** The interface is exactly why the
  whole `ingest` flow is fake-tested with zero AWS.

## How movers plug in — exec-first

The **primary** extensibility path is the generic, exec-based **command mover**,
not per-transport SDK integrations. This mirrors how `vet` shells out to
cosign/syft/grype rather than importing their SDKs: steward needn't know Globus or
DataSync exists.

```bash
steward ingest --dataset phs000178 \
  --source globus:dtn.ncbi.nlm.nih.gov/dbgap/phs000178 --dest s3://sre/genomic/phs000178/ \
  --dua-id DUA-2025-001 --principal arn:…:role/Alice \
  --mover command --mover-command "globus transfer {source} {dest}"
```

`{source}` and `{dest}` are substituted into the command's argv. Any scriptable
transport works — `aws s3 cp {source} {dest}`, `rclone copy {source} {dest}`, a
site wrapper — with **no steward coupling and no new steward dependency**.

| Mover (`--mover`) | What it is | When |
|---|---|---|
| `local` (default) | the in-tree reference mover: copies a local path / `file://`, hashing as it copies | local/testing; the template every mover follows |
| `command` | runs `--mover-command` to move the bytes | **the extensibility path** — any transport an operator can invoke |
| *native SDK movers* | a Go impl importing a transport's SDK | **only** if a transport needs features the command path can't express (e.g. Globus activation/resumption telemetry) — not built speculatively |

## Safety — why a fully generic mover is sound

Two properties make accepting an arbitrary operator-supplied transport safe:

1. **No shell.** The command runs as **argv** (`exec`, not `sh -c`). `{source}` /
   `{dest}` are substituted as whole argv *elements*, never concatenated into a
   string a shell would re-split — so a crafted source/dest cannot inject
   arguments or commands. (Verified by a test that feeds a source full of shell
   metacharacters and asserts it arrives as one untouched argument.)

2. **Zero trust in the transport.** steward never believes the mover about what it
   moved. The mover reports a digest, but steward records it as **asserted**
   (`integrity_verified=false`); `steward provenance verify` independently
   recomputes the digest against the destination before the gate will pass. The
   command mover goes further: for a local destination it computes the sha256
   *itself* after the copy rather than parse the command's output at all. A buggy
   or hostile mover **cannot** assert "intact" and be believed.

This is the load-bearing point: pluggability is safe *because* of the
re-verification, not in spite of it. The mover is replaceable precisely because
nothing downstream trusts it.

## What a mover must (and must not) do

A `Mover` only **transports**. It must not apply any governance — no authorization,
no tagging, no retention, no destruction. Authorization happens *before* the mover
runs (`ingest` calls `Authorize` first, fail-closed), provenance recording happens
*after*, and handling/closeout are separate commands. A mover that tried to do
governance would be both out of its lane and untrusted anyway.

See `internal/mover` for the interface and the `local` / `command` impls, and ADR
0004 for the move-to-compute design.
