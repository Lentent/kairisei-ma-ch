package cnbootstrap

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func findCNPatchFile(patchRoots []string, relative string) (os.FileInfo, error) {
	for _, root := range patchRoots {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 {
			return nil, fmt.Errorf("official CN bundle %q is not a non-empty file", relative)
		}
		// Match both select_bundle in the generator and serveCNPatchFile:
		// earlier roots own duplicate paths, including different-sized official
		// variants. A shadowed copy must not change the catalog's selected size.
		return info, nil
	}
	return nil, os.ErrNotExist
}

func cnBootstrapPatchFile(patchRoots []string) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		serveCNPatchFile(writer, request, patchRoots, chi.URLParam(request, "*"))
	}
}

func cnBootstrapVersionedPatchFile(patchRoots []string, delivery []cnPatchDelivery) http.HandlerFunc {
	versions := make(map[string]string, len(delivery))
	for _, file := range delivery {
		versions[file.Name] = file.CRC
	}
	return func(writer http.ResponseWriter, request *http.Request) {
		versioned := chi.URLParam(request, "*")
		suffixAt := strings.LastIndex(versioned, ".v")
		if suffixAt <= 0 || len(versioned[suffixAt+2:]) != 8 {
			http.Error(writer, "invalid CN patch path", http.StatusBadRequest)
			return
		}
		if _, err := strconv.ParseUint(versioned[suffixAt+2:], 16, 32); err != nil {
			http.Error(writer, "invalid CN patch version", http.StatusBadRequest)
			return
		}
		if !strings.EqualFold(versions[versioned[:suffixAt]], versioned[suffixAt+2:]) {
			http.NotFound(writer, request)
			return
		}
		serveCNPatchFile(writer, request, patchRoots, versioned[:suffixAt])
	}
}

func serveCNPatchFile(writer http.ResponseWriter, request *http.Request, patchRoots []string, relative string) {
	clean := path.Clean(relative)
	if relative == "" || clean != relative || clean == "." || path.IsAbs(clean) ||
		strings.HasPrefix(clean, "../") || strings.Contains(clean, `\`) {
		http.Error(writer, "invalid CN patch path", http.StatusBadRequest)
		return
	}
	for _, root := range patchRoots {
		file, err := os.Open(filepath.Join(root, filepath.FromSlash(clean)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			http.Error(writer, "read CN patch file", http.StatusInternalServerError)
			return
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			_ = file.Close()
			http.Error(writer, "read CN patch file", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/octet-stream")
		writer.Header().Set("Cache-Control", "no-store")
		http.ServeContent(writer, request, path.Base(clean), info.ModTime(), file)
		_ = file.Close()
		return
	}
	http.NotFound(writer, request)
}

func cnBootstrapCatalog(content []byte) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/octet-stream")
		writer.Header().Set("Cache-Control", "no-store")
		_, _ = writer.Write(content)
	}
}
