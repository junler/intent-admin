// Package storage provides local and S3 drivers without transport-layer dependencies.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Driver, Endpoint, Region, Bucket, AccessKeyID, SecretKey, BaseURL, URLMode, LocalDir string
	UseSSL, PathStyle                                                                    bool
	PresignExpireSec                                                                     int
}
type Driver interface {
	Put(context.Context, string, io.Reader, int64, string) error
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
	URL(context.Context, string) (string, error)
}
type Preset struct {
	Preset    string `json:"preset"`
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	PathStyle bool   `json:"pathStyle"`
}

func Presets() []Preset {
	return []Preset{{"local", "本地磁盘", "", false}, {"minio", "MinIO", "minio:9000", true}, {"aws", "AWS S3", "s3.{region}.amazonaws.com", false}, {"oss", "阿里云 OSS", "oss-{region}.aliyuncs.com", false}, {"cos", "腾讯云 COS", "cos.{region}.myqcloud.com", false}, {"r2", "Cloudflare R2", "{account}.r2.cloudflarestorage.com", false}, {"kodo", "七牛 Kodo", "s3-{region}.qiniucs.com", false}, {"custom", "自定义 S3", "", false}}
}
func New(c Config) (Driver, error) {
	if c.URLMode == "" {
		c.URLMode = "static"
	}
	if c.URLMode != "static" && c.URLMode != "presigned" {
		return nil, errors.New("无效的文件访问模式")
	}
	if c.Driver == "local" {
		if c.LocalDir == "" {
			return nil, errors.New("请填写本地目录")
		}
		if c.URLMode == "presigned" {
			return nil, errors.New("本地存储不支持 S3 预签名，请使用受保护的下载接口")
		}
		root, err := filepath.Abs(c.LocalDir)
		if err != nil {
			return nil, err
		}
		// Canonicalise the operator-configured root once, here, instead of
		// rejecting symlinks above it on every request. The root is chosen by
		// the operator, not the caller, and legitimate paths run through
		// symlinks (/var -> private/var on macOS, /data -> /mnt/... on many
		// deployments). Escape prevention is enforced on the caller-supplied
		// key instead, which is the attacker-controlled part.
		if err = os.MkdirAll(root, 0700); err != nil {
			return nil, err
		}
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		return &local{root: root, base: c.BaseURL}, nil
	}
	if c.Driver != "s3" {
		return nil, errors.New("存储驱动仅支持 local 或 s3")
	}
	if c.Bucket == "" || c.Endpoint == "" || c.AccessKeyID == "" || c.SecretKey == "" {
		return nil, errors.New("S3 endpoint、bucket、Access Key、Secret 均必填")
	}
	if strings.Contains(c.Endpoint, "://") {
		u, e := url.Parse(c.Endpoint)
		if e != nil || u.Host == "" || u.Path != "" && u.Path != "/" || u.User != nil || u.RawQuery != "" {
			return nil, errors.New("S3 endpoint 不能包含路径或凭证")
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, errors.New("S3 endpoint 必须使用 HTTP(S)")
		}
		c.Endpoint = u.Host
		c.UseSSL = u.Scheme == "https"
	}
	lookup := minio.BucketLookupDNS
	if c.PathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(c.Endpoint, &minio.Options{Creds: credentials.NewStaticV4(c.AccessKeyID, c.SecretKey, ""), Secure: c.UseSSL, Region: c.Region, BucketLookup: lookup})
	if err != nil {
		return nil, fmt.Errorf("S3 配置错误: %w", err)
	}
	if c.BaseURL != "" {
		u, err := url.Parse(c.BaseURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("S3 公网地址必须是无凭据和查询参数的 HTTP(S) URL")
		}
		if c.URLMode == "presigned" && strings.Trim(u.Path, "/") != "" && strings.Trim(u.Path, "/") != c.Bucket {
			return nil, errors.New("预签名公网地址只能包含 endpoint 或 endpoint/bucket")
		}
	}
	if c.PresignExpireSec <= 0 {
		c.PresignExpireSec = 3600
	}
	if c.PresignExpireSec > 604800 {
		c.PresignExpireSec = 604800
	}
	return &s3{cfg: c, client: client}, nil
}
func validKey(key string) bool {
	return key != "" && !strings.ContainsAny(key, "\\\x00") && !strings.HasPrefix(key, "/") && path.Clean(key) == key && key != ".." && !strings.HasPrefix(key, "../")
}
func staticURL(base, key string) string {
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	for _, part := range strings.Split(key, "/") {
		u.Path = path.Join(u.Path, part)
	}
	return u.String()
}

type local struct{ root, base string }

