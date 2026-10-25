package internal

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		args   []string
		flags  map[string]string
		values []string
	}{
		{[]string{"init", "--ts", "game"}, map[string]string{"ts": "true"}, []string{"init", "game"}},
		{[]string{"init", "game", "--force", "--ts"}, map[string]string{"force": "true", "ts": "true"}, []string{"init", "game"}},
		{[]string{"build", "--project", "a.project.json", "src"}, map[string]string{"project": "a.project.json"}, []string{"build", "src"}},
		{[]string{"build", "--no-force"}, map[string]string{"force": "false"}, []string{"build"}},
		{[]string{"-V"}, map[string]string{"V": "true"}, []string{}},
		{[]string{"build", "--project", "-x"}, map[string]string{"project": "true", "x": "true"}, []string{"build"}},
	}
	for _, c := range cases {
		flags, values := Parse(c.args)
		if !reflect.DeepEqual(flags, c.flags) || !reflect.DeepEqual(values, c.values) {
			t.Errorf("Parse(%q) = %v, %v; want %v, %v", c.args, flags, values, c.flags, c.values)
		}
	}
}
