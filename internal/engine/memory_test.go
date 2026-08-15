package engine

import "testing"

func TestQuoteIdent(t *testing.T) {
	got, err := quoteIdent("orders")
	if err != nil {
		t.Fatalf("quoteIdent(\"orders\") returned error: %v", err)
	}
	if want := `"orders"`; got != want {
		t.Fatalf("quoteIdent(\"orders\") = %q, want %q", got, want)
	}
}

func TestQuoteIdent_RejectsInjection(t *testing.T) {
	for _, bad := range []string{
		`orders"; DROP TABLE tasks; --`,
		"orders WHERE 1=1",
		"orders; SELECT",
		"",
		"1orders",
	} {
		if _, err := quoteIdent(bad); err == nil {
			t.Fatalf("quoteIdent(%q) should have rejected, got no error", bad)
		}
	}
}
