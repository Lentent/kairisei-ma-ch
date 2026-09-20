package cnbootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/go-chi/chi/v5"
)

// The URL namespace tracks the wire catalogs, including per-file voice
// revisions, and refreshes cpk_file.csv when they change. version.dat stays 790.
func resourceVersionNamespace(catalog, cpkList, cpkState []byte) string {
	digest := sha256.New()
	for _, content := range [][]byte{catalog, cpkList, cpkState} {
		fmt.Fprintf(digest, "%d:", len(content))
		digest.Write(content)
	}
	return "resources-" + hex.EncodeToString(digest.Sum(nil))
}

func registerResourceVersions(router chi.Router, roots []string, catalog, cpkList, cpkState []byte) {
	// The namespace is a client cache token, not a filesystem selector.
	router.Route("/local/version/{namespace}", func(r chi.Router) {
		r.Get("/Android/patch/catalog.dat", cnBootstrapCatalog(catalog))
		r.Get("/Android/patch/*", cnBootstrapPatchFile(roots))
		r.Get("/CPK/cpk_file.csv", cnBootstrapCPKFileList(cpkList))
		r.Get("/CPK/cpk_patch.txt", cnBootstrapCPKPatchState(cpkState))
	})
}
