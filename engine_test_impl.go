package wapc

// __attribute__((weak))
// extern unsigned long long meteringFn(unsigned int op);
import "C"
import (
	"unsafe"
)

type wasmer_parser_operator_t uint32

func GetInternalCPointer() unsafe.Pointer {
	p := unsafe.Pointer(C.meteringFn)
	return p
}

//export meteringFn
func meteringFn(op wasmer_parser_operator_t) uint64 {
	if op >= 20 && op <= 197 {
		return 1
	}
	return 0
}
