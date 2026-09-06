package pipeline

import (
	"fmt"
	"reflect"

	"github.com/qiangli/ycode/internal/harness/spec"
)

func evalExpression(expr spec.Expression, state *State, overlay map[string]any) (bool, error) {
	resolve := func(op spec.Operand) (any, error) {
		if op.Field != "" {
			parts := splitFirst(op.Field)
			if v, ok := overlay[parts[0]]; ok {
				if parts[1] == "" {
					return v, nil
				}
				if got, found := field(v, parts[1]); found {
					return got, nil
				}
				return nil, nil
			}
			v, _, _, ok := state.Read(op.Field)
			if !ok {
				return nil, nil
			}
			return v, nil
		}
		return op.Literal, nil
	}
	cmp := func(ops []spec.Operand, fn func(any, any) bool) (bool, error) {
		a, e := resolve(ops[0])
		if e != nil {
			return false, e
		}
		b, e := resolve(ops[1])
		if e != nil {
			return false, e
		}
		return fn(a, b), nil
	}
	switch {
	case len(expr.Eq) > 0:
		return cmp(expr.Eq, reflect.DeepEqual)
	case len(expr.Ne) > 0:
		return cmp(expr.Ne, func(a, b any) bool { return !reflect.DeepEqual(a, b) })
	case len(expr.LT) > 0:
		return cmp(expr.LT, func(a, b any) bool { return ordered(a, b) < 0 })
	case len(expr.LTE) > 0:
		return cmp(expr.LTE, func(a, b any) bool { return ordered(a, b) <= 0 })
	case len(expr.GT) > 0:
		return cmp(expr.GT, func(a, b any) bool { return ordered(a, b) > 0 })
	case len(expr.GTE) > 0:
		return cmp(expr.GTE, func(a, b any) bool { return ordered(a, b) >= 0 })
	case len(expr.Exists) > 0:
		v, e := resolve(expr.Exists[0])
		return v != nil, e
	case len(expr.In) > 0:
		needle, e := resolve(expr.In[0])
		if e != nil {
			return false, e
		}
		hay, e := resolve(expr.In[1])
		if e != nil {
			return false, e
		}
		rv := reflect.ValueOf(hay)
		if rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) {
			for i := 0; i < rv.Len(); i++ {
				if reflect.DeepEqual(needle, rv.Index(i).Interface()) {
					return true, nil
				}
			}
		}
		return false, nil
	case len(expr.All) > 0:
		for _, x := range expr.All {
			ok, e := evalExpression(x, state, overlay)
			if e != nil || !ok {
				return ok, e
			}
		}
		return true, nil
	case len(expr.Any) > 0:
		for _, x := range expr.Any {
			ok, e := evalExpression(x, state, overlay)
			if e != nil {
				return false, e
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case len(expr.Not) > 0:
		ok, e := evalExpression(expr.Not[0], state, overlay)
		return !ok, e
	default:
		return false, fmt.Errorf("pipeline: empty expression")
	}
}

func splitFirst(s string) [2]string {
	for i, c := range s {
		if c == '.' {
			return [2]string{s[:i], s[i+1:]}
		}
	}
	return [2]string{s, ""}
}
func ordered(a, b any) int {
	af, aok := number(a)
	bf, bok := number(b)
	if aok && bok {
		if af < bf {
			return -1
		}
		if af > bf {
			return 1
		}
		return 0
	}
	as, sa := a.(string)
	bs, sb := b.(string)
	if sa && sb {
		if as < bs {
			return -1
		}
		if as > bs {
			return 1
		}
	}
	return 0
}
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	}
	return 0, false
}
