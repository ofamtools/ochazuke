package namecheck

import "testing"

func TestDefinedStringValuesDoNotHideDispatchNameBranches(t *testing.T) {
	fixture(t, map[string]string{
		"a.go": `package p
const paneData paneID = "data"
const parameterData string = "data"
const aliasData stringAlias = "data"
func paneValue(v paneID) bool { return v == paneData }
func paneSwitch(v paneID) int { switch v { case paneData: return 1 }; return 0 }
func literalParameter(v string) bool { return v == "data" }
func constantParameter(v string) bool { return v == parameterData }
func aliasParameter(v string) bool { return v == aliasData }
var panes = map[paneID]int{paneData:1}
var parameters = map[string]int{parameterData:1}
`,
		"z.go": `package p
type paneID string
type stringAlias = string
`,
	})
	rep, err := Scan(Config{Vocab: Vocab{"data": {"parameter"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []string{"paneValue", "paneSwitch", "var panes"} {
		wantCounts(t, counts(rep, fn), map[string]int{})
	}
	for _, fn := range []string{"literalParameter", "constantParameter", "aliasParameter"} {
		wantCounts(t, counts(rep, fn), map[string]int{"compare data": 1})
	}
	wantCounts(t, counts(rep, "var parameters"), map[string]int{"table data": 1})
}
