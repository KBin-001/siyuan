package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/siyuan-note/siyuan/kernel/apicontract"
	"github.com/siyuan-note/siyuan/kernel/model"
)

func TestManualS3SyncAPIPreservesLocalAuthorization(t *testing.T) {
	syncTestConfiguration(t)
	for _, role := range []model.Role{model.RoleReader, model.RoleEditor, model.RoleAdministrator} {
		t.Run(strconv.FormatUint(uint64(role), 10), func(t *testing.T) {
			engine := gin.New()
			engine.Use(func(c *gin.Context) { c.Set(model.RoleContextKey, role); c.Next() })
			engine.POST("/api/sync/performSync", contractRouteHandlers(apicontract.PerformSync, performSync)...)
			engine.POST("/api/sync/setSyncProviderS3", contractRouteHandlers(apicontract.SetSyncProviderS3, setSyncProviderS3)...)
			for _, route := range []string{"performSync", "setSyncProviderS3"} {
				response := httptest.NewRecorder()
				engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/sync/"+route, strings.NewReader(`{}`)))
				if role != model.RoleAdministrator {
					if response.Code != http.StatusForbidden {
						t.Fatalf("%s allowed non-administrator access: %d", route, response.Code)
					}
					continue
				}
				var result struct{ Code int }
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Code != -1 {
					t.Fatalf("%s bypassed read-only mode: %s, %v", route, response.Body.String(), err)
				}
			}
		})
	}
}
