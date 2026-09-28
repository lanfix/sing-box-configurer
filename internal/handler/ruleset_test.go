package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lanfix/sing-box-configurer/internal/rules"
)

// testRuleSet — набор правил для проверки ответов.
var testRuleSet = rules.SingBoxRuleSet{
	Version: rules.RuleVersion,
	Rules: []map[string]any{
		{
			"ip_cidr": []string{"185.73.192.0/22"},
		},
	},
}

// serveRuleSet вызывает writeRuleSet с заголовком If-None-Match.
func serveRuleSet(ruleSet rules.SingBoxRuleSet, err error, ifNoneMatch string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/ruleset/bypass", nil)

	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}

	rec := httptest.NewRecorder()
	writeRuleSet(rec, req, ruleSet, err)

	return rec
}

func TestWriteRuleSetETag(t *testing.T) {
	first := serveRuleSet(testRuleSet, nil, "")

	etag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || etag == "" || first.Body.Len() == 0 {
		t.Fatalf("first response: code=%d etag=%q body=%d", first.Code, etag, first.Body.Len())
	}

	// Неизменный набор с тем же ETag — 304 без тела.
	second := serveRuleSet(testRuleSet, nil, etag)

	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Errorf("unchanged response: code=%d body=%d", second.Code, second.Body.Len())
	}

	changed := rules.SingBoxRuleSet{
		Version: rules.RuleVersion,
		Rules:   []map[string]any{},
	}

	third := serveRuleSet(changed, nil, etag)

	if third.Code != http.StatusOK || third.Header().Get("ETag") == etag {
		t.Errorf("changed response: code=%d etag=%q", third.Code, third.Header().Get("ETag"))
	}
}

func TestWriteRuleSetNotReady(t *testing.T) {
	rec := serveRuleSet(rules.SingBoxRuleSet{}, rules.ErrRuleSetNotReady, "")

	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("ETag") != "" {
		t.Errorf("not ready: code=%d etag=%q", rec.Code, rec.Header().Get("ETag"))
	}

	rec = serveRuleSet(rules.SingBoxRuleSet{}, errors.New("boom"), "")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("error: code=%d", rec.Code)
	}
}
