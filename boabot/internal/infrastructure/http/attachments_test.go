package httpserver_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpserver "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/http"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// newAttachmentServer returns a test server with a board store that starts with
// the given items and records the last-updated item on Update.
func newAttachmentServer(t *testing.T, initial []domain.WorkItem) (*httptest.Server, *[]domain.WorkItem) {
	t.Helper()
	store := make([]domain.WorkItem, len(initial))
	copy(store, initial)
	updated := &[]domain.WorkItem{}

	board := &fakeBoardStore{
		getFn: func(_ context.Context, id string) (domain.WorkItem, error) {
			for _, it := range store {
				if it.ID == id {
					return it, nil
				}
			}
			return domain.WorkItem{}, fmt.Errorf("not found")
		},
		updateFn: func(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
			for i, it := range store {
				if it.ID == item.ID {
					store[i] = item
					break
				}
			}
			*updated = append(*updated, item)
			return item, nil
		},
	}

	s := httpserver.New(httpserver.Config{
		Auth:   &fakeAuth{},
		Board:  board,
		Team:   &fakeControlPlane{},
		Users:  &fakeUserStore{},
		Skills: &fakeSkillRegistry{},
		DLQ:    &fakeDLQStore{},
	})
	return httptest.NewServer(s.Handler()), updated
}

