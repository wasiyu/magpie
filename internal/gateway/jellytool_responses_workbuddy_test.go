package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/product"
	"github.com/yetone/magpie/internal/provider"
)

func TestWorkBuddyResponsesThroughCurrentGateway(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, agent := range []string{"WorkBuddy/5.11.3 WorkBuddy/5.11.3 CLI/2.176.0", "other-client/1"} {
			t.Run(strconv.FormatBool(stream)+"/"+agent, func(t *testing.T) {
				usage := `{"input_tokens":10,"output_tokens":2,"input_tokens_details":{"cached_tokens":3,"cache_write_tokens_details":{"ttl_30m_tokens":2}}}`
				reply := `{"id":"r","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":` + usage + `}`
				f := &fake{t: t, ctype: "application/json", reply: reply}
				if stream {
					f.ctype = "text/event-stream"
					f.reply = sse(`data: {"type":"response.output_text.delta","delta":"ok"}`, `data: {"type":"response.completed","response":`+reply+`}`)
				}
				setup(t, provider.Responses, f)
				// RunJellytool starts the normal backend, which serves Front.
				gateway := httptest.NewServer(New().Front())
				defer gateway.Close()
				request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/responses", strings.NewReader(`{"model":"fake/m1","input":"hi","stream":`+strconv.FormatBool(stream)+`}`))
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("User-Agent", agent)
				request.Header.Set("Content-Type", "application/json")
				response, err := gateway.Client().Do(request)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil || response.StatusCode != http.StatusOK {
					t.Fatalf("response: %s %s (%v)", response.Status, body, err)
				}
				var object map[string]any
				if stream {
					messages := events(string(body))
					if len(messages) == 0 {
						t.Fatalf("missing response events: %s", body)
					}
					object, _ = messages[len(messages)-1]["response"].(map[string]any)
				} else if err := json.Unmarshal(body, &object); err != nil {
					t.Fatal(err)
				}
				gotUsage, _ := object["usage"].(map[string]any)
				details, _ := gotUsage["input_tokens_details"].(map[string]any)
				_, nested := details["cache_write_tokens_details"]
				if wantNormalized := product.Jellytool && agent != "other-client/1"; nested == wantNormalized {
					t.Fatalf("WorkBuddy usage normalization was bypassed (Jellytool=%v): %s", product.Jellytool, body)
				}
				if details["cached_tokens"] != float64(3) {
					t.Fatalf("numeric cache usage changed: %s", body)
				}
			})
		}
	}
}

func TestIsWorkBuddyRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		ua   string
		want bool
	}{
		{name: "actual", ua: "WorkBuddy/5.9.3 WorkBuddy/5.9.3 CLI/2.165.0", want: true},
		{name: "extra spaces and case", ua: "  workbuddy/5.9.3   CLI/2.165.0 ", want: true},
		{name: "codex", ua: "codex_cli_rs/0.165.0", want: false},
		{name: "lookalike", ua: "NotWorkBuddy/5.9.3", want: false},
		{name: "empty", ua: "", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			r.Header.Set("User-Agent", tc.ua)
			if got := isWorkBuddyRequest(r); got != tc.want {
				t.Fatalf("isWorkBuddyRequest(%q) = %v, want %v", tc.ua, got, tc.want)
			}
		})
	}
}

func TestNormalizeWorkBuddyResponsesUsageKeepsExtensions(t *testing.T) {
	raw := []byte(` {"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":3,"cache_write_tokens_details":{"ttl_30m_tokens":2},"label":"keep"},"output_tokens_details":{"reasoning_tokens":2,"provider":{"name":"keep"}}},"output":[{"usage":{"input_tokens_details":{"cached_tokens":{"nested":1}}}}],"tools":[{"parameters":{"max":7}}]} `)
	got, changed := normalizeWorkBuddyResponsesJSON(raw)
	if !changed {
		t.Fatal("usage was not normalized")
	}
	want := `{"input_tokens":10,"input_tokens_details":{"cached_tokens":3},"input_tokens_details_extra":{"cache_write_tokens_details":{"ttl_30m_tokens":2},"label":"keep"},"output_tokens_details":{"reasoning_tokens":2},"output_tokens_details_extra":{"provider":{"name":"keep"}}}`
	if !bytes.Contains(got, []byte(want)) {
		t.Fatalf("top-level usage changed unexpectedly: %s", got)
	}
	if !bytes.Contains(got, []byte(`"output":[{"usage":{"input_tokens_details":{"cached_tokens":{"nested":1}}}}]`)) {
		t.Fatalf("nested output usage was changed: %s", got)
	}
	if !bytes.Contains(got, []byte(`"max":7`)) {
		t.Fatalf("tool parameter number changed: %s", got)
	}
}

