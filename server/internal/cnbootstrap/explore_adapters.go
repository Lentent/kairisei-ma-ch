package cnbootstrap

import (
	"encoding/json"
	"errors"
	"net/http"
)

func cnBootstrapExploreStart(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "ExploreStart")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(payload, &fields); err != nil || len(fields) != 2 {
			http.Error(writer, "invalid CN ExploreStart payload", http.StatusBadRequest)
			return
		}
		if _, exists := fields["arthur_type"]; !exists {
			http.Error(writer, "missing CN ExploreStart arthur_type", http.StatusBadRequest)
			return
		}
		if _, exists := fields["deck_idx"]; !exists {
			http.Error(writer, "missing CN ExploreStart deck_idx", http.StatusBadRequest)
			return
		}
		businessHandler.ServeHTTP(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/ExploreStart", payload),
		)
	}
}

func cnBootstrapExploreEnd(businessHandler http.Handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		payload, err := readCNSessionPayload(request, "ExploreEnd")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusUnauthorized)
			return
		}
		if len(payload) != 0 {
			var fields map[string]json.RawMessage
			if json.Unmarshal(payload, &fields) != nil || len(fields) != 0 {
				http.Error(writer, "invalid CN ExploreEnd payload", http.StatusBadRequest)
				return
			}
		}
		forwardCNBusiness(
			writer,
			adaptCNBusinessRequest(request, http.MethodPost, "/ExploreEnd", []byte(`{}`)),
			businessHandler,
			adaptCNExploreEndResponse,
		)
	}
}

func adaptCNExploreEndResponse(content []byte) ([]byte, error) {
	return adaptCNProtocolMethod(content, func(method map[string]json.RawMessage) (any, error) {
		if len(method["user"]) == 0 {
			return nil, errors.New("CN ExploreEnd response has no user")
		}
		if err := remapCNNamedRewardArrays(method); err != nil {
			return nil, err
		}
		return map[string]any{
			"user":                            method["user"],
			"result_rewards":                  rawOrEmptyArray(method["result_rewards"]),
			"deck_cards":                      rawOrEmptyArray(method["deck_cards"]),
			"new_cards":                       rawOrEmptyArray(method["new_cards"]),
			"new_stack_cards":                 rawOrEmptyArray(method["new_stack_cards"]),
			"new_items":                       rawOrEmptyArray(method["new_items"]),
			"new_sphrs":                       rawOrEmptyArray(method["new_sphrs"]),
			"new_buddys":                      rawOrEmptyArray(method["new_buddys"]),
			"is_result_reward_in_present_box": rawOrZero(method["is_result_reward_in_present_box"]),
			"unlock_notice":                   rawOrEmptyArray(method["unlock_notice"]),
		}, nil
	})
}
