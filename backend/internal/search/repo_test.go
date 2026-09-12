package search

import (
	"reflect"
	"testing"
)

// The search must behave like a real keyword search: whitespace splits the
// query into terms, every term must match (AND), a term matches any searched
// column (OR). These tests pin the SQL shape + arg order the repo relies on.
func TestSplitTerms(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"   ", []string{}},
		{"字节", []string{"字节"}},
		{"  字节\t后端  ", []string{"字节", "后端"}},
		{"深圳 后端", []string{"深圳", "后端"}},
		{"字节　后端", []string{"字节", "后端"}}, // 全角空格也算分隔
	}
	for _, c := range cases {
		if got := splitTerms(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitTerms(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

func TestSplitTermsCapsPathologicalInput(t *testing.T) {
	in := "a b c d e f g h i j k"
	if got := splitTerms(in); len(got) != maxSearchTerms {
		t.Errorf("expected cap at %d terms, got %d", maxSearchTerms, len(got))
	}
}

func TestLikeConjunction(t *testing.T) {
	cases := []struct {
		name    string
		columns []string
		terms   []string
		start   int
		wantSQL string
		wantArg []string
	}{
		{
			name:    "single term across three columns ORs inside",
			columns: []string{"a.position", "a.company_name", "a.notes"},
			terms:   []string{"字节"},
			start:   2,
			wantSQL: "(a.position ILIKE $2 OR a.company_name ILIKE $2 OR a.notes ILIKE $2)",
			wantArg: []string{"%字节%"},
		},
		{
			name:    "two terms AND across, OR inside, placeholders ascend",
			columns: []string{"a.position", "a.company_name", "a.notes"},
			terms:   []string{"字节", "后端"},
			start:   2,
			wantSQL: "(a.position ILIKE $2 OR a.company_name ILIKE $2 OR a.notes ILIKE $2) AND " +
				"(a.position ILIKE $3 OR a.company_name ILIKE $3 OR a.notes ILIKE $3)",
			wantArg: []string{"%字节%", "%后端%"},
		},
		{
			name:    "single column stays unparenthesized",
			columns: []string{"c.name"},
			terms:   []string{"腾讯"},
			start:   2,
			wantSQL: "c.name ILIKE $2",
			wantArg: []string{"%腾讯%"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sql, args := likeConjunction(c.columns, c.terms, c.start)
			if sql != c.wantSQL {
				t.Errorf("sql:\n got %s\nwant %s", sql, c.wantSQL)
			}
			if len(args) != len(c.wantArg) {
				t.Fatalf("args: got %#v want %#v", args, c.wantArg)
			}
			for i := range args {
				if args[i] != c.wantArg[i] {
					t.Errorf("args[%d]: got %#v want %#v", i, args[i], c.wantArg[i])
				}
			}
		})
	}
}

// User input containing LIKE wildcards must never widen the match.
func TestEscapeLike(t *testing.T) {
	if got := escapeLike(`100%_ \`); got != `100\%\_ \\` {
		t.Errorf("escapeLike = %q", got)
	}
}
