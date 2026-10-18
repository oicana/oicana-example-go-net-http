package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	oicana "go.oicana.com/oicana"
)

type certificateHandlers struct {
	templates *templateRegistry
}

func (h certificateHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /certificates", h.create)
}

type createCertificate struct {
	// Name to create the certificate for
	Name string `json:"name" example:"Jane Doe"`
} //@name CreateCertificate

// create godoc
//
//	@Description	Create a certificate
//	@Tags			certificates
//	@Accept			json
//	@Produce		application/pdf
//	@Param			payload	body		createCertificate	true	"Certificate details"
//	@Success		200		{file}		binary				"The compiled PDF certificate"
//	@Failure		400		{object}	errorResponse
//	@Router			/certificates [post]
func (h certificateHandlers) create(w http.ResponseWriter, r *http.Request) {
	template, ok := h.templates.get("certificate")
	if !ok {
		slog.Error("Certificate template not found!")
		writeError(w, http.StatusInternalServerError, "Certificate template not found!")
		return
	}

	var request createCertificate
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return
	}

	pdf, err := template.ExportPdf(oicana.Inputs{JSON: map[string]any{"certificate": request}})
	if err != nil {
		writeOicanaError(w, "certificate", err)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="certificate.pdf"`)
	w.Write(pdf)
}
