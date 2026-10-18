package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

type blobStore struct {
	dir   string
	mu    sync.RWMutex
	blobs map[uuid.UUID][]byte
}

func newBlobStore(dir string) *blobStore {
	return &blobStore{dir: dir, blobs: make(map[uuid.UUID][]byte)}
}

func (s *blobStore) get(id uuid.UUID) ([]byte, bool) {
	s.mu.RLock()
	data, ok := s.blobs[id]
	s.mu.RUnlock()
	if ok {
		return data, true
	}

	path := filepath.Join(s.dir, id.String())
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Warn(fmt.Sprintf("Failed to read blob %s", id), "path", path, "error", err)
		return nil, false
	}
	s.mu.Lock()
	s.blobs[id] = data
	s.mu.Unlock()
	return data, true
}

func (s *blobStore) add(data []byte) (uuid.UUID, error) {
	id := uuid.New()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return uuid.Nil, err
	}
	if err := os.WriteFile(filepath.Join(s.dir, id.String()), data, 0o644); err != nil {
		return uuid.Nil, err
	}
	s.mu.Lock()
	s.blobs[id] = data
	s.mu.Unlock()
	return id, nil
}

type blobHandlers struct {
	blobs *blobStore
}

func (h blobHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /blobs", h.upload)
}

type uploadResponse struct {
	// The UUID assigned to the uploaded blob
	ID uuid.UUID `json:"id" swaggertype:"string" format:"uuid" example:"550e8400-e29b-41d4-a716-446655440000"`
} //@name UploadResponse

// upload godoc
//
//	@Description	Upload a blob (image, file, etc.) to use as template input. Returns a UUID to reference the blob in compilation requests.
//	@Tags			blob
//	@Accept			multipart/form-data
//	@Produce		json
//	@Param			file	formData	file			true	"The file to upload"
//	@Success		200		{object}	uploadResponse	"Blob uploaded successfully"
//	@Failure		400		{object}	errorResponse	"Invalid file upload"
//	@Failure		500		{object}	errorResponse	"Failed to save file to disk"
//	@Router			/blobs [post]
func (h blobHandlers) upload(w http.ResponseWriter, r *http.Request) {
	file, _, err := r.FormFile("file")
	if err != nil {
		slog.Error("Failed to read the uploaded file", "error", err)
		writeError(w, http.StatusBadRequest, "No file field provided")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		slog.Error("Failed to read the uploaded file", "error", err)
		writeError(w, http.StatusBadRequest, "Failed to read file")
		return
	}

	id, err := h.blobs.add(data)
	if err != nil {
		slog.Error("Failed to store the blob", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to save file")
		return
	}
	slog.Info(fmt.Sprintf("Stored blob %s to disk and cache", id))
	writeJSON(w, http.StatusOK, uploadResponse{ID: id})
}
