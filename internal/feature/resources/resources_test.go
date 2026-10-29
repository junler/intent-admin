package resources

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/junler/intent-admin/internal/model"
)

func TestCredentialsNeverMarshalFromModels(t *testing.T) {
	rows := []any{model.AIProvider{APIKeyEnc: "private-ciphertext"}, model.StorageConfig{SecretKeyEnc: "private-ciphertext"}}
	for _, row := range rows {
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "private-ciphertext") || strings.Contains(string(raw), "_enc") {
			t.Fatalf("credential serialized: %s", raw)
		}
	}
}
func TestUploadMIMESafety(t *testing.T) {
	for _, name := range []string{"payload.svg", "payload.html", "payload.js", "payload.php"} {
		if err := validateFile(name, "application/octet-stream", []byte("data"), false); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	if err := validateFile("image.png", "text/html", []byte("<html>"), true); err == nil {
		t.Fatal("accepted HTML as avatar")
	}
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := validateFile("avatar.png", "image/png", b.Bytes(), true); err != nil {
		t.Fatal(err)
	}
	if err := validateFile("avatar.jpg", "image/png", b.Bytes(), true); err == nil {
		t.Fatal("accepted mismatched image extension")
	}
}
