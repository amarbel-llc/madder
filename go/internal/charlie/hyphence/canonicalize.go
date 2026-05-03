package hyphence

import "sort"

// canonicalOrder maps each metadata prefix to its sort rank per RFC
// 0001 §Canonical Line Order. Lower rank emits first. Comments ('%')
// don't appear here because Document captures them as anchored
// LeadingComments / TrailingComments rather than as standalone
// MetadataLine entries — Canonicalize only sorts non-comment lines.
//
// Locked vs aliased vs bare object reference distinction is not
// modeled today (see #128); all '<' lines share rank 1 and rely on
// stable sort to preserve source order.
var canonicalOrder = map[byte]int{
	'#': 0, // description
	'<': 1, // object reference
	'-': 2, // tag / reference
	'@': 3, // blob reference
	'!': 4, // type
}

// Canonicalize sorts doc.Metadata in place per RFC 0001 §Canonical
// Line Order. Stable sort within each prefix bucket preserves
// insertion order. Each MetadataLine carries its LeadingComments
// across the sort. TrailingComments remain at the document tail.
func Canonicalize(doc *Document) {
	sort.SliceStable(doc.Metadata, func(i, j int) bool {
		return canonicalOrder[doc.Metadata[i].Prefix] < canonicalOrder[doc.Metadata[j].Prefix]
	})
}
