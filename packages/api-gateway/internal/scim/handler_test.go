package scim

import "testing"

func TestParseUserNameEq(t *testing.T) {
	cases := []struct {
		in       string
		wantUser string
		wantOK   bool
	}{
		{`userName eq "alice@example.com"`, "alice@example.com", true},
		{`userName Eq "Bob@example.com"`, "bob@example.com", true},
		{`userName eq alice@example.com`, "alice@example.com", true},
		{`displayName eq "Alice"`, "", false},
		{``, "", false},
	}
	for _, tc := range cases {
		got, ok := parseUserNameEq(tc.in)
		if ok != tc.wantOK || got != tc.wantUser {
			t.Errorf("parseUserNameEq(%q) = (%q,%v), want (%q,%v)", tc.in, got, ok, tc.wantUser, tc.wantOK)
		}
	}
}

func TestApplyActivePatch(t *testing.T) {
	f := false
	cases := []struct {
		name         string
		ops          []Operation
		wantActive   bool
		wantTouched  bool
	}{
		{
			name: "replace_path_active_false",
			ops:  []Operation{{Op: "replace", Path: "active", Value: false}},
			wantActive: false, wantTouched: true,
		},
		{
			name: "replace_bodymap_active_false",
			ops:  []Operation{{Op: "Replace", Value: map[string]any{"active": false}}},
			wantActive: false, wantTouched: true,
		},
		{
			name: "replace_string_false",
			ops:  []Operation{{Op: "replace", Path: "active", Value: "False"}},
			wantActive: false, wantTouched: true,
		},
		{
			name: "unrelated_path_not_touched",
			ops:  []Operation{{Op: "replace", Path: "displayName", Value: "Bob"}},
			wantActive: true, wantTouched: false,
		},
	}
	_ = f
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, touched, err := applyActivePatch(PatchRequest{Operations: tc.ops})
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if touched != tc.wantTouched {
				t.Fatalf("touched = %v, want %v", touched, tc.wantTouched)
			}
			if touched && a != tc.wantActive {
				t.Fatalf("active = %v, want %v", a, tc.wantActive)
			}
		})
	}
}
