package expr

import (
	"math"
	"reflect"
)

const maxEqualDepth = 64

var stringType = reflect.TypeOf("")

// Eval evaluates the compiled expression against env.
// Zero-value Expr{} and empty expressions evaluate to true.
func (e Expr) Eval(env map[string]any) bool {
	if e.root == nil {
		return true
	}
	return isTruthy(e.root.eval(env))
}

func (n literalNode) eval(_ map[string]any) any {
	return n.val
}

func (n listNode) eval(env map[string]any) any {
	out := make([]any, len(n.elems))
	for i, elem := range n.elems {
		out[i] = elem.eval(env)
	}
	return out
}

func (n identNode) eval(env map[string]any) any {
	return resolvePath(env, n.path)
}

func (n unaryNode) eval(env map[string]any) any {
	return !isTruthy(n.operand.eval(env))
}

func (n binaryNode) eval(env map[string]any) any {
	switch n.op {
	case tokAnd:
		if !isTruthy(n.left.eval(env)) {
			return false
		}
		return isTruthy(n.right.eval(env))
	case tokOr:
		if isTruthy(n.left.eval(env)) {
			return true
		}
		return isTruthy(n.right.eval(env))
	case tokEq:
		return valuesEqual(n.left.eval(env), n.right.eval(env))
	case tokNeq:
		return !valuesEqual(n.left.eval(env), n.right.eval(env))
	case tokLt, tokLte, tokGt, tokGte:
		return compareOrdering(n.op, n.left.eval(env), n.right.eval(env))
	case tokIn:
		return evalIn(n.left.eval(env), n.right.eval(env))
	default:
		return nil
	}
}

func resolvePath(env map[string]any, path []string) any {
	if env == nil || len(path) == 0 {
		return nil
	}
	cur, ok := env[path[0]]
	if !ok {
		return nil
	}
	for _, seg := range path[1:] {
		if cur == nil {
			return nil
		}
		switch m := cur.(type) {
		case map[string]any:
			next, ok := m[seg]
			if !ok {
				return nil
			}
			cur = next
		case map[string]string:
			next, ok := m[seg]
			if !ok {
				return nil
			}
			cur = next
		case map[string]bool:
			next, ok := m[seg]
			if !ok {
				return nil
			}
			cur = next
		case map[string]int:
			next, ok := m[seg]
			if !ok {
				return nil
			}
			cur = next
		case map[string]int64:
			next, ok := m[seg]
			if !ok {
				return nil
			}
			cur = next
		case map[string]float64:
			next, ok := m[seg]
			if !ok {
				return nil
			}
			cur = next
		default:
			rv := reflect.ValueOf(cur)
			if rv.Kind() != reflect.Map || rv.IsNil() {
				return nil
			}
			keyType := rv.Type().Key()
			if keyType.Kind() != reflect.String {
				return nil
			}
			var kv reflect.Value
			if keyType == stringType {
				kv = reflect.ValueOf(seg)
			} else {
				kv = reflect.ValueOf(seg).Convert(keyType)
			}
			val := rv.MapIndex(kv)
			if !val.IsValid() || !val.CanInterface() {
				return nil
			}
			cur = val.Interface()
		}
	}
	return cur
}

func isTruthy(v any) bool {
	if v == nil {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int8:
		return x != 0
	case int16:
		return x != 0
	case int32:
		return x != 0
	case int64:
		return x != 0
	case uint:
		return x != 0
	case uint8:
		return x != 0
	case uint16:
		return x != 0
	case uint32:
		return x != 0
	case uint64:
		return x != 0
	case uintptr:
		return x != 0
	case float32:
		return x != 0 && !math.IsNaN(float64(x))
	case float64:
		return x != 0 && !math.IsNaN(x)
	case []any:
		return len(x) > 0
	case []string:
		return len(x) > 0
	case []int:
		return len(x) > 0
	case []float64:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	case map[string]string:
		return len(x) > 0
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Bool:
		return rv.Bool()
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len() > 0
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint() != 0
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		return f != 0 && !math.IsNaN(f)
	case reflect.Ptr, reflect.Interface, reflect.Func, reflect.Chan:
		return !rv.IsNil()
	default:
		return true
	}
}

type numKind uint8

const (
	numInt numKind = iota
	numUint
	numFloat
)

type numVal struct {
	kind numKind
	i    int64
	u    uint64
	f    float64
}

