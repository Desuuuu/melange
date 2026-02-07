package sqlgen

import (
	"fmt"
	"strings"
)

func generateCheckFunction(a RelationAnalysis, inline InlineSQLData, noWildcard bool) (string, error) {
	plan := BuildCheckPlan(a, inline, noWildcard)
	blocks, err := BuildCheckBlocks(plan)
	if err != nil {
		return "", fmt.Errorf("building check blocks for %s.%s: %w", a.ObjectType, a.Relation, err)
	}
	return RenderCheckFunction(plan, blocks)
}

func generateDispatcher(analyses []RelationAnalysis, noWildcard bool) (string, error) {
	fnName := "check_permission"
	if noWildcard {
		fnName = "check_permission_no_wildcard"
	}

	cases := buildDispatcherCases(analyses, noWildcard)
	if len(cases) == 0 {
		return renderEmptyDispatcher(fnName), nil
	}
	return renderDispatcherWithCases(fnName, cases), nil
}

func buildDispatcherCases(analyses []RelationAnalysis, noWildcard bool) []DispatcherCase {
	var cases []DispatcherCase
	for _, a := range analyses {
		if !a.Capabilities.CheckAllowed {
			continue
		}
		cases = append(cases, DispatcherCase{
			ObjectType:        a.ObjectType,
			Relation:          a.Relation,
			CheckFunctionName: functionNameForDispatcher(a, noWildcard),
		})
	}
	return cases
}

func renderDispatcherWithCases(fnName string, cases []DispatcherCase) string {
	caseExpr := buildDispatcherCaseExpr(cases)

	internalFn := PlpgsqlFunction{
		Name:    fnName + "_internal",
		Args:    dispatcherInternalArgs(),
		Returns: "INTEGER",
		Body: []Stmt{
			Comment{Text: "Depth limit check: prevent excessively deep permission resolution chains"},
			Comment{Text: "This catches both recursive TTU patterns and long userset chains"},
			If{
				Cond: Gte{Left: ArrayLength{Array: Visited}, Right: Int(25)},
				Then: []Stmt{Raise{Message: "resolution too complex", ErrCode: "M2002"}},
			},
			ReturnValue{Value: Raw("(SELECT " + caseExpr.SQL() + ")")},
		},
		Header: []string{
			"Generated internal dispatcher for " + fnName + "_internal",
			"Routes to specialized functions with p_visited for cycle detection in TTU patterns",
			"Enforces depth limit of 25 to prevent stack overflow from deep permission chains",
			"Phase 5: All relations use specialized functions - no generic fallback",
		},
	}

	publicFn := SqlFunction{
		Name:    fnName,
		Args:    dispatcherPublicArgs(),
		Returns: "INTEGER",
		Body:    Raw("SELECT " + fnName + "_internal(p_subject_type, p_subject_id, p_relation, p_object_type, p_object_id, ARRAY[]::TEXT[])"),
		Header: []string{
			"Generated dispatcher for " + fnName,
			"Routes to specialized functions for all known type/relation pairs",
		},
	}

	return internalFn.SQL() + "\n\n" + publicFn.SQL() + "\n"
}

func renderEmptyDispatcher(fnName string) string {
	internalFn := SqlFunction{
		Name:    fnName + "_internal",
		Args:    dispatcherInternalArgs(),
		Returns: "INTEGER",
		Body:    Raw("SELECT 0"),
		Header: []string{
			"Generated dispatcher for " + fnName + " (no relations defined)",
			"Returns 0 (deny) for all requests",
		},
	}

	publicFn := SqlFunction{
		Name:    fnName,
		Args:    dispatcherPublicArgs(),
		Returns: "INTEGER",
		Body:    Raw("SELECT 0"),
	}

	return internalFn.SQL() + "\n\n" + publicFn.SQL() + "\n"
}

func generateBulkDispatcher(analyses []RelationAnalysis) string {
	cases := buildDispatcherCases(analyses, false)
	if len(cases) == 0 {
		return renderEmptyBulkDispatcher()
	}
	return renderBulkDispatcherWithCases(cases)
}

func renderEmptyBulkDispatcher() string {
	fn := SqlFunction{
		Name:    "check_permission_bulk",
		Args:    bulkDispatcherArgs(),
		Returns: "TABLE(idx INTEGER, allowed INTEGER)",
		Body:    Raw("SELECT NULL::INTEGER, NULL::INTEGER WHERE false"),
		Header: []string{
			"Generated bulk dispatcher for check_permission_bulk (no relations defined)",
			"Returns 0 (deny) for all requests",
		},
	}
	return fn.SQL() + "\n"
}

