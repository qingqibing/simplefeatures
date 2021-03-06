package main

import (
	"database/sql"
	"testing"

	"github.com/peterstace/simplefeatures/geom"
)

type PostGIS struct {
	db *sql.DB
}

func (p PostGIS) WKTIsValidWithReason(wkt string) (bool, string) {
	var isValid bool
	var reason string
	err := p.db.QueryRow(`
		SELECT
			ST_IsValid(ST_GeomFromText($1)),
			ST_IsValidReason(ST_GeomFromText($1))`,
		wkt,
	).Scan(&isValid, &reason)
	if err != nil {
		// It's not possible to distinguish between problems with the geometry
		// and problems with the database except by string-matching. It's
		// better to just report all errors, even if this means there will be
		// some false errors in the case of connectivity problems (or similar).
		return false, err.Error()
	}
	return isValid, reason
}

func (p PostGIS) WKBIsValidWithReason(t *testing.T, wkb string) (bool, string) {
	var isValid bool
	err := p.db.QueryRow(`SELECT ST_IsValid(ST_GeomFromWKB(decode($1, 'hex')))`, wkb).Scan(&isValid)
	if err != nil {
		return false, err.Error()
	}
	var reason string
	err = p.db.QueryRow(`SELECT ST_IsValidReason(ST_GeomFromWKB(decode($1, 'hex')))`, wkb).Scan(&reason)
	if err != nil {
		return false, err.Error()
	}
	return isValid, reason
}

func (p PostGIS) GeoJSONIsValidWithReason(t *testing.T, geojson string) (bool, string) {
	var isValid bool
	err := p.db.QueryRow(`SELECT ST_IsValid(ST_GeomFromGeoJSON($1))`, geojson).Scan(&isValid)
	if err != nil {
		return false, err.Error()
	}

	var reason string
	err = p.db.QueryRow(`SELECT ST_IsValidReason(ST_GeomFromGeoJSON($1))`, geojson).Scan(&reason)
	if err != nil {
		return false, err.Error()
	}
	return isValid, reason
}

func (p PostGIS) geomFunc(t *testing.T, g geom.Geometry, stFunc string) geom.Geometry {
	var result geom.Geometry
	if err := p.db.QueryRow(
		"SELECT ST_AsBinary("+stFunc+"(ST_GeomFromWKB($1)))", g,
	).Scan(&result); err != nil {
		t.Fatalf("pg error: %v", err)
	}
	return result
}

func (p PostGIS) boolFunc(t *testing.T, g geom.Geometry, stFunc string) bool {
	var b bool
	if err := p.db.QueryRow(
		"SELECT "+stFunc+"(ST_GeomFromWKB($1))", g,
	).Scan(&b); err != nil {
		t.Fatalf("pg error: %v", err)
	}
	return b
}

func (p PostGIS) intFunc(t *testing.T, g geom.Geometry, stFunc string) int {
	var i int
	if err := p.db.QueryRow(
		"SELECT "+stFunc+"(ST_GeomFromWKB($1))", g,
	).Scan(&i); err != nil {
		t.Fatalf("pg error: %v", err)
	}
	return i
}

func (p PostGIS) stringFunc(t *testing.T, g geom.Geometry, stFunc string) string {
	var str string
	if err := p.db.QueryRow(
		"SELECT "+stFunc+"(ST_GeomFromWKB($1))", g,
	).Scan(&str); err != nil {
		t.Fatalf("pg error: %v", err)
	}
	return str
}

func (p PostGIS) float64Func(t *testing.T, g geom.Geometry, stFunc string) float64 {
	var f float64
	if err := p.db.QueryRow(
		"SELECT "+stFunc+"(ST_GeomFromWKB($1))", g,
	).Scan(&f); err != nil {
		t.Fatalf("pg error: %v", err)
	}
	return f
}

func (p PostGIS) bytesFunc(t *testing.T, g geom.Geometry, stFunc string) []byte {
	var bytes []byte
	if err := p.db.QueryRow(
		"SELECT "+stFunc+"(ST_GeomFromWKB($1))", g,
	).Scan(&bytes); err != nil {
		t.Fatalf("pg error: %v", err)
	}
	return bytes
}

func (p PostGIS) binary(t *testing.T, g1, g2 geom.Geometry, stFunc string, dest interface{}) {
	if err := p.db.QueryRow(
		"SELECT "+stFunc+"(ST_GeomFromWKB($1), ST_GeomFromWKB($2))",
		g1, g2,
	).Scan(dest); err != nil {
		t.Fatalf("pg error: %v", err)
	}
}

