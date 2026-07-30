package service

import "testing"

func TestExtractJSONObject(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "pure JSON object", input: `{"title":"a"}`, want: `{"title":"a"}`},
		{name: "preamble text", input: "Result:\n```json\n{\"title\":\"a\"}\n```", want: `{"title":"a"}`},
		{name: "nested objects", input: "some text\n{\"outer\":{\"inner\":1}}", want: `{"outer":{"inner":1}}`},
		{name: "braces in string", input: `{"text":"a } and { b","nested":{"ok":true}} trailing`, want: `{"text":"a } and { b","nested":{"ok":true}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := extractJSONObject(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
	for _, input := range []string{"no object here", `{"title":"a"`} {
		if _, err := extractJSONObject(input); err == nil {
			t.Fatalf("extractJSONObject(%q) succeeded", input)
		}
	}
}
