package test

import (
	"testing"

	"github.com/pthm/melange/pkg/parser"
	"github.com/pthm/melange/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestIntersectionParsing(t *testing.T) {
	dsl := `
model
  schema 1.1

type user

type document
  relations
    define writer: [user]
    define editor: [user]
    define viewer: writer and editor
`

	types, err := parser.ParseSchemaString(dsl)
	require.NoError(t, err)
	require.Len(t, types, 2) // user, document

	// Find document type
	var docType *schema.TypeDefinition
	for i := range types {
		if types[i].Name == "document" {
			docType = &types[i]
			break
		}
	}
	require.NotNil(t, docType)

	// Find viewer relation
	var viewerRel *schema.RelationDefinition
	for i := range docType.Relations {
		if docType.Relations[i].Name == "viewer" {
			viewerRel = &docType.Relations[i]
			break
		}
	}
	require.NotNil(t, viewerRel)

	// Check intersection groups
	t.Logf("viewer.IntersectionGroups: %+v", viewerRel.IntersectionGroups)
	t.Logf("viewer.ImpliedBy: %+v", viewerRel.ImpliedBy)
	t.Logf("viewer.SubjectTypeRefs: %+v", viewerRel.SubjectTypeRefs)

	require.Len(t, viewerRel.IntersectionGroups, 1, "should have one intersection group")
	require.ElementsMatch(t, viewerRel.IntersectionGroups[0].Relations, []string{"writer", "editor"})
	require.Empty(t, viewerRel.ImpliedBy, "ImpliedBy should be empty for intersection")

	// Check model generation
	models := schema.ToAuthzModels(types)
	t.Logf("Generated %d models", len(models))

	// Count intersection rules for viewer
	var intersectionRules []schema.AuthzModel
	for _, m := range models {
		if m.ObjectType == "document" && m.Relation == "viewer" {
			t.Logf("viewer model: %+v", m)
			if m.RuleGroupMode != nil && *m.RuleGroupMode == "intersection" {
				intersectionRules = append(intersectionRules, m)
			}
		}
	}

	require.Len(t, intersectionRules, 2, "should have 2 intersection rules for viewer")

	// Check the rules have correct check_relations
	checkRelations := make([]string, 0, len(intersectionRules))
	for _, r := range intersectionRules {
		require.NotNil(t, r.CheckRelation)
		checkRelations = append(checkRelations, *r.CheckRelation)
	}
	require.ElementsMatch(t, checkRelations, []string{"writer", "editor"})
}

func TestIntersectionExclusionInSubtractParsing(t *testing.T) {
	dsl := `
model
  schema 1.1

type user

type document
  relations
    define writer: [user]
    define editor: [user]
    define owner: [user]
    define viewer: writer but not (editor and owner)
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
	require.Len(t, viewerRel.ExcludedIntersectionGroups, 1, "should have one excluded intersection group")
	require.ElementsMatch(t, viewerRel.ExcludedIntersectionGroups[0].Relations, []string{"editor", "owner"})
}
