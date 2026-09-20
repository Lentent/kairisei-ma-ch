package cnbootstrap

import (
	"net/http"

	"kairisei.local/server/internal/accountstore"
)

type cnDeploymentHandler struct {
	http.Handler
	admin    http.Handler
	database *accountstore.Database
}

func (handler *cnDeploymentHandler) AdminHandler() http.Handler {
	return handler.admin
}

// Close is called by the service owner after HTTP/Admin/BattleSv have drained.
func (handler *cnDeploymentHandler) Close() error {
	return handler.database.Close()
}
