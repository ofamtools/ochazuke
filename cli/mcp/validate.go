package mcp

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Adapted from matomeru internal/mcp: validate the schema subset tools expose.
func validate(schema map[string]any, args map[string]any) error {
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]string)
	for _, name := range required {
		if v, ok := args[name]; !ok || v == nil {
			return fmt.Errorf("%s is required", name)
		}
	}
	names := make([]string, 0, len(args))
	for name := range args {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		prop, ok := props[name].(map[string]any)
		if !ok {
			if extra, _ := schema["additionalProperties"].(bool); !extra {
				return fmt.Errorf("unknown argument %q", name)
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
		return validate(prop, m)
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
