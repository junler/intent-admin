// Command healthcheck probes the local HTTP service without a shell or curl in
// the production image.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	port := os.Getenv("INTENT_ADMIN_APP_PORT")
	if port == "" {
		port = "8080"
	}
	client := http.Client{Timeout: 4 * time.Second}
	res, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer res.Body.Close()
	var body struct {
		Code int `json:"code"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&body) != nil || body.Code != 200 {
		os.Exit(1)
	}
}