// TolerantEquals checks if the two geometries are equal, accounting for some
// numeric tolerance and ignoring ordering.
func (p PostGIS) TolerantEquals(t *testing.T, g1, g2 geom.Geometry) bool {
	// The snap to grid can sometimes mess up the equality check if the
	// geometry is split different in the two forms. Try without snap to grid
	// first.
	var eq bool
	if err := p.db.QueryRow(`
		SELECT ST_Equals(
			ST_GeomFromWKB($1),
			ST_GeomFromWKB($2)
		)`, g1, g2,
	).Scan(&eq); err != nil {
		t.Fatalf("pg err: %v", err)
	}
	if eq {
		return true
	}

	if err := p.db.QueryRow(`
		SELECT ST_Equals(
			ST_SnapToGrid(ST_GeomFromWKB($1), 0, 0, 0.00001, 0.00001),
			ST_SnapToGrid(ST_GeomFromWKB($2), 0, 0, 0.00001, 0.00001)
		)`, g1, g2,
	).Scan(&eq); err != nil {
		t.Fatalf("pg err: %v", err)
	}
	return eq
}

func (p PostGIS) boolBinary(t *testing.T, g1, g2 geom.Geometry, stFunc string) bool {
	var b bool
	p.binary(t, g1, g2, stFunc, &b)
	return b
}

func (p PostGIS) geomBinary(t *testing.T, g1, g2 geom.Geometry, stFunc string) geom.Geometry {
	var result geom.Geometry
	if err := p.db.QueryRow(
		"SELECT ST_AsBinary("+stFunc+"(ST_GeomFromWKB($1), ST_GeomFromWKB($2)))",
		g1, g2,
	).Scan(&result); err != nil {
		t.Fatalf("pg error: %v", err)
	}
	return result
}

func (p PostGIS) AsText(t *testing.T, g geom.Geometry) string {
	return string(p.bytesFunc(t, g, "ST_AsText"))
}

func (p PostGIS) AsBinary(t *testing.T, g geom.Geometry) []byte {
	return p.bytesFunc(t, g, "ST_AsBinary")
}

func (p PostGIS) AsGeoJSON(t *testing.T, g geom.Geometry) []byte {
	return p.bytesFunc(t, g, "ST_AsGeoJSON")
}

func (p PostGIS) IsEmpty(t *testing.T, g geom.Geometry) bool {
	return p.boolFunc(t, g, "ST_IsEmpty")
}

func (p PostGIS) Dimension(t *testing.T, g geom.Geometry) int {
	return p.intFunc(t, g, "ST_Dimension")
}

func (p PostGIS) Envelope(t *testing.T, g geom.Geometry) geom.Geometry {
	return p.geomFunc(t, g, "ST_Envelope")
}

func (p PostGIS) IsSimple(t *testing.T, g geom.Geometry) bool {
	return p.boolFunc(t, g, "ST_IsSimple")
}

func (p PostGIS) Boundary(t *testing.T, g geom.Geometry) geom.Geometry {
	return p.geomFunc(t, g, "ST_Boundary")
}

func (p PostGIS) ConvexHull(t *testing.T, g geom.Geometry) geom.Geometry {
	return p.geomFunc(t, g, "ST_ConvexHull")
}

func (p PostGIS) IsValid(t *testing.T, g geom.Geometry) bool {
	return p.boolFunc(t, g, "ST_IsValid")
}

func (p PostGIS) IsRing(t *testing.T, g geom.Geometry) bool {
	// ST_IsRing returns an error whenever it gets anything other than an ST_LineString.
	return p.stringFunc(t, g, "ST_GeometryType") == "ST_LineString" &&
		p.boolFunc(t, g, "ST_IsRing")
}

func (p PostGIS) OrderingEquals(t *testing.T, g1, g2 geom.Geometry) bool {
	return p.boolBinary(t, g1, g2, "ST_OrderingEquals")
}

func (p PostGIS) Equals(t *testing.T, g1, g2 geom.Geometry) bool {
	return p.boolBinary(t, g1, g2, "ST_Equals")
}

func (p PostGIS) Intersects(t *testing.T, g1, g2 geom.Geometry) bool {
	return p.boolBinary(t, g1, g2, "ST_Intersects")
}

func (p PostGIS) Intersection(t *testing.T, g1, g2 geom.Geometry) geom.Geometry {
	return p.geomBinary(t, g1, g2, "ST_Intersection")
}

func (p PostGIS) Length(t *testing.T, g geom.Geometry) float64 {
	return p.float64Func(t, g, "ST_Length")
}

func (p PostGIS) Area(t *testing.T, g geom.Geometry) float64 {
	return p.float64Func(t, g, "ST_Area")
}

func (p PostGIS) Centroid(t *testing.T, g geom.Geometry) geom.Geometry {
	return p.geomFunc(t, g, "ST_Centroid")
}

func (p PostGIS) Reverse(t *testing.T, g geom.Geometry) geom.Geometry {
	return p.geomFunc(t, g, "ST_Reverse")
}
