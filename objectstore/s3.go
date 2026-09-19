// Package objectstore 提供最小 S3 兼容对象存储客户端（AWS Signature V4，纯标准库，无第三方依赖）。
//
// 背景：read 域有 3 个服务真实使用对象存储——
//   - `acat-read-app-file`（读写，bucket `acat-read-files`）
//   - `acat-read-admin-file`（读写，bucket `acat-read-files`）
//   - `acat-read-app-comic`（只读代理漫画图，bucket `acat-comic-images`）
//
// 三者都用 AWS SDK v2 + `Region.US_EAST_1` + `forcePathStyle(true)` 访问 MinIO；
// 迁移到 Go 后把这份能力上提到公共库，避免每个服务各写一份 SigV4 签名。
//
// 对齐点：
//   - 区域固定 `us-east-1`、path-style；签名头 `host;x-amz-content-sha256;x-amz-date`（PUT 另加 `content-type`）；
//   - 数据库 `path` 字段形如 `<bucket>/<key>`，取 object key 时剥掉第一个 '/' 之前的内容；
//   - 失败一律返回错误。
package objectstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// emptyPayloadHash 是空请求体的 SHA-256（GET/DELETE 无 body）。
	emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	// region 与 Region.US_EAST_1 一致。
	region = "us-east-1"
	// service 是 SigV4 的 service 名。
	service = "s3"
)

// Client 是 S3 兼容客户端；endpoint 未配置时由调用方持有 nil 并按“对象存储不可用”处理。
type Client struct {
	endpoint  *url.URL
	accessKey string
	secretKey string
	bucket    string
	http      *http.Client
	now       func() time.Time
}

// Config 描述连接参数。
type Config struct {
	// Endpoint 形如 http://minio:9000。
	Endpoint string
	// AccessKey / SecretKey 静态凭据。
	AccessKey string
	SecretKey string
	// Bucket 桶名。
	Bucket string
	// Timeout 单次请求超时，默认 30s。
	Timeout time.Duration
}

// New 构造客户端；endpoint 为空时返回 (nil, nil)，由调用方按“对象存储不可用”处理。
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, nil
	}
	parsed, err := url.Parse(strings.TrimSpace(cfg.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("解析对象存储 endpoint 失败: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("对象存储 endpoint 非法: %s", cfg.Endpoint)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		endpoint:  parsed,
		accessKey: cfg.AccessKey,
		secretKey: cfg.SecretKey,
		bucket:    cfg.Bucket,
		http:      &http.Client{Timeout: timeout},
		now:       time.Now,
	}, nil
}

// Bucket 返回生效的桶名。
func (c *Client) Bucket() string {
	if c == nil {
		return ""
	}
	return c.bucket
}

// ObjectNameFromPath。
// 数据库存的是 `<bucket>/<key>`，取 key 时剥掉第一个 '/' 之前的内容。
func ObjectNameFromPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("对象存储路径为空")
	}
	if index := strings.Index(path, "/"); index >= 0 {
		return path[index+1:], nil
	}
	return path, nil
}

// Get 发起 SigV4 签名的 GetObject 请求，返回响应体（由调用方关闭）。
//
// 与 Java 的差异：Java 用 AWS SDK（自动重试、chunked 签名校验）；Go 侧只做单次签名请求，
// 把非 2xx 视为失败，语义（成功返回流、失败抛错 → HTTP 500）一致。
func (c *Client) Get(ctx context.Context, objectName string) (io.ReadCloser, error) {
	response, err := c.do(ctx, http.MethodGet, objectName, nil, nil, "")
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer func() { _ = response.Body.Close() }()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("对象存储返回 %d", response.StatusCode)
	}
	return response.Body, nil
}

// Put 上传对象（整体签名，payload 为内存字节）。
//
// 让 `x-amz-content-sha256` 覆盖真实内容，把请求体整体读入内存后再签名上传。
// 文件域（头像/封面/样例）体积有限，接单次请求足够；返回值语义与 Java 一致（失败抛错）。
func (c *Client) Put(ctx context.Context, objectName, contentType string, body []byte) error {
	response, err := c.do(ctx, http.MethodPut, objectName, body, nil, contentType)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("对象存储返回 %d", response.StatusCode)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

// Delete 删除对象；对象不存在时 S3 也返回 204，视为成功。
func (c *Client) Delete(ctx context.Context, objectName string) error {
	response, err := c.do(ctx, http.MethodDelete, objectName, nil, nil, "")
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("对象存储返回 %d", response.StatusCode)
	}
	return nil
}

// HeadBucket 探测桶是否存在；已存在返回 nil，404 返回 ErrBucketMissing。
func (c *Client) HeadBucket(ctx context.Context) error {
	response, err := c.doBucket(ctx, http.MethodHead)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode == http.StatusNotFound {
		return ErrBucketMissing
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("对象存储返回 %d", response.StatusCode)
	}
	return nil
}

// CreateBucket 创建桶。
func (c *Client) CreateBucket(ctx context.Context) error {
	response, err := c.doBucket(ctx, http.MethodPut)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("对象存储返回 %d", response.StatusCode)
	}
	return nil
}

// EnsureBucket。
// 其他异常只记录不阻断启动（调用方决定日志口径）。
func (c *Client) EnsureBucket(ctx context.Context) error {
	if c == nil {
		return errors.New("对象存储客户端未配置")
	}
	err := c.HeadBucket(ctx)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrBucketMissing) {
		return err
	}
	return c.CreateBucket(ctx)
}

