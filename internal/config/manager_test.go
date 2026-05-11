package config

import "testing"

func TestValidateBaseURLFormat(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"http://localhost:2121", true},
		{"https://example.com:443", true},
		{"localhost:2121", false},
		{"ftp://localhost:2121", false},
		{"http://", false},
		{"http://localhost:2121/x", false},
	}

	for _, c := range cases {
		err := ValidateBaseURLFormat(c.in)
		if c.want && err != nil {
			t.Fatalf("esperava válido para %q, mas deu erro: %v", c.in, err)
		}
		if !c.want && err == nil {
			t.Fatalf("esperava inválido para %q, mas veio nil", c.in)
		}
	}
}

