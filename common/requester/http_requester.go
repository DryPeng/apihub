package requester

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"one-api/common"
	"one-api/types"
	"strconv"

	"github.com/gin-gonic/gin"
)

type HttpErrorHandler func(*http.Response) *types.OpenAIError

type HTTPRequester struct {
	HTTPClient        HTTPClient
	requestBuilder    RequestBuilder
	CreateFormBuilder func(io.Writer) FormBuilder
	ErrorHandler      HttpErrorHandler
	proxyAddr         string
}

// NewHTTPRequester 创建一个新的 HTTPRequester 实例。
// proxyAddr: 是代理服务器的地址。
// errorHandler: 是一个错误处理函数，它接收一个 *http.Response 参数并返回一个 *types.OpenAIErrorResponse。
// 如果 errorHandler 为 nil，那么会使用一个默认的错误处理函数。
func NewHTTPRequester(proxyAddr string, errorHandler HttpErrorHandler) *HTTPRequester {
	return &HTTPRequester{
		HTTPClient:     HTTPClient{},
		requestBuilder: NewRequestBuilder(),
		CreateFormBuilder: func(body io.Writer) FormBuilder {
			return NewFormBuilder(body)
		},
		ErrorHandler: errorHandler,
		proxyAddr:    proxyAddr,
	}
}

type requestOptions struct {
	body   any
	header http.Header
}

type requestOption func(*requestOptions)

// 创建请求
func (r *HTTPRequester) NewRequest(method, url string, setters ...requestOption) (*http.Request, error) {
	args := &requestOptions{
		body:   nil,
		header: make(http.Header),
	}
	for _, setter := range setters {
		setter(args)
	}
	req, err := r.requestBuilder.Build(method, url, args.body, args.header)
	if err != nil {
		return nil, err
	}

	return req, nil
}

// 发送请求
func (r *HTTPRequester) SendRequest(req *http.Request, response any, outputResp bool) (*http.Response, *types.OpenAIErrorWithStatusCode) {
	client := r.HTTPClient.getClientFromPool(r.proxyAddr)
	resp, err := client.Do(req)
	r.HTTPClient.returnClientToPool(client)
	if err != nil {
		return nil, common.ErrorWrapper(err, "http_request_failed", http.StatusInternalServerError)
	}

	if !outputResp {
		defer resp.Body.Close()
	}

	// 处理响应
	if r.IsFailureStatusCode(resp) {
		return nil, HandleErrorResp(resp, r.ErrorHandler)
	}

	// 解析响应
	if outputResp {
		var buf bytes.Buffer
		tee := io.TeeReader(resp.Body, &buf)
		err = json.NewDecoder(tee).Decode(response)

		// 将响应体重新写入 resp.Body
		resp.Body = io.NopCloser(&buf)
	} else {
		err = json.NewDecoder(resp.Body).Decode(response)
	}

	if err != nil {
		return nil, common.ErrorWrapper(err, "decode_response_failed", http.StatusInternalServerError)
	}

	if outputResp {
		return resp, nil
	}

	return nil, nil
}

// 发送请求 RAW
func (r *HTTPRequester) SendRequestRaw(req *http.Request) (*http.Response, *types.OpenAIErrorWithStatusCode) {
	// 发送请求
	client := r.HTTPClient.getClientFromPool(r.proxyAddr)
	resp, err := client.Do(req)
	r.HTTPClient.returnClientToPool(client)
	if err != nil {
		return nil, common.ErrorWrapper(err, "http_request_failed", http.StatusInternalServerError)
	}

	// 处理响应
	if r.IsFailureStatusCode(resp) {
		return nil, HandleErrorResp(resp, r.ErrorHandler)
	}

	return resp, nil
}

// 获取流式响应
func RequestStream[T streamable](requester *HTTPRequester, resp *http.Response, handlerPrefix HandlerPrefix[T]) (*streamReader[T], *types.OpenAIErrorWithStatusCode) {
	// 如果返回的头是json格式 说明有错误
	if resp.Header.Get("Content-Type") == "application/json" {
		return nil, HandleErrorResp(resp, requester.ErrorHandler)
	}

	return &streamReader[T]{
		reader:        bufio.NewReader(resp.Body),
		response:      resp,
		handlerPrefix: handlerPrefix,
	}, nil
}

// 设置请求体
func (r *HTTPRequester) WithBody(body any) requestOption {
	return func(args *requestOptions) {
		args.body = body
	}
}

// 设置请求头
func (r *HTTPRequester) WithHeader(header map[string]string) requestOption {
	return func(args *requestOptions) {
		for k, v := range header {
			args.header.Set(k, v)
		}
	}
}

// 设置Content-Type
func (r *HTTPRequester) WithContentType(contentType string) requestOption {
	return func(args *requestOptions) {
		args.header.Set("Content-Type", contentType)
	}
}

// 判断是否为失败状态码
func (r *HTTPRequester) IsFailureStatusCode(resp *http.Response) bool {
	return resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest
}

// 处理错误响应
func HandleErrorResp(resp *http.Response, toOpenAIError HttpErrorHandler) *types.OpenAIErrorWithStatusCode {

	openAIErrorWithStatusCode := &types.OpenAIErrorWithStatusCode{
		StatusCode: resp.StatusCode,
		OpenAIError: types.OpenAIError{
			Message: "",
			Type:    "upstream_error",
			Code:    "bad_response_status_code",
			Param:   strconv.Itoa(resp.StatusCode),
		},
	}

	defer resp.Body.Close()

	if toOpenAIError != nil {
		errorResponse := toOpenAIError(resp)

		if errorResponse != nil && errorResponse.Message != "" {
			openAIErrorWithStatusCode.OpenAIError = *errorResponse
		}
	}

	if openAIErrorWithStatusCode.OpenAIError.Message == "" {
		openAIErrorWithStatusCode.OpenAIError.Message = fmt.Sprintf("bad response status code %d", resp.StatusCode)
	}

	return openAIErrorWithStatusCode
}

func (r *HTTPRequester) SetEventStreamHeaders(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
}
