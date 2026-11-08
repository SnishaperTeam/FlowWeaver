package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyInstallAssetPendingMatch(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x.7z")
	if err := os.WriteFile(f, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	sum, err := fileSHA256Hex(f)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{}
	a.setPendingUpdateVerified(f, sum, "v1.0.0")
	if err := a.verifyInstallAsset(f); err != nil {
		t.Fatalf("pending match should verify: %v", err)
	}
	a.setPendingUpdateVerified(f, strings.Repeat("0", 64), "v1.0.0")
	if err := a.verifyInstallAsset(f); err == nil {
		t.Fatal("hash mismatch must be rejected")
	}
}
