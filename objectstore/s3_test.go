package objectstore

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestObjectNameFromPath。
func TestObjectNameFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"acat-comic-images/acat-fun/read/comic/chapter/a.webp", "acat-fun/read/comic/chapter/a.webp"},
		{"acat-read-files/acat-fun/read/user/avatar/a.png", "acat-fun/read/user/avatar/a.png"},
		{"bucket/key.webp", "key.webp"},
		{"nobucket.webp", "nobucket.webp"},
	}
	for _, item := range cases {
		got, err := ObjectNameFromPath(item.path)
		if err != nil {
			t.Fatalf("ObjectNameFromPath(%q) 返回错误: %v", item.path, err)
		}
		if got != item.want {
			t.Fatalf("ObjectNameFromPath(%q) = %q, 期望 %q", item.path, got, item.want)
		}
	}
	for _, blank := range []string{"", "   ", "\t"} {
		if _, err := ObjectNameFromPath(blank); err == nil {
			t.Fatalf("空白路径 %q 应该报错", blank)
		}
	}
}

// TestNewWithoutEndpoint 未配置 endpoint 时返回 nil 客户端（调用方按“对象存储不可用”处理）。
func TestNewWithoutEndpoint(t *testing.T) {
	client, err := New(Config{})
	if err != nil {
		t.Fatalf("New 返回错误: %v", err)
	}
	if client != nil {
		t.Fatal("endpoint 为空时应返回 nil 客户端")
	}
}

// TestBucket 暴露桶名（健康检查/排障用）。
func TestBucket(t *testing.T) {
	client, err := New(Config{Endpoint: "http://127.0.0.1:9000", Bucket: "acat-comic-images"})
	if err != nil {
		t.Fatalf("New 返回错误: %v", err)
	}
	if client.Bucket() != "acat-comic-images" {
		t.Fatalf("Bucket() = %q", client.Bucket())
	}
	var nilClient *Client
	if nilClient.Bucket() != "" {
		t.Fatal("nil 客户端 Bucket() 应为空串")
	}
}

// newStubClient 构造一个 endpoint 固定为 127.0.0.1:9000（让签名可复算）、
// 时间固定、并把真实请求转发到 httptest 服务器的客户端。
func newStubClient(t *testing.T, bucket string, handler http.HandlerFunc) (*Client, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("解析测试服务器地址失败: %v", err)
	}
	client, err := New(Config{
		Endpoint:  "http://127.0.0.1:9000",
		AccessKey: "AKIDEXAMPLE",
		SecretKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
		Bucket:    bucket,
	})
	if err != nil {
		t.Fatalf("New 返回错误: %v", err)
	}
	dialer := &net.Dialer{}
	client.http = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, serverURL.Host)
		},
	}}
	client.now = func() time.Time {
		return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	}
	return client, server.Close
}

// TestGetSignsRequest 用**独立实现（Python hmac 脚本）算出的期望签名**锁定 SigV4 输出，
// 同时校验 path-style 路径与对象 key 的百分号编码。
func TestGetSignsRequest(t *testing.T) {
	const (
		wantPath = "/acat-comic-images/e2e/a%20b.webp"
		wantAuth = "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20260102/us-east-1/s3/aws4_request, " +
			"SignedHeaders=host;x-amz-content-sha256;x-amz-date, " +
			"Signature=8a039329b8ad498d7c8a0c8bbbe03994470ff55d3763de9dd4422ba07bb93712"
	)
	var gotPath, gotAuth, gotDate string
	client, closeServer := newStubClient(t, "acat-comic-images", func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.EscapedPath()
		gotAuth = req.Header.Get("Authorization")
		gotDate = req.Header.Get("x-amz-date")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("RIFF----WEBP"))
	})
	defer closeServer()

	stream, err := client.Get(context.Background(), "e2e/a b.webp")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	defer func() { _ = stream.Close() }()
	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}

	if gotPath != wantPath {
		t.Fatalf("请求路径 = %q, 期望 %q", gotPath, wantPath)
	}
	if gotAuth != wantAuth {
		t.Fatalf("Authorization =\n%s\n期望\n%s", gotAuth, wantAuth)
	}
	if gotDate != "20260102T030405Z" {
		t.Fatalf("x-amz-date = %q", gotDate)
	}
	if string(body) != "RIFF----WEBP" {
		t.Fatalf("响应体 = %q", string(body))
	}
}

