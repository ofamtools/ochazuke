package ochazuke

// Reciprocal exposes an operation from its other typed noun end. Parameter is
// the original argument naming that end; Verb is the action on that noun, and
// Subject is its argument naming the original noun's thing. Catalog hosts wire
// the reverse action to the original handler, preserving its rights and checks.
type Reciprocal struct {
	Parameter string `json:"parameter"`
	Verb      string `json:"verb"`
	Subject   string `json:"subject"`
}
