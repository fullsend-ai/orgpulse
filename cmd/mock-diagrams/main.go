// Command mock-diagrams serves a canned chart payload for every request so
// dashboards have something to render during local development (`make dev`).
// Point a diagram's endpoint (or a subscription element URL) at it.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

const chartElementMock = `{"atomicMetricsValue": 42, "type": "bar", "label": "Sample", "lastupdate": %d, "data": {"labels": ["Mon", "Tue", "Wed", "Thu", "Fri"], "datasets": [{"label": "Sample metric", "data": [3, 5, 2, 8, 4]}]}}`

var mockLastUpdate = time.Now().UnixMilli()

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "9999"
	}

	log.Printf("mock-diagrams listening on :%s", port) // #nosec G706
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           http.HandlerFunc(mockHandler),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

func mockHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-Id")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprintf(w, chartElementMock, mockLastUpdate); err != nil {
		http.Error(w, fmt.Sprintf("write error: %v", err), http.StatusInternalServerError)
		return
	}
}
