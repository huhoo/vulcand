package scroll

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/mailgun/vulcand/Godeps/_workspace/src/github.com/gorilla/mux"
	"github.com/mailgun/vulcand/Godeps/_workspace/src/github.com/mailgun/log"
)

// Response objects that apps' handlers are advised to return.
//
// Allows to easily return JSON-marshallable responses, e.g.:
//
//  Response{"message": "OK"}
type Response map[string]interface{}

// Represents handler's config.
type HandlerConfig struct {
	// List of HTTP methods the handler should match.
	Methods []string

	// Path the handler should match.
	Path string

	// Key/value pairs of specific HTTP headers the handler should match (e.g. Content-Type).
	Headers []string

	// Unique identifier used when emitting performance metrics for the handler.
	MetricName string

	// Whether to register the handler in vulcand.
	Register bool
}

// Defines the signature of a handler function that can be registered by an app.
//
// The 3rd parameter is a map of variables extracted from the request path, e.g. if a request path was:
//  /resources/{resourceID}
// and the request was made to:
//  /resources/1
// then the map will contain the resource ID value:
//  {"resourceID": 1}
//
// A handler function should return a JSON marshallable object, e.g. Response.
type HandlerFunc func(http.ResponseWriter, *http.Request, map[string]string) (interface{}, error)

// Wraps the provided handler function encapsulating boilerplate code so handlers do not have to
// implement it themselves: parsing a request's form, formatting a proper JSON response, emitting
// the request stats, etc.
func MakeHandler(app *App, fn HandlerFunc, config *HandlerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := parseForm(r); err != nil {
			ReplyInternalError(w, fmt.Sprintf("Failed to parse request form: %v", err))
			return
		}

		start := time.Now()
		response, err := fn(w, r, mux.Vars(r))
		elapsedTime := time.Since(start)

		var status int
		if err != nil {
			response, status = responseAndStatusFor(err)
		} else {
			status = http.StatusOK
		}

		log.Infof("Request completed: status [%v] method [%v] path [%v] form [%v] time [%v] error [%v]",
			status, r.Method, r.URL, r.Form, elapsedTime, err)

		app.stats.TrackRequest(config.MetricName, status, elapsedTime)

		Reply(w, response, status)
	}
}

// Defines a signature of a handler function, just like HandlerFunc.
//
// In addition to the HandlerFunc a request's body is passed into this function as a 4th parameter.
type HandlerWithBodyFunc func(http.ResponseWriter, *http.Request, map[string]string, []byte) (interface{}, error)

// Make a handler out of HandlerWithBodyFunc, just like regular MakeHandler function.
func MakeHandlerWithBody(app *App, fn HandlerWithBodyFunc, config *HandlerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := parseForm(r); err != nil {
			ReplyInternalError(w, fmt.Sprintf("Failed to parse request form: %v", err))
			return
		}

		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			ReplyInternalError(w, fmt.Sprintf("Failed to read request body: %v", err))
			return
		}

		start := time.Now()
		response, err := fn(w, r, mux.Vars(r), body)
		elapsedTime := time.Since(start)

		var status int
		if err != nil {
			response, status = responseAndStatusFor(err)
		} else {
			status = http.StatusOK
		}

		log.Infof("Request completed: status [%v] method [%v] path [%v] form [%v] time [%v] error [%v]",
			status, r.Method, r.URL, r.Form, elapsedTime, err)

		app.stats.TrackRequest(config.MetricName, status, elapsedTime)

		Reply(w, response, status)
	}
}

// Reply with the provided HTTP response and status code.
//
// Response body must be JSON-marshallable, otherwise the response
// will be "Internal Server Error".
func Reply(w http.ResponseWriter, response interface{}, status int) {
	// marshal the body of the response
	marshalledResponse, err := json.Marshal(response)
	if err != nil {
		ReplyInternalError(w, fmt.Sprintf("Failed to marshal response: %v %v", response, err))
		return
	}

	// write JSON response
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(marshalledResponse)
}

// ReplyError converts registered error into HTTP response code and writes it back
func ReplyError(w http.ResponseWriter, err error) {
	response, status := responseAndStatusFor(err)
	Reply(w, response, status)
}

// Helper that replies with the 500 code and happened error message.
func ReplyInternalError(w http.ResponseWriter, message string) {
	log.Errorf("Internal server error: %v", message)
	Reply(w, Response{"message": message}, http.StatusInternalServerError)
}

// Parse the request data based on its content type.
func parseForm(r *http.Request) error {
	if isMultipart(r) == true {
		return r.ParseMultipartForm(0)
	} else {
		return r.ParseForm()
	}
}

// Determine whether the request is multipart/form-data or not.
func isMultipart(r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	return strings.HasPrefix(contentType, "multipart/form-data")
}