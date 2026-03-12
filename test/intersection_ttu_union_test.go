package test

import (
	"testing"

	"github.com/pthm/melange/pkg/parser"
	"github.com/pthm/melange/pkg/schema"
	"github.com/stretchr/testify/require"
)

// TestIntersectionWithTTUUnionParsing verifies that intersection rules containing
// unions of tuple-to-userset patterns are correctly parsed.
//
// Schema: can_view: viewer and (member from group or owner from group)
// This should produce 2 intersection groups (via distributive law):
//   - Group 1: viewer AND (member from group)
//   - Group 2: viewer AND (owner from group)
func TestIntersectionWithTTUUnionParsing(t *testing.T) {
	dsl := `
model
  schema 1.1

type user

type group
  relations
    define owner: [user]
    define member: [user]

type folder
  relations
    define group: [group]
    define viewer: [user]
    define can_view: viewer and (member from group or owner from group)
`

	types, err := parser.ParseSchemaString(dsl)
	require.NoError(t, err)

	// Find the can_view relation on folder
	var canViewRel *schema.RelationDefinition
	for i := range types {
		if types[i].Name != "folder" {
			continue
		}
		for j := range types[i].Relations {
			if types[i].Relations[j].Name == "can_view" {
				canViewRel = &types[i].Relations[j]
				break
			}
		}
	}
	require.NotNil(t, canViewRel, "can_view relation not found")

	// Should have 2 intersection groups due to distributive expansion
	require.Len(t, canViewRel.IntersectionGroups, 2,
		"expected 2 intersection groups from distributive law: (viewer AND member from group) OR (viewer AND owner from group)")

	// Each group should have the viewer relation
	for i, g := range canViewRel.IntersectionGroups {
		require.Contains(t, g.Relations, "viewer",
			"group %d should contain 'viewer' relation", i)
		require.Len(t, g.ParentRelations, 1,
			"group %d should have exactly 1 parent relation", i)
		require.Equal(t, "group", g.ParentRelations[0].LinkingRelation,
			"group %d parent relation should link via 'group'", i)
	}

	// Verify we have both member and owner parent relations across groups
	parentRels := make(map[string]bool)
	for _, g := range canViewRel.IntersectionGroups {
		for _, pr := range g.ParentRelations {
			parentRels[pr.Relation] = true
		}
	}
	require.True(t, parentRels["member"], "should have 'member from group' parent relation")
	require.True(t, parentRels["owner"], "should have 'owner from group' parent relation")
}
