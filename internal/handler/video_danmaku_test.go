package handler

import (
	"encoding/base64"
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVideoDanmakuHiddenCandidatesAdvanceCursorWithoutLeaking(t *testing.T) {
	mock := followTestDB(t)
	mock.ExpectQuery("SELECT .* FROM `videos`.*").WillReturnRows(sqlmock.NewRows([]string{"video_id"}).AddRow(7))
	mock.ExpectQuery("SELECT .* FROM `video_danmakus`.*ORDER BY position_ms ASC, id ASC LIMIT").WithArgs(uint64(7), uint64(0), uint64(30000), 2).WillReturnRows(sqlmock.NewRows([]string{"id", "video_id", "position_ms", "content", "hidden"}).AddRow(4, 7, 1000, "SECRET", true).AddRow(5, 7, 1000, "visible", false))
	r := gin.New()
	r.GET("/videos/:video_id/danmakus", VideoDanmakus)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/videos/7/danmakus?from_ms=0&to_ms=30000&limit=1", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "SECRET") {
		t.Fatal(w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			Items  []any  `json:"items"`
			More   bool   `json:"has_more"`
			Cursor string `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Items) != 0 || !body.Data.More {
		t.Fatal(body)
	}
	raw, err := base64.RawURLEncoding.DecodeString(body.Data.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	var cursor danmakuCursor
	_ = json.Unmarshal(raw, &cursor)
	if cursor.ID != 4 {
		t.Fatal(cursor)
	}
}

func TestVideoDanmakuCursorBoundToWindowAndVideo(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		video    uint64
		valid    bool
	}{{"0", "30000", 7, true}, {"0", "30001", 7, false}, {"30000", "60000", 7, false}, {"0", "30000", 8, false}} {
		raw, _ := json.Marshal(danmakuCursor{Video: 7, From: 0, To: 30000, Position: 1000, ID: 4, Version: 1})
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/?from_ms="+tc.from+"&to_ms="+tc.to+"&cursor="+base64.RawURLEncoding.EncodeToString(raw), nil)
		_, _, ok := parseDanmakuWindow(c, tc.video)
		if ok != tc.valid {
			t.Fatal(tc)
		}
	}
}
