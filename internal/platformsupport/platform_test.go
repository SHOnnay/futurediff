package platformsupport

import "testing"

func TestWindowsExplicitlyUnsupported(t *testing.T) {
	for _, p := range Matrix() {
		if p.GOOS == "windows" && p.Level != Unsupported {
			t.Fatal("Windows must remain explicit unsupported until named pipes exist")
		}
	}
}
func TestPrimaryLinuxSupported(t *testing.T) {
	for _, p := range Matrix() {
		if p.GOOS == "linux" && p.GOARCH == "amd64" && p.Level == Supported {
			return
		}
	}
	t.Fatal("linux amd64 support missing")
}
