package format

import "testing"

func TestFormattingAndChunking(t *testing.T) {
	wa := ForChannel("whatsapp", "## Status\nAll systems **online** — see [docs](https://example.com).")
	want := "*Status*\nAll systems *online* — see docs (https://example.com)."
	if wa != want {
		t.Fatalf("whatsapp format mismatch:\ngot:  %q\nwant: %q", wa, want)
	}

	chunks := SplitChunks("line-1\nline-2\nline-3\nline-4", 14)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %v", chunks)
	}
	for _, c := range chunks {
		if len(c) > 14 {
			t.Fatalf("chunk exceeded limit: %q", c)
		}
	}
}
