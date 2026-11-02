package server

import (
	"encoding/base64"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/sdk/wework/encrypt"
	"github.com/gofiber/fiber/v3"
)

func TestHandleRequestReturnsErrorForMalformedCallbackXML(t *testing.T) {
	const (
		token    = "test-token"
		receiver = "corp-id"
		nonce    = "test-nonce"
	)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	aesKey := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	crypt := encrypt.NewWXBizMsgCrypt(token, aesKey, receiver, encrypt.XMLType)
	body, cryptErr := crypt.EncryptMsg("not xml", timestamp, nonce)
	if cryptErr != nil {
		t.Fatalf("EncryptMsg() error = %v", cryptErr)
	}
	var envelope struct {
		Signature string `xml:"Signature"`
	}
	if err := xml.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal encrypted envelope: %v", err)
	}

	srv, err := NewServer(receiver, token, aesKey, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	app := fiber.New()
	app.Post("/callback", srv.Serve)
	request := httptest.NewRequest(http.MethodPost,
		"/callback?nonce="+nonce+"&msg_signature="+envelope.Signature+"&timestamp="+timestamp,
		strings.NewReader(string(body)))
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 400 {
		t.Fatalf("status = %d, want error status", response.StatusCode)
	}
}

func TestServeAcknowledgesValidGenericCallback(t *testing.T) {
	const (
		token    = "test-token"
		receiver = "corp-id"
		nonce    = "test-nonce"
	)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	aesKey := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	plain := `<xml><ToUserName>corp-id</ToUserName><FromUserName>user-id</FromUserName><MsgType>text</MsgType><Content>hello</Content><MsgId>42</MsgId></xml>`
	crypt := encrypt.NewWXBizMsgCrypt(token, aesKey, receiver, encrypt.XMLType)
	body, cryptErr := crypt.EncryptMsg(plain, timestamp, nonce)
	if cryptErr != nil {
		t.Fatalf("EncryptMsg() error = %v", cryptErr)
	}
	var envelope struct {
		Signature string `xml:"Signature"`
	}
	if err := xml.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal encrypted envelope: %v", err)
	}

	var received map[string]any
	srv, err := NewServer(receiver, token, aesKey, func(message map[string]any) map[string]any {
		received = message
		return nil
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	app := fiber.New()
	app.Post("/callback", srv.Serve)
	request := httptest.NewRequest(http.MethodPost,
		"/callback?nonce="+nonce+"&msg_signature="+envelope.Signature+"&timestamp="+timestamp,
		strings.NewReader(string(body)))
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer response.Body.Close()
	ack, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read callback response: %v", err)
	}
	if response.StatusCode != http.StatusOK || string(ack) != "success" {
		t.Fatalf("callback response = %d %q, want 200 success", response.StatusCode, ack)
	}
	if received["MsgType"] != "text" || received["Content"] != "hello" || received["FromUserName"] != "user-id" {
		t.Fatalf("generic callback handler received malformed message: %#v", received)
	}
}

func TestServeRejectsStaleCallbackAndVerificationTimestamps(t *testing.T) {
	const (
		token    = "test-token"
		receiver = "corp-id"
		nonce    = "stale-nonce"
	)
	timestamp := strconv.FormatInt(time.Now().Add(-11*time.Minute).Unix(), 10)
	aesKey := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	crypt := encrypt.NewWXBizMsgCrypt(token, aesKey, receiver, encrypt.XMLType)
	body, cryptErr := crypt.EncryptMsg(`<xml><ToUserName>corp-id</ToUserName><MsgType>text</MsgType></xml>`, timestamp, nonce)
	if cryptErr != nil {
		t.Fatalf("EncryptMsg() error = %v", cryptErr)
	}
	var envelope struct {
		Encrypt   string `xml:"Encrypt"`
		Signature string `xml:"Signature"`
	}
	if err := xml.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal encrypted envelope: %v", err)
	}
	srv, err := NewServer(receiver, token, aesKey, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	app := fiber.New()
	app.Get("/callback", srv.Serve)
	app.Post("/callback", srv.Serve)

	query := url.Values{}
	query.Set("nonce", nonce)
	query.Set("msg_signature", envelope.Signature)
	query.Set("timestamp", timestamp)
	postURL := "/callback?" + query.Encode()
	query.Set("echostr", envelope.Encrypt)
	getURL := "/callback?" + query.Encode()
	for _, tc := range []struct {
		name   string
		method string
		url    string
		body   string
	}{
		{"post callback", http.MethodPost, postURL, string(body)},
		{"get verification", http.MethodGet, getURL, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.url, strings.NewReader(tc.body))
			response, err := app.Test(request)
			if err != nil {
				t.Fatalf("app.Test() error = %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode < 400 {
				t.Fatalf("status = %d, want stale timestamp rejection", response.StatusCode)
			}
		})
	}
}