// TestPutSignsRequest 锁定 PutObject 的签名（含 content-type 与真实 payload 哈希）。
func TestPutSignsRequest(t *testing.T) {
	const (
		wantPayload = "21910b9985bc5be25ae5c1092a90745b776b1d9fcc971e1c7e0966ccd125d023"
		wantAuth    = "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20260102/us-east-1/s3/aws4_request, " +
			"SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date, " +
			"Signature=c644dba251c9d73fde37d439c15d336ecbf8ea7cedd90c5b08df05c92f017a29"
	)
	var (
		gotPath, gotAuth, gotPayload, gotType, gotBody string
		gotLength                                      int64
	)
	client, closeServer := newStubClient(t, "acat-read-files", func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.EscapedPath()
		gotAuth = req.Header.Get("Authorization")
		gotPayload = req.Header.Get("x-amz-content-sha256")
		gotType = req.Header.Get("Content-Type")
		gotLength = req.ContentLength
		raw, _ := io.ReadAll(req.Body)
		gotBody = string(raw)
		w.WriteHeader(http.StatusOK)
	})
	defer closeServer()

	err := client.Put(context.Background(), "acat-fun/read/user/avatar/abc.webp", "image/webp", []byte("hello-object"))
	if err != nil {
		t.Fatalf("Put 返回错误: %v", err)
	}
	if gotPath != "/acat-read-files/acat-fun/read/user/avatar/abc.webp" {
		t.Fatalf("请求路径 = %q", gotPath)
	}
	if gotAuth != wantAuth {
		t.Fatalf("Authorization =\n%s\n期望\n%s", gotAuth, wantAuth)
	}
	if gotPayload != wantPayload {
		t.Fatalf("x-amz-content-sha256 = %q, 期望 %q", gotPayload, wantPayload)
	}
	if gotType != "image/webp" {
		t.Fatalf("Content-Type = %q", gotType)
	}
	if gotLength != int64(len("hello-object")) || gotBody != "hello-object" {
		t.Fatalf("请求体 = %q（长度 %d）", gotBody, gotLength)
	}
}

// TestDeleteSignsRequest 锁定 DeleteObject 的签名（空 payload）。
func TestDeleteSignsRequest(t *testing.T) {
	const wantAuth = "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20260102/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;x-amz-content-sha256;x-amz-date, " +
		"Signature=5863e8ce2526c5172182be07ab290eac7029219ed8c668281fd296022c5bce53"
	var gotAuth, gotMethod string
	client, closeServer := newStubClient(t, "acat-read-files", func(w http.ResponseWriter, req *http.Request) {
		gotAuth = req.Header.Get("Authorization")
		gotMethod = req.Method
		w.WriteHeader(http.StatusNoContent)
	})
	defer closeServer()

	if err := client.Delete(context.Background(), "acat-fun/read/user/avatar/abc.webp"); err != nil {
		t.Fatalf("Delete 返回错误: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("请求方法 = %q", gotMethod)
	}
	if gotAuth != wantAuth {
		t.Fatalf("Authorization =\n%s\n期望\n%s", gotAuth, wantAuth)
	}
}

// TestGetNon2xx 非 2xx 视为失败。
func TestGetNon2xx(t *testing.T) {
	client, closeServer := newStubClient(t, "b", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	defer closeServer()

	if _, err := client.Get(context.Background(), "k"); err == nil {
		t.Fatal("403 应返回错误")
	}
}

// TestEnsureBucket。
func TestEnsureBucket(t *testing.T) {
	var methods []string
	client, closeServer := newStubClient(t, "acat-read-files", func(w http.ResponseWriter, req *http.Request) {
		methods = append(methods, req.Method)
		if req.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	defer closeServer()

	if err := client.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("EnsureBucket 返回错误: %v", err)
	}
	if strings.Join(methods, ",") != "HEAD,PUT" {
		t.Fatalf("请求序列 = %v, 期望 [HEAD PUT]", methods)
	}
}

// TestEnsureBucketExists 桶已存在时不再创建。
func TestEnsureBucketExists(t *testing.T) {
	var methods []string
	client, closeServer := newStubClient(t, "acat-read-files", func(w http.ResponseWriter, req *http.Request) {
		methods = append(methods, req.Method)
		w.WriteHeader(http.StatusOK)
	})
	defer closeServer()

	if err := client.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("EnsureBucket 返回错误: %v", err)
	}
	if strings.Join(methods, ",") != "HEAD" {
		t.Fatalf("请求序列 = %v, 期望 [HEAD]", methods)
	}
}

// TestNilClientOperations 未配置对象存储时所有操作都报错。
func TestNilClientOperations(t *testing.T) {
	var client *Client
	if err := client.EnsureBucket(context.Background()); err == nil {
		t.Fatal("nil 客户端 EnsureBucket 应报错")
	}
	if _, err := client.Get(context.Background(), "k"); err == nil {
		t.Fatal("nil 客户端 Get 应报错")
	}
	if err := client.Put(context.Background(), "k", "text/plain", []byte("x")); err == nil {
		t.Fatal("nil 客户端 Put 应报错")
	}
	if err := client.Delete(context.Background(), "k"); err == nil {
		t.Fatal("nil 客户端 Delete 应报错")
	}
}
