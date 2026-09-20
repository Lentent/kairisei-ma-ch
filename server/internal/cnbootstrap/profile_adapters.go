package cnbootstrap

import (
	"net/http"
)

func cnBootstrapSessionOnlyBusiness(
	businessHandler http.Handler,
	operation string,
	method string,
	requestPath string,
) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if err := requireCNSessionOnly(request, operation); err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		businessHandler.ServeHTTP(
			writer,
			adaptCNBusinessRequest(request, method, requestPath, nil),
		)
	}
}

func cnBootstrapExactBusiness(
	businessHandler http.Handler,
	operation string,
	requestPath string,
	expected ...string,
) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, operation)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		if err := requireCNExactFields(payload, operation, expected...); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		businessHandler.ServeHTTP(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, requestPath, payload),
		)
	}
}
