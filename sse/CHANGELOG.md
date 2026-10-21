# Changelog

All notable changes to this module are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-10-21

### Added

- `Event`, validated before it is written: identifiers without NUL, CR or LF, types without
  line breaks, UTF-8 text. Line breaks in the data become one data line each.
- `NewStream`, `Stream.Send` and `Stream.KeepAlive`, which stream the events of one handler. The
  event stream headers are flushed at once, and every write has its own deadline
  (`WithWriteTimeout`), so the server's `WriteTimeout` does not end streams while a client that
  stops reading is dropped. `ErrStreamingUnsupported` reports a writer that cannot flush before
  anything is written.
- `NewBroker`, `Broker.Publish` and `Broker.Serve`, which deliver each event to the subscribers of
  its topics, once each and in the same order for everyone. Publishing never waits: a subscriber
  that falls behind (`WithBuffer`) is disconnected with `ErrSlowConsumer`. Events without an
  identifier get one with a random prefix per broker; an identifier still in the history is
  refused with `ErrDuplicateID`. Keep-alive comments are written after a silent interval
  (`WithKeepAlive`).
- Resuming with `Last-Event-ID` from an in-memory history (`WithHistory`), a position sent to new
  subscriptions, and an optional reset event for identifiers that can no longer be resolved
  (`WithResetEvent`).
- `WithReplay` and `ReplayFunc`, which recover missed events from a durable message bus. The
  broker filters them by topic and switches to the live events without loss or duplicates;
  `ErrUnknownID` marks an identifier the bus does not know, and failures, panics included, end
  the stream so that the client retries.
- `Broker.Close`, to register with `http.Server.RegisterOnShutdown`: streams end normally, so
  that browsers reconnect elsewhere, and later calls report `ErrClosed`.
- `Decode`, a parser of event streams that follows the standard, with `WithMaxEventSize` and
  `ErrEventTooLarge`.
- `NewClient` and `Client.Events`, which consume a stream and reconnect as browsers do, with
  `Last-Event-ID`, the server's reconnection time and a capped backoff. `ResponseError` reports
  a status or content type that does not start a stream. `WithHTTPClient` sets the HTTP client.
- `Option` and `ErrInvalidConfig`, shared by every constructor.
