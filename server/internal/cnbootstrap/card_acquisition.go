package cnbootstrap

import (
	"net/http"

	adminapi "kairisei.local/server/internal/admin"
	"kairisei.local/server/internal/httpapi"
)

func cnBootstrapHowToGetCardShow(business http.Handler, operations *adminapi.Operations) http.HandlerFunc {
	withPublication := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		groups, err := operations.TeamBattleGroupAllowlist()
		if err != nil {
			http.Error(writer, "read local card acquisition publication", http.StatusInternalServerError)
			return
		}
		business.ServeHTTP(writer, httpapi.WithCardAcquisitionGroups(request, groups))
	})
	return cnBootstrapExactBusiness(withPublication, "HowToGetCardShow", "/HowToGetCardShow", "cardids")
}