// resolve rejects symlinks at every existing component as well as lexical traversal.
func (l *local) resolve(key string) (string, error) {
	if !validKey(key) {
		return "", errors.New("非法文件路径")
	}
	// l.root was canonicalised in New, so only the key's own components can
	// introduce a symlink that escapes it.
	root := l.root
	cur := root
	if info, err := os.Lstat(root); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("本地存储目录不能是符号链接")
	}
	for _, part := range strings.Split(key, "/") {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("文件路径不能包含符号链接")
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	return cur, nil
}
func (l *local) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	p, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, err := io.Copy(file, r)
	closeErr := file.Close()
	if err != nil || closeErr != nil || size >= 0 && n != size {
		_ = os.Remove(p)
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		return errors.New("文件长度与声明不符")
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(p)
		return err
	}
	return nil
}
func (l *local) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := l.resolve(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}
func (l *local) Delete(ctx context.Context, key string) error {
	p, err := l.resolve(key)
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
func (l *local) URL(ctx context.Context, key string) (string, error) {
	if !validKey(key) {
		return "", errors.New("非法文件路径")
	}
	return staticURL(l.base, key), nil
}

type s3 struct {
	cfg    Config
	client *minio.Client
}

func (s *s3) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	if !validKey(key) {
		return errors.New("非法对象键")
	}
	_, err := s.client.PutObject(ctx, s.cfg.Bucket, key, r, size, minio.PutObjectOptions{ContentType: mime})
	return err
}
func (s *s3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !validKey(key) {
		return nil, errors.New("非法对象键")
	}
	o, err := s.client.GetObject(ctx, s.cfg.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	if _, err = o.Stat(); err != nil {
		_ = o.Close()
		return nil, err
	}
	return o, nil
}
func (s *s3) Delete(ctx context.Context, key string) error {
	if !validKey(key) {
		return errors.New("非法对象键")
	}
	return s.client.RemoveObject(ctx, s.cfg.Bucket, key, minio.RemoveObjectOptions{})
}
func (s *s3) URL(ctx context.Context, key string) (string, error) {
	if !validKey(key) {
		return "", errors.New("非法对象键")
	}
	if s.cfg.URLMode == "presigned" {
		signer := s.client
		if s.cfg.BaseURL != "" {
			public, err := url.Parse(s.cfg.BaseURL)
			if err != nil {
				return "", err
			}
			region := s.cfg.Region
			if region == "" {
				region, err = s.client.GetBucketLocation(ctx, s.cfg.Bucket)
				if err != nil {
					return "", err
				}
			}
			lookup := minio.BucketLookupDNS
			if s.cfg.PathStyle || strings.Trim(public.Path, "/") == s.cfg.Bucket {
				lookup = minio.BucketLookupPath
			}
			publicEndpoint := public.Host
			if lookup == minio.BucketLookupDNS && strings.HasPrefix(strings.ToLower(public.Hostname()), strings.ToLower(s.cfg.Bucket)+".") {
				publicEndpoint = strings.TrimPrefix(public.Host, s.cfg.Bucket+".")
			}
			signer, err = minio.New(publicEndpoint, &minio.Options{Creds: credentials.NewStaticV4(s.cfg.AccessKeyID, s.cfg.SecretKey, ""), Secure: public.Scheme == "https", Region: region, BucketLookup: lookup})
			if err != nil {
				return "", err
			}
		}
		u, err := signer.PresignedGetObject(ctx, s.cfg.Bucket, key, time.Duration(s.cfg.PresignExpireSec)*time.Second, nil)
		if err != nil {
			return "", err
		}
		return u.String(), nil
	}
	if s.cfg.BaseURL != "" {
		return staticURL(s.cfg.BaseURL, key), nil
	}
	scheme := "http"
	if s.cfg.UseSSL {
		scheme = "https"
	}
	return staticURL(scheme+"://"+s.cfg.Endpoint+"/"+s.cfg.Bucket, key), nil
}

type TestResult struct {
	OK           bool   `json:"ok"`
	BucketExists bool   `json:"bucketExists"`
	Writable     bool   `json:"writable"`
	Readable     bool   `json:"readable"`
	PublicURLOK  bool   `json:"publicUrlOk"`
	LatencyMS    int64  `json:"latencyMs"`
	Error        string `json:"error,omitempty"`
	Warning      string `json:"warning,omitempty"`
}

func Probe(ctx context.Context, c Config) (result TestResult) {
	start := time.Now()
	defer func() { result.LatencyMS = time.Since(start).Milliseconds() }()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	d, err := New(c)
	if err != nil {
		result.Error = err.Error()
		return
	}
	if s, ok := d.(*s3); ok {
		exists, err := s.client.BucketExists(ctx, c.Bucket)
		if err != nil || !exists {
			result.Error = "存储桶不存在或没有访问权限"
			return
		}
	}
	result.BucketExists = true
	key := ".intent-admin-probe/" + uuid.NewString()
	content := strings.Repeat("x", 1024)
	if err = d.Put(ctx, key, strings.NewReader(content), 1024, "application/octet-stream"); err != nil {
		result.Error = "无法写入存储，请检查写入权限"
		return
	}
	result.Writable = true
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := d.Delete(cleanup, key); err != nil {
			result.OK = false
			result.Error = "探针清理失败，请检查删除权限"
		}
	}()
	f, err := d.Open(ctx, key)
	if err != nil {
		result.Error = "能写入但无法读取，请检查读取权限"
		return
	}
	raw, err := io.ReadAll(io.LimitReader(f, 2048))
	_ = f.Close()
	if err != nil || string(raw) != content {
		result.Error = "读取内容校验失败"
		return
	}
	result.Readable = true
	if c.Driver == "local" {
		result.PublicURLOK = true
		result.OK = true
		return
	}
	target, err := d.URL(ctx, key)
	if err != nil {
		result.Error = "生成访问 URL 失败"
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		result.Error = "访问 URL 无效"
		return
	}
	res, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		result.Error = "可读写存储，但公网 URL 无法访问"
		return
	}
	raw, err = io.ReadAll(io.LimitReader(res.Body, 2048))
	_ = res.Body.Close()
	if err != nil || res.StatusCode != 200 || string(raw) != content {
		result.Error = "可读写存储，但公网 URL 不可读，请检查 baseUrl 或桶策略"
		return
	}
	result.PublicURLOK = true
	result.OK = true
	return
}
