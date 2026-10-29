// Command storage-init creates the private development bucket once MinIO is up.
package main

import (
	"context"
	"fmt"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"os"
	"time"
)

func setting(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
func main() {
	endpoint := setting("MINIO_ENDPOINT", "minio:9000")
	bucket := setting("MINIO_BUCKET", "intent-admin")
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(os.Getenv("MINIO_ROOT_USER"), os.Getenv("MINIO_ROOT_PASSWORD"), ""), Secure: os.Getenv("MINIO_USE_SSL") == "true", Region: "us-east-1", BucketLookup: minio.BucketLookupPath})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Invalid object storage configuration")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for {
		exists, checkErr := client.BucketExists(ctx, bucket)
		if checkErr == nil {
			if exists {
				fmt.Println("Private object-storage bucket is ready")
				return
			}
			if err = client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err == nil {
				fmt.Println("Private object-storage bucket created")
				return
			}
		}
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "Object-storage initialization timed out")
			os.Exit(1)
		case <-time.After(2 * time.Second):
		}
	}
}
