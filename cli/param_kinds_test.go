package cli

import (
	"fmt"
	"testing"
)

// The CLI reads a flag's text by the parameter's type in its own way:
// strconv.ParseBool for a boolean ("t", "TRUE" and "1" are true), JSON for
// an array or an object, a string for everything else, number included
// (the runtime then refuses the string: the disagreement the parameter
// kinds table closes).
func TestTypedFlagValuesPerKind(t *testing.T) {
	cases := []struct{ kind, text, want string }{
		{"integer", "7", "int:7"},
		{"integer", "1.5", `ERR strconv.Atoi: parsing "1.5": invalid syntax`},
		{"integer", " 7 ", `ERR strconv.Atoi: parsing " 7 ": invalid syntax`},
		{"boolean", "true", "bool:true"},
		{"boolean", "TRUE", "bool:true"},
		{"boolean", "t", "bool:true"},
		{"boolean", "0", "bool:false"},
		{"boolean", "yes", `ERR strconv.ParseBool: parsing "yes": invalid syntax`},
		{"array", "[1]", "[]interface {}:[1]"},
		{"object", `{"a":1}`, "map[string]interface {}:map[a:1]"},
		{"array", "x", "ERR invalid character 'x' looking for beginning of value"},
		{"string", "7", "string:7"},
		{"", "7", "string:7"},
		{"path", "7", "string:7"},
		{"any", "7", "string:7"},
		{"number", "1.5", "string:1.5"},
		{"weird", "1.5", "string:1.5"},
	}
	for _, c := range cases {
		v, err := typed(c.kind, c.text)
		got := fmt.Sprintf("%T:%v", v, v)
		if err != nil {
			got = "ERR " + err.Error()
		}
		if got != c.want {
			t.Errorf("typed(%q, %q) = %s, want %s", c.kind, c.text, got, c.want)
		}
	}
}