func TestServeAcceptsFreshVerificationTimestamp(t *testing.T) {
	const (
		token    = "test-token"
		receiver = "corp-id"
		nonce    = "fresh-verification"
		plain    = "challenge"
	)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	aesKey := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	crypt := encrypt.NewWXBizMsgCrypt(token, aesKey, receiver, encrypt.XMLType)
	body, cryptErr := crypt.EncryptMsg(plain, timestamp, nonce)
	if cryptErr != nil {
		t.Fatalf("EncryptMsg() error = %v", cryptErr)
	}
	var envelope struct {
		Encrypt   string `xml:"Encrypt"`
		Signature string `xml:"Signature"`
	}
	if err := xml.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal encrypted envelope: %v", err)
	}
	srv, err := NewServer(receiver, token, aesKey, nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	app := fiber.New()
	app.Get("/callback", srv.Serve)
	query := url.Values{}
	query.Set("nonce", nonce)
	query.Set("msg_signature", envelope.Signature)
	query.Set("timestamp", timestamp)
	query.Set("echostr", envelope.Encrypt)
	request := httptest.NewRequest(http.MethodGet, "/callback?"+query.Encode(), nil)
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer response.Body.Close()
	got, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read verification response: %v", err)
	}
	if response.StatusCode != http.StatusOK || string(got) != plain {
		t.Fatalf("verification response = %d %q, want 200 %q", response.StatusCode, got, plain)
	}
}

func TestServeAcknowledgesDuplicateCallbackWithoutRepeatingHandler(t *testing.T) {
	const (
		token    = "test-token"
		receiver = "corp-id"
	)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	aesKey := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	plain := `<xml><ToUserName>corp-id</ToUserName><FromUserName>user-id</FromUserName><MsgType>text</MsgType><Content>hello</Content><MsgId>42</MsgId></xml>`
	crypt := encrypt.NewWXBizMsgCrypt(token, aesKey, receiver, encrypt.XMLType)
	type signedCallback struct {
		nonce     string
		body      []byte
		signature string
	}
	callbacks := make([]signedCallback, 0, 2)
	for _, nonce := range []string{"repeat-nonce", "different-nonce"} {
		body, cryptErr := crypt.EncryptMsg(plain, timestamp, nonce)
		if cryptErr != nil {
			t.Fatalf("EncryptMsg() error = %v", cryptErr)
		}
		var envelope struct {
			Signature string `xml:"Signature"`
		}
		if err := xml.Unmarshal(body, &envelope); err != nil {
			t.Fatalf("unmarshal encrypted envelope: %v", err)
		}
		callbacks = append(callbacks, signedCallback{nonce: nonce, body: body, signature: envelope.Signature})
	}
	var calls atomic.Int32
	srv, err := NewServer(receiver, token, aesKey, func(map[string]any) map[string]any {
		calls.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	app := fiber.New()
	app.Post("/callback", srv.Serve)
	for i, callback := range callbacks {
		request := httptest.NewRequest(http.MethodPost,
			"/callback?nonce="+callback.nonce+"&msg_signature="+callback.signature+"&timestamp="+timestamp,
			strings.NewReader(string(callback.body)))
		response, err := app.Test(request)
		if err != nil {
			t.Fatalf("app.Test() attempt %d error = %v", i+1, err)
		}
		ack, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			t.Fatalf("read callback response: %v", readErr)
		}
		if response.StatusCode != http.StatusOK || string(ack) != "success" {
			t.Fatalf("callback response = %d %q, want 200 success", response.StatusCode, ack)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("handler calls = %d, want 1", got)
	}
}
