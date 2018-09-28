package mapping

import (
	"github.com/omniscale/imposm3/element"
	"github.com/omniscale/imposm3/geom"
)

func (m *Mapping) pointMatcher() (NodeMatcher, error) {
	mappings := make(TagTableMapping)
	m.mappings(PointTable, mappings)
	filters := make(tableElementFilters)
	m.addFilters(filters)
	tables, err := m.tables(PointTable)
	return &tagMatcher{
		mappings:   mappings,
		filters:    filters,
		tables:     tables,
	}, err
}

func (m *Mapping) lineStringMatcher() (WayMatcher, error) {
	mappings := make(TagTableMapping)
	m.mappings(LineStringTable, mappings)
	filters := make(tableElementFilters)
	m.addFilters(filters)
	areaTags, linearTags := m.getAreaTags()
	tables, err := m.tables(LineStringTable)
	return &tagMatcher{
		mappings:   mappings,
		filters:    filters,
		tables:     tables,
		areaTags:   areaTags,
		linearTags: linearTags,
		matchLines: true,
		matchAreas: false,
	}, err
}

func (m *Mapping) polygonMatcher() (RelWayMatcher, error) {
	mappings := make(TagTableMapping)
	m.mappings(PolygonTable, mappings)
	filters := make(tableElementFilters)
	m.addFilters(filters)
	areaTags, linearTags := m.getAreaTags()
	relFilters := make(tableElementFilters)
	m.addRelationFilters(PolygonTable, relFilters)
	tables, err := m.tables(PolygonTable)
	return &tagMatcher{
		mappings:   mappings,
		filters:    filters,
		tables:     tables,
		relFilters: relFilters,
		areaTags:   areaTags,
		linearTags: linearTags,
		matchLines: false,
		matchAreas: true,
	}, err
}

func (m *Mapping) relationMatcher() (RelationMatcher, error) {
	mappings := make(TagTableMapping)
	m.mappings(RelationTable, mappings)
	filters := make(tableElementFilters)
	m.addFilters(filters)
	relFilters := make(tableElementFilters)
	m.addRelationFilters(RelationTable, relFilters)
	tables, err := m.tables(RelationTable)
	return &tagMatcher{
		mappings:   mappings,
		filters:    filters,
		tables:     tables,
		relFilters: relFilters,
	}, err
}

func (m *Mapping) relationMemberMatcher() (RelationMatcher, error) {
	mappings := make(TagTableMapping)
	m.mappings(RelationMemberTable, mappings)
	filters := make(tableElementFilters)
	m.addFilters(filters)
	relFilters := make(tableElementFilters)
	m.addRelationFilters(RelationMemberTable, relFilters)
	tables, err := m.tables(RelationMemberTable)
	return &tagMatcher{
		mappings:   mappings,
		filters:    filters,
		tables:     tables,
		relFilters: relFilters,
	}, err
}

type NodeMatcher interface {
	MatchNode(node *element.Node) []Match
}

type WayMatcher interface {
	MatchWay(way *element.Way) []Match
}

type RelationMatcher interface {
	MatchRelation(rel *element.Relation) []Match
}

type RelWayMatcher interface {
	WayMatcher
	RelationMatcher
}

type Match struct {
	Key     string
	Value   string
	Table   DestTable
	builder *rowBuilder
}

func (m *Match) Row(elem *element.OSMElem, geom *geom.Geometry) []interface{} {
	return m.builder.MakeRow(elem, geom, *m)
}

func (m *Match) MemberRow(rel *element.Relation, member *element.Member, geom *geom.Geometry) []interface{} {
	return m.builder.MakeMemberRow(rel, member, geom, *m)
}

type tagMatcher struct {
	mappings   TagTableMapping
	tables     map[string]*rowBuilder
	filters    tableElementFilters
	relFilters tableElementFilters
	areaTags   map[Key]struct{}
	linearTags map[Key]struct{}
	matchLines bool
	matchAreas bool
}

func (tm *tagMatcher) MatchNode(node *element.Node) []Match {
	return tm.match(node.Tags, false, false)
}

func (tm *tagMatcher) MatchWay(way *element.Way) []Match {
	return tm.match(way.Tags, way.IsClosed(), false)
}

