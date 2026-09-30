package cnbootstrap

import (
	"net/http"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/admin"
)

type cnDeploymentHandler struct {
	http.Handler
	admin      http.Handler
	database   *accountstore.Database
	operations *admin.Operations
}

func (handler *cnDeploymentHandler) AdminHandler() http.Handler {
	return handler.admin
}

// Close is called by the service owner after HTTP/Admin/BattleSv have drained.
func (handler *cnDeploymentHandler) Close() error {
	if handler.operations != nil {
		handler.operations.CloseMaintenance()
	}
	return handler.database.Close()
}
