package db

import "testing"

func TestUpBlock(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "normal Up and Down markers",
			input: `-- +goose Up
CREATE TABLE foo (id TEXT PRIMARY KEY);

-- +goose Down
DROP TABLE foo;
`,
			want: "CREATE TABLE foo (id TEXT PRIMARY KEY);",
		},
		{
			name:  "no markers at all",
			input: "CREATE TABLE bar (id TEXT PRIMARY KEY);",
			want:  "CREATE TABLE bar (id TEXT PRIMARY KEY);",
		},
		{
			name: "only Down marker, no Up",
			input: `-- +goose Down
DROP TABLE baz;
`,
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := upBlock(tc.input)
			if got != tc.want {
				t.Fatalf("upBlock(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}
