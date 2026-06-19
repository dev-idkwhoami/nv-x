package capture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCountExternalConsumersIgnoresOwnedPIDs(t *testing.T) {
	root := t.TempDir()
	device := filepath.Join(root, "video10")
	if err := os.WriteFile(device, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, pid := range []string{"100", "200"} {
		fdDir := filepath.Join(root, pid, "fd")
		if err := os.MkdirAll(fdDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(device, filepath.Join(fdDir, "3")); err != nil {
			t.Fatal(err)
		}
	}
	got := CountExternalConsumers(root, device, map[int]bool{100: true})
	if got != 1 {
		t.Fatalf("expected one external consumer, got %d", got)
	}
}
