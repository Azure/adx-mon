# GC-aware collector log pools

## Goals and Overview

Collector often runs with aggressive memory and CPU limits to prevent it from
impacting other workloads on the host. In extreme cases where collector retains
too much memory, the OS will constantly spin paging executable code in and out
of memory to keep the process under the limits.

In these cases, it's important to optimize collector toward using potentially
higher CPU in an effort to avoid retaining objects in pools that can push it
toward the Memory limits which put it into a failure spiral.

Phase One replaces the channel-backed `LogPool` and `LogBatchPool`
implementations with `sync.Pool` and to reset the stored objects at pool Put
time instead of after Get. `sync.Pool` stores objects with weak references
which may cause more allocations after GC, but allows objects that were quickly
created to handle load at process startup to eventually drain to reclaim memory
usage as load reduces. Similarly, resetting at Put removes retained objects in
the heap while objects are stored in the pool instead of needlessly retaining
data we will no longer need.

## Scope and behavior

Replace the collector's `LogPool` and `LogBatchPool` channel-backed caches with
`pkg/pool.ResettingPool[T]`, a typed wrapper around the standard library's
`sync.Pool`, constructed with `pool.NewResettingPool`.
Other users of `pkg/pool.Generic` (metrics exporters, WAL, storage, and byte
buffers) are unchanged.

- `Get()` returns `*types.Log` or `*types.LogBatch` directly; size arguments and
  call-site type assertions are removed.
- `Put()` resets the object before caching it. Logs release body, attribute,
  and resource values immediately, rather than retaining them until the next
  checkout. Batches release their active log pointers and acknowledgement
  closure. Map capacity and slice capacity remain available for reuse.
- Idle objects, including map storage and batch backing arrays, may be
  discarded by the runtime. Factories recreate initialized objects after a
  cache miss or garbage collection.
- New batches have capacity for 1,024 logs, matching the old source checkout
  hint. Previously OTLP could create a batch with capacity one; it now shares
  this initial capacity. New and reused batches both have a no-op `Ack`.
- `DisposeLogBatch` still returns each log separately and then the batch.
  Returning a batch directly does not return its logs to their pool.

The old caches retained up to 1,024 logs and 200 batches regardless of whether
they were ever used again. The new caches are opportunistic: they are neither
bounded queues nor an allocation-free guarantee. They can retain more objects
between collections, and collection can cause subsequent allocations. They do
not guarantee immediate RSS reduction or change the lifetime of in-flight logs.

## Ownership

Callers must stop using an object after `Put` or `DisposeLogBatch`. This was
already required for safe reuse, but reset-on-return makes violations visible
even without another checkout. The test-only `CountingSink` previously retained
a pooled log after `Send` returned; it now snapshots the final record with
`Log.Copy()` instead.
Its snapshot fix is included in both versions of the upstream benchmark control.

The exported pool API changes from `Get(size).(*types.Log)` to `Get()` (and
equivalently for batches). External native or interpreted plugins that call
`Get` must migrate. Existing `Put(log)` and `Log.Copy()` callers are compatible.
The AKS logtransform native and actual Collector/Yaegi execution paths are
validated against the new implementation without modifying their source.

## Validation and benchmark method

We exercised this codebase against our internal logging pipeline implementation
that utilizes Yaegi. Native and yaegi implementations did not show any significant
change in performance. The move from typed pools did remove an allocation from
the pooling path (no longer needing an Interface wrapper) which was a wash with
slightly higher allocation pressure.