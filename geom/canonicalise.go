package geom

func canonicalPointsAndLines(points []Point, lines []Line) (Geometry, error) {
	// Deduplicate.
	points = dedupPoints(points)
	lines = dedupLines(lines)

	// Remove any points that are covered by lines.
	var newPoints []Point
	for _, pt := range points {
		hasInter := false
		for _, ln := range lines {
			if pt.Intersects(ln.AsGeometry()) {
				hasInter = true
				break
			}
		}
		if !hasInter {
			newPoints = append(newPoints, pt)
		}
	}
	points = newPoints

	switch {
	case len(points) == 0 && len(lines) == 0:
		return NewGeometryCollection(nil).AsGeometry(), nil
	case len(points) == 0:
		// Lines only.
		if len(lines) == 1 {
			return lines[0].AsGeometry(), nil
		}
		var lineStrings []LineString
		for _, ln := range lines {
			lnStr, err := NewLineStringC(ln.Coordinates())
			if err != nil {
				return Geometry{}, err
			}
			lineStrings = append(lineStrings, lnStr)
		}
		return NewMultiLineString(lineStrings).AsGeometry(), nil
	case len(lines) == 0:
		// Points only.
		if len(points) == 1 {
			return points[0].AsGeometry(), nil
		}
		return NewMultiPoint(points).AsGeometry(), nil
	default:
		all := make([]Geometry, len(points)+len(lines))
		for i, pt := range points {
			all[i] = pt.AsGeometry()
		}
		for i, ln := range lines {
			all[len(points)+i] = ln.AsGeometry()
		}
		return NewGeometryCollection(all).AsGeometry(), nil
	}
}

func dedupPoints(pts []Point) []Point {
	var dedup []Point
	seen := make(map[XY]bool)
	for _, pt := range pts {
		xy := pt.XY()
		if !seen[xy] {
			dedup = append(dedup, pt)
			seen[xy] = true
		}
	}
	return dedup
}

func dedupLines(lines []Line) []Line {
	type xyxy struct {
		a, b XY
	}
	var dedup []Line
	seen := make(map[xyxy]bool)
	for _, ln := range lines {
		k := xyxy{ln.a.XY, ln.b.XY}
		if !k.a.Less(k.b) {
			k.a, k.b = k.b, k.a
		}
		if !seen[k] {
			dedup = append(dedup, ln)
			seen[k] = true
		}
	}
	return dedup
}
