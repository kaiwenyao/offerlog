package service

import "testing"

func TestSlugifyAndGeneratedKey(t *testing.T) {
	if got := slugify("Expected Level"); got != "expected_level" {
		t.Fatalf("slugify latin = %q", got)
	}
	if got := slugify("期望职级"); got != "" {
		t.Fatalf("slugify CJK should be empty, got %q", got)
	}
	k := generatedPropKey("期望职级")
	if k == "" || k == "期望职级" {
		t.Fatalf("generatedPropKey = %q, want a stable ASCII key", k)
	}
	if generatedPropKey("期望职级") != k {
		t.Fatal("generatedPropKey must be stable for the same name")
	}
	if generatedPropKey("另一字段") == k {
		t.Fatal("different names must not share a generated key")
	}
}
