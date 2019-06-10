package simplefeatures

type Geometry interface {
	// AsText returns the WKT representation of the geometry.
	AsText() []byte

	// AppendWKT appends the WKT representation of the geometry to dst and
	// returns the resultant slice.
	AppendWKT(dst []byte) []byte

	// IsSimple returns true iff the geometry doesn't contain any anomalous
	// geometry points such as self intersection or self tangency. The precise
	// condition will differ for each type of geometry.
	IsSimple() bool
}