package rest

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/gofiber/fiber/v3"
)

// sendFileStubUsecase implements domainSend.ISendUsecase by embedding the
// interface (so unrelated methods are never invoked by these tests) while
// recording the FileRequest actually received by SendFile.
type sendFileStubUsecase struct {
	domainSend.ISendUsecase
	receivedRequest domainSend.FileRequest
	called          bool
}

func (s *sendFileStubUsecase) SendFile(_ context.Context, request domainSend.FileRequest) (domainSend.GenericResponse, error) {
	s.called = true
	s.receivedRequest = request
	return domainSend.GenericResponse{Status: "ok"}, nil
}

func newSendFileTestApp(stub *sendFileStubUsecase) *fiber.App {
	app := fiber.New()
	app.Use(middleware.Recovery())
	controller := Send{Service: stub}
	app.Post("/send/file", controller.SendFile)
	return app
}

// TestSendFileJSONBodyWithFileURLDoesNotPanic is a regression test for
// https://github.com/aldinokemal/go-whatsapp-web-multidevice/issues/744:
// a JSON request carrying file_url (no multipart file part) must reach the
// usecase instead of panicking into a 500 from the unguarded FormFile error.
func TestSendFileJSONBodyWithFileURLDoesNotPanic(t *testing.T) {
	stub := &sendFileStubUsecase{}
	app := newSendFileTestApp(stub)

	body := `{"phone":"628123456789@s.whatsapp.net","file_url":"https://example.com/doc.pdf"}`
	req := httptest.NewRequest(http.MethodPost, "/send/file", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d (request should not panic into a 500)", resp.StatusCode, fiber.StatusOK)
	}
	if !stub.called {
		t.Fatalf("usecase SendFile was not called")
	}
	if stub.receivedRequest.FileURL == nil || *stub.receivedRequest.FileURL != "https://example.com/doc.pdf" {
		t.Fatalf("FileURL = %v, want https://example.com/doc.pdf", stub.receivedRequest.FileURL)
	}
	if stub.receivedRequest.File != nil {
		t.Fatalf("File = %+v, want nil since no multipart file part was sent", stub.receivedRequest.File)
	}
}

// TestSendFileMultipartStillPopulatesFile ensures the original multipart
// upload path keeps working after guarding the FormFile error.
func TestSendFileMultipartStillPopulatesFile(t *testing.T) {
	stub := &sendFileStubUsecase{}
	app := newSendFileTestApp(stub)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("phone", "628123456789@s.whatsapp.net"); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	part, err := writer.CreateFormFile("file", "doc.pdf")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte("%PDF-1.4 fake content")); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/send/file", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}
	if !stub.called {
		t.Fatalf("usecase SendFile was not called")
	}
	if stub.receivedRequest.File == nil {
		t.Fatalf("File = nil, want populated multipart.FileHeader")
	}
	if stub.receivedRequest.File.Filename != "doc.pdf" {
		t.Fatalf("File.Filename = %q, want doc.pdf", stub.receivedRequest.File.Filename)
	}
}
