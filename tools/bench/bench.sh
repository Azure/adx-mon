#!/bin/bash

# Usage
# This script is intended to compare the performance of two different implementations of a Go package with the same Benchmark defined.
# Run this script with the argument "compile" to compile the base implementation within the directory containing your benchmark.
# Subsequently, run the script without arguments to run the benchmark and compare the results.

# Example within adx-mon/transform:
# $ cd transform
# $ ../tools/bench/bench.sh compile
# <make changes>
# $ ../tools/bench/bench.sh

# Requires https://pkg.go.dev/golang.org/x/perf/cmd/benchstat in your PATH

function benchmark() {
    go test -c -o new.test .

    # Define an array of flags
    flags=("-test.benchmem" "-test.bench" ".")

    # Interleave the benchmarks for 10 iterations each to minimize system noise affecting a single run.
    ./base.test "${flags[@]}" > base.bench
    ./new.test "${flags[@]}" > new.bench

    for i in `seq 2 10`; do
        ./base.test "${flags[@]}" >> base.bench
        ./new.test "${flags[@]}" >> new.bench
    done

    benchstat base.bench new.bench
}

function compile_base() {
    go test -c -o base.test .
}

if [ "$1" == "compile" ]; then
    compile_base
else
    benchmark
fi