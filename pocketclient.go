package pocketclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client

	mu    sync.RWMutex
	Token string

	authCollection string
	authIdentity   string
	authPassword   string
	autoReauth     bool
}

type Query map[string]any
type Headers map[string]string

type File struct {
	Name string
	Data []byte
}

type Response[T any] struct {
	Page       int `json:"page"`
	PerPage    int `json:"perPage"`
	TotalItems int `json:"totalItems"`
	TotalPages int `json:"totalPages"`
	Items      []T `json:"items"`
}

type authResponse struct {
	Token  string          `json:"token"`
	Record json.RawMessage `json:"record"`
}

type Error struct {
	Status int
	Method string
	Path   string
	Body   string
}

func (e Error) Error() string {
	return fmt.Sprintf("%s %s failed: %d %s", e.Method, e.Path, e.Status, e.Body)
}

func IsError(err error, status ...int) (Error, bool) {
	var e Error
	if !errors.As(err, &e) {
		return e, false
	}
	if len(status) == 0 {
		return e, true
	}
	for _, s := range status {
		if e.Status == s {
			return e, true
		}
	}
	return e, false
}

type authRequest struct {
	Identity string `json:"identity"`
	Password string `json:"password"`
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTP:       &http.Client{Timeout: 15 * time.Second},
		autoReauth: true,
	}
}

func (c *Client) WithToken(token string) *Client {
	c.setToken(token)
	return c
}

func (c *Client) DisableAutoReauth() *Client {
	c.autoReauth = false
	return c
}

func (c *Client) AuthPassword(collection, identity, password string, output any) error {
	var auth authResponse

	err := c.POST(
		"/api/collections/"+collection+"/auth-with-password",
		authRequest{Identity: identity, Password: password},
		&auth,
	)

	if err != nil {
		return err
	}

	c.setToken(auth.Token)

	c.authCollection = collection
	c.authIdentity = identity
	c.authPassword = password

	if output != nil && len(auth.Record) > 0 {
		return json.Unmarshal(auth.Record, output)
	}

	return nil
}

func (c *Client) Create(collection string, body any, output any, query ...Query) error {
	return c.POST("/api/collections/"+collection+"/records", body, output, query...)
}

func (c *Client) View(collection, id string, output any, query ...Query) error {
	return c.GET("/api/collections/"+collection+"/records/"+id, output, query...)
}

func (c *Client) Update(collection, id string, body any, output any, query ...Query) error {
	return c.PATCH("/api/collections/"+collection+"/records/"+id, body, output, query...)
}

func (c *Client) List(collection string, output any, query ...Query) error {
	return c.GET("/api/collections/"+collection+"/records", output, query...)
}

func (c *Client) Delete(collection, id string, output any, query ...Query) error {
	return c.DELETE("/api/collections/"+collection+"/records/"+id, output, query...)
}

func (c *Client) Raw(method, path string, body any, output any, query ...Query) error {
	return c.do(context.Background(), method, path, body, output, nil, firstQuery(query), true)
}

func (c *Client) RawCtx(ctx context.Context, method, path string, body any, output any, query ...Query) error {
	return c.do(ctx, method, path, body, output, nil, firstQuery(query), true)
}

func (c *Client) RawWithHeaders(ctx context.Context, method, path string, body any, output any, headers Headers, query ...Query) error {
	return c.do(ctx, method, path, body, output, headers, firstQuery(query), true)
}

func (c *Client) GET(path string, output any, query ...Query) error {
	return c.do(context.Background(), http.MethodGet, path, nil, output, nil, firstQuery(query), true)
}

func (c *Client) POST(path string, body any, output any, query ...Query) error {
	return c.do(context.Background(), http.MethodPost, path, body, output, nil, firstQuery(query), true)
}

func (c *Client) PATCH(path string, body any, output any, query ...Query) error {
	return c.do(context.Background(), http.MethodPatch, path, body, output, nil, firstQuery(query), true)
}

func (c *Client) DELETE(path string, output any, query ...Query) error {
	return c.do(context.Background(), http.MethodDelete, path, nil, output, nil, firstQuery(query), true)
}

