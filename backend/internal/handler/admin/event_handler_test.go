package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPreviewImportRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &EventHandler{}
	router := gin.New()
	router.POST("/imports/preview", handler.PreviewImport)

	body := bytes.Repeat([]byte(" "), int(maxEventImportBodyBytes+1))
	request := httptest.NewRequest(http.MethodPost, "/imports/preview", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestParseEventResourceIDRejectsInvalidValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, value := range []string{"", "abc", "0", "-1"} {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Params = gin.Params{{Key: "id", Value: value}}
		if _, ok := parseEventResourceID(context); ok {
			t.Fatalf("invalid ID accepted: %q", value)
		}
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("ID %q returned status %d", value, recorder.Code)
		}
	}
}
