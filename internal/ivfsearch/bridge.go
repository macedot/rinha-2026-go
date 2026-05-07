package ivfsearch

// #cgo CFLAGS: -O3 -march=haswell -mtune=haswell -flto -fomit-frame-pointer -DNDEBUG
// #cgo LDFLAGS: -lm
// #include <stdlib.h>
// #include "bridge.h"
import "C"
import (
	"fmt"
	"unsafe"
)

func BridgeLoadIndex(path string) error {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	if C.rinha_load_index(cpath) != 0 {
		return fmt.Errorf("failed to load index via C bridge")
	}
	return nil
}

func BridgeSetParams(nprobe, fullNprobe, candidates int) {
	C.rinha_set_search_params(C.int(nprobe), C.int(fullNprobe), C.int(candidates))
}

func BridgeSearch(qFloat [Dim]float32) int {
	return int(C.rinha_search((*C.float)(unsafe.Pointer(&qFloat[0]))))
}

func BridgeGetInst() [7]uint64 {
	var out [7]C.uint64_t
	C.rinha_get_inst(&out[0])
	var result [7]uint64
	for i := range out {
		result[i] = uint64(out[i])
	}
	return result
}

func BridgeResetInst() {
	C.rinha_reset_inst()
}