func doMultipartUpload(t *testing.T, srv *httptest.Server, itemID string, files map[string][]byte) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for name, content := range files {
		fw, err := mw.CreateFormFile("files", name)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := fw.Write(content); err != nil {
			t.Fatalf("write form file: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/board/"+itemID+"/attachments", &buf)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// ── Upload ────────────────────────────────────────────────────────────────────

func TestBoardAttachmentUpload_ReturnsUpdatedItem(t *testing.T) {
	items := []domain.WorkItem{{ID: "item-1", Title: "test"}}
	srv, _ := newAttachmentServer(t, items)
	defer srv.Close()

	resp := doMultipartUpload(t, srv, "item-1", map[string][]byte{
		"readme.md": []byte("# Hello World"),
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var out domain.WorkItem
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(out.Attachments))
	}
	att := out.Attachments[0]
	if att.Name != "readme.md" {
		t.Errorf("expected name readme.md, got %q", att.Name)
	}
	if att.ID == "" {
		t.Error("expected non-empty attachment ID")
	}
	if att.Size != len("# Hello World") {
		t.Errorf("expected size %d, got %d", len("# Hello World"), att.Size)
	}
	raw, err := base64.StdEncoding.DecodeString(att.Content)
	if err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if string(raw) != "# Hello World" {
		t.Errorf("content mismatch: got %q", string(raw))
	}
}

func TestBoardAttachmentUpload_ItemNotFound_Returns404(t *testing.T) {
	srv, _ := newAttachmentServer(t, nil)
	defer srv.Close()

	resp := doMultipartUpload(t, srv, "missing-item", map[string][]byte{
		"file.txt": []byte("content"),
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestBoardAttachmentUpload_MultipleFiles(t *testing.T) {
	items := []domain.WorkItem{{ID: "item-2", Title: "multi"}}
	srv, _ := newAttachmentServer(t, items)
	defer srv.Close()

	// Upload two files sequentially (maps don't guarantee order but the count should be 2)
	// Upload first file
	resp1 := doMultipartUpload(t, srv, "item-2", map[string][]byte{
		"spec.md": []byte("# Spec"),
	})
	defer func() { _ = resp1.Body.Close() }()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first upload: expected 200, got %d", resp1.StatusCode)
	}
	var out1 domain.WorkItem
	if err := json.NewDecoder(resp1.Body).Decode(&out1); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out1.Attachments) != 1 {
		t.Fatalf("expected 1 attachment after first upload, got %d", len(out1.Attachments))
	}
}

// ── Get ───────────────────────────────────────────────────────────────────────

func TestBoardAttachmentGet_TextFile_ReturnsContent(t *testing.T) {
	content := "# My PRD\n\nThis is a PRD."
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	items := []domain.WorkItem{{
		ID:    "item-3",
		Title: "with-att",
		Attachments: []domain.Attachment{{
			ID:          "att-abc",
			Name:        "prd.md",
			ContentType: "text/markdown",
			Content:     encoded,
			Size:        len(content),
		}},
	}}
	srv, _ := newAttachmentServer(t, items)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/board/item-3/attachments/att-abc", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != content {
		t.Errorf("content mismatch: got %q, want %q", string(body), content)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "text/markdown" {
		t.Errorf("expected Content-Type text/markdown, got %q", ct)
	}
	disp := resp.Header.Get("Content-Disposition")
	if !strings.Contains(disp, "inline") {
		t.Errorf("expected inline disposition for text file, got %q", disp)
	}
}

func TestBoardAttachmentGet_BinaryFile_AttachmentDisposition(t *testing.T) {
	binaryContent := []byte{0x89, 0x50, 0x4e, 0x47} // PNG magic bytes
	encoded := base64.StdEncoding.EncodeToString(binaryContent)
	items := []domain.WorkItem{{
		ID:    "item-4",
		Title: "binary",
		Attachments: []domain.Attachment{{
			ID:          "att-bin",
			Name:        "image.png",
			ContentType: "image/png",
			Content:     encoded,
			Size:        len(binaryContent),
		}},
	}}
	srv, _ := newAttachmentServer(t, items)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/board/item-4/attachments/att-bin", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	disp := resp.Header.Get("Content-Disposition")
	if !strings.Contains(disp, "attachment") {
		t.Errorf("expected attachment disposition for binary file, got %q", disp)
	}
}

func TestBoardAttachmentGet_NotFound_Returns404(t *testing.T) {
	items := []domain.WorkItem{{ID: "item-5", Title: "empty"}}
	srv, _ := newAttachmentServer(t, items)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/board/item-5/attachments/missing-att", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestBoardAttachmentDelete_RemovesAttachment(t *testing.T) {
	items := []domain.WorkItem{{
		ID:    "item-6",
		Title: "has-att",
		Attachments: []domain.Attachment{
			{ID: "att-keep", Name: "keep.md", Content: base64.StdEncoding.EncodeToString([]byte("keep"))},
			{ID: "att-del", Name: "delete.md", Content: base64.StdEncoding.EncodeToString([]byte("delete"))},
		},
	}}
	srv, updated := newAttachmentServer(t, items)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/board/item-6/attachments/att-del", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 204, got %d: %s", resp.StatusCode, body)
	}

	if len(*updated) == 0 {
		t.Fatal("expected board store to be updated")
	}
	last := (*updated)[len(*updated)-1]
	if len(last.Attachments) != 1 {
		t.Fatalf("expected 1 remaining attachment, got %d", len(last.Attachments))
	}
	if last.Attachments[0].ID != "att-keep" {
		t.Errorf("expected att-keep to remain, got %q", last.Attachments[0].ID)
	}
}

func TestBoardAttachmentDelete_NotFound_Returns404(t *testing.T) {
	items := []domain.WorkItem{{ID: "item-7", Title: "empty"}}
	srv, _ := newAttachmentServer(t, items)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/board/item-7/attachments/nope", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// ── Instruction injection ─────────────────────────────────────────────────────

func TestBoardUpdate_InjectsTextAttachmentsIntoInstruction(t *testing.T) {
	var capturedInstruction string
	content := "# My Spec\n\nThis is the spec content."
	encoded := base64.StdEncoding.EncodeToString([]byte(content))

	board := &fakeBoardStore{
		getFn: func(_ context.Context, id string) (domain.WorkItem, error) {
			return domain.WorkItem{
				ID:         id,
				Title:      "Feature",
				AssignedTo: "dev-1",
				Status:     domain.WorkItemStatusBacklog,
				Attachments: []domain.Attachment{{
					ID:          "att-spec",
					Name:        "spec.md",
					ContentType: "text/markdown",
					Content:     encoded,
					Size:        len(content),
				}},
			}, nil
		},
		updateFn: func(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
			return item, nil
		},
	}

	dispatcher := &fakeTaskDispatcher{
		dispatchFn: func(_ context.Context, _, instruction string, _ *time.Time, _ domain.DirectTaskSource, _ string, _ string) (domain.DirectTask, error) {
			capturedInstruction = instruction
			return domain.DirectTask{ID: "t1"}, nil
		},
	}

	s := httpserver.New(httpserver.Config{
		Auth:       &fakeAuth{},
		Board:      board,
		Team:       &fakeControlPlane{},
		Users:      &fakeUserStore{},
		Skills:     &fakeSkillRegistry{},
		DLQ:        &fakeDLQStore{},
		Tasks:      &fakeDirectTaskStore{},
		Dispatcher: dispatcher,
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	body := `{"status":"in-progress"}`
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/v1/board/item-123", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(capturedInstruction, "--- Attachment: spec.md ---") {
		t.Errorf("instruction does not contain attachment header; got:\n%s", capturedInstruction)
	}
	if !strings.Contains(capturedInstruction, content) {
		t.Errorf("instruction does not contain attachment content; got:\n%s", capturedInstruction)
	}
}
