package markl

import (
	"bytes"
	"testing"
)

// BenchmarkGetMarklId measures Hash.GetMarklId cost across the two code paths:
// the "written == 0" branch that currently skips hash.Sum, and the normal
// "written > 0" branch that computes a digest. Run before and after removing
// the branch to see whether skipping Sum is a worthwhile optimization.
//
//	just test-go -bench=BenchmarkGetMarklId -benchmem -run=^$ ./internal/bravo/markl/...
func BenchmarkGetMarklId(b *testing.B) {
	formats := []struct {
		name string
		fh   *FormatHash
	}{
		{"SHA256", &FormatHashSha256},
		{"Blake2b256", &FormatHashBlake2b256},
	}

	payloads := []struct {
		name string
		size int
	}{
		{"NoWrite", 0},
		{"Write_64B", 64},
		{"Write_1KB", 1024},
		{"Write_64KB", 64 * 1024},
	}

	for _, f := range formats {
		for _, p := range payloads {
			payload := bytes.Repeat([]byte{0xab}, p.size)

			b.Run(f.name+"/"+p.name, func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					hash, repool := f.fh.Get()
					if p.size > 0 {
						if _, err := hash.Write(payload); err != nil {
							b.Fatal(err)
						}
					}
					_, idRepool := hash.GetMarklId()
					idRepool()
					repool()
				}
			})
		}
	}
}
