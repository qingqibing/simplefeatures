package geom

import (
	"bytes"
	"database/sql/driver"
	"errors"
	"io"
	"math"
	"sort"
	"unsafe"
)

// Polygon is a planar surface, defined by 1 exiterior boundary and 0 or more
// interior boundaries. Each interior boundary defines a hole in the polygon.
//
// Its assertions are:
//
// 1. The outer ring and holes must be valid linear rings (i.e. be simple and
//    closed LineStrings).
//
// 2. Each pair of rings must only intersect at a single point.
//
// 3. The interior of the polygon is connected.
//
// 4. The holes must be fully inside the outer ring.
//
type Polygon struct {
	outer LineString
	holes []LineString
}

// NewPolygon creates a polygon given its outer and inner rings. No rings may
// cross each other, and can only intersect each with each other at a point.
func NewPolygon(outer LineString, holes []LineString, opts ...ConstructorOption) (Polygon, error) {
	if !doCheapValidations(opts) && !doExpensiveValidations(opts) {
		return Polygon{outer, holes}, nil
	}

	// Overview:
	//
	// 1. Create slice of all rings, ordered by min X coordinate.
	// 2. Loop over each ring.
	//    a. Remove any ring from the heap that has max X coordinate less than
	//       the min X of the current ring.
	//    b. Check to see if the current ring intersects with any in the heap.
	//    c. Insert the current ring into the heap.

	rings := seq(1 + len(holes))
	type interval struct {
		minX, maxX float64
	}
	intervals := make([]struct {
		minX, maxX float64
	}, len(rings))
	ring := func(i int) LineString {
		if i == len(holes) {
			return outer
		}
		return holes[i]
	}
	for i := range intervals {
		env, ok := ring(i).Envelope()
		if !ok {
			return Polygon{}, errors.New("polygon rings must not be empty")
		}
		intervals[i].minX = env.Min().X
		intervals[i].maxX = env.Max().X
	}

	for _, i := range rings {
		r := ring(i)
		if doCheapValidations(opts) && !r.IsClosed() {
			return Polygon{}, errors.New("polygon rings must be closed")
		}
		if doExpensiveValidations(opts) && !r.IsSimple() {
			return Polygon{}, errors.New("polygon rings must be simple")
		}
	}

	if !doExpensiveValidations(opts) {
		return Polygon{outer, holes}, nil
	}

	sort.Slice(rings, func(i, j int) bool {
		return intervals[i].minX < intervals[j].minX
	})

	nextInterVert := len(rings)
	interVerts := make(map[XY]int)
	graph := newGraph()

	h := intHeap{less: func(i, j int) bool {
		return intervals[i].maxX < intervals[j].maxX
	}}

	for _, current := range rings {
		currentRing := ring(current)
		currentX := intervals[current].minX
		for len(h.data) > 0 && intervals[h.data[0]].maxX < currentX {
			h.pop()
		}
		for _, other := range h.data {
			otherRing := ring(other)
			if current < len(holes) && other < len(holes) {
				// Check is skipped if the outer ring is involved.
				nestedFwd := pointRingSide(
					currentRing.StartPoint().XY(),
					otherRing,
				) == interior
				nestedRev := pointRingSide(
					otherRing.StartPoint().XY(),
					currentRing,
				) == interior
				if nestedFwd || nestedRev {
					return Polygon{}, errors.New("polygon must not have nested rings")
				}
			}

			intersects, ext := hasIntersectionMultiLineStringWithMultiLineString(
				currentRing.AsMultiLineString(),
				otherRing.AsMultiLineString(),
				true,
			)
			if !intersects {
				continue
			}
			if ext.multiplePoints {
				return Polygon{}, errors.New("polygon rings must not intersect at multiple points")
			}

			interVert, ok := interVerts[ext.singlePoint]
			if !ok {
				interVert = nextInterVert
				nextInterVert++
				interVerts[ext.singlePoint] = interVert
			}
			graph.addEdge(interVert, current)
			graph.addEdge(interVert, other)
		}
		h.push(current)
	}

	// All inner rings must be inside the outer ring.
	for _, hole := range holes {
		for i := 0; i < hole.NumPoints(); i++ {
			pt := hole.PointN(i)
			if pointRingSide(pt.XY(), outer) == exterior {
				return Polygon{}, errors.New("hole must be inside outer ring")
			}
		}
	}

	// Connectedness check: a graph is created where the intersections and
	// rings are modelled as vertices. Edges are added to the graph between an
	// intersection vertex and a ring vertex if the ring participates in that
	// intersection. The interior of the polygon is connected iff the graph
	// does not contain a cycle.
	if graph.hasCycle() {
		return Polygon{}, errors.New("polygon interiors must be connected")
	}

	return Polygon{outer: outer, holes: holes}, nil
}

