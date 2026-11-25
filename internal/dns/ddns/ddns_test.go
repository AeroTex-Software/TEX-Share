package ddns

import "testing"

func TestParseIPBody(t *testing.T) {
	cases := []struct {
		body  string
		want6 bool
		want  string
	}{
		{"203.0.113.10\n", false, "203.0.113.10"},
		{"2001:db8::1", true, "2001:db8::1"},
		{"fl=abc\nh=1.1.1.1\nip=198.51.100.7\nts=1\n", false, "198.51.100.7"},
		{"2001:db8::2", false, ""},
		{"not-an-ip", false, ""},
	}
	for _, c := range cases {
		if got := parseIPBody(c.body, c.want6); got != c.want {
			t.Errorf("parseIPBody(%q, %v) = %q, want %q", c.body, c.want6, got, c.want)
		}
	}
}
