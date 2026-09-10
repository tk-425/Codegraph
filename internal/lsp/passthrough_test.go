package lsp

import "testing"

func TestStderrPassthroughEnabled(t *testing.T) {
	tests := []struct {
		name string
		flag bool
		env  string
		want bool
	}{
		{name: "off by default", flag: false, env: "", want: false},
		{name: "flag enables", flag: true, env: "", want: true},
		{name: "environment enables", flag: false, env: "1", want: true},
		{name: "flag wins when both set", flag: true, env: "1", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := StderrPassthrough
			t.Cleanup(func() { StderrPassthrough = original })
			StderrPassthrough = test.flag
			t.Setenv(StderrPassthroughEnv, test.env)

			if got := StderrPassthroughEnabled(); got != test.want {
				t.Errorf("StderrPassthroughEnabled() = %v, want %v", got, test.want)
			}
		})
	}
}