func toNumber(v any) (numVal, bool) {
	switch x := v.(type) {
	case int:
		return numVal{kind: numInt, i: int64(x)}, true
	case int8:
		return numVal{kind: numInt, i: int64(x)}, true
	case int16:
		return numVal{kind: numInt, i: int64(x)}, true
	case int32:
		return numVal{kind: numInt, i: int64(x)}, true
	case int64:
		return numVal{kind: numInt, i: x}, true
	case uint:
		return numVal{kind: numUint, u: uint64(x)}, true
	case uint8:
		return numVal{kind: numUint, u: uint64(x)}, true
	case uint16:
		return numVal{kind: numUint, u: uint64(x)}, true
	case uint32:
		return numVal{kind: numUint, u: uint64(x)}, true
	case uint64:
		return numVal{kind: numUint, u: x}, true
	case uintptr:
		return numVal{kind: numUint, u: uint64(x)}, true
	case float32:
		return numVal{kind: numFloat, f: float64(x)}, true
	case float64:
		return numVal{kind: numFloat, f: x}, true
	case nil, bool, string:
		return numVal{}, false
	}

	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return numVal{}, false
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return numVal{kind: numInt, i: rv.Int()}, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return numVal{kind: numUint, u: rv.Uint()}, true
	case reflect.Float32, reflect.Float64:
		return numVal{kind: numFloat, f: rv.Float()}, true
	default:
		return numVal{}, false
	}
}

func compareNumbers(a, b numVal) (int, bool) {
	switch a.kind {
	case numInt:
		switch b.kind {
		case numInt:
			if a.i < b.i {
				return -1, true
			}
			if a.i > b.i {
				return 1, true
			}
			return 0, true
		case numUint:
			if a.i < 0 {
				return -1, true
			}
			au := uint64(a.i)
			if au < b.u {
				return -1, true
			}
			if au > b.u {
				return 1, true
			}
			return 0, true
		case numFloat:
			return cmpIntFloat(a.i, b.f)
		}
	case numUint:
		switch b.kind {
		case numInt:
			if b.i < 0 {
				return 1, true
			}
			bu := uint64(b.i)
			if a.u < bu {
				return -1, true
			}
			if a.u > bu {
				return 1, true
			}
			return 0, true
		case numUint:
			if a.u < b.u {
				return -1, true
			}
			if a.u > b.u {
				return 1, true
			}
			return 0, true
		case numFloat:
			return cmpUintFloat(a.u, b.f)
		}
	case numFloat:
		switch b.kind {
		case numInt:
			c, ok := cmpIntFloat(b.i, a.f)
			return -c, ok
		case numUint:
			c, ok := cmpUintFloat(b.u, a.f)
			return -c, ok
		case numFloat:
			if math.IsNaN(a.f) || math.IsNaN(b.f) {
				return 0, false
			}
			if a.f < b.f {
				return -1, true
			}
			if a.f > b.f {
				return 1, true
			}
			return 0, true
		}
	}
	return 0, false
}

func cmpIntFloat(i int64, f float64) (int, bool) {
	if math.IsNaN(f) {
		return 0, false
	}
	const maxInt64AsFloat = 9223372036854775808.0  // 2^63, exact IEEE 754
	const minInt64AsFloat = -9223372036854775808.0 // -2^63, exact IEEE 754
	if f >= maxInt64AsFloat {
		return -1, true
	}
	if f < minInt64AsFloat {
		return 1, true
	}
	if f == minInt64AsFloat {
		if i == math.MinInt64 {
			return 0, true
		}
		return 1, true
	}
	trunc := math.Trunc(f)
	frac := f - trunc
	ti := int64(trunc)
	if i < ti {
		return -1, true
	}
	if i > ti {
		return 1, true
	}
	if frac > 0 {
		return -1, true
	}
	if frac < 0 {
		return 1, true
	}
	return 0, true
}

func cmpUintFloat(u uint64, f float64) (int, bool) {
	if math.IsNaN(f) {
		return 0, false
	}
	const maxUint64PlusOneAsFloat = 18446744073709551616.0 // 2^64, exact IEEE 754
	if f < 0 {
		return 1, true
	}
	if f >= maxUint64PlusOneAsFloat {
		return -1, true
	}
	trunc := math.Trunc(f)
	frac := f - trunc
	tu := uint64(trunc)
	if u < tu {
		return -1, true
	}
	if u > tu {
		return 1, true
	}
	if frac > 0 {
		return -1, true
	}
	return 0, true
}

func compareOrdering(op tokKind, lhs, rhs any) bool {
	na, okA := toNumber(lhs)
	nb, okB := toNumber(rhs)
	if !okA || !okB {
		return false
	}
	cmp, ok := compareNumbers(na, nb)
	if !ok {
		return false
	}
	switch op {
	case tokLt:
		return cmp < 0
	case tokLte:
		return cmp <= 0
	case tokGt:
		return cmp > 0
	case tokGte:
		return cmp >= 0
	default:
		return false
	}
}

