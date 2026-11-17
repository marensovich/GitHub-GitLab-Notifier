package config

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// expand replaces ${VAR} and ${VAR:-default} in s with values from env.
// $$ is a literal $. An unset variable without a default is an error.
func expand(s string, env func(string) (string, bool)) (string, error) {
	if !strings.Contains(s, "$") {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '$' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		switch s[i+1] {
		case '$':
			b.WriteByte('$')
			i++
		case '{':
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				return "", fmt.Errorf("unterminated ${ in %q", s)
			}
			expr := s[i+2 : i+2+end]
			name, def, hasDef := strings.Cut(expr, ":-")
			if name == "" {
				return "", fmt.Errorf("empty variable name in %q", s)
			}
			v, ok := env(name)
			switch {
			case ok && v != "":
				b.WriteString(v)
			case hasDef:
				b.WriteString(def)
			case ok:
				// Set but empty: an empty value is what the user asked for.
			default:
				return "", fmt.Errorf("environment variable %s is not set (use ${%s:-} for an empty default)", name, name)
			}
			i += 2 + end
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}

// expandNode expands every scalar under n; path names the YAML key in errors.
func expandNode(n *yaml.Node, path string, env func(string) (string, bool)) error {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			if err := expandNode(c, path, env); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			p := key
			if path != "" {
				p = path + "." + key
			}
			if err := expandNode(n.Content[i+1], p, env); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			if err := expandNode(c, path+"["+strconv.Itoa(i)+"]", env); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		v, err := expand(n.Value, env)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if v != n.Value {
			n.Value = v
			if n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) == 0 {
				// Let the expanded value be read as a number or a bool
				// where the field expects one.
				n.Tag = ""
			} else {
				n.Tag = "!!str"
			}
		}
	}
	return nil
}
