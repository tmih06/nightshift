package api_test

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/tmih06/nightshift/api"
	v1 "github.com/tmih06/nightshift/api/v1"
)

// schemaTypes maps each named schema of api/openapi.yaml to the Go type that
// carries its body.
func schemaTypes() map[string]reflect.Type {
	return map[string]reflect.Type{
		"Problem":         reflect.TypeFor[v1.Problem](),
		"Info":            reflect.TypeFor[v1.Info](),
		"Fleet":           reflect.TypeFor[v1.Fleet](),
		"Pod":             reflect.TypeFor[v1.Pod](),
		"Pool":            reflect.TypeFor[v1.Pool](),
		"Harness":         reflect.TypeFor[v1.Harness](),
		"HarnessModel":    reflect.TypeFor[v1.HarnessModel](),
		"Logs":            reflect.TypeFor[v1.Logs](),
		"ClaimRequest":    reflect.TypeFor[v1.ClaimRequest](),
		"Claim":           reflect.TypeFor[v1.Claim](),
		"Target":          reflect.TypeFor[v1.Target](),
		"CheckoutRequest": reflect.TypeFor[v1.CheckoutRequest](),
		"Checkout":        reflect.TypeFor[v1.Checkout](),
		"CheckoutEvent":   reflect.TypeFor[v1.CheckoutEvent](),
		"RunSource":       reflect.TypeFor[v1.RunSource](),
		"RunRequest":      reflect.TypeFor[v1.RunRequest](),
		"Run":             reflect.TypeFor[v1.Run](),
		"Runs":            reflect.TypeFor[v1.Runs](),
		"Volume":          reflect.TypeFor[v1.Volume](),
		"Volumes":         reflect.TypeFor[v1.Volumes](),
		"Terminal":        reflect.TypeFor[v1.Terminal](),
		"Terminals":       reflect.TypeFor[v1.Terminals](),
		"TerminalRequest": reflect.TypeFor[v1.TerminalRequest](),
		"Key":             reflect.TypeFor[v1.Key](),
		"Input":           reflect.TypeFor[v1.Input](),
		"Screen":          reflect.TypeFor[v1.Screen](),
		"Provider":        reflect.TypeFor[v1.Provider](),
		"Accounts":        reflect.TypeFor[v1.Accounts](),
		"Account":         reflect.TypeFor[v1.Account](),
		"AccountUsage":    reflect.TypeFor[v1.AccountUsage](),
		"Published":       reflect.TypeFor[v1.Published](),
		"LoginRequest":    reflect.TypeFor[v1.LoginRequest](),
		"Login":           reflect.TypeFor[v1.Login](),
		"LoginCompletion": reflect.TypeFor[v1.LoginCompletion](),
		"AccountPatch":    reflect.TypeFor[v1.AccountPatch](),
		"ActiveAccount":   reflect.TypeFor[v1.ActiveAccount](),
		"PublishReport":   reflect.TypeFor[v1.PublishReport](),
	}
}

type object = map[string]any

func loadSpec(t *testing.T) object {
	t.Helper()
	var spec object
	if err := yaml.Unmarshal(api.OpenAPI, &spec); err != nil {
		t.Fatalf("api/openapi.yaml does not parse: %v", err)
	}
	return spec
}

// asObject is v as a YAML mapping, or nil when it is not one.
func asObject(v any) object {
	m, ok := v.(object)
	if !ok {
		return nil
	}
	return m
}

