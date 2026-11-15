package handler

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestNumericPageAcceptsEmptyFirstCursor(t *testing.T) {
	for _, tc := range []struct {
		query  string
		ok     bool
		cursor uint64
	}{{"", true, 0}, {"?cursor=", true, 0}, {"?cursor=12", true, 12}, {"?cursor=bad", false, 0}, {"?limit=0", false, 0}, {"?limit=51", false, 0}} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/"+tc.query, nil)
		before, _, ok := commentAdminPage(c)
		if ok != tc.ok || before != tc.cursor {
			t.Fatalf("%s: %d %v", tc.query, before, ok)
		}
	}
}
func TestReportTargetTypesAndCanonicalReferences(t *testing.T) {
	for _, tc := range []struct {
		kind, ref string
		ok        bool
	}{{"video", "7", true}, {"post", "0", false}, {"user", "7", false}, {"comment", "-1", false}, {"danmaku", "7", false}, {"danmaku", "386c0a41-a36c-4e01-87d6-0598eb8a649a", true}} {
		_, ok := validReportRef(tc.kind, tc.ref)
		if ok != tc.ok {
			t.Fatal(tc)
		}
	}
}
