package releaseconfig

import "gopkg.in/yaml.v3"

// scalarNode is a plain string scalar YAML node, used by MoveKeys to grow a
// mapping node in place.
//
// Ported from nerv's internal/releaseconfig/setup.go (ImportSection and its
// *EngineConflictError are deferred to U3, which ports the comment-preserving
// internal/config editor they depend on); this one helper moved.go also
// needs is kept here so moved.go can be ported now without the rest of
// setup.go.
func scalarNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}