// ErrBucketMissing 表示桶不存在（HeadBucket 404）。
var ErrBucketMissing = errors.New("对象存储桶不存在")

func (c *Client) do(ctx context.Context, method, objectName string, body []byte, query url.Values, contentType string) (*http.Response, error) {
	if c == nil {
		return nil, errors.New("对象存储客户端未配置")
	}
	request, err := c.newRequest(ctx, method, objectName, body, query, contentType)
	if err != nil {
		return nil, err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("请求对象存储失败: %w", err)
	}
	return response, nil
}

func (c *Client) doBucket(ctx context.Context, method string) (*http.Response, error) {
	if c == nil {
		return nil, errors.New("对象存储客户端未配置")
	}
	request, err := c.newBucketRequest(ctx, method)
	if err != nil {
		return nil, err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("请求对象存储失败: %w", err)
	}
	return response, nil
}

func (c *Client) newBucketRequest(ctx context.Context, method string) (*http.Request, error) {
	canonicalURI := "/" + c.bucket
	target := *c.endpoint
	prefix := strings.TrimSuffix(c.endpoint.Path, "/")
	target.Path = prefix + "/" + c.bucket
	target.RawPath = prefix + canonicalURI
	target.RawQuery = ""

	request, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("构造对象存储请求失败: %w", err)
	}
	// 方法必须与真实请求一致（HEAD=探测桶、PUT=建桶），否则签名不匹配。
	c.sign(request, canonicalURI, method, emptyPayloadHash, "")
	return request, nil
}

func (c *Client) newRequest(ctx context.Context, method, objectName string, body []byte, query url.Values, contentType string) (*http.Request, error) {
	// SigV4 的 CanonicalURI 必须是「已百分号编码」的路径；同时要让 net/http 原样发送该编码，
	// 因此 Path 放未编码值、RawPath 放编码值（否则会被二次转义成 %2520）。
	canonicalURI := "/" + c.bucket + "/" + escapePath(objectName)
	target := *c.endpoint
	prefix := strings.TrimSuffix(c.endpoint.Path, "/")
	target.Path = prefix + "/" + c.bucket + "/" + objectName
	target.RawPath = prefix + canonicalURI
	if query != nil {
		target.RawQuery = query.Encode()
	} else {
		target.RawQuery = ""
	}

	payloadHash := emptyPayloadHash
	var reader io.Reader
	if body != nil {
		sum := sha256.Sum256(body)
		payloadHash = hex.EncodeToString(sum[:])
		reader = bytes.NewReader(body)
	}

	request, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("构造对象存储请求失败: %w", err)
	}
	if body != nil {
		request.ContentLength = int64(len(body))
	}
	c.sign(request, canonicalURI, method, payloadHash, contentType)
	return request, nil
}

// sign 按 AWS SigV4 计算并写入 Authorization 头。
func (c *Client) sign(request *http.Request, canonicalURI, method, payloadHash, contentType string) {
	amzDate := c.now().UTC()
	dateStamp := amzDate.Format("20060102")
	amzDateString := amzDate.Format("20060102T150405Z")
	host := request.URL.Host

	// SigV4 要求 CanonicalHeaders 与 SignedHeaders 都按「头名小写字典序」排列：
	// `content-type` < `host` < `x-amz-content-sha256` < `x-amz-date`。把 host 写在
	// content-type 之前会让带 Content-Type 的 PUT 签名不匹配（MinIO 返回
	// SignatureDoesNotMatch），而 GET/HEAD（无 content-type）看起来完全正常。
	canonicalHeaders := ""
	signedHeaders := ""
	if contentType != "" {
		canonicalHeaders += "content-type:" + contentType + "\n"
		signedHeaders = "content-type"
	}
	canonicalHeaders += "host:" + host + "\n"
	if signedHeaders == "" {
		signedHeaders = "host"
	} else {
		signedHeaders += ";host"
	}
	canonicalHeaders += "x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDateString + "\n"
	signedHeaders += ";x-amz-content-sha256;x-amz-date"

	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI,
		request.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := strings.Join([]string{dateStamp, region, service, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDateString,
		scope,
		sha256Hex(canonicalRequest),
	}, "\n")

	signature := hex.EncodeToString(hmacSHA256(c.signingKey(dateStamp), stringToSign))

	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	request.Header.Set("x-amz-date", amzDateString)
	request.Header.Set("x-amz-content-sha256", payloadHash)
	request.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.accessKey, scope, signedHeaders, signature))
}

// signingKey 推导 SigV4 签名密钥。
func (c *Client) signingKey(dateStamp string) []byte {
	dateKey := hmacSHA256([]byte("AWS4"+c.secretKey), dateStamp)
	regionKey := hmacSHA256(dateKey, region)
	serviceKey := hmacSHA256(regionKey, service)
	return hmacSHA256(serviceKey, "aws4_request")
}

// escapePath 按 AWS SigV4 规则编码对象 key（保留 '/'，其余按 RFC3986 百分号编码）。
func escapePath(objectName string) string {
	segments := strings.Split(objectName, "/")
	for index, segment := range segments {
		segments[index] = awsEscape(segment)
	}
	return strings.Join(segments, "/")
}

func awsEscape(value string) string {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~"
	var builder strings.Builder
	for _, b := range []byte(value) {
		if strings.IndexByte(unreserved, b) >= 0 {
			builder.WriteByte(b)
			continue
		}
		fmt.Fprintf(&builder, "%%%02X", b)
	}
	return builder.String()
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}