func TestNormalizeWorkBuddyResponsesResponseUsageOnly(t *testing.T) {
	raw := []byte(`{"response":{"usage":{"input_tokens_details":{"cached_tokens":{"ttl":2}}}},"other":{"usage":{"input_tokens_details":{"cached_tokens":{"ttl":3}}}}}`)
	got, changed := normalizeWorkBuddyResponsesJSON(raw)
	if !changed || !bytes.Contains(got, []byte(`"input_tokens_details_extra":{"cached_tokens":{"ttl":2}}`)) {
		t.Fatalf("response.usage was not normalized: %s", got)
	}
	if !bytes.Contains(got, []byte(`"other":{"usage":{"input_tokens_details":{"cached_tokens":{"ttl":3}}}}`)) {
		t.Fatalf("unrelated usage was changed: %s", got)
	}
}

func TestNormalizeWorkBuddyUsageKeepsExistingExtraFields(t *testing.T) {
	raw := []byte(`{"usage":{"input_tokens_details":{"cached_tokens":{"ttl":2},"label":"moved"},"input_tokens_details_extra":{"existing":true},"input_tokens_details_extra_2":{"existing":2}}}`)
	got, changed := normalizeWorkBuddyResponsesJSON(raw)
	if !changed {
		t.Fatal("usage was not normalized")
	}
	for _, want := range []string{
		`"input_tokens_details_extra":{"existing":true}`,
		`"input_tokens_details_extra_2":{"existing":2}`,
		`"input_tokens_details_extra_3":{"cached_tokens":{"ttl":2},"label":"moved"}`,
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Fatalf("missing collision-safe extension %s: %s", want, got)
		}
	}
}

func TestWorkBuddyResponsesWriterPreservesNonWorkBuddy(t *testing.T) {
	body := `{"usage":{"input_tokens_details":{"cached_tokens":{"ttl":2}}}}`
	for _, userAgent := range []string{"WorkBuddy/5.9.3 WorkBuddy/5.9.3 CLI/2.165.0", "other-client/1"} {
		t.Run(userAgent, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{}`))
			r.Header.Set("User-Agent", userAgent)
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(body))
			})
			writeWorkBuddyResponsesThrough(w, r, next)
			if userAgent == "other-client/1" && w.Body.String() != body {
				t.Fatalf("non-WorkBuddy response changed: %s", w.Body)
			}
			if userAgent != "other-client/1" && !strings.Contains(w.Body.String(), `"cached_tokens":{"ttl":2}`) {
				t.Fatalf("WorkBuddy extension was not retained: %s", w.Body)
			}
		})
	}
}

func TestWorkBuddyResponsesWriterStripsStaleIntegrityHeaders(t *testing.T) {
	body := `{"usage":{"input_tokens_details":{"cached_tokens":{"ttl":2}}}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("ETag", `"stale"`)
		w.Header().Set("Content-MD5", "stale-md5")
		w.Header().Set("Digest", "sha-256=stale")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer upstream.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeWorkBuddyResponsesThrough(w, r, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamResp, err := http.Get(upstream.URL)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			defer upstreamResp.Body.Close()
			for key, values := range upstreamResp.Header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.WriteHeader(upstreamResp.StatusCode)
			_, _ = io.Copy(w, upstreamResp.Body)
		}))
	}))
	defer proxy.Close()

	clientReq, err := http.NewRequest(http.MethodPost, proxy.URL+"/v1/responses", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	clientReq.Header.Set("User-Agent", "WorkBuddy/5.9.3 WorkBuddy/5.9.3 CLI/2.165.0")
	resp, err := proxy.Client().Do(clientReq)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"usage":{"input_tokens_details":{},"input_tokens_details_extra":{"cached_tokens":{"ttl":2}}}}` {
		t.Fatalf("unexpected response body: %s", got)
	}
	if resp.ContentLength != int64(len(got)) {
		t.Fatalf("content length = %d, body length = %d", resp.ContentLength, len(got))
	}
	for _, key := range []string{"ETag", "Content-MD5", "Digest"} {
		if value := resp.Header.Get(key); value != "" {
			t.Fatalf("stale %s header = %q", key, value)
		}
	}
}

func TestWorkBuddyResponsesWriterNormalizesSplitSSEEvents(t *testing.T) {
	body := "data: {\"usage\":{\"input_tokens_details\":{\"cached_tokens\":{\"ttl\":2}}}}\n\n" +
		"data: {\"output\":[{\"usage\":{\"input_tokens_details\":{\"cached_tokens\":{\"ttl\":3}}}}]}\n\n"
	for _, userAgent := range []string{"WorkBuddy/5.9.3 WorkBuddy/5.9.3 CLI/2.165.0", "other-client/1"} {
		t.Run(userAgent, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{}`))
			r.Header.Set("User-Agent", userAgent)
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				for _, part := range []string{body[:17], body[17:]} {
					_, _ = w.Write([]byte(part))
				}
			})
			writeWorkBuddyResponsesThrough(w, r, next)
			got := w.Body.String()
			if userAgent == "other-client/1" {
				if got != body {
					t.Fatalf("non-WorkBuddy stream changed: %q", got)
				}
				return
			}
			if !strings.Contains(got, `"input_tokens_details":{},"input_tokens_details_extra":{"cached_tokens":{"ttl":2}}`) {
				t.Fatalf("stream usage was not normalized: %s", got)
			}
			if !strings.Contains(got, `"output":[{"usage":{"input_tokens_details":{"cached_tokens":{"ttl":3}}}}]`) {
				t.Fatalf("nested stream usage was changed: %s", got)
			}
		})
	}
}

