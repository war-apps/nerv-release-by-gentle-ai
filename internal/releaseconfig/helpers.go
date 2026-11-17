package releaseconfig

import "gopkg.in/yaml.v3"

// scalarNode is a plain string scalar YAML node, used by MoveKeys and
// ImportSection (setup.go) to grow a mapping node in place.
func scalarNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}
