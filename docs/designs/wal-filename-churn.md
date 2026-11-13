# WAL filename churn and reclaimable slab

Investigation and local validation, 2026-11-13. This patch removes speculative
successors when no write is pending. It does **not** eliminate the approximately
400 data-bearing WAL files/second required by the observed workload and current
rotation policy, or establish that production memory growth is resolved.

## Expected memory impact

The intended benefit is less **new reclaimable kernel filesystem metadata**
from avoidable unique WAL names. No before/after Linux slab measurement or
production memory saving is available for this patch. It is not possible to
state a defensible MiB/hour reduction from the file-operation regression alone.

| Memory component | Expected effect of this change | What has been measured |
| --- | --- | --- |
| Reclaimable slab / container working set | Avoiding empty successors reduces opportunities to populate inode, dentry, and related metadata caches. Existing cache entries remain until normal reclamation, so memory need not fall immediately. Continuously busy streams can see little benefit. | Before the patch, production reclaimable slab grew 44.36–46.71 MiB/hour. The isolated ext4 probe reproduced retention after deletion. Neither measurement is a patched-versus-baseline comparison. |
| Live segment state / Go heap | While awaiting another write, a deferred successor holds no file descriptor, flusher goroutine, or active writer state. This can reduce simultaneous live state. Writer buffers returned to the existing pool may remain retained, so there is no established per-segment retained-heap saving. | Local sampled end-of-test heap was about 3.09 MiB before and 3.00 MiB after in one run. This small, noisy difference does not establish a heap reduction. |
| Anonymous RSS | Less live segment state may affect process memory, but allocator, stack, and pool behavior prevent predicting an RSS reduction. | Local maximum total process RSS was 20.23 MiB before and 19.53 MiB after in one run. Anonymous RSS was not separately measured, and the difference is not a validated saving. |
| File cache | The unused successor's header is never written, avoiding its cache activity. Data-bearing payloads still use the same WAL path. | No separate before/after file-cache measurement. |
| Unreclaimable slab | No specific reduction is established; this is not the component showing the supplied production growth. | Production was approximately 1.8–2.6 MiB before the patch; no patched comparison. |

The demonstrated result is **212 fewer unique creates and unlinks (10.6%)**
in the finite mixed-workload regression, not 10.6% less container memory or
slab growth. Do not apply that percentage to the production 44–47 MiB/hour
slope or use the probe's approximately 304 bytes/name as a production savings
estimate. Most observed production turnover already corresponds to data-bearing
segments, and the fraction of eliminated names that would have stayed cached
is unknown. Production benefit could be small even though the unused-file
lifecycle is demonstrably removed.

