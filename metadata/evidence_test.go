package metadata

import (
	"encoding/json"
	"testing"
)

func TestJudgmentInputOmitsOnlyAbsentManualReference(t *testing.T) {
	cases := []struct {
		name string
		ref  Ref
		want string
	}{
		{name: "absent"},
		{name: "source only", ref: Ref{Provider: "thetvdb", Kind: Show}, want: `{"provider":"thetvdb","kind":"show"}`},
		{name: "movie record", ref: Ref{Provider: "tmdb", Kind: Movie, ID: "42"}, want: `{"provider":"tmdb","kind":"movie","id":"42"}`},
		{name: "show slug", ref: Ref{Provider: "thetvdb", Kind: Show, Slug: "example-show"}, want: `{"provider":"thetvdb","kind":"show","slug":"example-show"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := JudgmentInputFor(Request{Kind: Show, Ref: tc.ref, Query: Query{Kind: Show, Title: "Example", ChineseTitle: "示例", OriginalTitle: "Example", Year: 2020}})
			data, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			actual, exists := fields["ref"]
			if tc.want == "" {
				if exists {
					t.Fatalf("未提供人工约束时不应发送 ref: %s", actual)
				}
				return
			}
			if !exists || string(actual) != tc.want {
				t.Fatalf("真实人工约束丢失或变化: got=%s want=%s", actual, tc.want)
			}
		})
	}
}
