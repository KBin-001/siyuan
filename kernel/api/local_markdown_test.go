package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/siyuan-note/siyuan/kernel/apicontract"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func TestAPIContractLocalMarkdown(t *testing.T) {
	old := util.Container
	util.Container = util.ContainerAndroid
	t.Cleanup(func() { util.Container = old })
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/api/export/localMarkdown", localMarkdown)
	for _, body := range []string{`{"action":"status"}`, `{"action":4}`, `{"action":"configure","config":{"enabled":"true"}}`, `{"config":{}}`} {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest("POST", "/api/export/localMarkdown", strings.NewReader(body)))
		requireAPIContract(t, "POST", "/api/export/localMarkdown", recorder)
		var envelope struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Code != -1 || string(envelope.Data) != "null" {
			t.Fatalf("invalid request succeeded: %s", recorder.Body.String())
		}
	}
	definition := apicontract.LocalMarkdown.Definition()
	if definition.Authorization != apicontract.AuthenticatedAccess|apicontract.AdminAccess|apicontract.WritableAccess {
		t.Fatal("missing endpoint authorization")
	}
}
