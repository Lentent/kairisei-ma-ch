// Package cdnsync provides the optional, short-lived resource upload command.
// Normal server startup never loads its credentials or creates an S3 client.
package cdnsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"

	"kairisei.local/server/internal/cnbootstrap"
)

type config struct {
	EndpointURL     string `json:"endpoint_url"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	PublicURL       string `json:"public_url"`
	Region          string `json:"region"`
	Prefix          string `json:"prefix"`
	AddressingStyle string `json:"addressing_style"`
	SessionToken    string `json:"session_token"`
	Workers         int    `json:"workers"`
}

func loadConfig(filename string) (config, error) {
	c := config{Region: "auto", Prefix: "cn602", AddressingStyle: "path", Workers: 8}
	f, err := os.Open(filename)
	if errors.Is(err, os.ErrNotExist) {
		return c, errors.New("请将 cdn-sync.example.json 复制为 cdn-sync.json，填写五项配置后重新运行")
	}
	if err != nil {
		return c, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if err != nil {
		return c, err
	}
	if len(data) > 64*1024 {
		return c, errors.New("CDN 同步配置超过 64KB")
	}
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return c, errors.New("CDN 同步 JSON 格式错误或含未知字段，请对照示例（末项不要加逗号）")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return c, errors.New("CDN 同步配置只能包含一个 JSON 对象")
	}
	for name, value := range map[string]string{"endpoint_url": c.EndpointURL, "bucket": c.Bucket, "access_key_id": c.AccessKeyID, "secret_access_key": c.SecretAccessKey, "public_url": c.PublicURL} {
		if strings.Contains(value, "YOUR_") || strings.ContainsAny(value, "<>\r\n") {
			return c, fmt.Errorf("请填写 %s，不能保留示例占位符", name)
		}
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{1,62}$`).MatchString(c.Bucket) {
		return c, errors.New("请填写正确的 bucket 桶名称")
	}
	if c.EndpointURL != "" {
		c.EndpointURL, err = cnbootstrap.NormalizeCDNBaseURL(c.EndpointURL)
		if err != nil {
			return c, errors.New("endpoint_url 必须是存储服务提供的 HTTP(S) S3 API 地址")
		}
	} else if c.Region == "" || c.Region == "auto" {
		return c, errors.New("请填写 endpoint_url；只有 AWS S3 可省略，并须填写实际 region")
	}
	c.PublicURL, err = cnbootstrap.NormalizeCDNBaseURL(c.PublicURL)
	if err != nil || c.PublicURL == "" {
		return c, errors.New("请填写 public_url：桶的公开下载根地址，不含 cn602 后缀")
	}
	u, _ := url.Parse(c.PublicURL)
	if strings.HasSuffix(strings.ToLower(u.Hostname()), ".r2.cloudflarestorage.com") || c.PublicURL == c.EndpointURL {
		return c, errors.New("public_url 不能使用 S3 上传地址；R2 请填已启用的 r2.dev 地址或自定义域名")
	}
	c.Prefix = strings.Trim(c.Prefix, "/")
	if c.Prefix != "" && (path.Clean(c.Prefix) != c.Prefix || c.Prefix == "." || c.Prefix == ".." || strings.HasPrefix(c.Prefix, "../") || strings.ContainsAny(c.Prefix, "\\:?#% \r\n\t")) {
		return c, errors.New("prefix 必须是普通相对目录，不能包含 ..、空白或 URL 转义字符")
	}
	if c.Region == "" {
		return c, errors.New("region 不能为空；R2 使用 auto")
	}
	if c.AddressingStyle != "path" && c.AddressingStyle != "virtual" && c.AddressingStyle != "auto" {
		return c, errors.New("addressing_style 只能是 path、virtual 或 auto")
	}
	if c.Workers < 1 || c.Workers > 32 {
		return c, errors.New("workers 必须为 1–32，默认 8")
	}
	if c.AccessKeyID == "" && c.SecretAccessKey == "" {
		c.AccessKeyID, c.SecretAccessKey = os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY")
		if c.SessionToken == "" {
			c.SessionToken = os.Getenv("AWS_SESSION_TOKEN")
		}
	}
	if c.AccessKeyID == "" || c.SecretAccessKey == "" {
		return c, errors.New("请填写 access_key_id 和 secret_access_key（不是 Cloudflare 令牌值），或同时设置 AWS 凭据环境变量")
	}
	return c, nil
}

func (c config) baseURL() string {
	if c.Prefix == "" {
		return c.PublicURL
	}
	return c.PublicURL + "/" + c.Prefix
}

// SDK/network errors can contain URLs (including proxy credentials). Do not
// expose them in progress or receipts; report status/API code at the boundary.
func (c config) redact(message string) string {
	for _, secret := range []string{c.AccessKeyID, c.SecretAccessKey, c.SessionToken} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return message
}
