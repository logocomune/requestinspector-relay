package httpapi

import (
	"testing"
	"testing/quick"
	"time"
)

func TestCursorRoundTripProperty(t *testing.T) {
	property := func(seconds uint32, id string) bool {
		if id == "" || len(id) > 128 {
			return true
		}
		at := time.Unix(int64(seconds)+1, int64(seconds%1_000_000_000)).UTC()
		decoded, err := decodeCursor(encodeCursor(at, id))
		return err == nil && decoded.ID == id && decoded.CompletedAt == at.UnixNano()
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestParseRange(t *testing.T) {
	tests := []struct {
		value string
		size  int64
		want  byteRange
		err   bool
	}{
		{value: "bytes=0-0", size: 10, want: byteRange{0, 0}},
		{value: "bytes=4-", size: 10, want: byteRange{4, 9}},
		{value: "bytes=-3", size: 10, want: byteRange{7, 9}},
		{value: "bytes=8-99", size: 10, want: byteRange{8, 9}},
		{value: "bytes=10-", size: 10, err: true},
		{value: "bytes=0-1,4-5", size: 10, err: true},
		{value: "items=0-1", size: 10, err: true},
		{value: "bytes=-0", size: 10, err: true},
	}
	for _, test := range tests {
		got, err := parseRange(test.value, test.size)
		if (err != nil) != test.err || !test.err && got != test.want {
			t.Errorf("parseRange(%q, %d) = %+v, %v", test.value, test.size, got, err)
		}
	}
}

func FuzzDecodeCursor(f *testing.F) {
	f.Add(encodeCursor(time.Unix(1, 0), "exchange"))
	f.Add("broken")
	f.Add("")
	f.Fuzz(func(t *testing.T, value string) {
		cursor, err := decodeCursor(value)
		if err == nil && (cursor.ID == "" || cursor.CompletedAt <= 0 || len(cursor.ID) > 128) {
			t.Fatalf("accepted invalid cursor: %+v", cursor)
		}
	})
}

func FuzzParseRange(f *testing.F) {
	f.Add("bytes=0-10", int64(20))
	f.Add("bytes=-5", int64(20))
	f.Add("broken", int64(0))
	f.Fuzz(func(t *testing.T, value string, size int64) {
		selected, err := parseRange(value, size)
		if err == nil && (selected.start < 0 || selected.end < selected.start || selected.end >= size) {
			t.Fatalf("range escaped body: %+v size=%d", selected, size)
		}
	})
}
