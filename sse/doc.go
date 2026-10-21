// Package sse implements server-sent events as defined by the WHATWG HTML
// Living Standard: streams written by a handler, a broker that delivers events
// by topic and resumes clients after reconnections, and a client that decodes
// streams and reconnects. It has no dependency outside the standard library.
//
// # Single stream
//
// A [Stream] carries the events of one response, produced by one handler, such
// as the progress of a job. [NewStream] sets the event stream headers and
// flushes them; [Stream.Send] writes one event, splitting its data into data
// lines. Every write has its own deadline ([WithWriteTimeout]), so the server's
// WriteTimeout can stay and idle periods are allowed, while a client that
// stops reading is dropped. A writer that cannot flush, such as the one of
// http.TimeoutHandler, gives [ErrStreamingUnsupported] before anything is
// written.
//
// # Broker
//
// A [Broker] delivers each event published with [Broker.Publish] to the
// requests subscribed with [Broker.Serve] to any of its topics, once each, in
// the same order for everyone. Publishing never waits: a subscriber that falls
// behind is disconnected with [ErrSlowConsumer]. Topics are chosen by your
// handler from the authenticated identity; the broker does not decide who may
// follow what. Serve always completes its response with status 200, so that
// browsers reconnect.
//
// # Resuming
//
// Browsers reconnect with the Last-Event-ID of the last event they received.
// The broker keeps recent events in memory ([WithHistory]) and sends the
// missed ones first. New subscriptions receive a position, the newest
// identifier, so that they can resume even before their first event. When an
// identifier can no longer be resolved, the client receives the reset event of
// [WithResetEvent], if configured, so that it reloads its state; otherwise a
// position.
//
// # Replay from a durable bus
//
// When an identifier is older than the history, a [ReplayFunc] set with
// [WithReplay] reads the missed events from your message bus and passes each
// one, with its topics, to send. The broker filters them by topic, and switches
// to the live events without losing or repeating any. A failure ends the
// stream, and the client retries when it reconnects.
//
// # Several replicas
//
// A broker lives in one process. With several replicas, publish to your
// message bus, and have every replica feed its broker with every message,
// using the bus message identifier as the event identifier and ignoring
// [ErrDuplicateID]. A client then resumes on any replica.
//
// # Shutting down
//
// http.Server.Shutdown waits for streams until its deadline, because they never
// become idle. Register [Broker.Close] as its hook:
//
//	srv.RegisterOnShutdown(broker.Close)
//
// Every stream then ends normally, and browsers reconnect elsewhere and resume.
//
// # Consuming from Go
//
// [Client.Events] connects with a request and yields its events, reconnecting
// as browsers do: after the server's retry, with the last identifier in
// Last-Event-ID. It stops on 204 and fails on other statuses with
// [ResponseError]. [Decode] parses a stream once, for example a response in a
// test.
//
// # Contexts, goroutines and defaults
//
// [Stream], [Broker.Serve] and [Client.Events] use the context of the HTTP
// request, as net/http does, rather than a separate parameter: when the client
// goes away, they stop. The package starts no goroutine: a subscription runs in
// the goroutine of its request, and a client in the goroutine that iterates.
//
// Defaults: a write timeout of 10 seconds, a keep-alive after 15 seconds of
// silence, a history of 1000 events, a queue of 64 events per subscriber, no
// reset event, a client reconnection time of 3 seconds with a maximum of 30
// seconds after consecutive failures, and events of at most 1 MiB when
// decoding.
//
// The examples show each flow: Stream, Broker, WithResetEvent, WithReplay,
// Broker.Close, Client and Decode. The README of the module covers integration
// with an existing server, message buses and browsers in more detail.
package sse
