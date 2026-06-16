package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestFetchModels_OpenAI_Non2xxReturnsHelpfulError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("error code: 1020"))
	}))
	defer srv.Close()

	ch := model.Channel{
		Name:    "test",
		Type:    outbound.OutboundTypeOpenAIChat,
		Proxy:   false,
		BaseUrls: []model.BaseUrl{{URL: srv.URL, Delay: 0}},
		Keys:    []model.ChannelKey{{Enabled: true, ChannelKey: "sk-test"}},
	}

	_, err := FetchModels(context.Background(), ch)
	if err == nil {
		t.Fatalf("expected error")
	}
	got := err.Error()
	if got == "" || got == "invalid character 'e' looking for beginning of value" {
		t.Fatalf("expected helpful status/body error, got: %q", got)
	}
	if want := "status=403"; !strings.Contains(got, want) {
		t.Fatalf("expected error to contain %q, got %q", want, got)
	}
	if want := "error code: 1020"; !strings.Contains(got, want) {
		t.Fatalf("expected error to contain body snippet %q, got %q", want, got)
	}
}
