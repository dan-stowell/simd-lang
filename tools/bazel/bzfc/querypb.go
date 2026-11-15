// querypb.go: reading `bazel query --output=streamed_proto` (length-delimited blaze_query.Target messages, Bazel's
// src/main/protobuf/build.proto) with a small wire-format decoder: bzfc has no dependencies.
//
// Only what the query front end reads is kept: rules (name, class, attributes), source files and generated files
// (name, generating rule). An attribute is kept as its value or, when configurable, as its selector list; the value
// fields of Attribute and of Attribute.SelectorEntry have different numbers, both tables are below.
package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Attribute.Discriminator
const (
	atInteger        = 1
	atString         = 2
	atLabel          = 3
	atOutput         = 4
	atStringList     = 5
	atLabelList      = 6
	atOutputList     = 7
	atDistribSet     = 8
	atLicense        = 9
	atStringDict     = 10
	atFilesetList    = 11
	atLabelListDict  = 12
	atStringListDict = 13
	atBoolean        = 14
	atTristate       = 15
	atIntegerList    = 16
	atUnknown        = 18
	atLabelDictUnary = 19
	atSelectorList   = 20
	atLabelKeyedDict = 21
)

// qVal: one attribute value (or one selector entry's). Str holds string, label and output values; List string and
// label lists; Dict the string dict, label dict (unary) and label-keyed string dict, in order; ListDict the
// label-list and string-list dicts.
type qVal struct {
	Int      int64
	Str      string
	Bool     bool
	List     []string
	Dict     [][2]string
	ListDict []qListEntry
	IntList  []int64
}

type qListEntry struct {
	Key   string
	Value []string
}

type qAttr struct {
	Name     string
	Type     int
	Explicit bool
	qVal
	Sel *qSelList
}

type qSelList struct {
	Type  int
	Elems []qSelector
}

type qSelector struct {
	Entries    []qSelEntry
	HasDefault bool
	NoMatch    string
}

type qSelEntry struct {
	Label     string
	IsDefault bool
	qVal
}

type qRule struct {
	Name, Class, Location string
	Attrs                 map[string]*qAttr
	Inputs, Outputs       []string
}

type qTarget struct {
	Type    int // Target.Discriminator: 1 rule, 2 source file, 3 generated file, 4 package group, 5 environment group
	Rule    *qRule
	Name    string // source or generated file
	GenRule string // generated file: the rule that generates it
}

// pbuf: a protobuf message being read.
type pbuf struct {
	b   []byte
	err error
}

func (p *pbuf) varint() uint64 {
	v, n := binary.Uvarint(p.b)
	if n <= 0 {
		p.err = errors.New("bad varint")
		p.b = nil
		return 0
	}
	p.b = p.b[n:]
	return v
}

// next: the next field's number and wire type, its varint value (wire type 0) or its bytes (wire type 2).
func (p *pbuf) next() (field int, wt int, v uint64, data []byte, ok bool) {
	if len(p.b) == 0 || p.err != nil {
		return 0, 0, 0, nil, false
	}
	k := p.varint()
	field, wt = int(k>>3), int(k&7)
	switch wt {
	case 0:
		v = p.varint()
	case 1:
		if len(p.b) < 8 {
			p.err = errors.New("short fixed64")
			return 0, 0, 0, nil, false
		}
		v = binary.LittleEndian.Uint64(p.b)
		p.b = p.b[8:]
	case 2:
		n := p.varint()
		if uint64(len(p.b)) < n {
			p.err = errors.New("short bytes")
			return 0, 0, 0, nil, false
		}
		data = p.b[:n]
		p.b = p.b[n:]
	case 5:
		if len(p.b) < 4 {
			p.err = errors.New("short fixed32")
			return 0, 0, 0, nil, false
		}
		v = uint64(binary.LittleEndian.Uint32(p.b))
		p.b = p.b[4:]
	default:
		p.err = fmt.Errorf("wire type %d", wt)
		return 0, 0, 0, nil, false
	}
	return field, wt, v, data, p.err == nil
}

// packed or not: repeated int32
func ints(wt int, v uint64, data []byte) []int64 {
	if wt == 0 {
		return []int64{int64(int32(v))}
	}
	var out []int64
	q := pbuf{b: data}
	for len(q.b) > 0 && q.err == nil {
		out = append(out, int64(int32(q.varint())))
	}
	return out
}

func decodeKV(data []byte) (string, string) {
	var k, v string
	q := pbuf{b: data}
	for {
		f, _, _, d, ok := q.next()
		if !ok {
			break
		}
		switch f {
		case 1:
			k = string(d)
		case 2:
			v = string(d)
		}
	}
	return k, v
}

func decodeKList(data []byte) qListEntry {
	var e qListEntry
	q := pbuf{b: data}
	for {
		f, _, _, d, ok := q.next()
		if !ok {
			break
		}
		switch f {
		case 1:
			e.Key = string(d)
		case 2:
			e.Value = append(e.Value, string(d))
		}
	}
	return e
}

