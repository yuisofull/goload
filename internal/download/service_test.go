package download

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateStorageKeyPrefersResolvedFileNameExtension(t *testing.T) {
	svc := &service{}
	key := svc.generateStorageKey(TaskRequest{
		TaskID:    1,
		FileName:  "cjrCRgMpxacNCRvmjfgpfwGR",
		SourceURL: "https://example.com/download/cjrCRgMpxacNCRvmjfgpfwGR?token=abc",
	}, "design_rationale_example_1.pdf")

	if !strings.HasPrefix(key, "1/design_rationale_example_1-") {
		t.Fatalf("expected key to use resolved filename, got %q", key)
	}
	if filepath.Ext(key) != ".pdf" {
		t.Fatalf("expected key to preserve .pdf extension, got %q", key)
	}
}

func TestGenerateStorageKeyFallsBackToURLPathWithoutQuery(t *testing.T) {
	svc := &service{}
	key := svc.generateStorageKey(TaskRequest{
		TaskID:    7,
		SourceURL: "https://example.com/files/archive.tar.gz?signature=abc",
	}, "")

	if !strings.HasPrefix(key, "7/archive.tar-") {
		t.Fatalf("expected key to use URL path filename, got %q", key)
	}
	if filepath.Ext(key) != ".gz" {
		t.Fatalf("expected key to preserve .gz extension, got %q", key)
	}
}