// NewPolygonC creates a new polygon from its Coordinates. The first dimension
// of the Coordinates slice is the ring number, and the second dimension of the
// Coordinates slice is the position within the ring.
func NewPolygonC(coords [][]Coordinates, opts ...ConstructorOption) (Polygon, error) {
	if len(coords) == 0 {
		return Polygon{}, errors.New("Polygon must have an outer ring")
	}
	rings := make([]LineString, len(coords))
	for i := range rings {
		var err error
		rings[i], err = NewLineStringC(coords[i], opts...)
		if err != nil {
			return Polygon{}, err
		}
	}
	return NewPolygon(rings[0], rings[1:], opts...)
}

// NewPolygonXY creates a new polygon from its XYs. The first dimension of the
// XYs slice is the ring number, and the second dimension of the Coordinates
// slice is the position within the ring.
func NewPolygonXY(pts [][]XY, opts ...ConstructorOption) (Polygon, error) {
	return NewPolygonC(twoDimXYToCoords(pts), opts...)
}

// AsGeometry converts this Polygon into a Geometry.
func (p Polygon) AsGeometry() Geometry {
	return Geometry{polygonTag, unsafe.Pointer(&p)}
}

// ExteriorRing gives the exterior ring of the polygon boundary.
func (p Polygon) ExteriorRing() LineString {
	return p.outer
}

// NumInteriorRings gives the number of interior rings in the polygon boundary.
func (p Polygon) NumInteriorRings() int {
	return len(p.holes)
}

// InteriorRingN gives the nth (zero indexed) interior ring in the polygon
// boundary. It will panic if n is out of bounds with respect to the number of
// interior rings.
func (p Polygon) InteriorRingN(n int) LineString {
	return p.holes[n]
}

func (p Polygon) AsText() string {
	return string(p.AppendWKT(nil))
}

func (p Polygon) AppendWKT(dst []byte) []byte {
	dst = append(dst, []byte("POLYGON")...)
	return p.appendWKTBody(dst)
}

func (p Polygon) appendWKTBody(dst []byte) []byte {
	dst = append(dst, '(')
	dst = p.outer.appendWKTBody(dst)
	for _, h := range p.holes {
		dst = append(dst, ',')
		dst = h.appendWKTBody(dst)
	}
	return append(dst, ')')
}

// IsSimple always returns true. All Polygons are simple.
func (p Polygon) IsSimple() bool {
	return true
}

func (p Polygon) Intersection(g Geometry) (Geometry, error) {
	return intersection(p.AsGeometry(), g)
}

func (p Polygon) Intersects(g Geometry) bool {
	return hasIntersection(p.AsGeometry(), g)
}

func (p Polygon) IsEmpty() bool {
	return false
}

func (p Polygon) Equals(other Geometry) (bool, error) {
	return equals(p.AsGeometry(), other)
}

func (p Polygon) Envelope() (Envelope, bool) {
	return p.outer.Envelope()
}

func (p Polygon) rings() []LineString {
	dst := make([]LineString, 0, 1+len(p.holes))
	return appendRings(dst, p)
}

func appendRings(dst []LineString, p Polygon) []LineString {
	dst = append(dst, p.outer)
	dst = append(dst, p.holes...)
	return dst
}

func (p Polygon) Boundary() MultiLineString {
	if len(p.holes) == 0 {
		return p.outer.AsMultiLineString()
	}
	bounds := make([]LineString, 1+len(p.holes))
	bounds[0] = p.outer
	for i, h := range p.holes {
		bounds[1+i] = h
	}
	return NewMultiLineString(bounds)
}

func (p Polygon) Value() (driver.Value, error) {
	var buf bytes.Buffer
	err := p.AsBinary(&buf)
	return buf.Bytes(), err
}

func (p Polygon) AsBinary(w io.Writer) error {
	marsh := newWKBMarshaller(w)
	marsh.writeByteOrder()
	marsh.writeGeomType(wkbGeomTypePolygon)
	rings := p.rings()
	marsh.writeCount(len(rings))
	for _, ring := range rings {
		numPts := ring.NumPoints()
		marsh.writeCount(numPts)
		for i := 0; i < numPts; i++ {
			pt := ring.PointN(i)
			marsh.writeFloat64(pt.XY().X)
			marsh.writeFloat64(pt.XY().Y)
		}
	}
	return marsh.err
}

// ConvexHull returns the convex hull of the Polygon, which is always another
// Polygon.
func (p Polygon) ConvexHull() Geometry {
	return convexHull(p.AsGeometry())
}

func (p Polygon) MarshalJSON() ([]byte, error) {
	return marshalGeoJSON("Polygon", p.Coordinates())
}

// Coordinates returns the coordinates of the rings making up the Polygon
// (external ring first, then internal rings after).
func (p Polygon) Coordinates() [][]Coordinates {
	rings := p.rings()
	coords := make([][]Coordinates, len(rings))
	for i, r := range rings {
		n := r.NumPoints()
		coords[i] = make([]Coordinates, n)
		for j := 0; j < n; j++ {
			coords[i][j] = r.PointN(j).Coordinates()
		}
	}
	return coords
}

