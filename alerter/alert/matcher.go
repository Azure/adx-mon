package alert

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

const (
	// MaxSchemaDepth is the maximum combinator nesting depth permitted in a /destinations JSON Schema.
	MaxSchemaDepth = 8

	// MaxSchemaPatterns is the maximum total number of compiled RE2 patterns permitted in a /destinations JSON Schema.
	MaxSchemaPatterns = 64

	// maxJSONTokenDepth bounds raw JSON nesting inside metadata annotations such as "examples".
	maxJSONTokenDepth = 32
)

// Matcher evaluates destination strings against a compiled, allow-listed JSON Schema string subset.
type Matcher struct {
	root matchNode
}

type matchNode interface {
	match(s string) bool
}

type boolNode bool

func (b boolNode) match(_ string) bool { return bool(b) }

type objectNode struct {
	hasConst   bool
	constVal   string
	hasEnum    bool
	enumSet    map[string]struct{}
	pattern    *regexp.Regexp
	anyOfNodes []matchNode
	oneOfNodes []matchNode
	allOfNodes []matchNode
}

func (n *objectNode) match(s string) bool {
	if n.hasConst && s != n.constVal {
		return false
	}
	if n.hasEnum {
		if _, ok := n.enumSet[s]; !ok {
			return false
		}
	}
	if n.pattern != nil && !n.pattern.MatchString(s) {
		return false
	}
	if len(n.anyOfNodes) > 0 {
		matchedAny := false
		for _, sub := range n.anyOfNodes {
			if sub.match(s) {
				matchedAny = true
				break
			}
		}
		if !matchedAny {
			return false
		}
	}
	if len(n.oneOfNodes) > 0 {
		matches := 0
		for _, sub := range n.oneOfNodes {
			if sub.match(s) {
				matches++
				if matches > 1 {
					return false
				}
			}
		}
		if matches != 1 {
			return false
		}
	}
	for _, sub := range n.allOfNodes {
		if !sub.match(s) {
			return false
		}
	}
	return true
}

type compilerState struct {
	patterns int
}

// CompileMatcher parses and compiles an allow-listed JSON Schema string matcher.
// Any unknown keyword, non-string type, invalid regex, depth > 8, or pattern count > 64 is rejected fail-closed.
func CompileMatcher(raw []byte) (*Matcher, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("empty destinations schema")
	}
	if err := validateStrictJSONStructure(trimmed); err != nil {
		return nil, err
	}

	st := &compilerState{}
	node, err := st.compileSchema(trimmed, 0)
	if err != nil {
		return nil, err
	}
	return &Matcher{root: node}, nil
}

// Match reports whether destination string s satisfies the compiled schema.
func (m *Matcher) Match(s string) bool {
	if m == nil || m.root == nil {
		return false
	}
	return m.root.match(s)
}

// Filter returns the ordered subset of candidates accepted by m.
func (m *Matcher) Filter(candidates []string) []string {
	if m == nil || len(candidates) == 0 {
		return nil
	}
	var out []string
	for _, c := range candidates {
		if m.Match(c) {
			out = append(out, c)
		}
	}
	return out
}

func (st *compilerState) compileSchema(raw []byte, depth int) (matchNode, error) {
	if depth > MaxSchemaDepth {
		return nil, fmt.Errorf("destinations schema exceeds maximum nesting depth %d", MaxSchemaDepth)
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("empty subschema")
	}

	switch trimmed[0] {
	case 't', 'f':
		var b bool
		if err := json.Unmarshal(trimmed, &b); err != nil {
			return nil, fmt.Errorf("invalid boolean schema: %w", err)
		}
		return boolNode(b), nil
	case '{':
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &fields); err != nil {
			return nil, fmt.Errorf("invalid object schema: %w", err)
		}
		return st.compileObjectSchema(fields, depth)
	default:
		return nil, errors.New("destinations schema must be a JSON boolean or object")
	}
}

