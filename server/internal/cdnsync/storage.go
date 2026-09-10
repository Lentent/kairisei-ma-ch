package cdnsync

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"kairisei.local/server/internal/cnbootstrap"
)

func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.DisableCompression = true
	transport.DialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = 60 * time.Second
	transport.MaxIdleConnsPerHost = 32
	return &http.Client{Transport: transport, Timeout: 15 * time.Minute,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			// SDK credentials must only reach the configured storage endpoint.
			return http.ErrUseLastResponse
		}}
}

func newS3(c config, client *http.Client) *s3.Client {
	options := s3.Options{
		Region: c.Region, HTTPClient: client, UsePathStyle: c.AddressingStyle == "path",
		RetryMaxAttempts:           4,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: c.AccessKeyID, SecretAccessKey: c.SecretAccessKey, SessionToken: c.SessionToken}, nil
		}),
	}
	if c.EndpointURL != "" {
		options.BaseEndpoint = aws.String(c.EndpointURL)
	}
	return s3.New(options)
}

func statusCode(err error) int {
	var status interface{ HTTPStatusCode() int }
	if errors.As(err, &status) {
		return status.HTTPStatusCode()
	}
	return 0
}

func networkError(err error) error {
	if errors.Is(err, context.Canceled) {
		return errors.New("同步已取消；重新运行即可继续")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("请求超时；检查网络或代理后重试")
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		return fmt.Errorf("存储请求失败 HTTP %d / %s；请检查桶、密钥权限、endpoint 和 region", statusCode(err), api.ErrorCode())
	}
	return errors.New("网络请求失败；请检查网络、TLS 证书及 HTTP_PROXY / HTTPS_PROXY / NO_PROXY 设置后重试")
}

func remoteMatches(ctx context.Context, client *s3.Client, bucket, key string, object cnbootstrap.CDNObject) (bool, error) {
	head, err := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: &key})
	if statusCode(err) == 404 {
		return false, nil
	}
	if err != nil {
		return false, networkError(err)
	}
	encoding := aws.ToString(head.ContentEncoding)
	if aws.ToInt64(head.ContentLength) != object.Bytes || head.Metadata["sha256"] != object.SHA256 || (encoding != "" && encoding != "identity") {
		return false, errors.New("远端同名对象与本地版本不一致，已拒绝覆盖；请更新资源版本或 prefix")
	}
	return true, nil
}

func syncObject(ctx context.Context, client *s3.Client, c config, object cnbootstrap.CDNObject) (bool, error) {
	key := object.Key
	if c.Prefix != "" {
		key = c.Prefix + "/" + key
	}
	match, err := remoteMatches(ctx, client, c.Bucket, key, object)
	if err != nil || match {
		return false, err
	}
	f, err := os.Open(object.SourcePath)
	if err != nil {
		return false, err
	}
	defer f.Close()
	sha, md := sha256.New(), md5.New() // MD5 is a transfer checksum, not authentication.
	buffer := make([]byte, 1024*1024)
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			return false, networkError(err)
		}
		n, readErr := f.Read(buffer)
		if n > 0 {
			sha.Write(buffer[:n])
			md.Write(buffer[:n])
			size += int64(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return false, readErr
		}
	}
	if size != object.Bytes || hex.EncodeToString(sha.Sum(nil)) != object.SHA256 {
		return false, errors.New("本地资源大小或 SHA-256 已变化，未上传")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &c.Bucket, Key: &key, Body: f, ContentLength: &object.Bytes,
		ContentMD5:  aws.String(base64.StdEncoding.EncodeToString(md.Sum(nil))),
		ContentType: aws.String("application/octet-stream"), CacheControl: aws.String("public, max-age=31536000, immutable"),
		Metadata: map[string]string{"sha256": object.SHA256}, IfNoneMatch: aws.String("*"),
	})
	// A concurrent uploader may have won; accept only the identical object.
	if statusCode(err) == 412 {
		match, headErr := remoteMatches(ctx, client, c.Bucket, key, object)
		if headErr != nil {
			return false, headErr
		}
		if match {
			return false, nil
		}
	}
	if err != nil {
		return false, networkError(err)
	}
	match, err = remoteMatches(ctx, client, c.Bucket, key, object)
	if err != nil {
		return false, err
	}
	if !match {
		return false, errors.New("上传后对象不可读，请稍后重新运行")
	}
	return true, nil
}

func publicSamples(objects []cnbootstrap.CDNObject) []cnbootstrap.CDNObject {
	selected := map[string]cnbootstrap.CDNObject{objects[0].Key: objects[0]}
	for _, prefix := range []string{"patch/", "cpk/"} {
		var largest cnbootstrap.CDNObject
		for _, object := range objects {
			if strings.HasPrefix(object.Key, prefix) && object.Bytes > largest.Bytes {
				largest = object
			}
		}
		if largest.Key != "" {
			selected[largest.Key] = largest
		}
	}
	for _, object := range objects {
		if object.Key == "cpk/CPK/cv_navi_5.cpk.v2" {
			selected[object.Key] = object
		}
	}
	result := make([]cnbootstrap.CDNObject, 0, len(selected))
	for _, object := range objects {
		if _, ok := selected[object.Key]; ok {
			result = append(result, object)
		}
	}
	return result
}

func checkPublic(ctx context.Context, client *http.Client, base string, object cnbootstrap.CDNObject) error {
	u, err := url.Parse(base)
	if err != nil {
		return errors.New("公开下载地址无效")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + object.Key
	u.RawPath = ""
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	head, err := http.NewRequestWithContext(ctx, http.MethodHead, u.String(), nil)
	if err != nil {
		return err
	}
	head.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(head)
	if err != nil {
		return networkError(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || response.ContentLength != object.Bytes || !identityEncoding(response.Header) {
		return fmt.Errorf("公开 HEAD 不匹配（HTTP %d），请确认桶公开可读、public_url 和 prefix 正确", response.StatusCode)
	}
	length := min(int64(64), object.Bytes)
	get, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	get.Header.Set("Accept-Encoding", "identity")
	get.Header.Set("Range", fmt.Sprintf("bytes=0-%d", length-1))
	response, err = client.Do(get)
	if err != nil {
		return networkError(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 206 || response.Header.Get("Content-Range") != fmt.Sprintf("bytes 0-%d/%d", length-1, object.Bytes) || !identityEncoding(response.Header) {
		return errors.New("公开地址未保留 Range 206；请关闭压缩／转码并允许字节范围下载")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, length+1))
	if err != nil {
		return networkError(err)
	}
	f, err := os.Open(object.SourcePath)
	if err != nil {
		return err
	}
	defer f.Close()
	expected := make([]byte, length)
	if _, err := io.ReadFull(f, expected); err != nil {
		return err
	}
	if !bytes.Equal(data, expected) {
		return errors.New("公开下载的原始字节与本地资源不同，未启用 CDN")
	}
	return nil
}

func identityEncoding(header http.Header) bool {
	value := header.Get("Content-Encoding")
	return value == "" || value == "identity"
}
