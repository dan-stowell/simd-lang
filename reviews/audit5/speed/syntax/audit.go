// Audit-only wrapper. The four adjacent Go compiler scanner sources are unmodified.
package syntax

import (
	"bytes"
	"fmt"
	gotoken "go/token"
)

type Counter struct {
	s      scanner
	r      bytes.Reader
	errors int
}

// Init is deliberately inside the timed region in the count benchmarks.
func (c *Counter) Init(src []byte) {
	c.errors = 0
	c.r.Reset(src)
	c.s.init(&c.r, func(_, _ uint, _ string) { c.errors++ }, 0)
}

// Count returns native token count, excluding EOF, with no token normalization.
func (c *Counter) Count() (n, errors int) {
	for {
		c.s.next()
		if c.s.tok == _EOF {
			break
		}
		n++
	}
	return n, c.errors
}

func Scan(src []byte) (int, int) {
	var c Counter
	c.Init(src)
	return c.Count()
}

// Visitor is for correctness/dump controls, not the native scan-only timing.
type Visitor struct {
	c      Counter
	src    []byte
	starts []int
}

func NewVisitor(src []byte) *Visitor {
	v := &Visitor{src: src, starts: []int{0}}
	for i, ch := range src {
		if ch == '\n' {
			v.starts = append(v.starts, i+1)
		}
	}
	v.c.Init(src)
	return v
}

// Next maps the compiler scanner's token metadata to go/gotoken.
// It exposes both raw and canonical byte offsets. For an implicit semicolon
// due to a multiline comment, the compiler records the comment's opening slash;
// go/scanner records its first newline. canonical performs that adjustment.
func (v *Visitor) Next() (raw, canonical int, tok gotoken.Token) {
	s := &v.c.s
	s.next()
	raw = v.starts[int(s.line)-1] + int(s.col) - 1
	canonical = raw
	tok = normalized(s)
	if s.tok == _Semi && s.lit == "newline" && raw+1 < len(v.src) &&
		v.src[raw] == '/' && v.src[raw+1] == '*' {
		n := bytes.IndexByte(v.src[raw:], '\n')
		if n < 0 {
			panic("multiline semicolon without newline")
		}
		canonical += n
	}
	return
}

func (v *Visitor) Errors() int { return v.c.errors }

var operators = [...]gotoken.Token{
	Def: gotoken.COLON, Not: gotoken.NOT, Recv: gotoken.ARROW, Tilde: gotoken.TILDE,
	OrOr: gotoken.LOR, AndAnd: gotoken.LAND, Eql: gotoken.EQL, Neq: gotoken.NEQ,
	Lss: gotoken.LSS, Leq: gotoken.LEQ, Gtr: gotoken.GTR, Geq: gotoken.GEQ,
	Add: gotoken.ADD, Sub: gotoken.SUB, Or: gotoken.OR, Xor: gotoken.XOR,
	Mul: gotoken.MUL, Div: gotoken.QUO, Rem: gotoken.REM, And: gotoken.AND,
	AndNot: gotoken.AND_NOT, Shl: gotoken.SHL, Shr: gotoken.SHR,
}

var assignments = [...]gotoken.Token{
	Add: gotoken.ADD_ASSIGN, Sub: gotoken.SUB_ASSIGN, Or: gotoken.OR_ASSIGN,
	Xor: gotoken.XOR_ASSIGN, Mul: gotoken.MUL_ASSIGN, Div: gotoken.QUO_ASSIGN,
	Rem: gotoken.REM_ASSIGN, And: gotoken.AND_ASSIGN, AndNot: gotoken.AND_NOT_ASSIGN,
	Shl: gotoken.SHL_ASSIGN, Shr: gotoken.SHR_ASSIGN,
}

var simple = [...]gotoken.Token{
	_EOF: gotoken.EOF, _Name: gotoken.IDENT, _Assign: gotoken.ASSIGN,
	_Define: gotoken.DEFINE, _Arrow: gotoken.ARROW, _Star: gotoken.MUL,
	_Lparen: gotoken.LPAREN, _Lbrack: gotoken.LBRACK, _Lbrace: gotoken.LBRACE,
	_Rparen: gotoken.RPAREN, _Rbrack: gotoken.RBRACK, _Rbrace: gotoken.RBRACE,
	_Comma: gotoken.COMMA, _Semi: gotoken.SEMICOLON, _Colon: gotoken.COLON,
	_Dot: gotoken.PERIOD, _DotDotDot: gotoken.ELLIPSIS,
	_Break: gotoken.BREAK, _Case: gotoken.CASE, _Chan: gotoken.CHAN,
	_Const: gotoken.CONST, _Continue: gotoken.CONTINUE, _Default: gotoken.DEFAULT,
	_Defer: gotoken.DEFER, _Else: gotoken.ELSE, _Fallthrough: gotoken.FALLTHROUGH,
	_For: gotoken.FOR, _Func: gotoken.FUNC, _Go: gotoken.GO, _Goto: gotoken.GOTO,
	_If: gotoken.IF, _Import: gotoken.IMPORT, _Interface: gotoken.INTERFACE,
	_Map: gotoken.MAP, _Package: gotoken.PACKAGE, _Range: gotoken.RANGE,
	_Return: gotoken.RETURN, _Select: gotoken.SELECT, _Struct: gotoken.STRUCT,
	_Switch: gotoken.SWITCH, _Type: gotoken.TYPE, _Var: gotoken.VAR,
}

func normalized(s *scanner) gotoken.Token {
	switch s.tok {
	case _Literal:
		return [...]gotoken.Token{gotoken.INT, gotoken.FLOAT, gotoken.IMAG, gotoken.CHAR, gotoken.STRING}[s.kind]
	case _Operator:
		return operators[s.op]
	case _AssignOp:
		return assignments[s.op]
	case _IncOp:
		if s.op == Add {
			return gotoken.INC
		}
		if s.op == Sub {
			return gotoken.DEC
		}
		panic("invalid increment operator")
	default:
		if int(s.tok) >= len(simple) || simple[s.tok] == gotoken.ILLEGAL {
			panic(fmt.Sprintf("unmapped compiler token %d", s.tok))
		}
		return simple[s.tok]
	}
}
