package httperror

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type Response struct {
	Error Error `json:"error"`
}

func BadRequest(message string) Error {
	return Error{Code: "bad_request", Message: message}
}

func BadRequestWithDetails(message string, details any) Error {
	return Error{Code: "bad_request", Message: message, Details: details}
}

func NotFound(message string) Error {
	return Error{Code: "not_found", Message: message}
}

func Conflict(message string) Error {
	return Error{Code: "conflict", Message: message}
}

func NotImplemented(message string) Error {
	return Error{Code: "not_implemented", Message: message}
}

func Internal(message string) Error {
	return Error{Code: "internal_error", Message: message}
}

func Envelope(err Error) Response {
	return Response{Error: err}
}