func (st *compilerState) compileObjectSchema(fields map[string]json.RawMessage, depth int) (matchNode, error) {
	node := &objectNode{}

	for key, rawVal := range fields {
		switch key {
		case "$schema", "$id", "title", "description", "$comment":
			if _, err := parseStrictJSONString(rawVal, key); err != nil {
				return nil, err
			}
		case "examples":
			if err := validateJSONArray(rawVal, key); err != nil {
				return nil, err
			}
		case "type":
			typ, err := parseStrictJSONString(rawVal, "type")
			if err != nil {
				return nil, err
			}
			if typ != "string" {
				return nil, fmt.Errorf("unsupported schema type %q: only \"string\" is allowed", typ)
			}
		case "const":
			val, err := parseStrictJSONString(rawVal, "const")
			if err != nil {
				return nil, err
			}
			node.hasConst = true
			node.constVal = val
		case "enum":
			items, err := parseStrictStringSlice(rawVal, "enum")
			if err != nil {
				return nil, err
			}
			if len(items) == 0 {
				return nil, errors.New("schema keyword \"enum\" must contain at least one string")
			}
			node.hasEnum = true
			node.enumSet = make(map[string]struct{}, len(items))
			for _, it := range items {
				node.enumSet[it] = struct{}{}
			}
		case "pattern":
			pat, err := parseStrictJSONString(rawVal, "pattern")
			if err != nil {
				return nil, err
			}
			st.patterns++
			if st.patterns > MaxSchemaPatterns {
				return nil, fmt.Errorf("destinations schema exceeds maximum pattern count %d", MaxSchemaPatterns)
			}
			re, err := regexp.Compile(pat)
			if err != nil {
				return nil, fmt.Errorf("invalid RE2 pattern: %w", err)
			}
			node.pattern = re
		case "anyOf":
			subs, err := st.compileSubschemaList(rawVal, "anyOf", depth+1)
			if err != nil {
				return nil, err
			}
			node.anyOfNodes = subs
		case "oneOf":
			subs, err := st.compileSubschemaList(rawVal, "oneOf", depth+1)
			if err != nil {
				return nil, err
			}
			node.oneOfNodes = subs
		case "allOf":
			subs, err := st.compileSubschemaList(rawVal, "allOf", depth+1)
			if err != nil {
				return nil, err
			}
			node.allOfNodes = subs
		default:
			return nil, fmt.Errorf("unsupported JSON Schema keyword %q", key)
		}
	}

	return node, nil
}

func (st *compilerState) compileSubschemaList(raw json.RawMessage, keyword string, nextDepth int) ([]matchNode, error) {
	if nextDepth > MaxSchemaDepth {
		return nil, fmt.Errorf("destinations schema exceeds maximum nesting depth %d", MaxSchemaDepth)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("schema keyword %q must be a JSON array", keyword)
	}
	var rawList []json.RawMessage
	if err := json.Unmarshal(trimmed, &rawList); err != nil {
		return nil, fmt.Errorf("invalid %q array: %w", keyword, err)
	}
	if len(rawList) == 0 {
		return nil, fmt.Errorf("schema keyword %q must contain at least one subschema", keyword)
	}
	nodes := make([]matchNode, 0, len(rawList))
	for _, elem := range rawList {
		sub, err := st.compileSchema(elem, nextDepth)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, sub)
	}
	return nodes, nil
}

func parseStrictJSONString(raw json.RawMessage, keyword string) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return "", fmt.Errorf("schema keyword %q must be a string", keyword)
	}
	var s string
	if err := json.Unmarshal(trimmed, &s); err != nil {
		return "", fmt.Errorf("invalid string for %q: %w", keyword, err)
	}
	return s, nil
}

func parseStrictStringSlice(raw json.RawMessage, keyword string) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("schema keyword %q must be an array of strings", keyword)
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(trimmed, &elems); err != nil {
		return nil, fmt.Errorf("invalid array for %q: %w", keyword, err)
	}
	out := make([]string, 0, len(elems))
	for i, elem := range elems {
		s, err := parseStrictJSONString(elem, fmt.Sprintf("%s[%d]", keyword, i))
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func validateJSONArray(raw json.RawMessage, keyword string) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return fmt.Errorf("schema keyword %q must be a JSON array", keyword)
	}
	var elems []any
	if err := json.Unmarshal(trimmed, &elems); err != nil {
		return fmt.Errorf("invalid array for %q: %w", keyword, err)
	}
	return nil
}

// validateStrictJSONStructure rejects duplicate object keys, excessive JSON token nesting,
// and trailing tokens after the root JSON value.
func validateStrictJSONStructure(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := walkJSONValue(dec, 0); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected trailing JSON token in destinations schema")
		}
		return fmt.Errorf("invalid JSON in destinations schema: %w", err)
	}
	return nil
}

func walkJSONValue(dec *json.Decoder, depth int) error {
	if depth > maxJSONTokenDepth {
		return fmt.Errorf("JSON nesting exceeds maximum token depth %d", maxJSONTokenDepth)
	}
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("invalid JSON in destinations schema: %w", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return fmt.Errorf("invalid JSON object key: %w", err)
			}
			key, isStr := keyTok.(string)
			if !isStr {
				return errors.New("invalid non-string JSON object key")
			}
			if _, dup := seen[key]; dup {
				return fmt.Errorf("duplicate JSON key %q in destinations schema", key)
			}
			seen[key] = struct{}{}
			if err := walkJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
		endTok, err := dec.Token()
		if err != nil || endTok != json.Delim('}') {
			return errors.New("malformed JSON object in destinations schema")
		}
	case '[':
		for dec.More() {
			if err := walkJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
		endTok, err := dec.Token()
		if err != nil || endTok != json.Delim(']') {
			return errors.New("malformed JSON array in destinations schema")
		}
	}
	return nil
}
