package devices

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListSyntheticSysfs(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "video10"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "video10", "name"), []byte("Sony Camera RAW\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := List(root, "/dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one device, got %d", len(got))
	}
	if got[0].Path != "/dev/video10" || got[0].Name != "Sony Camera RAW" {
		t.Fatalf("unexpected device: %+v", got[0])
	}
}