// value field numbers: Attribute's and SelectorEntry's
type valFields struct{ intV, str, boolV, tri, strList, strDict, labelListDict, strListDict, intList, labelDictUnary, labelKeyed int }

var (
	attrFields = valFields{3, 5, 14, 15, 6, 8, 10, 11, 17, 19, 22}
	selFields  = valFields{2, 3, 4, 5, 6, 8, 10, 11, 13, 15, 17}
)

// value: does field f (wire type wt) belong to the value? Then it is stored in x.
func (x *qVal) value(vf *valFields, f, wt int, v uint64, d []byte) bool {
	switch f {
	case vf.intV, vf.tri:
		x.Int = int64(int32(v))
	case vf.str:
		x.Str = string(d)
	case vf.boolV:
		x.Bool = v != 0
	case vf.strList:
		x.List = append(x.List, string(d))
	case vf.strDict, vf.labelDictUnary, vf.labelKeyed:
		k, s := decodeKV(d)
		x.Dict = append(x.Dict, [2]string{k, s})
	case vf.labelListDict, vf.strListDict:
		x.ListDict = append(x.ListDict, decodeKList(d))
	case vf.intList:
		x.IntList = append(x.IntList, ints(wt, v, d)...)
	default:
		return false
	}
	return true
}

func decodeAttr(data []byte) *qAttr {
	a := &qAttr{}
	q := pbuf{b: data}
	for {
		f, wt, v, d, ok := q.next()
		if !ok {
			break
		}
		switch f {
		case 1:
			a.Name = string(d)
		case 2:
			a.Type = int(v)
		case 13:
			a.Explicit = v != 0
		case 21:
			a.Sel = decodeSelList(d)
		default:
			a.value(&attrFields, f, wt, v, d)
		}
	}
	return a
}

func decodeSelList(data []byte) *qSelList {
	s := &qSelList{}
	q := pbuf{b: data}
	for {
		f, _, v, d, ok := q.next()
		if !ok {
			break
		}
		switch f {
		case 1:
			s.Type = int(v)
		case 2:
			var sel qSelector
			r := pbuf{b: d}
			for {
				f, _, v, d, ok := r.next()
				if !ok {
					break
				}
				switch f {
				case 1:
					sel.Entries = append(sel.Entries, decodeSelEntry(d))
				case 2:
					sel.HasDefault = v != 0
				case 3:
					sel.NoMatch = string(d)
				}
			}
			s.Elems = append(s.Elems, sel)
		}
	}
	return s
}

func decodeSelEntry(data []byte) qSelEntry {
	var e qSelEntry
	q := pbuf{b: data}
	for {
		f, wt, v, d, ok := q.next()
		if !ok {
			break
		}
		switch f {
		case 1:
			e.Label = string(d)
		case 16:
			e.IsDefault = v != 0
		default:
			e.value(&selFields, f, wt, v, d)
		}
	}
	return e
}

func decodeRule(data []byte) *qRule {
	r := &qRule{Attrs: map[string]*qAttr{}}
	q := pbuf{b: data}
	for {
		f, _, _, d, ok := q.next()
		if !ok {
			break
		}
		switch f {
		case 1:
			r.Name = string(d)
		case 2:
			r.Class = string(d)
		case 3:
			r.Location = string(d)
		case 4:
			a := decodeAttr(d)
			r.Attrs[a.Name] = a
		case 5:
			r.Inputs = append(r.Inputs, string(d))
		case 6:
			r.Outputs = append(r.Outputs, string(d))
		}
	}
	return r
}

func decodeTarget(data []byte) (*qTarget, error) {
	t := &qTarget{}
	q := pbuf{b: data}
	for {
		f, _, v, d, ok := q.next()
		if !ok {
			break
		}
		switch f {
		case 1:
			t.Type = int(v)
		case 2:
			t.Rule = decodeRule(d)
		case 3, 4: // SourceFile, GeneratedFile: name 1 (both); generating_rule 2 (GeneratedFile)
			r := pbuf{b: d}
			for {
				g, _, _, e, ok := r.next()
				if !ok {
					break
				}
				if g == 1 {
					t.Name = string(e)
				} else if g == 2 && f == 4 {
					t.GenRule = string(e)
				}
			}
		}
	}
	return t, q.err
}

// readQueryStream: every Target of a streamed_proto output.
func readQueryStream(r io.Reader) ([]*qTarget, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	var out []*qTarget
	for {
		n, err := binary.ReadUvarint(br)
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		b := make([]byte, n)
		if _, err := io.ReadFull(br, b); err != nil {
			return nil, err
		}
		t, err := decodeTarget(b)
		if err != nil {
			return nil, fmt.Errorf("target %d: %v", len(out), err)
		}
		out = append(out, t)
	}
}
