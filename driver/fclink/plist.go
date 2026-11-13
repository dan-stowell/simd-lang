package main

// A plist is one worker's records of one partition of a join (resolve.go): chunks cut from the worker's
// slab, so that filling it never copies records and touches only the memory the records need.
type plist[T any] struct {
	chunks [][]T
	n      int
}

const chunkRecs = 512

func (l *plist[T]) add(slab *[]T, v T) {
	k := len(l.chunks) - 1
	if k < 0 || len(l.chunks[k]) == chunkRecs {
		if len(*slab) < chunkRecs {
			*slab = make([]T, 512*chunkRecs)
		}
		l.chunks = append(l.chunks, (*slab)[:0:chunkRecs])
		*slab = (*slab)[chunkRecs:]
		k++
	}
	l.chunks[k] = append(l.chunks[k], v)
	l.n++
}