func (c *Client) do(ctx context.Context, method, path string, body any, output any, headers Headers, query Query, retry bool) error {
	reader, contentType, err := buildBody(body)
	if err != nil {
		return err
	}

	path = normalizePath(path)
	fullURL := c.BaseURL + path

	if qs := encodeQuery(query); qs != "" {
		fullURL += "?" + qs
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return err
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	if token := c.getToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if res.StatusCode == http.StatusUnauthorized && retry && c.autoReauth {
		if err := c.reauth(ctx); err != nil {
			return err
		}

		return c.do(ctx, method, path, body, output, headers, query, false)
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Error{
			Status: res.StatusCode,
			Method: method,
			Path:   path,
			Body:   string(data),
		}
	}

	if output != nil && len(data) > 0 {
		return json.Unmarshal(data, output)
	}

	return nil
}

func (c *Client) reauth(ctx context.Context) error {
	if c.authCollection == "" || c.authIdentity == "" || c.authPassword == "" {
		return fmt.Errorf("cannot reauth: no stored auth credentials")
	}

	var auth authResponse

	err := c.do(
		ctx,
		http.MethodPost,
		"/api/collections/"+c.authCollection+"/auth-with-password",
		authRequest{
			Identity: c.authIdentity,
			Password: c.authPassword,
		},
		&auth,
		nil,
		nil,
		false,
	)

	if err != nil {
		return err
	}

	c.setToken(auth.Token)
	return nil
}

func buildBody(body any) (io.Reader, string, error) {
	if body == nil {
		return nil, "", nil
	}

	if hasFiles(body) {
		buf, contentType, err := multipartBody(body)
		return buf, contentType, err
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, "", err
	}

	return bytes.NewReader(data), "application/json", nil
}

func hasFiles(v any) bool {
	rv := indirect(reflect.ValueOf(v))
	if !rv.IsValid() {
		return false
	}

	if rv.Kind() == reflect.Map {
		for _, key := range rv.MapKeys() {
			if isFileValue(rv.MapIndex(key)) {
				return true
			}
		}
		return false
	}

	if rv.Kind() != reflect.Struct {
		return false
	}

	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		if field.CanInterface() && isFileValue(field) {
			return true
		}
	}

	return false
}

func multipartBody(v any) (*bytes.Buffer, string, error) {
	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)

	rv := indirect(reflect.ValueOf(v))
	if !rv.IsValid() {
		return nil, "", fmt.Errorf("invalid multipart body")
	}

	switch rv.Kind() {
	case reflect.Map:
		for _, key := range rv.MapKeys() {
			name := fmt.Sprint(key.Interface())
			val := rv.MapIndex(key)
			if val.IsValid() {
				if err := writeMultipartValue(writer, name, val.Interface()); err != nil {
					return nil, "", err
				}
			}
		}

	case reflect.Struct:
		rt := rv.Type()

		for i := 0; i < rv.NumField(); i++ {
			field := rv.Field(i)
			sf := rt.Field(i)

			if !field.CanInterface() {
				continue
			}

			name := jsonName(sf)
			if name == "" || name == "-" || isZero(field) {
				continue
			}

			if err := writeMultipartValue(writer, name, field.Interface()); err != nil {
				return nil, "", err
			}
		}

	default:
		return nil, "", fmt.Errorf("multipart body must be struct or map")
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	return buf, writer.FormDataContentType(), nil
}

func writeMultipartValue(writer *multipart.Writer, name string, value any) error {
	switch v := value.(type) {
	case File:
		return addFile(writer, name, v)

	case []File:
		for _, file := range v {
			if err := addFile(writer, name, file); err != nil {
				return err
			}
		}
		return nil

	case []string:
		for _, item := range v {
			if err := writer.WriteField(name, item); err != nil {
				return err
			}
		}
		return nil

	case []int:
		for _, item := range v {
			if err := writer.WriteField(name, fmt.Sprint(item)); err != nil {
				return err
			}
		}
		return nil

	case []any:
		for _, item := range v {
			if err := writer.WriteField(name, fmt.Sprint(item)); err != nil {
				return err
			}
		}
		return nil

	default:
		return writer.WriteField(name, fmt.Sprint(value))
	}
}

func addFile(writer *multipart.Writer, fieldName string, file File) error {
	if len(file.Data) == 0 {
		return nil
	}

	if file.Name == "" {
		return fmt.Errorf("file name is required for field %q", fieldName)
	}

	part, err := writer.CreateFormFile(fieldName, file.Name)
	if err != nil {
		return err
	}

	_, err = part.Write(file.Data)
	return err
}

func isFileValue(v reflect.Value) bool {
	if !v.IsValid() {
		return false
	}

	v = indirect(v)
	if !v.IsValid() {
		return false
	}

	return v.Type() == reflect.TypeOf(File{}) || v.Type() == reflect.TypeOf([]File{})
}

func encodeQuery(query Query) string {
	if len(query) == 0 {
		return ""
	}

	values := url.Values{}

	for key, value := range query {
		switch v := value.(type) {
		case []string:
			for _, item := range v {
				values.Add(key, item)
			}
		case []int:
			for _, item := range v {
				values.Add(key, fmt.Sprint(item))
			}
		case []any:
			for _, item := range v {
				values.Add(key, fmt.Sprint(item))
			}
		default:
			values.Set(key, fmt.Sprint(value))
		}
	}

	return values.Encode()
}

func firstQuery(query []Query) Query {
	if len(query) == 0 {
		return nil
	}
	return query[0]
}

func normalizePath(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func jsonName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" {
		return field.Name
	}

	name := strings.Split(tag, ",")[0]
	if name == "" {
		return field.Name
	}

	return name
}

func indirect(v reflect.Value) reflect.Value {
	for v.IsValid() && v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

func isZero(v reflect.Value) bool {
	return v.IsZero()
}

func (c *Client) getToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Token
}

func (c *Client) setToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Token = token
}
