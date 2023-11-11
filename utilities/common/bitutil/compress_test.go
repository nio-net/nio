package bitutil

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/neatio-net/neatio/utilities/common/hexutil"
)

func TestEncodingCycle(t *testing.T) {
	tests := []string{

		"0x000000000000000000",
		"0xef0400",
		"0xdf7070533534333636313639343638373532313536346c1bc33339343837313070706336343035336336346c65fefb3930393233383838ac2f65fefb",
		"0x7b64000000",
		"0x000034000000000000",
		"0x0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000f0000000000000000000",
		"0x4912385c0e7b64000000",
		"0x000034000000000000000000000000000000",
		"0x00",
		"0x000003e834ff7f0000",
		"0x0000",
		"0x0000000000000000000000000000000000000000000000000000000000ff00",
		"0x895f0c6a020f850c6a020f85f88df88d",
		"0xdf7070533534333636313639343638373432313536346c1bc3315aac2f65fefb",
		"0x0000000000",
		"0xdf70706336346c65fefb",
		"0x00006d643634000000",
		"0xdf7070533534333636313639343638373532313536346c1bc333393438373130707063363430353639343638373532313536346c1bc333393438336336346c65fe",
	}
	for i, tt := range tests {
		data := hexutil.MustDecode(tt)

		proc, err := bitsetDecodeBytes(bitsetEncodeBytes(data), len(data))
		if err != nil {
			t.Errorf("test %d: failed to decompress compressed data: %v", i, err)
			continue
		}
		if !bytes.Equal(data, proc) {
			t.Errorf("test %d: compress/decompress mismatch: have %x, want %x", i, proc, data)
		}
	}
}

func TestDecodingCycle(t *testing.T) {
	tests := []struct {
		size  int
		input string
		fail  error
	}{
		{size: 0, input: "0x"},

		{size: 0, input: "0x0020", fail: errUnreferencedData},
		{size: 0, input: "0x30", fail: errUnreferencedData},
		{size: 1, input: "0x00", fail: errUnreferencedData},
		{size: 2, input: "0x07", fail: errMissingData},
		{size: 1024, input: "0x8000", fail: errZeroContent},

		{size: 29490, input: "0x343137343733323134333839373334323073333930783e3078333930783e70706336346c65303e", fail: errMissingData},
		{size: 59395, input: "0x00", fail: errUnreferencedData},
		{size: 52574, input: "0x70706336346c65c0de", fail: errExceededTarget},
		{size: 42264, input: "0x07", fail: errMissingData},
		{size: 52, input: "0xa5045bad48f4", fail: errExceededTarget},
		{size: 52574, input: "0xc0de", fail: errMissingData},
		{size: 52574, input: "0x"},
		{size: 29490, input: "0x34313734373332313433383937333432307333393078073034333839373334323073333930783e3078333937333432307333393078073061333930783e70706336346c65303e", fail: errMissingData},
		{size: 29491, input: "0x3973333930783e30783e", fail: errMissingData},

		{size: 1024, input: "0x808080608080"},
		{size: 1024, input: "0x808470705e3632383337363033313434303137393130306c6580ef46806380635a80"},
		{size: 1024, input: "0x8080808070"},
		{size: 1024, input: "0x808070705e36346c6580ef46806380635a80"},
		{size: 1024, input: "0x80808046802680"},
		{size: 1024, input: "0x4040404035"},
		{size: 1024, input: "0x4040bf3ba2b3f684402d353234373438373934409fe5b1e7ada94ebfd7d0505e27be4035"},
		{size: 1024, input: "0x404040bf3ba2b3f6844035"},
		{size: 1024, input: "0x40402d35323437343837393440bfd7d0505e27be4035"},
	}
	for i, tt := range tests {
		data := hexutil.MustDecode(tt.input)

		orig, err := bitsetDecodeBytes(data, tt.size)
		if err != tt.fail {
			t.Errorf("test %d: failure mismatch: have %v, want %v", i, err, tt.fail)
		}
		if err != nil {
			continue
		}
		if comp := bitsetEncodeBytes(orig); !bytes.Equal(comp, data) {
			t.Errorf("test %d: decompress/compress mismatch: have %x, want %x", i, comp, data)
		}
	}
}

func TestCompression(t *testing.T) {

	in := hexutil.MustDecode("0x4912385c0e7b64000000")
	out := hexutil.MustDecode("0x80fe4912385c0e7b64")

	if data := CompressBytes(in); !bytes.Equal(data, out) {
		t.Errorf("encoding mismatch for sparse data: have %x, want %x", data, out)
	}
	if data, err := DecompressBytes(out, len(in)); err != nil || !bytes.Equal(data, in) {
		t.Errorf("decoding mismatch for sparse data: have %x, want %x, error %v", data, in, err)
	}

	in = hexutil.MustDecode("0xdf7070533534333636313639343638373532313536346c1bc33339343837313070706336343035336336346c65fefb3930393233383838ac2f65fefb")
	out = hexutil.MustDecode("0xdf7070533534333636313639343638373532313536346c1bc33339343837313070706336343035336336346c65fefb3930393233383838ac2f65fefb")

	if data := CompressBytes(in); !bytes.Equal(data, out) {
		t.Errorf("encoding mismatch for dense data: have %x, want %x", data, out)
	}
	if data, err := DecompressBytes(out, len(in)); err != nil || !bytes.Equal(data, in) {
		t.Errorf("decoding mismatch for dense data: have %x, want %x, error %v", data, in, err)
	}

	if _, err := DecompressBytes([]byte{0xc0, 0x01, 0x01}, 2); err != errExceededTarget {
		t.Errorf("decoding error mismatch for long data: have %v, want %v", err, errExceededTarget)
	}
}

func BenchmarkEncoding1KBVerySparse(b *testing.B) { benchmarkEncoding(b, 1024, 0.0001) }
func BenchmarkEncoding2KBVerySparse(b *testing.B) { benchmarkEncoding(b, 2048, 0.0001) }
func BenchmarkEncoding4KBVerySparse(b *testing.B) { benchmarkEncoding(b, 4096, 0.0001) }

func BenchmarkEncoding1KBSparse(b *testing.B) { benchmarkEncoding(b, 1024, 0.001) }
func BenchmarkEncoding2KBSparse(b *testing.B) { benchmarkEncoding(b, 2048, 0.001) }
func BenchmarkEncoding4KBSparse(b *testing.B) { benchmarkEncoding(b, 4096, 0.001) }

func BenchmarkEncoding1KBDense(b *testing.B) { benchmarkEncoding(b, 1024, 0.1) }
func BenchmarkEncoding2KBDense(b *testing.B) { benchmarkEncoding(b, 2048, 0.1) }
func BenchmarkEncoding4KBDense(b *testing.B) { benchmarkEncoding(b, 4096, 0.1) }

func BenchmarkEncoding1KBSaturated(b *testing.B) { benchmarkEncoding(b, 1024, 0.5) }
func BenchmarkEncoding2KBSaturated(b *testing.B) { benchmarkEncoding(b, 2048, 0.5) }
func BenchmarkEncoding4KBSaturated(b *testing.B) { benchmarkEncoding(b, 4096, 0.5) }

func benchmarkEncoding(b *testing.B, bytes int, fill float64) {

	random := rand.NewSource(0)

	data := make([]byte, bytes)
	bits := int(float64(bytes) * 8 * fill)

	for i := 0; i < bits; i++ {
		idx := random.Int63() % int64(len(data))
		bit := uint(random.Int63() % 8)
		data[idx] |= 1 << bit
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bitsetDecodeBytes(bitsetEncodeBytes(data), len(data))
	}
}
