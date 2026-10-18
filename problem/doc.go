// Package problem produces and reads RFC 9457 problem details for HTTP APIs:
// documents of media type "application/problem+json" that tell a client which
// problem occurred, why, and what it can do about it.
//
// # Declaring problem types and extension names
//
// RFC 9457 §4 asks every problem type to document a type URI, a title and a
// status code. Declare each type once, with [NewType], and each extension member
// with [NewKey], which also fixes the Go type of its value. Their arguments are
// usually fixed in the source code, so declare them as package-level values
// with [Must]: an invalid declaration then stops the program when it starts, and
// any test of the declaring package reports it.
//
//	var (
//		OutOfCredit = problem.Must(problem.NewType(
//			"https://example.com/probs/out-of-credit",
//			"You do not have enough credit.",
//			http.StatusForbidden,
//		))
//		Balance = problem.Must(problem.NewKey[int]("balance"))
//	)
//
// A type URI is the identifier of the type. Prefer an absolute URI; a relative
// one must begin with "/". RFC 9457 recommends that the URI lead to
// human-readable documentation of the problem; writing and serving that
// documentation is up to you. "about:blank" cannot be declared: use [Status].
// Extension names start with a letter and use only letters, digits and '_',
// for three characters or more; the standard member names are refused in any
// capitalization.
//
// # Answering requests
//
// An occurrence of a problem is an [*Error], built with [Type.New] or, when the
// status code says it all, with [Status]. Options such as [WithDetail],
// [WithInstance] and [WithExtension] set its members. [Write] answers a request
// with it:
//
//	return OutOfCredit.New(
//		problem.WithDetail("Your current balance is 30, but that costs 50."),
//		problem.WithExtension(Balance, 30),
//		problem.WithCause(err),
//	)
//
//	// In the handler:
//	if err != nil {
//		logger.ErrorContext(ctx, "send message", "err", err)
//		_ = problem.Write(w, r, err)
//		return
//	}
//
// The response carries the problem's status code, the media type [MediaType]
// and a compact document whose members always appear in the same order. Headers
// set before Write, such as Retry-After, are kept. A HEAD request gets no body.
// Once a handler has written a status or a body, the status can no longer
// change, so call Write before writing anything else.
//
// # What never reaches the client
//
// An [*Error] is a Go error: it can be returned from any layer, wrapped with
// fmt.Errorf and combined with errors.Join. Write takes any error and writes the
// first [*Error] it finds in the tree, in the order of errors.As. Nothing else
// reaches the response:
//
//   - the context added by wrapping levels;
//   - the internal cause set with [WithCause], which errors.Is, errors.As and
//     the error's message still show to the program and its logs;
//   - every error that is not a problem. Write answers such an error with the
//     generic server error: status 500, type "about:blank" and title
//     "Internal Server Error", with no other member.
//
// A problem that cannot be written, because its status is outside 400–599, its
// instance is not a URI reference or an extension value has no JSON
// representation, is also replaced by the generic server error. Write then
// returns an error wrapping [ErrInvalid] that says why.
//
// [Type.Match] reports whether a problem of a given type is anywhere in an error
// tree, looking past problems of other types.
//
// # Reading problem details
//
// [Read] reads the problem document of a response from another API, and [Parse]
// reads a document alone. Both follow the rules of RFC 9457 for consumers: a
// member whose value has the wrong JSON type is ignored, a missing type is
// "about:blank", and extensions are kept whether or not they are known, so
// [Key.Lookup] and [Error.Extensions] can read them. Read also resolves relative
// "type" and "instance" members against the URL of the request.
//
// Read recognizes a problem document by its media type, and returns an error
// wrapping [ErrNotProblem] without reading the body otherwise. Both functions
// read at most 1 MiB and return an error wrapping [ErrMalformed] for larger,
// invalid or non-object documents. JSON nesting is limited to 10,000 levels.
// Read does not close the body.
//
// # Using it with the middleware module
//
// The refusal functions of github.com/paccolamano/go/middleware answer refused
// requests with problem details in two lines, with no dependency between the
// two modules:
//
//	reject := func(w http.ResponseWriter, r *http.Request, rej middleware.Rejection) {
//		_ = problem.Write(w, r, problem.Status(rej.Status))
//	}
package problem
