package cnbootstrap

import (
	"encoding/json"
	"errors"
	"net/http"
)

func cnBootstrapMissionShow(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "MissionShow")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		if err := requireCNExactFields(payload, "MissionShow", "is_reward"); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		businessHandler.ServeHTTP(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/MissionShow", payload),
		)
	}
}

func cnBootstrapMissionReward(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "MissionReward")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		if err := requireCNExactFields(payload, "MissionReward", "missionids"); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		businessHandler.ServeHTTP(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/MissionReward", payload),
		)
	}
}

func cnBootstrapPresentBoxShow(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if err := requireCNSessionOnly(request, "PresentBoxShow"); err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		businessHandler.ServeHTTP(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/PresentBoxShow", nil),
		)
	}
}

func cnBootstrapPresentBoxRecv(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "PresentBoxRecv")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		if err := requireCNExactFields(payload, "PresentBoxRecv", "presentid"); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/PresentBoxRecv", payload),
			businessHandler,
			adaptCNPresentReceiveResponse,
		)
	}
}

func cnBootstrapPresentBoxMultiRecv(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "PresentBoxMultiRecv2")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		if err := requireCNExactFields(payload, "PresentBoxMultiRecv2", "is_coin_recv", "receive_types"); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/PresentBoxMultiRecv2", payload),
			businessHandler,
			adaptCNPresentReceiveResponse,
		)
	}
}

func requireCNExactFields(payload []byte, operation string, expected ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil || len(fields) != len(expected) {
		return errors.New("invalid CN " + operation + " payload")
	}
	for _, name := range expected {
		if _, exists := fields[name]; !exists {
			return errors.New("missing CN " + operation + " field: " + name)
		}
	}
	return nil
}

func adaptCNPresentReceiveResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		if len(method["user"]) == 0 {
			return nil, errors.New("CN present receive response has no user")
		}
		if err := remapCNNamedRewardArrays(method); err != nil {
			return nil, err
		}
		return map[string]any{
			"user":               method["user"],
			"result_rewards":     rawOrEmptyArray(method["result_rewards"]),
			"new_cards":          rawOrEmptyArray(method["new_cards"]),
			"new_stack_cards":    rawOrEmptyArray(method["new_stack_cards"]),
			"new_items":          rawOrEmptyArray(method["new_items"]),
			"new_sphrs":          rawOrEmptyArray(method["new_sphrs"]),
			"new_buddys":         rawOrEmptyArray(method["new_buddys"]),
			"new_stampids":       rawOrEmptyArray(method["new_stampids"]),
			"presentid":          rawOrEmptyArray(method["presentid"]),
			"failed_presentid":   rawOrEmptyArray(method["failed_presentid"]),
			"auto_fusion_result": rawOrEmptyArray(method["auto_fusion_result"]),
			"auto_loveup_result": rawOrEmptyArray(method["auto_loveup_result"]),
		}, nil
	})
}