func isNull(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Ptr && rv.IsNil()
}

func toBool(v any) (bool, bool) {
	if b, ok := v.(bool); ok {
		return b, true
	}
	if v == nil {
		return false, false
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Bool {
		return rv.Bool(), true
	}
	return false, false
}

func toString(v any) (string, bool) {
	if s, ok := v.(string); ok {
		return s, true
	}
	if v == nil {
		return "", false
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.String {
		return rv.String(), true
	}
	return "", false
}

func valuesEqual(a, b any) bool {
	return valuesEqualDepth(a, b, 0)
}

func valuesEqualDepth(a, b any, depth int) bool {
	if depth > maxEqualDepth {
		return false
	}
	nullA, nullB := isNull(a), isNull(b)
	if nullA || nullB {
		return nullA && nullB
	}

	na, numA := toNumber(a)
	nb, numB := toNumber(b)
	if numA || numB {
		if !numA || !numB {
			return false
		}
		cmp, ok := compareNumbers(na, nb)
		return ok && cmp == 0
	}

	ba, boolA := toBool(a)
	bb, boolB := toBool(b)
	if boolA || boolB {
		return boolA && boolB && ba == bb
	}

	sa, strA := toString(a)
	sb, strB := toString(b)
	if strA || strB {
		return strA && strB && sa == sb
	}

	rvA := reflect.ValueOf(a)
	rvB := reflect.ValueOf(b)

	seqA := rvA.Kind() == reflect.Slice || rvA.Kind() == reflect.Array
	seqB := rvB.Kind() == reflect.Slice || rvB.Kind() == reflect.Array
	if seqA || seqB {
		if !seqA || !seqB || rvA.Len() != rvB.Len() {
			return false
		}
		n := rvA.Len()
		for i := 0; i < n; i++ {
			ea, eb := rvA.Index(i), rvB.Index(i)
			if !ea.CanInterface() || !eb.CanInterface() {
				return false
			}
			if !valuesEqualDepth(ea.Interface(), eb.Interface(), depth+1) {
				return false
			}
		}
		return true
	}

	mapA := rvA.Kind() == reflect.Map
	mapB := rvB.Kind() == reflect.Map
	if mapA || mapB {
		if !mapA || !mapB || rvA.Len() != rvB.Len() {
			return false
		}
		keyA := rvA.Type().Key()
		keyB := rvB.Type().Key()
		if keyA.Kind() == reflect.String && keyB.Kind() == reflect.String {
			iter := rvA.MapRange()
			for iter.Next() {
				k := iter.Key()
				va := iter.Value()
				var kb reflect.Value
				if keyA == keyB {
					kb = k
				} else {
					kb = reflect.ValueOf(k.String()).Convert(keyB)
				}
				vb := rvB.MapIndex(kb)
				if !vb.IsValid() || !va.CanInterface() || !vb.CanInterface() {
					return false
				}
				if !valuesEqualDepth(va.Interface(), vb.Interface(), depth+1) {
					return false
				}
			}
			return true
		}
		return reflect.DeepEqual(a, b)
	}

	return reflect.DeepEqual(a, b)
}

func evalIn(lhs, rhs any) bool {
	if isNull(rhs) {
		return false
	}
	switch xs := rhs.(type) {
	case []any:
		for _, elem := range xs {
			if valuesEqual(lhs, elem) {
				return true
			}
		}
		return false
	case []string:
		ls, ok := toString(lhs)
		if !ok {
			return false
		}
		for _, elem := range xs {
			if ls == elem {
				return true
			}
		}
		return false
	case []int:
		na, ok := toNumber(lhs)
		if !ok {
			return false
		}
		for _, elem := range xs {
			if cmp, cmpOK := compareNumbers(na, numVal{kind: numInt, i: int64(elem)}); cmpOK && cmp == 0 {
				return true
			}
		}
		return false
	case []float64:
		na, ok := toNumber(lhs)
		if !ok {
			return false
		}
		for _, elem := range xs {
			if cmp, cmpOK := compareNumbers(na, numVal{kind: numFloat, f: elem}); cmpOK && cmp == 0 {
				return true
			}
		}
		return false
	}

	rv := reflect.ValueOf(rhs)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		n := rv.Len()
		for i := 0; i < n; i++ {
			elem := rv.Index(i)
			if elem.CanInterface() && valuesEqual(lhs, elem.Interface()) {
				return true
			}
		}
		return false
	default:
		return false
	}
}