func (tm *tagMatcher) MatchRelation(rel *element.Relation) []Match {
	return tm.match(rel.Tags, true, true)
}

type orderedMatch struct {
	Match
	order int
}

func (tm *tagMatcher) match(tags element.Tags, closed bool, relation bool) []Match {
	tables := make(map[DestTable]orderedMatch)

	addTables := func(k, v string, tbls []orderedDestTable) {
		for _, t := range tbls {
			this := orderedMatch{
				Match: Match{
					Key:     k,
					Value:   v,
					Table:   t.DestTable,
					builder: tm.tables[t.Name],
				},
				order: t.order,
			}
			if other, ok := tables[t.DestTable]; ok {
				if other.order < this.order {
					this = other
				}
			}
			tables[t.DestTable] = this
		}
	}

	if values, ok := tm.mappings[Key("__any__")]; ok {
		addTables("__any__", "__any__", values["__any__"])
	}

	for k, v := range tags {
		values, ok := tm.mappings[Key(k)]
		if ok {
			if tbls, ok := values["__any__"]; ok {
				addTables(k, v, tbls)
			}
			if tbls, ok := values[Value(v)]; ok {
				addTables(k, v, tbls)
			}
		}
	}
	var matches []Match
	for t, match := range tables {
		filters, ok := tm.filters[t.Name]
		filteredOut := false
		if !relation && (tm.matchLines || tm.matchAreas) {
			if !closed {
				// open way is always a linestring
				filteredOut = tm.matchLines == false
			} else {
				// allow to include closed ways as linestrings if explicitly marked
				// default -> area
				// area=no or linear tags -> line
				// area=yes or area tags -> area
				filteredOut = tm.matchAreas && tags["area"] == "no" || !tm.matchAreas && tags["area"] != "no"
				for k, _ := range tm.linearTags {
					if _, ok := tags[string(k)]; ok {
						filteredOut = tm.matchAreas
						break
					}
				}
				// but area=yes or area tag means it shouldn't be a linestring
				if tags["area"] == "yes" {
					filteredOut = tm.matchAreas
				}
				for k, _ := range tm.areaTags {
					if _, ok := tags[string(k)]; ok {
						filteredOut = !tm.matchAreas
						break
					}
				}
			}
		}
		if ok && !filteredOut {
			for _, filter := range filters {
				if !filter(tags, Key(match.Key), closed) {
					filteredOut = true
					break
				}
			}
		}
		if relation && !filteredOut {
			filters, ok := tm.relFilters[t.Name]
			if ok {
				for _, filter := range filters {
					if !filter(tags, Key(match.Key), closed) {
						filteredOut = true
						break
					}
				}
			}
		}

		if !filteredOut {
			matches = append(matches, match.Match)
		}
	}
	return matches
}

type valueBuilder struct {
	key     Key
	colType ColumnType
}

func (v *valueBuilder) Value(elem *element.OSMElem, geom *geom.Geometry, match Match) interface{} {
	if v.colType.Func != nil {
		return v.colType.Func(elem.Tags[string(v.key)], elem, geom, match)
	}
	return nil
}

func (v *valueBuilder) MemberValue(rel *element.Relation, member *element.Member, geom *geom.Geometry, match Match) interface{} {
	if v.colType.Func != nil {
		if v.colType.FromMember {
			if member.Elem == nil {
				return nil
			}
			return v.colType.Func(member.Elem.Tags[string(v.key)], member.Elem, geom, match)
		}
		return v.colType.Func(rel.Tags[string(v.key)], &rel.OSMElem, geom, match)
	}
	if v.colType.MemberFunc != nil {
		return v.colType.MemberFunc(rel, member, match)
	}
	return nil
}

type rowBuilder struct {
	columns []valueBuilder
}

func (r *rowBuilder) MakeRow(elem *element.OSMElem, geom *geom.Geometry, match Match) []interface{} {
	var row []interface{}
	for _, column := range r.columns {
		row = append(row, column.Value(elem, geom, match))
	}
	return row
}

func (r *rowBuilder) MakeMemberRow(rel *element.Relation, member *element.Member, geom *geom.Geometry, match Match) []interface{} {
	var row []interface{}
	for _, column := range r.columns {
		row = append(row, column.MemberValue(rel, member, geom, match))
	}
	return row
}
