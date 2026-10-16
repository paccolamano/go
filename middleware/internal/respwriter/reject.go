package respwriter

import (
	"net/http"

	"github.com/paccolamano/go/middleware"
)

// Reject writes the response for a rejected request following the module's
// refusal standard: a nil fn writes the default plain-text response, and if
// fn writes neither a status nor a body, rej.Status is sent with an empty
// body. The caller sets the protocol headers of the condition beforehand.
func Reject(w http.ResponseWriter, r *http.Request, fn middleware.RejectFunc, rej middleware.Rejection) {
	if fn == nil {
		http.Error(w, http.StatusText(rej.Status), rej.Status)
		return
	}
	ow := New(w)
	fn(ow, r, rej)
	if !ow.Started() {
		w.WriteHeader(rej.Status)
	}
}
