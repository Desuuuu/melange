package test

import (
	"testing"

	"github.com/pthm/melange/pkg/parser"
	"github.com/pthm/melange/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestSubtractDifferenceExclusionParsing(t *testing.T) {
	dsl := `
model
  schema 1.1

type user

type document
  relations
    define writer: [user]
    define editor: [user]
    define owner: [user]
    define viewer: writer but not (editor but not owner)
`

	types, err := parser.ParseSchemaString(dsl)
	require.NoError(t, err)

	var viewerRel *schema.RelationDefinition
	for i := range types {
		if types[i].Name != "document" {
			continue
		}
		for j := range types[i].Relations {
			if types[i].Relations[j].Name == "viewer" {
				viewerRel = &types[i].Relations[j]
				break
			}
		}
	}
	require.NotNil(t, viewerRel)
	require.Len(t, viewerRel.ExcludedIntersectionGroups, 1)
	require.ElementsMatch(t, viewerRel.ExcludedIntersectionGroups[0].Relations, []string{"editor"})
	require.Contains(t, viewerRel.ExcludedIntersectionGroups[0].Exclusions, "editor")
	require.ElementsMatch(t, viewerRel.ExcludedIntersectionGroups[0].Exclusions["editor"], []string{"owner"})
}
