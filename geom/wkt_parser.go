package geom

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// Function names is the parser are chosen to match closely with the BNF
// productions in the WKT grammar.
//
// Convention: functions starting with 'next' consume token(s), and build the
// next production in the grammar.

// UnmarshalWKT parses a Well Known Text (WKT), and returns the corresponding
// Geometry.
func UnmarshalWKT(r io.Reader, opts ...ConstructorOption) (Geometry, error) {
	p := newParser(r, opts)
	geom := p.nextGeometryTaggedText()
	p.checkEOF()
	if p.err != nil {
		return Geometry{}, p.err
	}
	return geom, nil
}

func newParser(r io.Reader, opts []ConstructorOption) *parser {
	return &parser{lexer: newWKTLexer(r), opts: opts}
}

type parser struct {
	lexer *wktLexer
	opts  []ConstructorOption
	err   error
}

func (p *parser) check(err error) {
	if err != nil && p.err == nil {
		p.err = err
	}
}

func (p *parser) errorf(format string, args ...interface{}) {
	p.check(fmt.Errorf(format, args...))
}

func (p *parser) nextToken() string {
	tok, err := p.lexer.next()
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	p.check(err)
	return tok
}

func (p *parser) peekToken() string {
	tok, err := p.lexer.peek()
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	p.check(err)
	return tok
}

func (p *parser) checkEOF() {
	tok, err := p.lexer.next()
	if err != io.EOF {
		p.check(fmt.Errorf("expected EOF but encountered %v", tok))
	}
}

func (p *parser) nextGeometryTaggedText() Geometry {
	switch tok := p.nextToken(); strings.ToUpper(tok) {
	case "POINT":
		coords := p.nextPointText()
		if coords.Empty {
			return NewEmptyPoint(p.opts...).AsGeometry()
		} else {
			return NewPointC(coords.Value, p.opts...).AsGeometry()
		}
	case "LINESTRING":
		coords := p.nextLineStringText()
		switch len(coords) {
		case 0:
			return NewEmptyLineString(p.opts...).AsGeometry()
		case 2:
			ln, err := NewLineC(coords[0], coords[1], p.opts...)
			p.check(err)
			return ln.AsGeometry()
		default:
			ls, err := NewLineStringC(coords, p.opts...)
			p.check(err)
			return ls.AsGeometry()
		}
	case "POLYGON":
		coords := p.nextPolygonText()
		if len(coords) == 0 {
			return NewEmptyPolygon(p.opts...).AsGeometry()
		} else {
			poly, err := NewPolygonC(coords, p.opts...)
			p.check(err)
			return poly.AsGeometry()
		}
	case "MULTIPOINT":
		coords := p.nextMultiPointText()
		return NewMultiPointOC(coords, p.opts...).AsGeometry()
	case "MULTILINESTRING":
		coords := p.nextPolygonText() // same production as polygon
		mls, err := NewMultiLineStringC(coords, p.opts...)
		p.check(err)
		return mls.AsGeometry()
	case "MULTIPOLYGON":
		coords := p.nextMultiPolygonText()
		mp, err := NewMultiPolygonC(coords, p.opts...)
		p.check(err)
		return mp.AsGeometry()
	case "GEOMETRYCOLLECTION":
		return p.nextGeometryCollectionText()
	default:
		p.errorf("unexpected token: %v", tok)
		return Geometry{}
	}
}

func (p *parser) nextEmptySetOrLeftParen() string {
	tok := p.nextToken()
	if tok != "EMPTY" && tok != "(" {
		p.errorf("expected 'EMPTY' or '(' but encountered %v", tok)
	}
	return tok
}

func (p *parser) nextRightParen() {
	tok := p.nextToken()
	if tok != ")" {
		p.check(fmt.Errorf("expected ')' but encountered %v", tok))
	}
}

func (p *parser) nextCommaOrRightParen() string {
	tok := p.nextToken()
	if tok != ")" && tok != "," {
		p.check(fmt.Errorf("expected ')' or ',' but encountered %v", tok))
	}
	return tok
}

