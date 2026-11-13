package mcp

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"ofam.dev/ochazuke"
)

// Adapted from matomeru internal/mcp: validate the schema subset tools
// expose. owner names the tool (or the object) in an unknown-argument
// error, worded as the CLI words it (ochazuke.NoParameter): "owner has no
// parameter NAME: did you mean …; it takes …". A null an optional
// argument holds is no argument: it is dropped from args, never refused.
func validate(owner string, schema map[string]any, args map[string]any) error {
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]string)
	for _, name := range required {
		if v, ok := args[name]; !ok || v == nil {
			return fmt.Errorf("%s is required", name)
		}
	}
	names := make([]string, 0, len(args))
	for name, v := range args {
		if v == nil {
			delete(args, name) // not required (checked above): unset
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		prop, ok := props[name].(map[string]any)
		if !ok {
			if extra, _ := schema["additionalProperties"].(bool); !extra {
				known := slices.Sorted(func(yield func(string) bool) {
					for k := range props {
						if !yield(k) {
							return
						}
					}
				})
				takes := "it takes no parameters"
				if len(known) > 0 {
					takes = "it takes " + strings.Join(known, ", ")
				}
				return &ochazuke.UnknownNameError{Kind: "parameter", Name: name, Of: owner, Near: ochazuke.NearNames(name, known, ochazuke.NearShown), List: takes}
			}
			continue
		}
		if err := validateValue(prop, args[name]); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func validateValue(prop map[string]any, value any) error {
	if value == nil {
		return fmt.Errorf("must not be null")
	}
	want, _ := prop["type"].(string)
	switch want {
	case "string":
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("must be a string, not %s", jsonType(value))
		}
		if minLen, ok := prop["minLength"].(int); ok && len(s) < minLen {
			return fmt.Errorf("must not be empty")
		}
		if enum, ok := prop["enum"].([]string); ok && !slices.Contains(enum, s) {
			return fmt.Errorf("must be one of %s", strings.Join(enum, ", "))
		}
	case "integer":
		switch n := value.(type) {
		case int, int64:
		case float64:
			if n != float64(int64(n)) {
				return fmt.Errorf("must be an integer, not %v", n)
			}
		default:
			return fmt.Errorf("must be an integer, not %s", jsonType(value))
		}
	case "array":
		values, ok := value.([]any)
		if !ok {
			return fmt.Errorf("must be an array")
		}
		item, _ := prop["items"].(map[string]any)
		for _, v := range values {
			if err := validateValue(item, v); err != nil {
				return err
			}
		}
	case "number":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("must be a number")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("must be a boolean, not %s", jsonType(value))
		}
	case "object":
		m, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("must be an object, not %s", jsonType(value))
		}
		return validate("the object", prop, m)
	}

	var number float64
	numeric := true
	switch n := value.(type) {
	case int:
		number = float64(n)
	case int64:
		number = float64(n)
	case float64:
		number = n
	default:
		numeric = false
	}
	if numeric {
		if min, ok := prop["minimum"].(int); ok && number < float64(min) {
			return fmt.Errorf("must be at least %d", min)
		}
		if max, ok := prop["maximum"].(int); ok && number > float64(max) {
			return fmt.Errorf("must be at most %d", max)
		}
	}
	return nil
}

func jsonType(v any) string {
	switch v.(type) {
	case string:
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a boolean"
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	}
	return fmt.Sprintf("%T", v)
}
