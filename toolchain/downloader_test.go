package toolchain

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gonano-school/toolchain/types"
)

func TestSelectMatchesFilters(t *testing.T) {
	manifest := types.Manifest{
		"datasets/wikipedia/a.zim":      {Model: "gonano-base", Categories: []string{CategoryBase}},
		"datasets/wikipedia/b.zim":      {Model: "gonano-physics", Categories: []string{CategoryPhysics}},
		"datasets/stack_exchange/c.zim": {Model: "gonano-programming", Categories: []string{CategoryProgramming}},
	}
	if got := len(Select(manifest, Filter{})); got != 3 {
		t.Fatalf("empty filter selected %d, want 3", got)
	}
	if got := Select(manifest, Filter{Models: []string{"gonano-physics"}}); len(got) != 1 || got[0].Key != "datasets/wikipedia/b.zim" {
		t.Fatalf("model filter = %#v", got)
	}
	if got := Select(manifest, Filter{Categories: []string{CategoryBase, CategoryProgramming}}); len(got) != 2 {
		t.Fatalf("category filter = %d, want 2", len(got))
	}
	if got := Select(manifest, Filter{Sites: []string{"stack_exchange"}}); len(got) != 1 || got[0].Key != "datasets/stack_exchange/c.zim" {
		t.Fatalf("site filter = %#v", got)
	}
	// Union semantics: model OR category.
	if got := Select(manifest, Filter{Models: []string{"gonano-base"}, Categories: []string{CategoryPhysics}}); len(got) != 2 {
		t.Fatalf("union filter = %d, want 2", len(got))
	}
}

func downloadServer(t *testing.T, body []byte, failFirst int) (*httptest.Server, *int) {
	t.Helper()
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hits++
		if hits <= failFirst {
			http.Error(writer, "boom", http.StatusInternalServerError)
			return
		}
		writer.Write(body)
	}))
	return server, &hits
}

func singleItem(rawURL string, size int64) []Item {
	manifest := types.Manifest{
		"datasets/test/file.zim": {
			URL:        rawURL,
			Categories: []string{CategoryBase},
			Model:      "gonano-base",
			Size:       size,
		},
	}
	return Select(manifest, Filter{})
}

func TestDownloadWritesThenSkips(t *testing.T) {
	body := []byte("hello zim contents")
	server, hits := downloadServer(t, body, 0)
	defer server.Close()
	items := singleItem(server.URL+"/file.zim", int64(len(body)))

	root := t.TempDir()
	if err := Download(context.Background(), items, DownloadOptions{RootDir: root, Jobs: 1, HTTPClient: server.Client()}); err != nil {
		t.Fatalf("Download: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "datasets/test/file.zim"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != string(body) {
		t.Fatalf("content = %q, want %q", data, body)
	}
	if err := Download(context.Background(), items, DownloadOptions{RootDir: root, Jobs: 1, HTTPClient: server.Client()}); err != nil {
		t.Fatalf("second Download: %v", err)
	}
	if *hits != 1 {
		t.Fatalf("hits = %d, want 1 (second run should skip)", *hits)
	}
	if _, err := os.Stat(filepath.Join(root, "datasets/test/file.zim.part")); err == nil {
		t.Fatal(".part file left behind")
	}
}

func TestDownloadSizeMismatch(t *testing.T) {
	body := []byte("short")
	server, _ := downloadServer(t, body, 0)
	defer server.Close()
	items := singleItem(server.URL+"/file.zim", int64(len(body))+1)

	root := t.TempDir()
	if err := Download(context.Background(), items, DownloadOptions{RootDir: root, Jobs: 1, HTTPClient: server.Client()}); err == nil {
		t.Fatal("expected size mismatch error")
	}
	if _, err := os.Stat(filepath.Join(root, "datasets/test/file.zim")); err == nil {
		t.Fatal("mismatched file should be removed")
	}
	if _, err := os.Stat(filepath.Join(root, "datasets/test/file.zim.part")); err == nil {
		t.Fatal(".part file left behind after mismatch")
	}
}

func TestDownloadMaxBytesAborts(t *testing.T) {
	body := []byte("data")
	server, hits := downloadServer(t, body, 0)
	defer server.Close()
	items := singleItem(server.URL+"/file.zim", 1000)

	root := t.TempDir()
	if err := Download(context.Background(), items, DownloadOptions{RootDir: root, MaxBytes: 10, HTTPClient: server.Client()}); err == nil {
		t.Fatal("expected max-bytes error")
	}
	if *hits != 0 {
		t.Fatalf("hits = %d, want 0 (abort before download)", *hits)
	}
}

func TestDownloadDryRun(t *testing.T) {
	body := []byte("data")
	server, hits := downloadServer(t, body, 0)
	defer server.Close()
	items := singleItem(server.URL+"/file.zim", int64(len(body)))

	root := t.TempDir()
	if err := Download(context.Background(), items, DownloadOptions{RootDir: root, DryRun: true, HTTPClient: server.Client()}); err != nil {
		t.Fatalf("Download dry-run: %v", err)
	}
	if *hits != 0 {
		t.Fatalf("hits = %d, want 0", *hits)
	}
	if _, err := os.Stat(filepath.Join(root, "datasets/test/file.zim")); err == nil {
		t.Fatal("dry-run wrote a file")
	}
}

func TestDownloadRetries(t *testing.T) {
	body := []byte("retry body")
	server, hits := downloadServer(t, body, 1)
	defer server.Close()
	items := singleItem(server.URL+"/file.zim", int64(len(body)))

	root := t.TempDir()
	if err := Download(context.Background(), items, DownloadOptions{
		RootDir: root, Jobs: 1, Retry: 2, RetryDelay: time.Millisecond, HTTPClient: server.Client(),
	}); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if *hits != 2 {
		t.Fatalf("hits = %d, want 2", *hits)
	}
}

func TestDownloadHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "nope", http.StatusNotFound)
	}))
	defer server.Close()
	items := singleItem(server.URL+"/file.zim", 1)

	root := t.TempDir()
	if err := Download(context.Background(), items, DownloadOptions{RootDir: root, Jobs: 1, Retry: 0, HTTPClient: server.Client()}); err == nil {
		t.Fatal("expected HTTP error")
	}
}

func TestSelectionSize(t *testing.T) {
	items := []Item{
		{Dataset: types.Dataset{Size: 10}},
		{Dataset: types.Dataset{Size: 0}},
		{Dataset: types.Dataset{Size: 5}},
	}
	if got := SelectionSize(items); got != 15 {
		t.Fatalf("SelectionSize = %d, want 15", got)
	}
}
