package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strings"
)

func isWorkBuddyRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	ua := strings.Join(strings.Fields(r.Header.Get("User-Agent")), " ")
	return strings.HasPrefix(strings.ToLower(ua), "workbuddy/")
}

func writeWorkBuddyResponsesThrough(w http.ResponseWriter, r *http.Request, next http.Handler) {
	if r.Method != http.MethodPost || !strings.HasSuffix(strings.TrimRight(r.URL.Path, "/"), "/responses") || !isWorkBuddyRequest(r) {
		next.ServeHTTP(w, r)
		return
	}
	writer := &workBuddyResponseWriter{ResponseWriter: w}
	next.ServeHTTP(writer, r)
	writer.finish()
}

// workBuddyJSONMost is the largest whole JSON reply held to normalize its
// usage; a larger one goes on as it came. A Responses reply's usage is at
// its end, so holding a reply holds all of it: an image answered whole can
// be tens of MB, and holding it past this kept the client waiting.
var workBuddyJSONMost = 4 << 20

// The reply's kind is decided once, as its header goes (writeHead): an SSE
// stream is normalized an event at a time, a 2xx JSON reply is held whole up
// to workBuddyJSONMost, and anything else (an error, another type) goes on
// untouched, its Content-Length and integrity headers kept.
const (
	workBuddyPass = iota
	workBuddyStream
	workBuddyJSON
)

type workBuddyResponseWriter struct {
	http.ResponseWriter
	body     bytes.Buffer
	line     bytes.Buffer
	mode     int
	status   int
	finished bool
}