// TransformXY transforms this Polygon into another Polygon according to fn.
func (p Polygon) TransformXY(fn func(XY) XY, opts ...ConstructorOption) (Geometry, error) {
	coords := p.Coordinates()
	transform2dCoords(coords, fn)
	poly, err := NewPolygonC(coords, opts...)
	return poly.AsGeometry(), err
}

// EqualsExact checks if this Polygon is exactly equal to another Polygon.
func (p Polygon) EqualsExact(other Geometry, opts ...EqualsExactOption) bool {
	return other.IsPolygon() &&
		polygonExactEqual(p, other.AsPolygon(), opts)
}

// IsValid checks if this Polygon is valid
func (p Polygon) IsValid() bool {
	_, err := NewPolygonC(p.Coordinates())
	return err == nil
}

// Area of a Polygon is the outer ring's area minus the areas of all inner rings.
func (p Polygon) Area() float64 {
	area := math.Abs(signedAreaOfLinearRing(p.ExteriorRing()))
	n := p.NumInteriorRings()
	for i := 0; i < n; i++ {
		area -= math.Abs(signedAreaOfLinearRing(p.InteriorRingN(i)))
	}
	return area
}

// SignedArea gives the positive area of the polygon when the outer rings are
// wound CCW and any inner rings are wound CW, and the negative area of the
// polygon when the outer rings are wound CW and any inner rings are wound CCW.
// If the windings of the inner and outer rings are the same, then the area
// will be inconsistent.
func (p Polygon) SignedArea() float64 {
	signedArea := signedAreaOfLinearRing(p.ExteriorRing())
	n := p.NumInteriorRings()
	for i := 0; i < n; i++ {
		signedArea += signedAreaOfLinearRing(p.InteriorRingN(i))
	}
	return signedArea
}

func signedAreaOfLinearRing(lr LineString) float64 {
	// This is the "Shoelace Formula".
	var sum float64
	n := lr.NumPoints()
	for i := 0; i < n; i++ {
		pt0 := lr.PointN(i).XY()
		pt1 := lr.PointN((i + 1) % n).XY()
		sum += (pt1.X + pt0.X) * (pt1.Y - pt0.Y)
	}
	return sum / 2
}

// Centroid returns the polygon's centroid point.
// Returns true iff the polygon has a non-zero area, and thus the centroid is well defined.
func (p Polygon) Centroid() (Point, bool) {
	sumX, sumY, sumArea := centroidAndAreaOfPolygon(p)
	if sumArea == 0 {
		return Point{}, false
	}
	return NewPointF(sumX/sumArea, sumY/sumArea), true
}

func centroidAndAreaOfPolygon(p Polygon) (sumX float64, sumY float64, sumArea float64) {
	n := p.NumInteriorRings()
	areas := make([]float64, n+1)
	centroids := make([]XY, n+1)
	centroids[0], areas[0] = centroidAndAreaOfLinearRing(p.ExteriorRing())
	totalArea := areas[0]
	for i := 0; i < n; i++ {
		r := p.InteriorRingN(i)
		centroids[i+1], areas[i+1] = centroidAndAreaOfLinearRing(r)
		totalArea -= areas[i+1]
	}

	// Calculate weighted average (negative weights for holes).
	var avg XY
	for i, c := range centroids {
		c = c.Scale(areas[i])
		if i != 0 {
			c = c.Scale(-1)
		}
		avg = avg.Add(c)
	}
	return avg.X, avg.Y, totalArea
}

func centroidAndAreaOfLinearRing(lr LineString) (XY, float64) {
	n := lr.NumPoints()
	var x, y float64
	var area float64
	for i := 0; i < n; i++ {
		pt0 := lr.PointN(i).XY()
		pt1 := lr.PointN((i + 1) % n).XY()
		x += (pt0.X + pt1.X) * (pt0.X*pt1.Y - pt1.X*pt0.Y)
		y += (pt0.Y + pt1.Y) * (pt0.X*pt1.Y - pt1.X*pt0.Y)
		area += pt0.X*pt1.Y - pt1.X*pt0.Y
	}
	area /= 2
	return XY{x / 6 / area, y / 6 / area}, math.Abs(area)
}

// AsMultiPolygon is a helper that converts this Polygon into a MultiPolygon.
func (p Polygon) AsMultiPolygon() MultiPolygon {
	mp, err := NewMultiPolygon([]Polygon{p})
	if err != nil {
		// Cannot occur due to construction. A valid polygon will always be a
		// valid multipolygon.
		panic(err)
	}
	return mp
}

// Reverse in the case of Polygon outputs the coordinates of each ring in reverse order,
// but note the order of the inner rings is unchanged.
func (p Polygon) Reverse() Polygon {
	outer := p.outer.Reverse()
	holes := make([]LineString, len(p.holes))
	// Form the reversed slice.
	for i := 0; i < len(p.holes); i++ {
		holes[i] = p.holes[i].Reverse()
	}
	p2, err := NewPolygon(outer, holes)
	if err != nil {
		panic("Reverse of an existing Polygon should not fail")
	}
	return p2
}
