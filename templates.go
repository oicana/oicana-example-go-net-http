package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
	oicana "go.oicana.com/oicana"
)

var templateVersions = []struct{ id, version string }{
	{"accessibility", "0.1.0"},
	{"certificate", "0.1.0"},
	{"dependency", "0.1.0"},
	{"fonts", "0.1.0"},
	{"invoice", "0.1.0"},
	{"invoice_zugferd", "0.1.0"},
	{"minimal", "0.1.0"},
	{"table", "0.1.0"},
	{"multi_input", "0.1.0"},
}

// A Template is safe for concurrent use, so the registry only guards the map.
type templateRegistry struct {
	mu        sync.RWMutex
	templates map[string]*oicana.Template
}

func warmUpTemplates(dir string) *templateRegistry {
	registry := &templateRegistry{templates: make(map[string]*oicana.Template, len(templateVersions))}
	for _, t := range templateVersions {
		path := filepath.Join(dir, fmt.Sprintf("%s-%s.zip", t.id, t.version))
		files, err := os.ReadFile(path)
		if err != nil {
			slog.Error("Template file not found during warm-up", "path", path, "error", err)
			continue
		}
		template, err := oicana.NewTemplate(files)
		if err != nil {
			slog.Error("Template failed to compile during warm-up", "path", path, "error", err)
			continue
		}
		registry.templates[t.id] = template
		slog.Info(fmt.Sprintf("Warmed-up %s v%s", t.id, t.version))
	}
	return registry
}

func (r *templateRegistry) get(id string) (*oicana.Template, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	template, ok := r.templates[id]
	return template, ok
}

func (r *templateRegistry) remove(id string) bool {
	r.mu.Lock()
	template, ok := r.templates[id]
	delete(r.templates, id)
	r.mu.Unlock()
	if ok {
		template.Close()
	}
	return ok
}

func (r *templateRegistry) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, template := range r.templates {
		template.Close()
		delete(r.templates, id)
	}
}

type templateHandlers struct {
	templates *templateRegistry
	blobs     *blobStore
}

func (h templateHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /templates", h.list)
	mux.HandleFunc("GET /templates/{templateID}", h.download)
	mux.HandleFunc("POST /templates/{templateID}/compile", h.compile)
	mux.HandleFunc("POST /templates/{templateID}/preview", h.preview)
	mux.HandleFunc("POST /templates/{templateID}/reset", h.reset)
}

const compilationPayloadExample = `{
	"jsonInputs": [{
		"key": "invoice",
		"value": {
			"id": "2025-03-10t172205",
			"issuingDate": "2025-04-27",
			"deliveryDate": "2025-04-19",
			"dueDate": "2025-05-06",
			"biller": {
				"name": "Alex Johnson",
				"title": "Senior Consultant",
				"company": "Example Solutions GmbH",
				"vat-id": "DE1234567",
				"iban": "DE89370400440532013000",
				"address": {"country": "Germany", "city": "Berlin", "postal-code": "10115", "street": "Sample Street 23"}
			},
			"recipient": {
				"name": "Maria Schmidt",
				"title": "IT Manager",
				"vat-id": "DE7654321",
				"address": {"country": "Germany", "city": "Munich", "postal-code": "80331", "street": "Business Avenue 1"}
			},
			"items": [
				{"date": "2025-04-03", "description": "Wireless keyboard", "quantity": 4, "price": 25},
				{"date": "2025-04-03", "description": "USB-C cable", "quantity": 2, "price": 45},
				{"date": "2025-04-03", "description": "Network infrastructure consultation", "dur-min": 120, "price": 200}
			]
		}
	}],
	"blobInputs": [{"key": "banner", "blobId": "00000000-0000-0000-0000-000000000000"}]
}`

type compilationPayload struct {
	JSONInputs []jsonInput `json:"jsonInputs"`
	BlobInputs []blobInput `json:"blobInputs"`
} //@name CompilationPayload

type jsonInput struct {
	// The input key
	Key string `json:"key" example:"invoice"`
	// Any JSON value, passed to the template as is
	Value json.RawMessage `json:"value" swaggertype:"object"`
} //@name JsonInput

type blobInput struct {
	// The input key for the blob
	Key string `json:"key" example:"banner"`
	// UUID of the blob from the blob storage
	BlobID uuid.UUID `json:"blobId" swaggertype:"string" format:"uuid" example:"00000000-0000-0000-0000-000000000000"`
} //@name BlobInput

// list godoc
//
//	@Description	Get a list of all template IDs known to the service.
//	@Tags			template
//	@Produce		json
//	@Success		200	{array}	string
//	@Router			/templates [get]
func (h templateHandlers) list(w http.ResponseWriter, r *http.Request) {
	ids := make([]string, len(templateVersions))
	for i, t := range templateVersions {
		ids[i] = t.id
	}
	writeJSON(w, http.StatusOK, ids)
}