func (w *workBuddyResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *workBuddyResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	content, _, _ := mime.ParseMediaType(w.Header().Get("Content-Type"))
	switch {
	case content == "text/event-stream":
		w.mode = workBuddyStream
	case status >= 200 && status < 300 && (content == "application/json" || strings.HasSuffix(content, "+json")):
		w.mode = workBuddyJSON
	default:
		w.mode = workBuddyPass
	}
	if w.mode != workBuddyPass {
		// the body may change length, so what vouched for the old one goes
		for _, key := range []string{"Content-Length", "ETag", "Content-MD5", "Digest"} {
			w.Header().Del(key)
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *workBuddyResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	switch w.mode {
	case workBuddyStream:
		return w.writeStream(p)
	case workBuddyJSON:
		if w.body.Len()+len(p) <= workBuddyJSONMost {
			return w.body.Write(p)
		}
		// too large to hold: what was held and the rest go on as they came
		w.mode = workBuddyPass
		if w.body.Len() > 0 {
			if _, err := w.ResponseWriter.Write(w.body.Bytes()); err != nil {
				return 0, err
			}
			w.body.Reset()
		}
	}
	return w.ResponseWriter.Write(p)
}

func (w *workBuddyResponseWriter) Flush() {
	// a JSON reply held whole has nothing to send before its end
	if w.mode == workBuddyJSON {
		return
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *workBuddyResponseWriter) writeStream(p []byte) (int, error) {
	w.line.Write(p)
	for {
		idx := bytes.IndexByte(w.line.Bytes(), '\n')
		if idx < 0 {
			break
		}
		line := w.line.Next(idx + 1)
		// Comments carry no event data. Send them even while an event is
		// incomplete: the gateway's heartbeats deliberately have no blank line.
		if line[0] == ':' {
			if err := writeWorkBuddySSEEvent(w.ResponseWriter, line); err != nil {
				return len(p), err
			}
			continue
		}
		w.body.Write(line)
		if bytes.Equal(line, []byte("\n")) || bytes.Equal(line, []byte("\r\n")) {
			if err := writeWorkBuddySSEEvent(w.ResponseWriter, w.body.Bytes()); err != nil {
				return len(p), err
			}
			w.body.Reset()
		}
	}
	return len(p), nil
}

func (w *workBuddyResponseWriter) finish() {
	if w.finished {
		return
	}
	w.finished = true
	switch w.mode {
	case workBuddyStream:
		w.body.Write(w.line.Bytes())
		if w.body.Len() > 0 {
			_ = writeWorkBuddySSEEvent(w.ResponseWriter, w.body.Bytes())
		}
	case workBuddyJSON:
		body := w.body.Bytes()
		if normalized, changed := normalizeWorkBuddyResponsesJSON(body); changed {
			body = normalized
		}
		_, _ = w.ResponseWriter.Write(body)
	}
}

func writeWorkBuddySSEEvent(w http.ResponseWriter, event []byte) error {
	out := event
	if data, ok := workBuddySSEData(event); ok {
		if normalized, changed := normalizeWorkBuddyResponsesJSON(data); changed {
			out = replaceWorkBuddySSEData(event, normalized)
		}
	}
	if _, err := w.Write(out); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

func workBuddySSEData(event []byte) ([]byte, bool) {
	var data []byte
	found := false
	for _, line := range bytes.SplitAfter(event, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\n'})
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		if found {
			data = append(data, '\n')
		}
		value := line[len("data:"):]
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
		data = append(data, value...)
		found = true
	}
	return data, found
}

func replaceWorkBuddySSEData(event, data []byte) []byte {
	var out bytes.Buffer
	replaced := false
	for _, line := range bytes.SplitAfter(event, []byte{'\n'}) {
		body := line
		ending := []byte(nil)
		if bytes.HasSuffix(body, []byte("\r\n")) {
			body, ending = body[:len(body)-2], body[len(body)-2:]
		} else if bytes.HasSuffix(body, []byte{'\n'}) {
			body, ending = body[:len(body)-1], body[len(body)-1:]
		}
		if bytes.HasPrefix(body, []byte("data:")) {
			if !replaced {
				for _, part := range bytes.Split(data, []byte{'\n'}) {
					out.WriteString("data: ")
					out.Write(part)
					out.Write(ending)
				}
				replaced = true
			}
			continue
		}
		out.Write(line)
	}
	return out.Bytes()
}

func normalizeWorkBuddyResponsesJSON(raw []byte) ([]byte, bool) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return raw, false
	}
	changed := false
	if usage, ok := root["usage"]; ok {
		if normalized, ok := normalizeWorkBuddyUsage(usage); ok {
			root["usage"], changed = normalized, true
		}
	}
	if response, ok := root["response"]; ok {
		var object map[string]json.RawMessage
		if json.Unmarshal(response, &object) == nil {
			if usage, ok := object["usage"]; ok {
				if normalized, ok := normalizeWorkBuddyUsage(usage); ok {
					object["usage"], changed = normalized, true
					encoded, err := json.Marshal(object)
					if err == nil {
						root["response"] = encoded
					}
				}
			}
		} else {
			// Some Responses adapters wrap the response object as a JSON string.
			var encoded string
			if json.Unmarshal(response, &encoded) == nil {
				if normalized, ok := normalizeWorkBuddyResponsesJSON([]byte(encoded)); ok {
					if b, err := json.Marshal(string(normalized)); err == nil {
						root["response"], changed = b, true
					}
				}
			}
		}
	}
	if !changed {
		return raw, false
	}
	encoded, err := json.Marshal(root)
	if err != nil {
		return raw, false
	}
	return encoded, true
}

func normalizeWorkBuddyUsage(raw json.RawMessage) (json.RawMessage, bool) {
	var usage map[string]json.RawMessage
	if json.Unmarshal(raw, &usage) != nil {
		return nil, false
	}
	changed := false
	for _, key := range []string{"input_tokens_details", "output_tokens_details", "inputTokensDetails", "outputTokensDetails"} {
		details, ok := usage[key]
		if !ok {
			continue
		}
		normalized, extra, ok := normalizeWorkBuddyDetails(details)
		if !ok {
			continue
		}
		usage[key] = normalized
		extraKey := key + "_extra"
		for suffix := 2; ; suffix++ {
			if _, exists := usage[extraKey]; !exists {
				break
			}
			extraKey = fmt.Sprintf("%s_%d", key+"_extra", suffix)
		}
		usage[extraKey] = extra
		changed = true
	}
	if !changed {
		return nil, false
	}
	encoded, err := json.Marshal(usage)
	if err != nil {
		return nil, false
	}
	return encoded, true
}

func normalizeWorkBuddyDetails(raw json.RawMessage) (json.RawMessage, json.RawMessage, bool) {
	var details map[string]json.RawMessage
	if json.Unmarshal(raw, &details) != nil {
		return nil, nil, false
	}
	extra := make(map[string]json.RawMessage)
	changed := false
	for key, value := range details {
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.UseNumber()
		var decoded any
		if decoder.Decode(&decoded) == nil {
			if _, ok := decoded.(json.Number); ok {
				continue
			}
		}
		delete(details, key)
		extra[key] = value
		changed = true
	}
	if !changed {
		return nil, nil, false
	}
	normalized, err := json.Marshal(details)
	if err != nil {
		return nil, nil, false
	}
	extraRaw, err := json.Marshal(extra)
	if err != nil {
		return nil, nil, false
	}
	return normalized, extraRaw, true
}
