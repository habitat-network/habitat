package main

// Membership expansion. Group membership is expressed as relationship tuples
// granting a role on a group-space. A tuple's subject is either an individual
// user or another group (a space-role userset), the latter giving cross-group
// inheritance: "writers of group B are writers of group A". This file resolves
// those tuples — indexed from sap — into concrete member sets, following the
// built-in role implication owner > manager > writer > reader.

type role int

const (
	roleNone role = iota
	roleReader
	roleWriter
	roleManager
	roleOwner
)

func parseRole(s string) role {
	switch s {
	case "owner":
		return roleOwner
	case "manager":
		return roleManager
	case "writer":
		return roleWriter
	case "reader":
		return roleReader
	default:
		return roleNone
	}
}

// memberMinRole is the lowest role that counts as group membership: per the
// group profile lexicon, at least the writer role implies membership.
const memberMinRole = roleWriter

// manageMinRole is the lowest role that allows managing a group (editing it and
// adding members).
const manageMinRole = roleManager

// member is one resolved holder of a role on a group.
type member struct {
	DID      string
	Role     string
	Direct   bool   // granted directly on this group vs. inherited from another
	ViaGroup string // if inherited, the group-space the membership came from
}

// graph indexes tuples by the group-space they grant a role on, so holders of a
// role can be expanded transitively through inherited groups.
type graph struct {
	byObject map[string][]tupleRow
}

func newGraph(tuples []tupleRow) *graph {
	byObject := make(map[string][]tupleRow)
	for _, t := range tuples {
		byObject[t.ObjectSpace] = append(byObject[t.ObjectSpace], t)
	}
	return &graph{byObject: byObject}
}

// holders returns every user holding at least minRole on space, expanding
// inherited groups. Results are deduplicated by DID, preferring a direct grant
// over an inherited one.
func (g *graph) holders(space string, minRole role) []member {
	out := map[string]member{}
	g.collect(space, minRole, out, map[string]bool{})
	members := make([]member, 0, len(out))
	for _, m := range out {
		members = append(members, m)
	}
	return members
}

func (g *graph) collect(
	space string,
	minRole role,
	out map[string]member,
	visited map[string]bool,
) {
	if visited[space] {
		return
	}
	visited[space] = true
	defer delete(visited, space)

	for _, t := range g.byObject[space] {
		granted := parseRole(t.Relation)
		if granted < minRole {
			continue
		}
		switch t.SubjectKind {
		case "user":
			add(out, member{DID: t.SubjectDID, Role: t.Relation, Direct: true})
		case "group":
			// Holders of SubjectRole on the subject group receive this
			// grant, so they become members of `space` too.
			sub := map[string]member{}
			g.collect(t.SubjectGroup, parseRole(t.SubjectRole), sub, visited)
			for _, m := range sub {
				add(out, member{
					DID:      m.DID,
					Role:     t.Relation,
					Direct:   false,
					ViaGroup: t.SubjectGroup,
				})
			}
		}
	}
}

// add inserts m unless a direct grant for the same DID is already present.
func add(out map[string]member, m member) {
	existing, ok := out[m.DID]
	if ok && existing.Direct && !m.Direct {
		return
	}
	out[m.DID] = m
}

// isMember reports whether did is a member of space (holds at least writer).
func (g *graph) isMember(space, did string) bool {
	for _, m := range g.holders(space, memberMinRole) {
		if m.DID == did {
			return true
		}
	}
	return false
}

// canManage reports whether did can manage space (holds at least manager).
func (g *graph) canManage(space, did string) bool {
	for _, m := range g.holders(space, manageMinRole) {
		if m.DID == did {
			return true
		}
	}
	return false
}

// inheritedGroups returns the group-space URIs whose members `space` inherits.
func (g *graph) inheritedGroups(space string) []string {
	var groups []string
	seen := map[string]bool{}
	for _, t := range g.byObject[space] {
		if t.SubjectKind == "group" && !seen[t.SubjectGroup] {
			seen[t.SubjectGroup] = true
			groups = append(groups, t.SubjectGroup)
		}
	}
	return groups
}