// asText is v as a string, or "" when it is not one.
func asText(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func dig(m object, keys ...string) object {
	cur := m
	for _, k := range keys {
		next, ok := cur[k].(object)
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

// field is one JSON field of a Go struct.
type field struct {
	typ      reflect.Type
	optional bool
}

func jsonFields(t reflect.Type) map[string]field {
	out := map[string]field{}
	for i := range t.NumField() {
		f := t.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		out[name] = field{
			typ:      f.Type,
			optional: strings.Contains(opts, "omitempty") || strings.Contains(opts, "omitzero"),
		}
	}
	return out
}

// compare checks one schema against the Go type that carries it, down through
// inline objects, arrays, references, and maps.
func compare(spec, schema object, typ reflect.Type, where string) []string {
	if ref, ok := schema["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		target := dig(spec, "components", "schemas", name)
		if target == nil {
			return []string{where + ": unresolved " + ref}
		}
		return compare(spec, target, typ, where+"→"+name)
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Slice:
		items := asObject(schema["items"])
		if items == nil {
			return nil
		}
		return compare(spec, items, typ.Elem(), where+"[]")
	case reflect.Map:
		extra := asObject(schema["additionalProperties"])
		if extra == nil {
			return nil
		}
		return compare(spec, extra, typ.Elem(), where+"{}")
	case reflect.Struct:
		if typ.PkgPath() == "time" {
			return nil
		}
	default:
		return nil
	}
	return compareObject(spec, schema, typ, where)
}

func compareObject(spec, schema object, typ reflect.Type, where string) []string {
	var bad []string
	props := asObject(schema["properties"])
	fields := jsonFields(typ)
	required := map[string]bool{}
	if list, ok := schema["required"].([]any); ok {
		for _, r := range list {
			required[fmt.Sprint(r)] = true
		}
	}
	for name, f := range fields {
		prop, ok := props[name].(object)
		if !ok {
			bad = append(bad, fmt.Sprintf("%s: Go field %q is not in the schema", where, name))
			continue
		}
		if f.optional == required[name] {
			bad = append(bad, fmt.Sprintf("%s.%s: Go says optional=%v, schema says required=%v", where, name, f.optional, required[name]))
		}
		bad = append(bad, compare(spec, prop, f.typ, where+"."+name)...)
	}
	for name := range props {
		if _, ok := fields[name]; !ok {
			bad = append(bad, fmt.Sprintf("%s: schema property %q is not a Go field", where, name))
		}
	}
	return bad
}

// TestSchemasMatchWireTypes keeps api/openapi.yaml and api/v1 saying the same
// thing: a body cannot gain a field in one and not the other.
func TestSchemasMatchWireTypes(t *testing.T) {
	spec := loadSpec(t)
	schemas := dig(spec, "components", "schemas")
	types := schemaTypes()
	for name := range schemas {
		if _, ok := types[name]; !ok {
			t.Errorf("schema %s has no Go type in schemaTypes", name)
		}
	}
	for name, typ := range types {
		schema, ok := schemas[name].(object)
		if !ok {
			t.Errorf("Go type %s has no schema", name)
			continue
		}
		for _, msg := range compare(spec, schema, typ, name) {
			t.Error(msg)
		}
	}
}

var refRE = regexp.MustCompile(`\$ref: "([^"]+)"`)

// TestEveryReferenceResolves catches a $ref that points at nothing.
func TestEveryReferenceResolves(t *testing.T) {
	spec := loadSpec(t)
	for _, m := range refRE.FindAllStringSubmatch(string(api.OpenAPI), -1) {
		path := strings.Split(strings.TrimPrefix(m[1], "#/"), "/")
		if dig(spec, path[:len(path)-1]...)[path[len(path)-1]] == nil {
			t.Errorf("%s resolves to nothing", m[1])
		}
	}
}

// TestEveryOperationIsDocumented makes each operation carry an operationId, a
// tag, and a summary, which generators and humans both rely on.
func TestEveryOperationIsDocumented(t *testing.T) {
	spec := loadSpec(t)
	seen := map[string]string{}
	for path, item := range dig(spec, "paths") {
		for method, op := range asObject(item) {
			o, ok := op.(object)
			if !ok || method == "parameters" {
				continue
			}
			where := strings.ToUpper(method) + " " + path
			id := asText(o["operationId"])
			if id == "" {
				t.Errorf("%s has no operationId", where)
			} else if prev, dup := seen[id]; dup {
				t.Errorf("%s repeats operationId %s of %s", where, id, prev)
			}
			seen[id] = where
			if o["summary"] == nil || o["tags"] == nil {
				t.Errorf("%s needs a summary and a tag", where)
			}
			if o["responses"] == nil {
				t.Errorf("%s has no responses", where)
			}
		}
	}
}

var credentialName = regexp.MustCompile(`(?i)(token|secret|password|passwd|credential|apikey|managementkey|privatekey|refresh_?token)`)

// TestNoCredentialFields keeps the contract's promise that no response carries
// a credential: no wire type may have a field named like one.
func TestNoCredentialFields(t *testing.T) {
	for name, typ := range schemaTypes() {
		for field := range jsonFields(typ) {
			if credentialName.MatchString(field) {
				t.Errorf("%s.%s looks like a credential. The API never returns one.", name, field)
			}
		}
	}
	spec := loadSpec(t)
	for name, s := range dig(spec, "components", "schemas") {
		for prop := range dig(asObject(s), "properties") {
			if credentialName.MatchString(prop) {
				t.Errorf("schema %s.%s looks like a credential. The API never returns one.", name, prop)
			}
		}
	}
}
