package wal

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

func benchAdmission(tables int) *Admission {
	overrides := make([]TableLimit, 0, tables)
	for n := 0; n < tables; n++ {
		overrides = append(overrides, TableLimit{Database: "DB", Table: fmt.Sprintf("T%d", n)})
	}
	return NewAdmission(AdmissionOpts{Overrides: overrides})
}

// BenchmarkAdmissionReserveParallel measures contention on the shared admission
// state when many destinations write concurrently. This is the write hot path.
func BenchmarkAdmissionReserveParallel(b *testing.B) {
	for _, tables := range []int{1, 16, 256} {
		b.Run(fmt.Sprintf("tables=%d", tables), func(b *testing.B) {
			a := benchAdmission(tables)
			var next atomic.Int64
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				id := int(next.Add(1))
				d := Destination{Database: "DB", Table: fmt.Sprintf("T%d", id%tables)}
				path := fmt.Sprintf("/w/%d.wal", id)
				for pb.Next() {
					r, err := a.Reserve(path, d, 512, 0)
					if err != nil {
						b.Fatal(err)
					}
					r.Finish(512)
					r.Release()
				}
			})
		})
	}
}

// BenchmarkSegmentWriteParallel measures end-to-end WAL writes across distinct
// destinations, which is what a noisy multi-table workload actually exercises.
func BenchmarkSegmentWriteParallel(b *testing.B) {
	for _, name := range []string{"admission", "baseline"} {
		b.Run(name, func(b *testing.B) {
			dir := b.TempDir()
			var a *Admission
			if name == "admission" {
				a = benchAdmission(256)
			}
			var next atomic.Int64
			payload := []byte("0123456789abcdef")
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				id := int(next.Add(1))
				s, err := NewSegment(dir, fmt.Sprintf("DB_T%d_s", id%256), WithAdmission(a))
				if err != nil {
					b.Fatal(err)
				}
				defer s.Close()
				for pb.Next() {
					if _, err := s.Write(context.Background(), payload); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

// BenchmarkAdmissionStatusDuringWrites measures whether metric scrapes stall
// concurrent writers.
func BenchmarkAdmissionStatusDuringWrites(b *testing.B) {
	a := benchAdmission(64)
	for n := 0; n < 4096; n++ {
		a.Restore(fmt.Sprintf("/w/%d.wal", n), Destination{Database: "DB", Table: fmt.Sprintf("T%d", n%64)}, 1024)
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		d := Destination{Database: "DB", Table: "T0"}
		for {
			select {
			case <-stop:
				return
			default:
			}
			if r, err := a.Reserve("/w/live.wal", d, 128, 0); err == nil {
				r.Finish(128)
				r.Release()
			}
		}
	}()
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if len(a.Status()) == 0 {
			b.Fatal("empty status")
		}
	}
}
