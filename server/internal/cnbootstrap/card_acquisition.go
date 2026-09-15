package cnbootstrap

import (
	"net/http"

	"kairisei.local/server/internal/httpapi"
)

func cnBootstrapHowToGetCardShow(business http.Handler, operations *cnOperationStore) http.HandlerFunc {
	withPublication := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		groups, err := operations.teamBattleGroupAllowlist()
		if err != nil {
			http.Error(writer, "read local card acquisition publication", http.StatusInternalServerError)
			return
		}
		business.ServeHTTP(writer, httpapi.WithCardAcquisitionGroups(request, groups))
	})
	return cnBootstrapExactBusiness(withPublication, "HowToGetCardShow", "/HowToGetCardShow", "cardids")
}
