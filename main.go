// @title						Oicana example
// @version					1.0
// @description				Go net/http example with Oicana
// @license.name				MIT
// @license.url				https://github.com/oicana/oicana-example-go-net-http/blob/main/LICENSE
// @externalDocs.description	General documentation for Oicana.
// @externalDocs.url			https://oicana.com/docs/
// @tag.name					template
// @tag.description			Template API endpoints. Find used templates at https://github.com/oicana/oicana-example-templates.
// @tag.name					certificates
// @tag.description			Create certificates
// @tag.name					blob
// @tag.description			Blob storage endpoints. Upload files (images, documents) to use as template inputs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpSwagger "github.com/swaggo/http-swagger/v2"
	oicana "go.oicana.com/oicana"

	_ "github.com/oicana/oicana-example-go-net-http/docs"
)

const address = "127.0.0.1:3006"

func main() {
	oicana.ConfigureDiagnosticColor(oicana.NoColor)

	templates := warmUpTemplates("templates")
	defer templates.close()

	server := &http.Server{
		Addr:              address,
		Handler:           newHandler(templates, newBlobStore("blobs")),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("Failed to shut down the server", "error", err)
		}
	}()

	slog.Info(fmt.Sprintf("API docs at http://%s/swagger/", address))
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
	slog.Info("Server shut down")
}

func newHandler(templates *templateRegistry, blobs *blobStore) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `Visit the swagger documentation at <a href="/swagger/">/swagger/</a>`)
	})
	mux.Handle("GET /swagger/", httpSwagger.Handler())

	templateHandlers{templates: templates, blobs: blobs}.register(mux)
	certificateHandlers{templates: templates}.register(mux)
	blobHandlers{blobs: blobs}.register(mux)

	return logRequests(mux)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(recorder, r)
		slog.Info("Request handled", "method", r.Method, "path", r.URL.Path, "status", recorder.status,
			"duration", time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

type errorResponse struct {
	Message string `json:"message"`
} //@name ErrorResponse

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("Failed to write the response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Message: message})
}