The memory acceptance test is a sustained baseline-versus-patched comparison
on matching Linux/ext4 and WAL storage, at equal input rate and table mix,
recording both memory slopes and file-operation counts, followed by controlled
reclamation testing in disposable staging. Until then, this patch is an
avoidable-churn reduction with an unquantified kernel-memory benefit. The
[staging procedure below](#optional-linuxext4-experiment-and-staging-acceptance)
defines the remaining measurements.

## Source and production evidence

Baseline: [e27ca748](https://github.com/Azure/adx-mon/commit/e27ca74887b40a6ff5f2c855ccf764738024cdf9).
The [ingestor image build](https://github.com/Azure/adx-mon/actions/runs/43476461807/job/132061444298)
records both `build-arg:GIT_COMMIT` / OCI revision equal to that commit and
`containerimage.digest` equal to
`sha256:562bd0fbcbf631c2d4cc8a2cd69b7e276cdfbf5b51d9a5eb54928d2e1e9915d0`.
Mango's southafricanorth overlay independently pins that digest and names the
same source. The build record verifies the mapping beyond the overlay comment.

The supplied captures in `/tmp/zan-pprof-20261113` distinguish these components:

| Component | Evidence and interpretation |
| --- | --- |
| Go live heap | Post-GC profiles around 58–65 MiB; later pod 0 capture 62 MiB. HTTP/TLS buffers dominate. A small independent slow leak remains possible. |
| Anonymous memory | Cgroup `anon` ranges 226–234 MiB on pod 0 and 262–279 MiB on pod 2 in the nine deep samples. This includes more than live Go objects; do not identify it with sampled heap or extrapolate its noisy short-window slope. |
| File cache | Cgroup `file` ranges 39–59 and 59–77 MiB respectively; varies with activity. Measure `inactive_file` separately when interpreting working set. |
| Reclaimable slab | Recomputed least-squares slopes over 163 seconds: 44.36 and 46.71 MiB/hour. Working set includes this memory; pprof does not. |
| Unreclaimable slab | Approximately 1.8 and 2.6 MiB, without the corresponding upward trend. |

Node-global dentry and reclaimable kmalloc growth, approximately five million
negative dentries per host, and the isolated ext4 create/delete experiment
support a filesystem metadata retention mechanism. The experiment left about
2.90 MiB per additional 10,000 names despite deleting every file. This is
reclaimable memory, with no demonstrated reclamation failure: hosts had roughly
244 GiB available and zero reported memory PSI. The host counters are not exact
per-cgroup attribution, and the reproduction used the node filesystem rather
than the WAL PVC. In particular, multiplying every live delete by 304 bytes
would predict about 412 MiB/hour, far above the observed slope. The measured
54–59 negative dentries/second is much lower than file turnover.

## Why there are so many filenames

`NewSegment` writes an eight-byte header to a new prefix + flake-ID filename.
The prefix includes database, table, and schema; separate table/schema streams
must remain separate. `LocalStore.Import` buffers and validates intake, resolves
that stream's WAL, and appends the transferred blocks. It does not create one
file per incoming request or copy the sender's filename verbatim.

Queued tables not owned by this ingestor use `UnownedSegmentMaxAge`, default
one second, with rotation jitter up to 10% early. Hundreds of active unowned
streams therefore generate hundreds of data-bearing files/second even when
individual payloads are small. Owned streams use their configured longer age;
realtime streams retain their own age/size policy. Size thresholds also rotate
large streams. Ownership sweeps update existing WAL deadlines as ownership moves.

Across deep metric captures A to C (approximately 104 seconds), pod 0 sent
39,116 data-bearing peer-transfer segments in 20,100 requests and uploaded
3,095 segments: approximately 376 + 30 = 406 segments/second. It received
508,000 collector requests and 24,286 peer requests, roughly 4,884 and 234 per
second. These rates use capture file completion times, so are approximate.
The later independent 20-second inotify window reports 7,894 unique creates
and 7,860 deletes (395 and 393 per second). The windows differ; these counters
are not an exact accounting identity. Nevertheless, completed **data-bearing**
work already explains the order of magnitude of creation. It would be wrong
to attribute all 400 names/second to empty segments or a rotation race.

The separate raw event capture contains 559 created prefixes: 333 with 16–18
creates and 222 with 1–5 creates, demonstrating a mixture of busy and sparse
streams. It is not the complete event stream behind the 20-second summary and
must not be treated as that window's denominator.

Lifecycle accounting before this patch:

| Path | Creates and removals |
| --- | --- |
| First successful write/import | One uniquely named segment if none is active; subsequent blocks append to it. |
| Rotation with data, even with no writer pending | Precreates a successor. Old data closes, flushes/fsyncs as configured, and is indexed for batching. |
| Sparse burst followed by silence | One data file **plus one empty successor**; the next age rotation deletes the empty file. This is avoidable. |
| Fully idle WAL | After the empty successor expires, no files are created until another write. The preexisting idle fix already prevents endless empty rotation. |
| Successful upload or ownership transfer | Batcher unlinks completed source paths and removes index entries, releasing admission accounting. Receiver appends blocks to its own table/schema WAL. A segment merger streams source files; it does not create a temporary merged file. |
| Retry | Failed uploads/transfers retain source files for retry. Retrying an import normally appends to the active receiver file; a new name is needed only if no active file remains. Existing delivery/deduplication semantics are unchanged. |
| Concurrent rotation / close | `rotateMu` already serializes competing rotations. A successor invalidated by close or another lifecycle change is discarded and its budget released. This is not the old many-writers-create-many-successors bug. |
| Rejected or invalid first write | Can leave an empty active segment; existing empty-segment close/remove handling remains necessary. |

## Chosen change and safety

Use the existing lazy creation path whenever `inflightWriteBytes == 0`, whether
the outgoing segment contains data or is empty. Continue proactive creation for
pending writers, including write-triggered size rotations, and recheck pending
writes after acquiring `w.mu`. No new configuration toggle is needed.

The previous `isIdle` check conflated data in the outgoing segment with demand
for a successor. A completed burst no longer forces an unused unique name.
This is deliberately a partial mitigation: continuous streams still require
data-bearing rotation. Pending writers may finish on the old segment after
the decision to precreate, so this does not promise zero empty files under all
concurrency patterns.

The old segment still closes before publication, and age deadlines, jitter,
flush/fsync configuration, block verification, batching, retries, and budget
enforcement are unchanged. A writer racing a rotation either creates the next
segment under `w.mu` or retries an `ErrSegmentClosed` without duplicating that
write. No on-disk format, pathname identity, or reader lifetime changes. Every
new data file retains a unique ID and table/schema prefix. Empty/discarded
files still go through accounting-aware removal. Reduced speculative capacity
reservations cannot consume more disk or segment quota.

The lease and bounded retirement protections from
[fa652d40](https://github.com/Azure/adx-mon/commit/fa652d40cc5386f321f5372fd2594d02d058b75d)
remain in place: active writers, reservations, and indexed backlog pin state.
The CSV pool capacity bounds from
[fd6aba6e](https://github.com/Azure/adx-mon/commit/fd6aba6e3bdc7cbe99f2913567d26f08be22be14)
are untouched.

Alternatives considered:

- Reuse or truncate completed filenames: requires a new identity/lifetime
  protocol across readers, index entries, batch completion, retry, recovery,
  and admission. An old completion must never delete a newly assigned file;
  an open reader must never see another table's data. Renaming a pool file to
  a fresh unique WAL name also retains the unique-name churn. Not justified
  for this small fix.
- Lengthen unowned rotation: likely a much larger reduction for this workload,
  but changes ownership-transfer latency and batching behavior. Needs an
  explicitly evaluated latency policy, beyond removing unused successors.
- GC tuning, cache flushing, and sysctls: do not remove the proven unnecessary
  filesystem operations and are not part of this patch.

## Reproduction and local results

`TestWAL_FilenameChurn` uses real files, compression, imported blocks, readers,
and enforced admission. Fifty logical one-second periods contain 32 streams
written every period and 18 written every fifth period; half use `Write`, half
`Append`. Explicit monotonic deadlines avoid timing-sensitive assertions. Each
published segment is read back and removed, then the fixture is fully drained.

| Successful lifecycle operations | Baseline | Patched |
| --- | ---: | ---: |
| Data-bearing segments | 1,780 | 1,780 |
| Unique file creates | 1,992 | 1,780 |
| Unlinks | 1,992 | 1,780 |
| Empty successor files | 212 | 0 |
| Final resident files / charged bytes / charged segments | 0 / 0 / 0 | 0 / 0 / 0 |

The regression fails on the baseline with 1,992 creates versus 1,780 expected.
The mixed run eliminates 212 names, or 10.6%. Of those, 180 are the sparse
streams' extra files (50% reduction for those streams); 32 are terminal hot
stream successors. That terminal saving is a finite-run effect. An indefinitely
continuous stream has essentially no reduction; this topology's steady sparse
saving approaches 9.2%, not an assertion about production's traffic mix.
Counts use the preexisting creation hook, successful calls, and an empty final
directory to account for internal empty-file unlinks; they are lifecycle
operation counts, not a local kernel syscall trace. The source fixture's one
file is excluded. On Linux, syscall tracing can independently verify them.

On macOS/arm64, Go 1.27.3, a single profiled run had maximum process RSS of
20.23 MiB before and 19.53 MiB after; sampled end-of-test live heap was about
3.09 and 3.00 MiB. These are small, noisy test-process measurements, not a
memory-slope result or evidence about Linux slab. Anonymous RSS, file cache,
and reclaimable/unreclaimable Linux slab cannot be separately measured here.
The local Docker daemon was unavailable; matching Linux/ext4 validation was
not performed.

Three sequential 1,000-iteration `BenchmarkWAL_WriteAfterRotation` runs expose
the cost moved onto the next uncontended writer: baseline p50 0.25 microseconds,
p99 0.5–2.0 microseconds; patched p50 76–80 microseconds, p99 147–405 microseconds.
This is local filesystem latency, **not** a guarantee for Azure disk under load.
The existing contended `BenchmarkWAL_WriteDuringRotation` with 1 ms and 10 ms
ages showed p99 ranges 121–125 / 105–108 microseconds before and 107–109 /
106–108 after, three sequential one-second runs each. No contended regression
was observed; these short benchmarks are not a production latency acceptance
test. First-write latency on the actual staging disk remains a rollout gate.

Validation commands (all passed unless explicitly marked baseline failure):

```sh
go test ./pkg/wal ./storage ./ingestor/cluster ./ingestor ./metrics ./transform -count=1
go test -race ./pkg/wal ./storage ./ingestor/cluster ./ingestor ./metrics ./transform -count=1
go test ./pkg/wal -run 'TestWAL_(FilenameChurn|DeferredSuccessorRecovery|ConcurrentWritesDuringRotation|InflightWriteCreatesNextSegment|RotationWithoutPendingWritesDefersNextSegment)' -count=10
go test ./pkg/wal -run '^$' -bench '^BenchmarkWAL_WriteAfterRotation$' -benchtime=1000x -count=3
go test ./pkg/wal -run '^$' -bench '^BenchmarkWAL_WriteDuringRotation$' -benchtime=1s -count=3
git diff --check
```

`TestWAL_DeferredSuccessorRecovery` enables fsync, checks an old reader across a
lazy successor write, then reopens a repository and verifies both payloads,
sample metadata, and restored admission. It tests recovery of closed files,
not a simulated disk power failure. Existing concurrent-write, import, ownership
change, close-before-index, admission, retry, CSV pool, and retirement tests
remain covered by the package and race suites.

## Optional Linux/ext4 experiment and staging acceptance

This is deliberately outside mandatory CI. Use an isolated disposable staging
pod or VM on the same kernel and ext4 mount/storage class as the WAL PVC. Never
point it at a live WAL directory. Compile baseline and patched WAL test binaries
with the **same new test file** (a Go `-overlay` adding the file to the baseline
checkout avoids editing it). Supply a fresh scratch child through `TMPDIR`.

Run each variant in a fresh diagnostic cgroup, alternating order across several
runs; retain an idle control. For example, inside that disposable cgroup:

```sh
# wal.test is compiled for this Linux host. SCRATCH is dedicated scratch ext4.
TMPDIR="$SCRATCH" ./wal.test -test.run '^TestWAL_FilenameChurn$' \
  -test.v -test.count=100 -test.memprofile="$OUTPUT/heap.pprof"
```

The unpatched binary intentionally fails the new count assertion; inspect its
logs to distinguish that expected failure from I/O or data-validation failures.
The patched binary must pass every iteration. Run a separate short
`strace -f -e trace=openat,unlink,unlinkat` pass and count successful `O_CREAT`
and unlink calls for `DB_T*_schema_*.wal`, excluding the source fixture and
temporary-directory cleanup. Do not use the traced run for latency comparison.

Sample at one-second cadence from before the run through a post-exit idle
period, and retain raw timestamps:

- Cgroup v2 `memory.current`, `memory.stat` (`anon`, `file`, `inactive_file`,
  `slab_reclaimable`, `slab_unreclaimable`, `sock`), `memory.events`, and
  `memory.pressure`. Derive working set using the actual monitoring definition;
  do not sum overlapping memory.stat fields.
- Process `/proc/PID/status` (`RssAnon`, `RssFile`, `VmRSS`) and Go post-GC
  `heap.pprof` separately. The test binary's memory profile is written after
  the workload; kernel cache memory does not appear in it.
- Host `/proc/sys/fs/dentry-state`, `/proc/slabinfo` (where permitted),
  `/proc/meminfo`, and `/proc/pressure/memory`. Host slab is node-wide; compare
  with the idle control. Record kernel, mount options, device and filesystem.

Do not assert a fixed bytes-per-name or slab slope. Fresh cgroups, equivalent
storage, workload counters, and controls matter more than a single absolute
number. No `drop_caches` or sysctl changes are needed.

For sustained service validation, run both images under the same staging load
for at least 6–12 hours, including realistic table/schema cardinality, hot and
bursty traffic, collector and peer imports, owner reassignment, slow/failed
uploads, retry bursts, and disk/segment admission pressure. Record unique
creates/deletes, data-bearing closes, empty closes/discards, received requests,
transferred/uploaded segment counts, Go heap, all memory categories above,
backlog, error rates, and p50/p95/p99 end-to-end and first-write latency. Trace
creation/close call stacks briefly in staging if remaining churn cannot be
explained by data-bearing closes. A trace showing almost no empty successors
would imply little production benefit from this patch.

Acceptance requires unchanged delivered payloads/metadata and table isolation,
bounded backlog, correct admission after retries/removal, no new loss or errors,
and first-write/tail latency within the service's agreed budgets on actual
storage. Validate abrupt process termination after acknowledged writes with the
existing flush/fsync policy, recovery, and a reader held through batch completion.
Compare churn per input byte and per data-bearing segment; compare memory slopes
only at matched load. Less filename churn alone does not establish that all
working-set growth is fixed.

Finally, in a **separate disposable staging cgroup**, establish a bounded memory
budget with ample headroom above measured anon/file/working allocations, then
apply limited synthetic pressure with a timeout and an OOM/PSI abort threshold.
Observe reclaimable slab, memory.events, PSI, throughput, and latency through
pressure and recovery. No production limits, sysctls, cache flushes, deployments,
or restarts were performed or are authorized by this plan. Reclamation under
pressure and the production slab benefit remain unvalidated.