func (p *parser) nextPoint() Coordinates {
	// TODO: handle z, m, and zm points.
	x := p.nextSignedNumericLiteral()
	y := p.nextSignedNumericLiteral()
	return Coordinates{XY{x, y}}
}

func (p *parser) nextSignedNumericLiteral() float64 {
	var negative bool
	tok := p.nextToken()
	if tok == "-" {
		negative = true
		tok = p.nextToken()
	}
	f, err := strconv.ParseFloat(tok, 64)
	p.check(err)
	// NaNs and Infs are not allowed by the WKT grammar.
	if math.IsNaN(f) || math.IsInf(f, 0) {
		p.errorf("invalid signed numeric literal: %s", tok)
	}
	if negative {
		f *= -1
	}
	return f
}

func (p *parser) nextPointText() OptionalCoordinates {
	tok := p.nextEmptySetOrLeftParen()
	if tok == "EMPTY" {
		return OptionalCoordinates{Empty: true}
	}
	pt := p.nextPoint()
	p.nextRightParen()
	return OptionalCoordinates{Value: pt}
}

func (p *parser) nextLineStringText() []Coordinates {
	tok := p.nextEmptySetOrLeftParen()
	if tok == "EMPTY" {
		return nil
	}
	pt := p.nextPoint()
	pts := []Coordinates{pt}
	for {
		tok := p.nextCommaOrRightParen()
		if tok == "," {
			pts = append(pts, p.nextPoint())
		} else {
			break
		}
	}
	return pts
}

func (p *parser) nextPolygonText() [][]Coordinates {
	tok := p.nextEmptySetOrLeftParen()
	if tok == "EMPTY" {
		return nil
	}
	line := p.nextLineStringText()
	lines := [][]Coordinates{line}
	for {
		tok := p.nextCommaOrRightParen()
		if tok == "," {
			lines = append(lines, p.nextLineStringText())
		} else {
			break
		}
	}
	return lines
}

func (p *parser) nextMultiPointText() []OptionalCoordinates {
	tok := p.nextEmptySetOrLeftParen()
	if tok == "EMPTY" {
		return nil
	}
	pt := p.nextMultiPointStylePoint()
	pts := []OptionalCoordinates{pt}
	for {
		tok := p.nextCommaOrRightParen()
		if tok == "," {
			pt := p.nextMultiPointStylePoint()
			pts = append(pts, pt)
		} else {
			break
		}
	}
	return pts
}

func (p *parser) nextMultiPointStylePoint() OptionalCoordinates {
	// This is an extension of the spec, and is required to handle WKT output
	// from non-complying implementations. In particular, PostGIS doesn't
	// comply to the spec (it outputs points as part of a multipoint without
	// their surrounding parentheses).
	tok := p.peekToken()
	if tok == "EMPTY" {
		p.nextToken()
		return OptionalCoordinates{Empty: true}
	}
	var useParens bool
	if tok == "(" {
		p.nextToken()
		useParens = true
	}
	pt := p.nextPoint()
	if useParens {
		p.nextRightParen()
	}
	return OptionalCoordinates{Value: pt}
}

func (p *parser) nextMultiPolygonText() [][][]Coordinates {
	tok := p.nextEmptySetOrLeftParen()
	if tok == "EMPTY" {
		return nil
	}
	poly := p.nextPolygonText()
	polys := [][][]Coordinates{poly}
	for {
		tok := p.nextCommaOrRightParen()
		if tok == "," {
			poly := p.nextPolygonText()
			polys = append(polys, poly)
		} else {
			break
		}
	}
	return polys
}

func (p *parser) nextGeometryCollectionText() Geometry {
	tok := p.nextEmptySetOrLeftParen()
	if tok == "EMPTY" {
		return NewGeometryCollection(nil, p.opts...).AsGeometry()
	}
	geoms := []Geometry{
		p.nextGeometryTaggedText(),
	}
	for {
		tok := p.nextCommaOrRightParen()
		if tok == "," {
			geom := p.nextGeometryTaggedText()
			geoms = append(geoms, geom)
		} else {
			break
		}
	}
	return NewGeometryCollection(geoms, p.opts...).AsGeometry()
}
