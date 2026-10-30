package owl

import "net/http"

// responseWriter is the ResponseWriter a handler gets in ctx.Res. It keeps the
// status code until the first byte is written (or the handler ends), so a
// handler can call ctx.Status(...) and then set headers, as ctx.Json does with
// Content-Type. With net/http's own writer, a header set after WriteHeader is
// ignored.
//
// It also lets the router know if the handler wrote anything, to answer 500 when
// a handler fails before it did.
type responseWriter struct {
	http.ResponseWriter
	status    int // 0 until WriteHeader is called
	committed bool
}

func (w *responseWriter) WriteHeader(code int) {
	if w.committed {
		return
	}
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(code) // informational, the real status is still to come
		return
	}
	w.status = code
}

func (w *responseWriter) Write(b []byte) (int, error) {
	w.commit()
	return w.ResponseWriter.Write(b)
}

// commit sends the status and the headers, once.
func (w *responseWriter) commit() {
	if w.committed {
		return
	}
	w.committed = true
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.ResponseWriter.WriteHeader(w.status)
}

// Flush sends what is written so far, for streaming handlers.
func (w *responseWriter) Flush() {
	w.commit()
	http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap lets http.ResponseController reach the real writer (Hijack, deadlines).
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// written tells if the handler chose a status or wrote something.
func (w *responseWriter) written() bool { return w.committed || w.status != 0 }
