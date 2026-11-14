// The fclink command (driver/mkfclink.sh builds it): the linker of driver/fclink as a program of its own, for
// the go command's link step (tools/fc/test.sh, tools/fclink) and for fc's object-file path (FC_OBJFILES=1).
package main

import (
	"os"

	"cmd/internal/fclinklib"
)

func main() { fclinklib.Main(os.Args[1:]) }
