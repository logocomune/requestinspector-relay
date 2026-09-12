package proxy

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"testing"

	"github.com/logocomune/requestinspector-relay/internal/config"
)

func TestInspectContentEncodings(t *testing.T) {
	plain := []byte("decoded body")
	gzipBody := compressGzip(t, plain)
	deflateBody := compressZlib(t, plain)
	stackedBody := compressZlib(t, gzipBody)
	tests := []struct {
		name     string
		body     []byte
		encoding string
		want     []byte
		wantErr  bool
	}{
		{name: "identity", body: plain, want: plain},
		{name: "gzip", body: gzipBody, encoding: "gzip", want: plain},
		{name: "x-gzip", body: gzipBody, encoding: "x-gzip", want: plain},
		{name: "deflate", body: deflateBody, encoding: "deflate", want: plain},
		{name: "stacked", body: stackedBody, encoding: "gzip, deflate", want: plain},
		{name: "unsupported", body: plain, encoding: "br", want: plain, wantErr: true},
		{name: "corrupt", body: plain, encoding: "gzip", want: plain, wantErr: true},
		{name: "empty stack", body: plain, encoding: ", identity", want: plain, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := inspect(test.body, test.encoding, config.Preview{Enabled: true, Bytes: 1024}, 4096)
			if !bytes.Equal(got.Preview, test.want) || (got.Error != "") != test.wantErr {
				t.Fatalf("inspect() = %+v", got)
			}
			if test.encoding != "" && !test.wantErr && got.DecodedPreviewBytes == nil {
				t.Fatalf("decoded size missing: %+v", got)
			}
		})
	}
}

func TestInspectTruncationAndExpansionLimit(t *testing.T) {
	body := compressGzip(t, []byte("abcdefgh"))
	truncated := inspect(body, "gzip", config.Preview{Enabled: true, Bytes: 3}, 1024)
	if string(truncated.Preview) != "abc" || !truncated.PreviewTruncated || truncated.Error != "" {
		t.Fatalf("truncated inspection = %+v", truncated)
	}
	bomb := compressGzip(t, bytes.Repeat([]byte("a"), 100_000))
	limited := inspect(bomb, "gzip", config.Preview{Enabled: true, Bytes: 16 << 10}, 1<<20)
	if limited.Error == "" || !bytes.Equal(limited.Preview, bomb[:min(len(bomb), 16<<10)]) {
		t.Fatalf("bomb inspection = %+v", limited)
	}
	hardLimited := inspect(body, "gzip", config.Preview{Enabled: true, Bytes: 3}, 4)
	if hardLimited.Error == "" {
		t.Fatalf("hard-limit inspection = %+v", hardLimited)
	}
}

func TestInspectDisabledPreviewKeepsPlainBody(t *testing.T) {
	body := []byte("complete")
	got := inspect(body, "", config.Preview{Enabled: false, Bytes: 1}, int64(len(body)))
	if !bytes.Equal(got.Preview, body) || got.PreviewTruncated {
		t.Fatalf("inspection = %+v", got)
	}
}

func FuzzInspect(f *testing.F) {
	f.Add([]byte("plain"), "", uint16(16))
	f.Add([]byte{0x1f, 0x8b, 0x08}, "gzip", uint16(64))
	f.Add([]byte("encoded"), "br", uint16(32))
	f.Fuzz(func(t *testing.T, body []byte, encoding string, rawLimit uint16) {
		limit := int64(rawLimit) + 1
		got := inspect(body, encoding, config.Preview{Enabled: true, Bytes: limit}, limit)
		if int64(len(got.Preview)) > limit {
			t.Fatalf("preview length %d exceeds limit %d", len(got.Preview), limit)
		}
	})
}

func FuzzInspectGzipRoundTrip(f *testing.F) {
	f.Add([]byte("decoded content"))
	f.Add([]byte{0, 1, 2, 255})
	f.Fuzz(func(t *testing.T, plain []byte) {
		if len(plain) > 64<<10 {
			return
		}
		encoded := compressGzip(t, plain)
		limit := int64(len(plain)) + 1
		got := inspect(encoded, "gzip", config.Preview{Enabled: false, Bytes: 1}, limit)
		if int64(len(plain)) > int64(len(encoded))*maximumExpansionRatio {
			if got.Error == "" {
				t.Fatal("expansion-ratio overflow accepted")
			}
			return
		}
		if got.Error != "" || !bytes.Equal(got.Preview, plain) {
			t.Fatalf("round trip mismatch: error=%q", got.Error)
		}
	})
}

func compressGzip(t *testing.T, body []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func compressZlib(t *testing.T, body []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zlib.NewWriter(&buffer)
	if _, err := writer.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
