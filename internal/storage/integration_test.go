package storage

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// The fixture is a real MinIO server; no storage behavior is emulated.
func TestS3MinIOIntegration(t *testing.T) {
	endpoint := os.Getenv("INTENT_ADMIN_MINIO_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("requires real MinIO endpoint")
	}
	access, secret := os.Getenv("INTENT_ADMIN_MINIO_TEST_ACCESS_KEY"), os.Getenv("INTENT_ADMIN_MINIO_TEST_SECRET_KEY")
	if access == "" || secret == "" {
		t.Fatal("MinIO test credentials must be configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(access, secret, ""), Secure: false, Region: "us-east-1", BucketLookup: minio.BucketLookupPath})
	if err != nil {
		t.Fatal(err)
	}
	bucket := "intent-test-" + uuid.NewString()
	if err = client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.RemoveBucket(context.Background(), bucket) }()
	cfg := Config{Driver: "s3", Endpoint: endpoint, Region: "us-east-1", Bucket: bucket, AccessKeyID: access, SecretKey: secret, PathStyle: true, URLMode: "presigned", PresignExpireSec: 1}
	if public := os.Getenv("INTENT_ADMIN_MINIO_TEST_PUBLIC_ENDPOINT"); public != "" {
		cfg.BaseURL = "http://" + public + "/" + bucket
	}
	d, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	key := "attach/nested/test.txt"
	if err = d.Put(ctx, key, strings.NewReader("real-s3-data"), 12, "text/plain"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Delete(context.Background(), key) }()
	r, err := d.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(r)
	_ = r.Close()
	if string(raw) != "real-s3-data" {
		t.Fatal("wrong S3 bytes")
	}
	target, err := d.URL(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(target)
	if !strings.HasPrefix(parsed.Path, "/"+bucket+"/") || parsed.Query().Get("X-Amz-Signature") == "" {
		t.Fatal("missing path-style signed URL")
	}
	if cfg.BaseURL != "" {
		public, _ := url.Parse(cfg.BaseURL)
		if parsed.Host != public.Host {
			t.Fatal("URL was signed for internal host")
		}
	}
	response, err := http.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 || string(raw) != "real-s3-data" {
		t.Fatalf("signed download status=%d", response.StatusCode)
	}
	time.Sleep(2200 * time.Millisecond)
	response, err = http.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expired URL accepted: %d", response.StatusCode)
	}
	probeCfg := cfg
	probeCfg.PresignExpireSec = 60
	proof := Probe(ctx, probeCfg)
	if !proof.OK || !proof.PublicURLOK || !proof.Writable || !proof.Readable {
		t.Fatalf("S3 probe failed: %+v", proof)
	}
	if err = d.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if r, err = d.Open(ctx, key); err == nil {
		_ = r.Close()
		t.Fatal("deleted S3 object still readable")
	}
}
