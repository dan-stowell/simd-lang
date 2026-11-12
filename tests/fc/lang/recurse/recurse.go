// Package recurse: recursion, direct and mutual, and deep enough (100000 frames, each with a buffer, a
// struct or a slice) that the stack grows from its first 8 KiB to tens of MiB, being copied every time.
// No imports.
package recurse

// ---- recursion

func Fact(n uint64) uint64 {
	if n <= 1 {
		return 1
	}
	return n * Fact(n-1)
}
func FibRec(n int) int {
	if n < 2 {
		return n
	}
	return FibRec(n-1) + FibRec(n-2)
}
func Ackermann(m, n int) int {
	if m == 0 {
		return n + 1
	}
	if n == 0 {
		return Ackermann(m-1, 1)
	}
	return Ackermann(m-1, Ackermann(m, n-1))
}
func IsEven(n int) bool {
	if n == 0 {
		return true
	}
	return IsOdd(n - 1)
}
func IsOdd(n int) bool {
	if n == 0 {
		return false
	}
	return IsEven(n - 1)
}
func Hanoi(n int, from, to, via int) (moves int) {
	if n == 0 {
		return 0
	}
	return Hanoi(n-1, from, via, to) + 1 + Hanoi(n-1, via, to, from)
}

// Deep recursion: every frame holds a buffer, so the stack grows many times (and is copied: the pointer
// into the frame must be adjusted).
func Deep(n int, seed int64) int64 {
	var buf [16]int64
	for i := range buf {
		buf[i] = seed + int64(i)
	}
	p := &buf[n&15]
	if n == 0 {
		return *p
	}
	r := Deep(n-1, seed+1)
	return r + *p - buf[n&15] + buf[0] - seed
}
func DeepStruct(n int, acc Acc) Acc {
	if n == 0 {
		return acc
	}
	acc.Sum += int64(n)
	acc.Count++
	acc.Name = "n"
	r := DeepStruct(n-1, acc)
	r.Count += acc.Count - acc.Count
	return r
}

type Acc struct {
	Sum   int64
	Count int
	Name  string
	Pad   [6]int64
}

func DeepSlice(n int, xs []int) int {
	if n == 0 {
		return len(xs)
	}
	var local [4]int
	local[n&3] = n
	return DeepSlice(n-1, append(xs[:0:0], local[n&3])) + local[n&3] - n
}
func Permutations(n int) (count int) {
	var used [10]bool
	return permute(n, 0, &used)
}
func permute(n, k int, used *[10]bool) (count int) {
	if k == n {
		return 1
	}
	for i := 0; i < n; i++ {
		if !used[i] {
			used[i] = true
			count += permute(n, k+1, used)
			used[i] = false
		}
	}
	return
}
func Queens(n int) int { return queens(n, 0, 0, 0, 0) }
func queens(n, row int, cols, d1, d2 uint) (count int) {
	if row == n {
		return 1
	}
	for c := 0; c < n; c++ {
		bc, b1, b2 := uint(1)<<c, uint(1)<<(row+c), uint(1)<<(row-c+n)
		if cols&bc != 0 || d1&b1 != 0 || d2&b2 != 0 {
			continue
		}
		count += queens(n, row+1, cols|bc, d1|b1, d2|b2)
	}
	return
}