func TestWorkBuddyResponsesWriterKeepsCommentsLive(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for _, prefix := range []string{"", "event: response.completed" + ending} {
			t.Run(strconv.Quote(ending+prefix), func(t *testing.T) {
				w := httptest.NewRecorder()
				r := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				r.Header.Set("User-Agent", "WorkBuddy/5.11.3")
				writeWorkBuddyResponsesThrough(w, r, http.HandlerFunc(func(out http.ResponseWriter, _ *http.Request) {
					out.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(out, prefix)
					for i := 0; i < 2; i++ {
						io.WriteString(out, ": keep")
						io.WriteString(out, "alive"+ending)
						out.(http.Flusher).Flush()
						if got := strings.Count(w.Body.String(), ": keepalive"+ending); got != i+1 {
							t.Fatalf("heartbeat %d was buffered until the next event: %q", i+1, w.Body.String())
						}
					}
					io.WriteString(out, `data: {"usage":{"input_tokens_details":{"cached_tokens":3,"vendor":{"ttl":2}}}}`+ending+ending)
				}))
				if !strings.Contains(w.Body.String(), `"input_tokens_details":{"cached_tokens":3},"input_tokens_details_extra":{"vendor":{"ttl":2}}`) {
					t.Fatalf("heartbeats broke usage normalization: %s", w.Body.String())
				}
				if prefix != "" && !strings.Contains(w.Body.String(), prefix) {
					t.Fatalf("event name was lost: %s", w.Body.String())
				}
			})
		}
	}
}

func TestWorkBuddyResponsesWriterPreservesReadDeadlines(t *testing.T) {
	var deadlines []time.Time
	w := deadlineRecorder{httptest.NewRecorder(), &deadlines}
	r := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":"hi"}`))
	r.Header.Set("User-Agent", "WorkBuddy/5.11.3")
	writeWorkBuddyResponsesThrough(w, r, http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		if _, ok := New().requestBody(out, r, provider.Responses); !ok {
			t.Fatal("request body was rejected")
		}
		if len(deadlines) < 3 || deadlines[0].IsZero() || !deadlines[len(deadlines)-1].IsZero() {
			t.Fatalf("request read deadlines did not reach the connection and clear: %v", deadlines)
		}
	}))
}