// download godoc
//
//	@Description	Download a packed template.
//	@Tags			template
//	@Produce		application/zip
//	@Param			templateID	path		string	true	"The identifier of the template to download."	extensions(x-example=table)
//	@Success		200			{file}		binary
//	@Failure		404			{object}	errorResponse
//	@Router			/templates/{templateID} [get]
func (h templateHandlers) download(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("templateID")
	for _, t := range templateVersions {
		if t.id == id {
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, id))
			http.ServeFile(w, r, filepath.Join("templates", fmt.Sprintf("%s-%s.zip", t.id, t.version)))
			return
		}
	}
	writeError(w, http.StatusNotFound, "Template not found")
}

// compile godoc
//
//	@Description	Compile a template with given inputs.
//	@Tags			template
//	@Accept			json
//	@Produce		application/pdf
//	@Param			templateID	path		string				true	"The identifier of the template to compile."	extensions(x-example=invoice)
//	@Param			payload		body		compilationPayload	true	"Inputs for template compilation"
//	@Success		200			{file}		binary
//	@Failure		400			{object}	errorResponse
//	@Failure		404			{object}	errorResponse
//	@Router			/templates/{templateID}/compile [post]
func (h templateHandlers) compile(w http.ResponseWriter, r *http.Request) {
	h.export(w, r, pdfDocument)
}

// preview godoc
//
//	@Description	Generate a PNG preview of the template with given inputs.
//	@Tags			template
//	@Accept			json
//	@Produce		image/png
//	@Param			templateID	path		string				true	"The identifier of the template to preview."	extensions(x-example=invoice)
//	@Param			payload		body		compilationPayload	true	"Inputs for template compilation"
//	@Success		200			{file}		binary
//	@Failure		400			{object}	errorResponse
//	@Failure		404			{object}	errorResponse
//	@Router			/templates/{templateID}/preview [post]
func (h templateHandlers) preview(w http.ResponseWriter, r *http.Request) {
	h.export(w, r, pngPreview)
}

type output struct {
	format      oicana.Format
	mode        oicana.CompilationMode
	contentType string
	disposition string
	extension   string
}

var (
	pdfDocument = output{oicana.PDF, oicana.Production, "application/pdf", "attachment", "pdf"}
	pngPreview  = output{oicana.PNG(1), oicana.Development, "image/png", "inline", "png"}
)

func (h templateHandlers) export(w http.ResponseWriter, r *http.Request, out output) {
	id := r.PathValue("templateID")
	template, ok := h.templates.get(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Template '%s' not found!", id))
		return
	}

	var payload compilationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return
	}

	inputs := oicana.Inputs{
		JSON:  make(map[string]any, len(payload.JSONInputs)),
		Blobs: make(map[string]oicana.Blob, len(payload.BlobInputs)),
	}
	for _, input := range payload.JSONInputs {
		inputs.JSON[input.Key] = input.Value
	}
	for _, input := range payload.BlobInputs {
		data, ok := h.blobs.get(input.BlobID)
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"Blob with id %s not found. Please use an ID of a blob that was previously uploaded.", input.BlobID))
			return
		}
		inputs.Blobs[input.Key] = oicana.Blob{Data: data}
	}

	document, err := template.Export(inputs, out.format, oicana.WithMode(out.mode))
	if err != nil {
		writeOicanaError(w, id, err)
		return
	}

	w.Header().Set("Content-Type", out.contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s.%s"`, out.disposition, id, out.extension))
	w.Write(document)
}

// reset godoc
//
//	@Description	Remove a template from the cache and free it. The template is unavailable until the service restarts.
//	@Tags			template
//	@Param			templateID	path	string	true	"The identifier of the template to reset."	extensions(x-example=table)
//	@Success		204
//	@Failure		404
//	@Router			/templates/{templateID}/reset [post]
func (h templateHandlers) reset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("templateID")
	if !h.templates.remove(id) {
		slog.Error(fmt.Sprintf("Template '%s' not found in cache", id))
		w.WriteHeader(http.StatusNotFound)
		return
	}
	slog.Info(fmt.Sprintf("Template '%s' removed from cache", id))
	w.WriteHeader(http.StatusNoContent)
}

func writeOicanaError(w http.ResponseWriter, id string, err error) {
	switch {
	case errors.Is(err, oicana.ErrInvalidInput):
		slog.Error(fmt.Sprintf("Template '%s' received invalid input", id), "error", err)
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Template '%s' received invalid input: %v", id, err))
	case errors.Is(err, oicana.ErrCompilation):
		slog.Error(fmt.Sprintf("Template '%s' failed to compile with given inputs", id), "error", err)
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Template '%s' failed to compile with given inputs: %v", id, err))
	case errors.Is(err, oicana.ErrClosed):
		// The template was reset while this request was waiting for it.
		writeError(w, http.StatusNotFound, fmt.Sprintf("Template '%s' not found!", id))
	default:
		slog.Error(fmt.Sprintf("Template '%s' failed to export", id), "error", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Template '%s' failed to export!\n%v", id, err))
	}
}
