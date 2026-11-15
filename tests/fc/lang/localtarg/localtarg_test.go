package localtarg_test

import (
	"testing"

	"fclang/localtarg"
)

func TestLocalTypeArgument(t *testing.T) {
	if !localtarg.LocalSelfFunc() {
		t.Error("CallSelf[Bar] returned nil")
	}
}
