package main

import (
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

func editError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errEditConflict):
		writeError(w, 409, "edit_conflict", "This file changed since you opened it. Your edits are kept here; reopen the file to load the latest version before saving.")
	case errors.Is(err, errEditTooLarge):
		writeError(w, 413, "edit_limit", "The browser editor supports files up to 1 MiB. Use External edit for larger files.")
	case errors.Is(err, errNotText):
		writeError(w, 415, "not_text", "The browser editor supports UTF-8 text files only. Use External edit for this file.")
	case errors.Is(err, errEditLink):
		writeError(w, 400, "linked_file", "Linked files cannot be edited in the browser. Use External edit instead.")
	default:
		fileError(w, err)
	}
}

func registerEditRoutes(mux *http.ServeMux, cfg Config, root *os.Root) {
	mux.HandleFunc("GET /api/edit", func(w http.ResponseWriter, r *http.Request) {
		name, err := queryPath(r)
		if err != nil {
			editError(w, err)
			return
		}
		doc, err := ReadText(root, name)
		if err != nil {
			editError(w, err)
			return
		}
		writeJSON(w, doc)
	})
	mux.HandleFunc("PUT /api/edit", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Content  *string `json:"content"`
			Revision string  `json:"revision"`
		}
		if !decodeMutation(w, r, 6*maxEditBytes+4096, &input) {
			return
		}
		name, err := queryPath(r)
		if err != nil {
			editError(w, err)
			return
		}
		if input.Content == nil || input.Revision == "" {
			writeError(w, 400, "invalid_edit", "Content and the original revision are required.")
			return
		}
		saved, err := SaveText(root, name, input.Revision, *input.Content)
		if err != nil {
			editError(w, err)
			return
		}
		writeJSON(w, saved)
	})
	mux.HandleFunc("GET /api/external", func(w http.ResponseWriter, r *http.Request) {
		name, err := queryPath(r)
		if err != nil {
			fileError(w, err)
			return
		}
		file, _, err := OpenDownload(root, name)
		if err != nil {
			fileError(w, err)
			return
		}
		file.Close()
		info, err := root.Stat(".")
		if err != nil {
			fileError(w, err)
			return
		}
		account, err := user.LookupId(strconv.FormatUint(uint64(info.Sys().(*syscall.Stat_t).Uid), 10))
		if err != nil {
			fileError(w, fs.ErrPermission)
			return
		}
		host, _, _ := net.SplitHostPort(cfg.ListenAddr)
		absolute := filepath.Join(root.Name(), name)
		link := (&url.URL{Scheme: "sftp", User: url.User(account.Username), Host: net.JoinHostPort(host, "22"), Path: absolute}).String()
		writeJSON(w, map[string]any{"host": host, "port": 22, "username": account.Username, "path": absolute, "url": link})
	})
}
