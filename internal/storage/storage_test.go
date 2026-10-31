package storage

import (
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalDriverRoundTripAndTraversal(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	d, err := New(Config{Driver: "local", LocalDir: root, BaseURL: "https://files.example.test/uploads", URLMode: "static"})
	if err != nil {
		t.Fatal(err)
	}
	key := "attach/2026/10/hello.txt"
	if err = d.Put(ctx, key, strings.NewReader("hello"), 5, "text/plain"); err != nil {
		t.Fatal(err)
	}
	r, err := d.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(r)
	_ = r.Close()
	if string(raw) != "hello" {
		t.Fatal("wrong stored bytes")
	}
	u, err := d.URL(ctx, key)
	if err != nil || u != "https://files.example.test/uploads/"+key {
		t.Fatalf("url=%s err=%v", u, err)
	}
	for _, bad := range []string{"../secret", "/etc/passwd", "a/../../secret", "a\\..\\secret", "a/./file"} {
		if err = d.Put(ctx, bad, strings.NewReader("x"), 1, ""); err == nil {
			t.Fatalf("accepted traversal %s", bad)
		}
	}
	outside := t.TempDir()
	if err = os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err = d.Put(ctx, "link/escape.txt", strings.NewReader("x"), 1, ""); err == nil {
		t.Fatal("accepted symlink escape")
	}
	if err = d.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Open(ctx, key); err == nil {
		t.Fatal("deleted file still exists")
	}
}
// A symlink anywhere above the configured root is the operator's own choice —
// /var is a symlink to private/var on macOS, and /data often points at a mount.
// Only the caller-supplied key may not traverse one.
func TestLocalRootMayLiveBehindASymlink(t *testing.T) {
	ctx := context.Background()
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	d, err := New(Config{Driver: "local", LocalDir: filepath.Join(link, "uploads"), BaseURL: "https://files.example.test/uploads", URLMode: "static"})
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Put(ctx, "attach/hello.txt", strings.NewReader("hello"), 5, "text/plain"); err != nil {
		t.Fatalf("symlinked root rejected: %v", err)
	}
	r, err := d.Open(ctx, "attach/hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(r)
	_ = r.Close()
	if string(raw) != "hello" {
		t.Fatal("wrong stored bytes")
	}
	// Escape prevention must still hold for the key itself.
	if err = os.Symlink(t.TempDir(), filepath.Join(real, "uploads", "out")); err != nil {
		t.Fatal(err)
	}
	if err = d.Put(ctx, "out/escape.txt", strings.NewReader("x"), 1, ""); err == nil {
		t.Fatal("accepted symlink escape through the key")
	}
}

func TestLocalProbeCleansUp(t *testing.T) {
	root := t.TempDir()
	got := Probe(context.Background(), Config{Driver: "local", LocalDir: root, URLMode: "static"})
	if !got.OK || !got.Writable || !got.Readable {
		t.Fatalf("probe=%+v", got)
	}
	files := []string{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return err
	})
	if len(files) > 0 {
		t.Fatal("probe left files behind")
	}
}

func TestPresignedPublicVirtualHostIsNotDuplicated(t *testing.T) {
	d, err := New(Config{Driver: "s3", Endpoint: "internal.example.test:9000", Region: "us-east-1", Bucket: "example-bucket", AccessKeyID: "test-access", SecretKey: "test-private-key", BaseURL: "https://example-bucket.public.example.test", URLMode: "presigned", UseSSL: true, PresignExpireSec: 60})
	if err != nil {
		t.Fatal(err)
	}
	target, err := d.URL(context.Background(), "attach/test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(target, "https://example-bucket.public.example.test/attach/test.txt?") {
		parsed, _ := url.Parse(target)
		t.Fatalf("public host/path %s %s", parsed.Host, parsed.Path)
	}
}
