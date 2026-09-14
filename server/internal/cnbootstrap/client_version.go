package cnbootstrap

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// LOCAL_POLICY: installation versions are separate from catalog/CPK revisions.
// Keep the release name in sync with CNLocalClientRecipe.psd1 when publishing.
const cnMinimumClientVersion = "6.0.3"
const cnClientReleaseURL = "https://github.com/kuuhaku1314/kairisei-ma-ch/releases"

func cnClientVersionAllowed(version string) bool {
	parse := func(value string) ([3]uint64, bool) {
		var result [3]uint64
		parts := strings.Split(value, ".")
		if len(parts) != len(result) {
			return result, false
		}
		for i, part := range parts {
			n, err := strconv.ParseUint(part, 10, 16)
			if err != nil || strconv.FormatUint(n, 10) != part {
				return result, false
			}
			result[i] = n
		}
		return result, true
	}
	actual, ok := parse(version)
	minimum, _ := parse(cnMinimumClientVersion)
	if !ok {
		return false
	}
	for i := range actual {
		if actual[i] != minimum[i] {
			return actual[i] > minimum[i]
		}
	}
	return true
}

func cnClientUpdateTips() string {
	return "请安装 " + cnMinimumClientVersion + " 或更新的客户端。更新内容和客户端下载均在 GitHub Releases；请覆盖安装以保留数据。"
}

func cnBootstrapServerList(host string, port int) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		rows := csv.NewWriter(writer)
		// CONFIRMED: ExServerItem matches explicit version strings before the
		// ALL fallback; Title.doStartLogin blocks when updateUrl is nonempty.
		// A later released APK must be added here when its server is published.
		_ = rows.Write([]string{"ALL", "0", "本地服务器", host, strconv.Itoa(port), "0", "", cnMinimumClientVersion, "", "", ""})
		_ = rows.Write([]string{"ALL", "0", "请更新客户端", host, strconv.Itoa(port), "0", "", "ALL", cnClientReleaseURL, cnClientUpdateTips(), ""})
		rows.Flush()
	}
}

func cnRejectOldClient(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"res_code": -1, "res_str": cnClientUpdateTips() + "\n" + cnClientReleaseURL,
		"update_url": cnClientReleaseURL,
	})
}