func renderBulkDispatcherWithCases(cases []DispatcherCase) string {
	fn := SqlFunction{
		Name:    "check_permission_bulk",
		Args:    bulkDispatcherArgs(),
		Returns: "TABLE(idx INTEGER, allowed INTEGER)",
		Body:    Raw(buildBulkDispatcherBody(cases)),
		Header: []string{
			"Generated bulk dispatcher for check_permission_bulk",
			fmt.Sprintf("Routes %d (object_type, relation) pairs to specialized check functions", len(cases)),
		},
	}
	return fn.SQL() + "\n"
}

// buildBulkDispatcherBody constructs the CTE + UNION ALL query body for the bulk dispatcher.
func buildBulkDispatcherBody(cases []DispatcherCase) string {
	var sb strings.Builder

	sb.WriteString("WITH requests AS MATERIALIZED (\n")
	sb.WriteString("    SELECT t.* FROM UNNEST(p_subject_types, p_subject_ids, p_relations, p_object_types, p_object_ids)\n")
	sb.WriteString("        WITH ORDINALITY AS t(subject_type, subject_id, relation, object_type, object_id, idx)\n")
	sb.WriteString(")\n")

	// One UNION ALL branch per (object_type, relation) pair, plus a fallback for unknown pairs.
	notInPairs := make([]string, 0, len(cases))
	for i, c := range cases {
		if i > 0 {
			sb.WriteString("UNION ALL\n")
		}
		objLit := Lit(c.ObjectType).SQL()
		relLit := Lit(c.Relation).SQL()
		fmt.Fprintf(&sb, "SELECT r.idx::INTEGER, %s(r.subject_type, r.subject_id, r.object_id, ARRAY[]::TEXT[])\n",
			c.CheckFunctionName)
		fmt.Fprintf(&sb, "FROM requests r WHERE r.object_type = %s AND r.relation = %s\n", objLit, relLit)
		notInPairs = append(notInPairs, fmt.Sprintf("(%s,%s)", objLit, relLit))
	}

	sb.WriteString("UNION ALL\n")
	sb.WriteString("SELECT r.idx::INTEGER, 0\n")
	fmt.Fprintf(&sb, "FROM requests r WHERE (r.object_type, r.relation) NOT IN (%s)",
		strings.Join(notInPairs, ", "))

	return sb.String()
}

func functionNameForDispatcher(a RelationAnalysis, noWildcard bool) string {
	if noWildcard {
		return functionNameNoWildcard(a.ObjectType, a.Relation)
	}
	return functionName(a.ObjectType, a.Relation)
}

func buildDispatcherCaseExpr(cases []DispatcherCase) CaseExpr {
	whens := make([]CaseWhen, 0, len(cases))
	for _, c := range cases {
		cond := AndExpr{Exprs: []Expr{
			Eq{Left: ObjectType, Right: Lit(c.ObjectType)},
			Eq{Left: Raw("p_relation"), Right: Lit(c.Relation)},
		}}
		result := Func{
			Name: c.CheckFunctionName,
			Args: []Expr{SubjectType, SubjectID, ObjectID, Visited},
		}
		whens = append(whens, CaseWhen{Cond: cond, Result: result})
	}
	return CaseExpr{Whens: whens, Else: Int(0)}
}

func bulkDispatcherArgs() []FuncArg {
	return []FuncArg{
		{Name: "p_subject_types", Type: "TEXT[]"},
		{Name: "p_subject_ids", Type: "TEXT[]"},
		{Name: "p_relations", Type: "TEXT[]"},
		{Name: "p_object_types", Type: "TEXT[]"},
		{Name: "p_object_ids", Type: "TEXT[]"},
	}
}

func dispatcherPublicArgs() []FuncArg {
	return []FuncArg{
		{Name: "p_subject_type", Type: "TEXT"},
		{Name: "p_subject_id", Type: "TEXT"},
		{Name: "p_relation", Type: "TEXT"},
		{Name: "p_object_type", Type: "TEXT"},
		{Name: "p_object_id", Type: "TEXT"},
	}
}

func dispatcherInternalArgs() []FuncArg {
	return []FuncArg{
		{Name: "p_subject_type", Type: "TEXT"},
		{Name: "p_subject_id", Type: "TEXT"},
		{Name: "p_relation", Type: "TEXT"},
		{Name: "p_object_type", Type: "TEXT"},
		{Name: "p_object_id", Type: "TEXT"},
		{Name: "p_visited", Type: "TEXT []", Default: EmptyArray{}},
	}
}
