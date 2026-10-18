package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

var handler http.Handler

func TestMain(m *testing.M) {
	blobDir, err := os.MkdirTemp("", "blobs")
	if err != nil {
		panic(err)
	}
	if err := os.CopyFS(blobDir, os.DirFS("blobs")); err != nil {
		panic(err)
	}

	templates := warmUpTemplates("templates")
	handler = newHandler(templates, newBlobStore(blobDir))
	code := m.Run()
	templates.close()
	os.RemoveAll(blobDir)
	os.Exit(code)
}

func serve(method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func assertResponse(t *testing.T, response *httptest.ResponseRecorder, status int, contentType string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("expected status %d, got %d: %s", status, response.Code, response.Body)
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, contentType) {
		t.Fatalf("expected content type %q, got %q", contentType, got)
	}
}

func TestRoot(t *testing.T) {
	response := serve(http.MethodGet, "/", "", nil)
	assertResponse(t, response, http.StatusOK, "text/html")
	if !strings.Contains(response.Body.String(), "/swagger/") {
		t.Fatalf("expected a link to the swagger documentation, got %s", response.Body)
	}
}

func TestSwagger(t *testing.T) {
	assertResponse(t, serve(http.MethodGet, "/swagger/index.html", "", nil), http.StatusOK, "text/html")
	assertResponse(t, serve(http.MethodGet, "/swagger/doc.json", "", nil), http.StatusOK, "application/json")
}

func TestListTemplates(t *testing.T) {
	response := serve(http.MethodGet, "/templates", "", nil)
	assertResponse(t, response, http.StatusOK, "application/json")
	var ids []string
	if err := json.Unmarshal(response.Body.Bytes(), &ids); err != nil {
		t.Fatal(err)
	}
	if len(ids) != len(templateVersions) {
		t.Fatalf("expected %d templates, got %v", len(templateVersions), ids)
	}
}

func TestDownloadTemplate(t *testing.T) {
	assertResponse(t, serve(http.MethodGet, "/templates/table", "", nil), http.StatusOK, "application/zip")
	assertResponse(t, serve(http.MethodGet, "/templates/unknown", "", nil), http.StatusNotFound, "application/json")
}

func TestCompileSwaggerExample(t *testing.T) {
	var doc struct {
		Definitions map[string]struct {
			Example json.RawMessage `json:"example"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(serve(http.MethodGet, "/swagger/doc.json", "", nil).Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	payload := doc.Definitions["CompilationPayload"].Example

	response := serve(http.MethodPost, "/templates/invoice/compile", "application/json", payload)
	assertResponse(t, response, http.StatusOK, "application/pdf")
	if !bytes.HasPrefix(response.Body.Bytes(), []byte("%PDF")) {
		t.Fatal("expected a PDF document")
	}
	assertResponse(t, serve(http.MethodPost, "/templates/invoice/preview", "application/json", payload),
		http.StatusOK, "image/png")
}

func TestCompileErrors(t *testing.T) {
	tests := []struct {
		name, path, payload string
		status              int
	}{
		{"unknown template", "/templates/unknown/compile", `{"jsonInputs": []}`, http.StatusNotFound},
		{"malformed body", "/templates/table/compile", `{"jsonInputs": `, http.StatusBadRequest},
		{"unknown blob", "/templates/minimal/compile",
			`{"blobInputs": [{"key": "logo", "blobId": "11111111-1111-1111-1111-111111111111"}]}`, http.StatusBadRequest},
		{"undeclared input", "/templates/table/compile", `{"jsonInputs": [{"key": "unknown", "value": {}}]}`,
			http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serve(http.MethodPost, test.path, "application/json", []byte(test.payload))
			assertResponse(t, response, test.status, "application/json")
		})
	}
}

func TestCreateCertificate(t *testing.T) {
	response := serve(http.MethodPost, "/certificates", "application/json", []byte(`{"name": "Test User"}`))
	assertResponse(t, response, http.StatusOK, "application/pdf")
}

func TestUploadBlob(t *testing.T) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatal(err)
	}
	file.Write([]byte("test content"))
	form.Close()

	response := serve(http.MethodPost, "/blobs", form.FormDataContentType(), body.Bytes())
	assertResponse(t, response, http.StatusOK, "application/json")
	var upload struct{ ID string }
	if err := json.Unmarshal(response.Body.Bytes(), &upload); err != nil || upload.ID == "" {
		t.Fatalf("expected an id, got %s", response.Body)
	}
}

func TestResetTemplate(t *testing.T) {
	// Runs last, since it removes a template from the shared handler.
	if response := serve(http.MethodPost, "/templates/fonts/reset", "", nil); response.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", response.Code)
	}
	if response := serve(http.MethodPost, "/templates/fonts/reset", "", nil); response.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.Code)
	}
	assertResponse(t, serve(http.MethodPost, "/templates/fonts/compile", "application/json", []byte(`{}`)),
		http.StatusNotFound, "application/json")
}
