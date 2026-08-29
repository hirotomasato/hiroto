package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSyncBinaryCopiesNewFileOverOld(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "new")
	dst := filepath.Join(dir, "running")
	if err := os.WriteFile(src, []byte("v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syncBinary(src, dst); err != nil {
		t.Fatalf("syncBinary: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2" {
		t.Fatalf("dst = %q, want %q", got, "v2")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("src should be gone after rename, err=%v", err)
	}
}

func TestSyncBinaryNoOpSameFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bin")
	if err := os.WriteFile(p, []byte("same"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syncBinary(p, p); err != nil {
		t.Fatalf("syncBinary same path: %v", err)
	}
	// same inode, different path (hardlink-style case)
	alt := filepath.Join(dir, "alt")
	if err := os.Symlink(p, alt); err == nil {
		if err := syncBinary(alt, p); err != nil {
			t.Fatalf("syncBinary same inode: %v", err)
		}
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "same" {
		t.Fatalf("file changed unexpectedly: %q", got)
	}
}

func TestBinaryName(t *testing.T) {
	if runtime.GOOS == "windows" {
		if binaryName() != "hiroto.exe" {
			t.Fatalf("binaryName = %q", binaryName())
		}
		return
	}
	if binaryName() != "hiroto" {
		t.Fatalf("binaryName = %q", binaryName())
	}
}

func TestSamePath(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if samePath(a, b) {
		t.Fatal("different files must not match")
	}
	if !samePath(a, a) {
		t.Fatal("same path must match")
	}
	if err := os.Symlink(a, filepath.Join(dir, "link")); err == nil {
		if !samePath(a, filepath.Join(dir, "link")) {
			t.Fatal("symlink to same file must match")
		}
	}
}
