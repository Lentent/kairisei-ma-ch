package cnbootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCNClientVersionGateBeforeAccountCreation(t *testing.T) {
	for _, version := range []string{"", "6.0.2", "6.0.3", "5.9.99", "6.0", "garbage", "6.0.03", "6.0.4.0", "6.0.65536"} {
		body, _ := json.Marshal(map[string]string{"uuid": "00000000-0000-4000-8000-00000000a501", "clver": version})
		response := httptest.NewRecorder()
		// A nil account store proves rejected clients cannot mint a session.
		cnBootstrapLogin(nil, "127.0.0.1", 26020, CDNConfig{})(response, httptest.NewRequest(http.MethodPost, "/loginSDK.php", strings.NewReader(string(body))))
		var data map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil || data["res_code"] != float64(-1) || data["update_url"] != cnClientReleaseURL || data["sess_key"] != nil {
			t.Fatalf("version %q was not rejected with an update link: %s (%v)", version, response.Body.String(), err)
		}
	}
	for _, version := range []string{"6.0.4", "6.0.10", "6.1.0", "7.0.0"} {
		if !cnClientVersionAllowed(version) {
			t.Fatalf("valid newer version %s rejected", version)
		}
	}
}
