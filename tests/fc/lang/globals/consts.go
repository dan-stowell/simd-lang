package globals

const (
	A0 = iota
	A1
	A2
	_
	A4
)

type Weekday uint8

const (
	Sunday Weekday = iota
	Monday
	Tuesday
	Wednesday
	Thursday
	Friday
	Saturday
)

var weekdayNames = [...]string{Sunday: "Sun", Monday: "Mon", Tuesday: "Tue", Wednesday: "Wed", Thursday: "Thu", Friday: "Fri", Saturday: "Sat"}

func (d Weekday) String() string {
	if int(d) < len(weekdayNames) {
		return weekdayNames[d]
	}
	return "?"
}

const (
	_  = iota
	KB = 1 << (10 * iota)
	MB
	GB
	TB
)

type Flags uint16

const (
	FRead Flags = 1 << iota
	FWrite
	FExec
	_
	FDir
	FAll = FRead | FWrite | FExec | FDir
)

type Color int8

const (
	Red Color = -1 - iota
	Green
	Blue
)

const (
	X0, Y0 = iota, iota * 10
	X1, Y1
	X2, Y2
)

const (
	MaxI8   int8    = 1<<7 - 1
	MinI8   int8    = -1 << 7
	MaxU16  uint16  = 1<<16 - 1
	MinI64  int64   = -1 << 63
	MaxU64  uint64  = 1<<64 - 1
	Pi32    float32 = 3.14159265358979323846
	Pi64            = 3.14159265358979323846
	Hello           = "héllo, wörld"
	HelloN          = len(Hello)
	Letter  rune    = 'λ'
	Yes     bool    = A1 < A2
	Huge            = 1 << 100
	HugeLow         = Huge >> 98
	Mask            = Huge - 1
	MaskTop         = Mask >> 60 & 0xff
	Ratio           = 1.0 / 3
	TypedF  float64 = Huge
)

func Iotas() []int             { return []int{A0, A1, A2, A4, X0, Y0, X1, Y1, X2, Y2} }
func Sizes() []int             { return []int{KB, MB, GB, TB} }
func Colors() []Color          { return []Color{Red, Green, Blue} }
func FlagBits() []Flags        { return []Flags{FRead, FWrite, FExec, FDir, FAll} }
func DayName(d Weekday) string { return d.String() }
func NextDay(d Weekday) Weekday {
	return (d + 1) % (Saturday + 1)
}
func HasFlag(f, bit Flags) bool { return f&bit != 0 }

// Typed constants take part in run-time arithmetic with their type's wraparound.
func AddMaxI8(x int8) int8       { return MaxI8 + x }
func SubMinI8(x int8) int8       { return MinI8 - x }
func AddMaxU16(x uint16) uint16  { return MaxU16 + x }
func AddMaxU64(x uint64) uint64  { return MaxU64 + x }
func DivMinI64(x int64) int64    { return MinI64 / x }
func ScalePi32(x float32) float32 { return Pi32 * x }
func ScalePi64(x float64) float64 { return Pi64 * x }
func ScaleRatio(x float64) float64 { return Ratio * x }
func ScaleHuge(x float64) float64 { return TypedF * x }
func HelloAt(i int) byte         { return Hello[i] }
func HelloLen() (int, int)       { return HelloN, len(Hello) }
func LetterPlus(d rune) rune     { return Letter + d }
func SmallFromHuge(x int) int    { return x*HugeLow + MaskTop }
func YesAnd(b bool) bool         { return Yes && b }
