package tmdb

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestCreditsOptionalIntegerID(t *testing.T) {
	for _, kind := range []string{"movie", "tv"} {
		for _, tc := range []struct{ name, field, want string }{
			{"number", `"id":123,`, "123"},
			{"absent", "", ""},
			{"null", `"id":null,`, ""},
			{"zero", `"id":0,`, ""},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{%s"cast":[{"id":10,"name":"演员"}],"crew":[]}`, tc.field))
				var target any = &Credit{}
				if kind == "tv" {
					target = &TvAggregateCredits{}
				}
				if err := json.Unmarshal(body, target); err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(target)
				if err != nil {
					t.Fatal(err)
				}
				var saved map[string]json.RawMessage
				if err := json.Unmarshal(data, &saved); err != nil {
					t.Fatal(err)
				}
				if got := string(saved["id"]); got != tc.want {
					t.Fatalf("作品编号未按契约保存：得到 %q，期望 %q", got, tc.want)
				}
				var cast []struct {
					Id   int
					Name string
				}
				if err := json.Unmarshal(saved["cast"], &cast); err != nil {
					t.Fatal(err)
				}
				if len(cast) != 1 || cast[0].Id != 10 || cast[0].Name != "演员" {
					t.Fatalf("演员信息发生变化：%+v", cast)
				}
			})
		}
	}
}

func TestCreditsIntegerIDRejectsEmptyString(t *testing.T) {
	for _, target := range []any{&Credit{}, &TvAggregateCredits{}} {
		if err := json.Unmarshal([]byte(`{"id":""}`), target); err == nil {
			t.Fatalf("%T 不应把空字符串当成整数", target)
		}
	}
}
