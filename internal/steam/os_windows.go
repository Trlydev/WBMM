//go:build windows

package steam

import (
	"os/exec"
	"syscall"
	"unsafe"
)

var loadLibraryEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LoadLibraryExW")

// openLibrary loads the Steamworks DLL with its dependencies searched for
// only next to it and in System32, not in the working folder.
func openLibrary(path string) (uintptr, error) {
	const searchDLLLoadDir, searchSystem32 = 0x100, 0x800
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	h, _, callErr := loadLibraryEx.Call(uintptr(unsafe.Pointer(p)), 0, searchDLLLoadDir|searchSystem32)
	if h == 0 {
		return 0, callErr
	}
	return h, nil
}

func hideWindow(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
