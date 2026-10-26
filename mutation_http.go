package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
)

// Browser mutations require JSON and a custom header: cross-site forms cannot
// supply them, and this service does not grant cross-origin preflight access.
func decodeMutation(w http.ResponseWriter, r *http.Request, limit int64, input any) bool {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if r.Header.Get("X-Dashboard-Edit") != "1" || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != scheme+"://"+r.Host) {
		writeError(w, 403, "origin_required", "Make changes from the dashboard page on this device.")
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, 415, "json_required", "Send the request as JSON.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(input)
	if err == nil {
		var extra any
		err = decoder.Decode(&extra)
		if err == io.EOF {
			err = nil
		} else if err == nil {
			err = errors.New("extra JSON")
		}
	}
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			writeError(w, 413, "request_limit", "The request body is too large.")
		} else {
			writeError(w, 400, "invalid_request", "Send one JSON object with the required fields.")
		}
		return false
	}
	return true
}

func entryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fs.ErrExist):
		writeError(w, 409, "already_exists", "A file or folder with this name already exists. Choose another name.")
	case errors.Is(err, errEntryConfirmation):
		writeError(w, 400, "name_confirmation", "Enter the exact entry name to confirm deletion.")
	case errors.Is(err, errEditConflict):
		writeError(w, 409, "entry_changed", "This entry changed after you opened the confirmation. Cancel and try again.")
	default:
		fileError(w, err)
	}
}

func registerManageRoutes(mux *http.ServeMux, root *os.Root) {
	mux.HandleFunc("GET /api/entry", func(w http.ResponseWriter, r *http.Request) {
		path, err := queryPath(r)
		if err != nil {
			entryError(w, err)
			return
		}
		entry, err := InspectEntry(root, path)
		if err != nil {
			entryError(w, err)
			return
		}
		writeJSON(w, entry)
	})
	mux.HandleFunc("POST /api/entry", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Kind string `json:"kind"`
		}
		if !decodeMutation(w, r, 4096, &input) {
			return
		}
		path, err := queryPath(r)
		if err != nil {
			entryError(w, err)
			return
		}
		entry, err := CreateEntry(root, path, input.Kind)
		if err != nil {
			entryError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, entry)
	})
	mux.HandleFunc("DELETE /api/entry", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Revision string `json:"revision"`
			Name     string `json:"name"`
		}
		if !decodeMutation(w, r, 4096, &input) {
			return
		}
		if input.Revision == "" || input.Name == "" {
			writeError(w, 400, "invalid_request", "The entry revision and confirmed name are required.")
			return
		}
		path, err := queryPath(r)
		if err != nil {
			entryError(w, err)
			return
		}
		if err := DeleteEntry(root, path, input.Revision, input.Name); err != nil {
			entryError(w, err)
			return
		}
		writeJSON(w, map[string]string{"status": "deleted"})
	})
}
